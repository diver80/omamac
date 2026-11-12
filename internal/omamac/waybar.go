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

func barStatePath() string        { return filepath.Join(barDir(), "bar.json") }
func barSeenPath(slot int) string { return filepath.Join(barDir(), fmt.Sprintf("slot-%d.seen", slot)) }

// touchBarSlot tells the app that slot's module shows on the bar: Waybar's runs all the
// time and touches it every second, Omarchy's prints every 3 seconds.
func touchBarSlot(slot int) {
	path, now := barSeenPath(slot), time.Now()
	if os.Chtimes(path, now, now) == nil {
		return
	}
	if os.MkdirAll(barDir(), 0o700) == nil {
		_ = os.WriteFile(path, nil, 0o600)
	}
}

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
		st, err := os.Stat(barSeenPath(slot))
		live[slot] = err == nil && time.Since(st.ModTime()) < 10*time.Second
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
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	last := ""
	for {
		touchBarSlot(slot)
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

const barUsage = `usage: omamac bar <install | uninstall | N>

  install     put the menu bar items on the top bar, with their text; a click opens
              the panel: Omarchy's own bar (~/.config/omarchy/shell.json), else
              Waybar's configuration in use; backed up first (OmaMac does this itself
              at its start)
  uninstall   take them off again, and keep OmaMac from adding them
  N           what Omarchy's bar runs: prints the Nth menu bar item once (0: OmaMac)

omamac waybar N runs as Waybar's module instead: it prints the item as it changes.
`

// cliBar is `omamac bar …` and `omamac waybar …`.
func cliBar(configPath string, args []string, follow bool, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprint(stderr, barUsage)
		return 2
	}
	switch args[0] {
	case "install", "uninstall":
		bin := omamacCommand()
		var changed bool
		var err error
		if args[0] == "install" {
			n := 0
			if a, err := newApp(configPath, true); err == nil {
				n = len(a.barPanels())
			}
			changed, err = setUpBar(bin, n, stdout)
		} else {
			changed, err = takeDownBar(stdout)
		}
		if err != nil {
			fmt.Fprintln(stderr, "omamac:", err)
			return 1
		}
		saveWaybarMode(configPath, map[string]string{"install": "on", "uninstall": "off"}[args[0]])
		if !changed {
			fmt.Fprintln(stdout, "Nothing to change.")
		}
		if args[0] == "install" {
			fmt.Fprint(stdout, "\nOptional, in ~/.config/hypr/bindings.conf: a key for OmaMac's launcher\n"+
				"  bindd = SUPER ALT, O, OmaMac, exec, "+shellQuote(bin)+" open\n"+
				"and in ~/.config/hypr/hyprland.conf, should a popover open tiled:\n"+
				"  windowrule = float on, match:title ^(OmaMac · .*)$\n")
		}
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, barUsage)
		return 0
	}
	slot, err := strconv.Atoi(args[0])
	if err != nil || slot < 0 || slot > barSlots {
		fmt.Fprint(stderr, barUsage)
		return 2
	}
	if follow {
		return cliWaybarFollow(slot, stdout)
	}
	return cliBarOnce(slot, stdout)
}

// setUpBar puts OmaMac's modules on the bar there is, Omarchy's own or Waybar, for n
// menu bar items; changed: the bar shows a new configuration.
func setUpBar(bin string, n int, out io.Writer) (bool, error) {
	if omarchyShell() {
		return setUpOmarchyBar(bin, n, out)
	}
	changed, err := setUpWaybar(bin, out)
	if err == nil && changed {
		restartWaybar(out)
	}
	return changed, err
}

// takeDownBar takes them off again.
func takeDownBar(out io.Writer) (bool, error) {
	if omarchyShell() {
		return takeDownOmarchyBar(out)
	}
	changed, err := takeDownWaybar(out)
	if err == nil && changed {
		restartWaybar(out)
	}
	return changed, err
}

// barRuns reports whether the bar runs, to be set up.
func barRuns() bool {
	if omarchyShell() {
		return omarchyShellRuns()
	}
	return waybarArgs() != nil
}

// saveWaybarMode remembers in OmaMac's configuration whether its modules are wanted.
func saveWaybarMode(configPath, mode string) {
	if cfg, err := LoadConfig(configPath); err == nil && cfg.Waybar != mode {
		cfg.Waybar = mode
		_ = SaveConfig(configPath, cfg)
	}
}

// waybarSearchDirs are where Waybar looks for its configuration when it is given none:
// the user's folders, then the system's. getenv is the environment of the running
// Waybar (started by the session, it may differ from a terminal's).
func waybarSearchDirs(getenv func(string) string) (user, system []string) {
	home := getenv("HOME")
	if home == "" {
		home = homePath()
	}
	if d := getenv("XDG_CONFIG_HOME"); d != "" {
		user = append(user, filepath.Join(d, "waybar"))
	}
	user = append(user, filepath.Join(home, ".config", "waybar"), filepath.Join(home, "waybar"))
	for _, d := range filepath.SplitList(getenv("XDG_CONFIG_DIRS")) {
		if d != "" {
			system = append(system, filepath.Join(d, "waybar"))
		}
	}
	system = append(system, "/etc/xdg/waybar", "/usr/local/etc/xdg/waybar", "/usr/share/waybar")
	return user, system
}

// runningWaybar is the command line and environment of the running Waybar: nil and
// os.Getenv when none runs.
func runningWaybar() (args []string, getenv func(string) string) {
	pid := waybarPID()
	if pid == "" {
		return nil, os.Getenv
	}
	raw, _ := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	args = strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	env := map[string]string{}
	if raw, err := os.ReadFile(filepath.Join("/proc", pid, "environ")); err == nil {
		for _, kv := range strings.Split(string(raw), "\x00") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				env[k] = v
			}
		}
	}
	return args, func(k string) string {
		if v, ok := env[k]; ok {
			return v
		}
		return os.Getenv(k)
	}
}

// waybarArgs is the command line of the running Waybar, nil when none runs.
func waybarArgs() []string {
	args, _ := runningWaybar()
	return args
}

// waybarPID is the process ID of the running Waybar, "" when none runs.
func waybarPID() string {
	if goos != "linux" {
		return ""
	}
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		if _, err := strconv.Atoi(d.Name()); err != nil {
			continue
		}
		// "waybar", or a wrapper's name such as ".waybar-wrapped" (Nix)
		if comm, err := os.ReadFile(filepath.Join("/proc", d.Name(), "comm")); err == nil && strings.Contains(string(comm), "waybar") {
			return d.Name()
		}
	}
	return ""
}

// flagValue is the value of -short x, --long x or --long=x in args.
func flagValue(args []string, short, long string) string {
	for i, a := range args {
		switch {
		case (a == short || a == long) && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, long+"="):
			return strings.TrimPrefix(a, long+"=")
		}
	}
	return ""
}

// firstConfigIn is the first of Waybar's configuration files in dirs.
func firstConfigIn(dirs []string) string {
	for _, dir := range dirs {
		for _, name := range []string{"config.jsonc", "config", "config.json"} {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
				return filepath.Join(dir, name)
			}
		}
	}
	return ""
}

// ownedPath reports whether OmaMac may change path: the user's, but not Omarchy's own
// files (a git checkout its updates replace).
func ownedPath(path string) bool {
	home, _ := os.UserHomeDir()
	abs, _ := filepath.Abs(path)
	if home == "" || !strings.HasPrefix(abs, home+string(filepath.Separator)) {
		return false
	}
	return !strings.HasPrefix(abs, filepath.Join(home, ".local", "share", "omarchy")+string(filepath.Separator))
}

// waybarTarget is the configuration file to change: the one Waybar is given (-c) when
// it is the user's, else the user's own; a system configuration is copied into the
// user's folder first, which Waybar then reads instead.
func waybarTarget(args []string, user, system []string, out io.Writer) (string, error) {
	if given := flagValue(args, "-c", "--config"); given != "" {
		if !ownedPath(given) {
			return "", fmt.Errorf("Waybar runs with -c %s, which is not yours to change: add %q to its \"include\" and %s to \"modules-right\"",
				given, filepath.Join(user[0], waybarModulesFile), strings.Join(waybarModuleNames(), ", "))
		}
		return given, nil
	}
	if own := firstConfigIn(user); own != "" {
		return own, nil
	}
	sys := firstConfigIn(system)
	if sys == "" {
		running := "Waybar runs"
		if args == nil {
			running = "no Waybar process runs"
		}
		return "", fmt.Errorf("Waybar's configuration not found (%s; looked in %s)", running, strings.Join(append(user, system...), ", "))
	}
	text, err := os.ReadFile(sys)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(user[0], "config.jsonc")
	if err := os.MkdirAll(user[0], 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, text, 0o644); err != nil {
		return "", err
	}
	fmt.Fprintf(out, "✓ %s: a copy of %s, which Waybar reads from now on\n", dst, sys)
	return dst, nil
}

// setUpWaybar adds OmaMac's modules to the configuration Waybar uses; changed: Waybar
// has to read it again.
func setUpWaybar(bin string, out io.Writer) (bool, error) {
	args, getenv := runningWaybar()
	user, system := waybarSearchDirs(getenv)
	cfg, err := waybarTarget(args, user, system, out)
	if err != nil {
		return false, err
	}
	return installWaybar(cfg, bin, out)
}

// takeDownWaybar takes them out again.
func takeDownWaybar(out io.Writer) (bool, error) {
	args, getenv := runningWaybar()
	user, system := waybarSearchDirs(getenv)
	cfg := flagValue(args, "-c", "--config")
	if cfg == "" {
		cfg = firstConfigIn(user)
	}
	if cfg == "" || !ownedPath(cfg) {
		return false, nil
	}
	return uninstallWaybar(cfg, firstConfigIn(system), out)
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

// installWaybar puts OmaMac's modules into the configuration cfg: their definitions
// next to it (omamac.jsonc, running bin), their names first in modules-right, their
// spacing in its style.css. changed: something was written.
func installWaybar(cfg, bin string, out io.Writer) (changed bool, err error) {
	dir := filepath.Dir(cfg)
	modules := filepath.Join(dir, waybarModulesFile)
	if old, _ := os.ReadFile(modules); string(old) != waybarModules(bin) {
		if err := os.WriteFile(modules, []byte(waybarModules(bin)), 0o644); err != nil {
			return false, err
		}
		changed = true
		fmt.Fprintf(out, "✓ %s: the modules (running %s)\n", modules, bin)
	}

	text, err := os.ReadFile(cfg)
	if err != nil {
		return changed, err
	}
	edited, err := addToWaybarConfig(string(text), modules)
	if err != nil {
		return changed, fmt.Errorf("%s: %v – add %q to \"include\" and %s to \"modules-right\" yourself",
			cfg, err, modules, strings.Join(waybarModuleNames(), ", "))
	}
	if edited != string(text) {
		backup := fmt.Sprintf("%s.omamac-backup-%s", cfg, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, text, 0o644); err != nil {
			return changed, err
		}
		if err := os.WriteFile(cfg, []byte(edited), 0o644); err != nil {
			return changed, err
		}
		changed = true
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
			changed = true
			fmt.Fprintf(out, "✓ %s: spacing of the modules\n", css)
		}
	}
	return changed, nil
}

// uninstallWaybar takes OmaMac's modules out of cfg again. A configuration that is then
// the system's (system, which install copied) is removed, so Waybar reads the system's.
func uninstallWaybar(cfg, system string, out io.Writer) (changed bool, err error) {
	dir := filepath.Dir(cfg)
	modules := filepath.Join(dir, waybarModulesFile)
	if text, err := os.ReadFile(cfg); err == nil {
		edited := removeFromWaybarConfig(string(text), modules)
		if sys, err := os.ReadFile(system); system != "" && err == nil && string(sys) == edited {
			if err := os.Remove(cfg); err != nil {
				return changed, err
			}
			changed = true
			fmt.Fprintf(out, "✓ %s removed: Waybar reads %s again\n", cfg, system)
		} else if edited != string(text) {
			if err := os.WriteFile(cfg, []byte(edited), 0o644); err != nil {
				return changed, err
			}
			changed = true
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
					changed = true
					fmt.Fprintf(out, "✓ %s: OmaMac's styles taken out\n", css)
				}
			}
		}
	}
	if err := os.Remove(modules); err == nil {
		changed = true
		fmt.Fprintf(out, "✓ %s removed\n", modules)
	}
	return changed, nil
}

// removeTrays takes OmaMac's tray icons away as it quits (Linux): a bar that misses
// that the app ended would keep them, and add new ones at every start.
func (a *App) removeTrays() {
	if goos != "linux" {
		return
	}
	if a.mainTray != nil {
		a.mainTray.Destroy()
		a.mainTray = nil
	}
	for _, rt := range a.runtimes {
		if rt.tray != nil {
			rt.tray.Destroy()
			rt.tray = nil
		}
	}
}

// autoBar sets the top bar up as OmaMac starts on Linux (Config.Waybar ""/"on"): once
// the bar runs, it gets OmaMac's modules, or this program's path anew after an update;
// it reloads only when something changed. n: the menu bar items.
func (a *App) autoBar(mode string, n int) {
	if goos != "linux" || mode == "off" {
		return
	}
	for i := 0; !barRuns(); i++ { // the bar may start after OmaMac
		if i == 20 {
			return
		}
		time.Sleep(3 * time.Second)
	}
	var log bytes.Buffer
	changed, err := setUpBar(omamacCommand(), n, &log)
	a.updateUI(func() {
		if err != nil {
			logf("bar", "not set up: %v", err)
			if mode == "" {
				a.toast("⚠️ " + err.Error())
			}
			return
		}
		a.barModules = n
		if changed {
			logf("bar", "set up: %s", strings.ReplaceAll(strings.TrimSpace(log.String()), "\n", " · "))
		}
		if a.Config.Waybar == "" {
			a.Config.Waybar = "on"
			_ = SaveConfig(a.ConfigPath, a.Config)
			a.toast(T("OmaMac zeigt seine Menüleisten-Einträge jetzt in der oberen Leiste (abschalten: Einstellungen)"))
		}
	})
}

// syncOmarchyBar gives Omarchy's bar one module per menu bar item as they change (its
// modules are entries of shell.json; Waybar's are fixed slots).
func (a *App) syncOmarchyBar() {
	n := min(len(a.barPanels()), barSlots)
	if goos != "linux" || a.Config.Waybar != "on" || a.barModules < 0 || n == a.barModules || a.barSyncing || !omarchyShell() {
		return
	}
	a.barSyncing, a.barModules = true, n
	go func() {
		_, err := setUpOmarchyBar(omamacCommand(), n, io.Discard)
		a.updateUI(func() {
			a.barSyncing = false
			if err != nil {
				logf("bar", "modules not updated: %v", err)
			}
		})
	}()
}

// setBar switches the bar's modules on or off (settings).
func (a *App) setBar(on bool) {
	mode := map[bool]string{true: "on", false: "off"}[on]
	a.Config.Waybar = mode
	_ = SaveConfig(a.ConfigPath, a.Config)
	n := len(a.barPanels())
	go func() {
		var log bytes.Buffer
		var changed bool
		var err error
		if on {
			changed, err = setUpBar(omamacCommand(), n, &log)
		} else {
			changed, err = takeDownBar(&log)
		}
		a.updateUI(func() {
			if err != nil {
				a.toast("⚠️ " + err.Error())
				return
			}
			if on {
				a.barModules = n
			}
			if changed {
				logf("bar", "%s: %s", mode, strings.ReplaceAll(strings.TrimSpace(log.String()), "\n", " · "))
			}
		})
	}()
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
