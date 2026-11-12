package omamac

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Omarchy's top bar is Waybar, whose tray shows an item's icon but not its text: there,
// OmaMac's menu bar items are Waybar modules instead. The app writes what they show to
// bar.json; `omamac waybar N`, which Waybar runs for each module, prints the Nth item in
// the menu bar (0 is OmaMac's own) as Waybar's JSON whenever it changes, and a click runs
// `omamac open N`, which the running app gets (single instance, cmd/omamac). Fixed slots
// keep Waybar's configuration the same as panels come and go; an empty one is hidden.
// While a slot's module runs, its panel has no tray icon. `omamac waybar install` sets it
// all up in ~/.config/waybar.

// barSlots is the number of panel modules `omamac waybar install` adds.
const barSlots = 6

// barDir holds bar.json and the PID files of the running modules.
func barDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "omamac")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("omamac-%d", os.Getuid()))
}

func barStatePath() string       { return filepath.Join(barDir(), "bar.json") }
func barPidPath(slot int) string { return filepath.Join(barDir(), fmt.Sprintf("slot-%d.pid", slot)) }

// barItem is what a module shows, in the JSON of Waybar's custom modules.
type barItem struct {
	Text    string `json:"text"`
	Tooltip string `json:"tooltip,omitempty"`
	Class   string `json:"class,omitempty"`
}

// barState is bar.json: the app's process, its own item and the panels' in order.
type barState struct {
	PID    int       `json:"pid"`
	Master barItem   `json:"master"`
	Panels []barItem `json:"panels"`
}

// barPanels are the panels in the menu bar, in the order of the slots.
func (a *App) barPanels() []*PanelSpec {
	var out []*PanelSpec
	for _, p := range a.panels {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out
}

func barStateOf(pid int, panels []*PanelSpec) barState {
	st := barState{PID: pid, Master: barItem{Text: "⌘", Tooltip: T("OmaMac – Übersicht & Wal-Panel öffnen"), Class: "running"}}
	for _, p := range panels {
		class := p.Status
		if class == "" {
			class = "ok"
		}
		tip := T(p.Title)
		if s := strings.TrimSpace(T(p.Subtitle)); s != "" {
			tip += "\n" + s
		}
		st.Panels = append(st.Panels, barItem{Text: p.TrayLabel(), Tooltip: tip, Class: class})
	}
	return st
}

// writeBarState writes bar.json on Linux when it changed.
func (a *App) writeBarState() {
	if goos != "linux" || !a.desktop {
		return
	}
	data, err := json.Marshal(barStateOf(os.Getpid(), a.barPanels()))
	if err != nil || bytes.Equal(data, a.barWritten) {
		return
	}
	if err := writeFileAtomic(barStatePath(), data); err != nil {
		logf("waybar", "bar.json not written: %v", err)
		return
	}
	a.barWritten = data
}

// removeBarState empties the modules as the app quits.
func removeBarState() {
	if goos == "linux" {
		_ = os.Remove(barStatePath())
	}
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// barLive tells which slots have a running module (index 0: OmaMac's own).
type barLive [barSlots + 1]bool

func liveBarSlots() barLive {
	var live barLive
	if goos != "linux" {
		return live
	}
	for slot := range live {
		raw, err := os.ReadFile(barPidPath(slot))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		live[slot] = err == nil && processRuns(pid, "waybar")
	}
	return live
}

// processRuns reports whether process pid runs with word in its command line (Linux).
func processRuns(pid int, word string) bool {
	if pid <= 0 {
		return false
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return err == nil && bytes.Contains(cmdline, []byte(word))
}

// watchBarSlots re-syncs the trays as Waybar's modules start and stop.
func (a *App) watchBarSlots() {
	if goos != "linux" {
		return
	}
	for range time.Tick(3 * time.Second) {
		live := liveBarSlots()
		a.updateUI(func() {
			if live != a.barLive {
				logf("waybar", "modules running: %v", live)
				a.syncAllPanelRuntimes()
			}
		})
	}
}

// barItemFor is what slot shows of st; nil st: the app does not run.
func barItemFor(slot int, st *barState) barItem {
	if st == nil {
		if slot == 0 {
			return barItem{Text: "⌘", Tooltip: T("OmaMac starten"), Class: "stopped"}
		}
		return barItem{}
	}
	if slot == 0 {
		return st.Master
	}
	if slot <= len(st.Panels) {
		return st.Panels[slot-1]
	}
	return barItem{}
}

// readBarState reads bar.json, nil when the app that wrote it does not run.
func readBarState() *barState {
	raw, err := os.ReadFile(barStatePath())
	if err != nil {
		return nil
	}
	var st barState
	if json.Unmarshal(raw, &st) != nil || !processRuns(st.PID, "omamac") {
		return nil
	}
	return &st
}

// cliWaybarFollow is `omamac waybar N`: it prints slot's item whenever it changes, until
// Waybar stops it.
func cliWaybarFollow(slot int, stdout io.Writer) int {
	if err := os.MkdirAll(barDir(), 0o700); err == nil {
		pidFile := barPidPath(slot)
		me := strconv.Itoa(os.Getpid())
		if os.WriteFile(pidFile, []byte(me), 0o600) == nil {
			defer func() {
				if raw, _ := os.ReadFile(pidFile); string(raw) == me {
					_ = os.Remove(pidFile)
				}
			}()
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	last := ""
	for {
		line, _ := json.Marshal(barItemFor(slot, readBarState()))
		if string(line) != last {
			if _, err := fmt.Fprintf(stdout, "%s\n", line); err != nil {
				return 0 // Waybar went away
			}
			last = string(line)
		}
		select {
		case <-stop:
			return 0
		case <-tick.C:
		}
	}
}

const waybarUsage = `usage: omamac waybar <install | uninstall | N>

  install     add OmaMac's modules to Waybar (~/.config/waybar): the menu bar items
              with their text, a click opens the panel; the configuration is backed up
  uninstall   take them out again
  N           what Waybar runs for a module: prints the Nth menu bar item (0: OmaMac)
`

// cliWaybar is `omamac waybar …`.
func cliWaybar(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, waybarUsage)
		return 2
	}
	switch args[0] {
	case "install", "uninstall":
		dir := waybarConfigDir()
		bin := omamacCommand()
		var err error
		if args[0] == "install" {
			err = installWaybar(dir, bin, stdout)
		} else {
			err = uninstallWaybar(dir, stdout)
		}
		if err != nil {
			fmt.Fprintln(stderr, "omamac:", err)
			return 1
		}
		restartWaybar(stdout)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, waybarUsage)
		return 0
	}
	slot, err := strconv.Atoi(args[0])
	if err != nil || slot < 0 || slot > barSlots {
		fmt.Fprint(stderr, waybarUsage)
		return 2
	}
	return cliWaybarFollow(slot, stdout)
}

func waybarConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "waybar")
	}
	return homePath(".config", "waybar")
}

// omamacCommand is how Waybar runs OmaMac: the command on the PATH when it is this
// program (~/.local/bin/omamac of install.sh, which stays as the app updates), else
// this program's path.
func omamacCommand() string {
	self, err := os.Executable()
	if err != nil {
		return "omamac"
	}
	self, _ = filepath.EvalSymlinks(self)
	if p, err := exec.LookPath("omamac"); err == nil {
		if real, _ := filepath.EvalSymlinks(p); real == self {
			return p
		}
	}
	return self
}

const (
	waybarModulesFile = "omamac.jsonc"
	waybarCSSStart    = "/* omamac-start: written by `omamac waybar install` */"
	waybarCSSEnd      = "/* omamac-end */"
)

// waybarModuleNames are the modules in the order they go into modules-right.
func waybarModuleNames() []string {
	var names []string
	for i := 1; i <= barSlots; i++ {
		names = append(names, fmt.Sprintf("custom/omamac-%d", i))
	}
	return append(names, "custom/omamac")
}

// waybarModules is omamac.jsonc: the definitions of the modules, which Waybar's
// configuration includes.
func waybarModules(bin string) string {
	run := shellQuote(bin)
	mods := map[string]any{}
	for slot := 0; slot <= barSlots; slot++ {
		name, open := fmt.Sprintf("custom/omamac-%d", slot), fmt.Sprintf("%s open %d", run, slot)
		m := map[string]any{
			"exec":             fmt.Sprintf("%s waybar %d", run, slot),
			"return-type":      "json",
			"escape":           true,
			"restart-interval": 5,
			"on-click":         open,
		}
		if slot == 0 {
			name = "custom/omamac"
			m["on-click"] = run + " open"
			m["on-click-right"] = run + " open studio"
		}
		mods[name] = m
	}
	data, _ := json.MarshalIndent(mods, "", "  ")
	return "// Written by `omamac waybar install`; `omamac waybar uninstall` removes it.\n" + string(data) + "\n"
}

var (
	reModulesRight = regexp.MustCompile(`"modules-right"\s*:\s*\[`)
	reIncludeArray = regexp.MustCompile(`"include"\s*:\s*\[`)
	reIncludeOne   = regexp.MustCompile(`"include"\s*:\s*("[^"]*")`)
)

// firstMatch finds re in text outside of // comments: the indexes of the match and
// of its groups.
func firstMatch(re *regexp.Regexp, text string) []int {
	for _, loc := range re.FindAllStringSubmatchIndex(text, -1) {
		lineStart := strings.LastIndexByte(text[:loc[0]], '\n') + 1
		if !strings.Contains(text[lineStart:loc[0]], "//") {
			return loc
		}
	}
	return nil
}

// waybarModulesText is what install puts at the start of modules-right.
func waybarModulesText() string {
	var b strings.Builder
	for _, n := range waybarModuleNames() {
		b.WriteString(strconv.Quote(n) + ", ")
	}
	return b.String()
}

// addToWaybarConfig puts OmaMac's modules first in modules-right and includes their
// definitions from include. It changes nothing when they are there already.
func addToWaybarConfig(text, include string) (string, error) {
	if strings.Contains(text, `"custom/omamac"`) {
		return text, nil
	}
	mr := firstMatch(reModulesRight, text)
	if mr == nil {
		return "", errors.New(`no "modules-right" in the configuration`)
	}
	text = text[:mr[1]] + waybarModulesText() + text[mr[1]:]
	inc := strconv.Quote(include)
	if loc := firstMatch(reIncludeArray, text); loc != nil {
		return text[:loc[1]] + inc + ", " + text[loc[1]:], nil
	}
	if loc := firstMatch(reIncludeOne, text); loc != nil { // "include": "x" → [ours, "x"]
		return text[:loc[2]] + "[" + inc + ", " + text[loc[2]:loc[3]] + "]" + text[loc[3]:], nil
	}
	{
		brace := strings.IndexByte(text, '{')
		if brace < 0 || brace > mr[0] {
			return "", errors.New("the configuration has no object for the bar")
		}
		text = text[:brace+1] + "\n  \"include\": [" + inc + "]," + text[brace+1:]
	}
	return text, nil
}

// removeFromWaybarConfig undoes addToWaybarConfig.
func removeFromWaybarConfig(text, include string) string {
	inc := strconv.Quote(include)
	text = strings.Replace(text, waybarModulesText(), "", 1)
	text = strings.Replace(text, "\n  \"include\": ["+inc+"],", "", 1)
	if i := strings.Index(text, "["+inc+", "); i >= 0 { // a single include made an array
		if j := strings.IndexByte(text[i:], ']'); j > 0 {
			inner := text[i+len("["+inc+", ") : i+j]
			if !strings.Contains(inner, ",") {
				return text[:i] + inner + text[i+j+1:]
			}
		}
	}
	return strings.Replace(text, inc+", ", "", 1)
}

func waybarCSS() string {
	var ids []string
	for _, n := range waybarModuleNames() {
		ids = append(ids, "#"+strings.ReplaceAll(n, "/", "-"))
	}
	return waybarCSSStart + "\n" + strings.Join(ids, ",\n") + " {\n  margin: 0 7.5px;\n}\n" + waybarCSSEnd + "\n"
}

// waybarConfigFile is Waybar's configuration in dir.
func waybarConfigFile(dir string) string {
	for _, name := range []string{"config.jsonc", "config", "config.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

func installWaybar(dir, bin string, out io.Writer) error {
	cfg := waybarConfigFile(dir)
	if cfg == "" {
		return fmt.Errorf("no Waybar configuration in %s", dir)
	}
	modules := filepath.Join(dir, waybarModulesFile)
	if err := os.WriteFile(modules, []byte(waybarModules(bin)), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ %s: the modules (running %s)\n", modules, bin)

	text, err := os.ReadFile(cfg)
	if err != nil {
		return err
	}
	changed, err := addToWaybarConfig(string(text), modules)
	if err != nil {
		return fmt.Errorf("%s: %v – add %q to \"include\" and %s to \"modules-right\" yourself",
			cfg, err, modules, strings.Join(waybarModuleNames(), ", "))
	}
	if changed != string(text) {
		backup := fmt.Sprintf("%s.omamac-backup-%s", cfg, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, text, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(cfg, []byte(changed), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ %s: OmaMac first in modules-right (backup: %s)\n", cfg, filepath.Base(backup))
	} else {
		fmt.Fprintf(out, "✓ %s: OmaMac is in it already\n", cfg)
	}

	css := filepath.Join(dir, "style.css")
	if style, err := os.ReadFile(css); err == nil && !bytes.Contains(style, []byte(waybarCSSStart)) {
		f, err := os.OpenFile(css, os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = f.WriteString("\n" + waybarCSS())
			_ = f.Close()
		}
		if err == nil {
			fmt.Fprintf(out, "✓ %s: spacing of the modules\n", css)
		}
	}
	fmt.Fprint(out, "\nOptional, in ~/.config/hypr/bindings.conf: a key for OmaMac's launcher\n"+
		"  bindd = SUPER ALT, O, OmaMac, exec, "+shellQuote(bin)+" open\n"+
		"and in ~/.config/hypr/hyprland.conf, should a popover open tiled:\n"+
		"  windowrule = float on, match:title ^(OmaMac · .*)$\n")
	return nil
}

func uninstallWaybar(dir string, out io.Writer) error {
	modules := filepath.Join(dir, waybarModulesFile)
	if cfg := waybarConfigFile(dir); cfg != "" {
		text, err := os.ReadFile(cfg)
		if err != nil {
			return err
		}
		if changed := removeFromWaybarConfig(string(text), modules); changed != string(text) {
			if err := os.WriteFile(cfg, []byte(changed), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(out, "✓ %s: OmaMac's modules taken out\n", cfg)
		}
	}
	css := filepath.Join(dir, "style.css")
	if style, err := os.ReadFile(css); err == nil {
		s := string(style)
		if i := strings.Index(s, waybarCSSStart); i >= 0 {
			if j := strings.Index(s[i:], waybarCSSEnd); j >= 0 {
				end := i + j + len(waybarCSSEnd)
				if end < len(s) && s[end] == '\n' {
					end++
				}
				start := i
				if start > 0 && s[start-1] == '\n' {
					start--
				}
				if err := os.WriteFile(css, []byte(s[:start]+s[end:]), 0o644); err == nil {
					fmt.Fprintf(out, "✓ %s: OmaMac's styles taken out\n", css)
				}
			}
		}
	}
	if err := os.Remove(modules); err == nil {
		fmt.Fprintf(out, "✓ %s removed\n", modules)
	}
	return nil
}

// restartWaybar makes Waybar read its configuration again: Omarchy's way, else SIGUSR2.
func restartWaybar(out io.Writer) {
	if p, err := exec.LookPath("omarchy-restart-waybar"); err == nil {
		if exec.Command(p).Run() == nil {
			fmt.Fprintln(out, "✓ Waybar restarted")
			return
		}
	}
	if exec.Command("pkill", "-SIGUSR2", "-x", "waybar").Run() == nil {
		fmt.Fprintln(out, "✓ Waybar reloaded")
		return
	}
	fmt.Fprintln(out, "Restart Waybar to see OmaMac in it.")
}

// HandleOpen is `omamac open [what]` (Linux), from Waybar or a key: OmaMac's launcher,
// the main window ("studio", "dashboard"), or a panel's popover by its slot or ID.
// Without "open", a second start of the app shows the main window.
func (a *App) HandleOpen(args []string) {
	i := slices.Index(args, "open")
	if i < 0 {
		if len(args) == 0 {
			a.showStudio()
		}
		return
	}
	what := ""
	if i+1 < len(args) {
		what = args[i+1]
	}
	switch what {
	case "", "launcher", "0":
		a.ToggleWalPanel()
	case "studio", "main":
		a.showStudio()
	case "dashboard":
		a.setTab(TabDashboard)
		a.showStudio()
	default:
		if n, err := strconv.Atoi(what); err == nil {
			if panels := a.barPanels(); n >= 1 && n <= len(panels) {
				a.togglePanelFlyout(panels[n-1].ID)
			}
			return
		}
		a.togglePanelFlyout(what)
	}
}
