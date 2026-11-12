package omamac

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// jsoncToJSON drops the // comments of a JSONC file (outside of strings), as Waybar does.
func jsoncToJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var b strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		b.WriteByte(c)
	}
	// Waybar takes trailing commas; encoding/json does not.
	clean := regexp.MustCompile(`,(\s*[\]}])`).ReplaceAllString(b.String(), "$1")
	var v map[string]any
	if err := json.Unmarshal([]byte(clean), &v); err != nil {
		t.Fatalf("not valid JSONC: %v\n%s", err, clean)
	}
	return v
}

// OmaMac's modules go first into Omarchy's modules-right, their file is included, and
// uninstall gives back the configuration as it was.
func TestWaybarConfigOmarchy(t *testing.T) {
	raw, err := os.ReadFile("testdata/omarchy-waybar.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	orig := string(raw)
	const inc = "/home/me/.config/waybar/omamac.jsonc"
	got, err := addToWaybarConfig(orig, inc)
	if err != nil {
		t.Fatal(err)
	}
	v := jsoncToJSON(t, got)
	right := v["modules-right"].([]any)
	if len(right) != barSlots+1+6 || right[0] != "custom/omamac-1" || right[barSlots] != "custom/omamac" || right[barSlots+1] != "group/tray-expander" {
		t.Fatalf("modules-right: %v", right)
	}
	if incs := v["include"].([]any); len(incs) != 1 || incs[0] != inc {
		t.Fatalf("include: %v", v["include"])
	}
	if again, _ := addToWaybarConfig(got, inc); again != got {
		t.Fatal("a second install changed the configuration")
	}
	if back := removeFromWaybarConfig(got, inc); back != orig {
		t.Fatalf("uninstall left a different configuration:\n%s", back)
	}
}

func TestWaybarConfigVariants(t *testing.T) {
	const inc = "/x/omamac.jsonc"
	cases := []struct{ name, in string }{
		{"include array", `{"include": ["/a.jsonc"], "modules-right": ["clock"]}`},
		{"include string", `{"include": "/a.jsonc", "modules-right": ["clock"]}`},
		{"commented line first", "{\n  // \"modules-right\": [\"old\"],\n  \"modules-right\": [\"clock\"]\n}"},
	}
	for _, tc := range cases {
		got, err := addToWaybarConfig(tc.in, inc)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		v := jsoncToJSON(t, got)
		if r := v["modules-right"].([]any); r[0] != "custom/omamac-1" || r[len(r)-1] != "clock" {
			t.Fatalf("%s: modules-right %v", tc.name, r)
		}
		incs, _ := v["include"].([]any)
		if len(incs) == 0 || incs[0] != inc {
			t.Fatalf("%s: include %v", tc.name, v["include"])
		}
		if tc.name != "commented line first" && !strings.Contains(got, `"/a.jsonc"`) {
			t.Fatalf("%s: the old include is gone", tc.name)
		}
		if back := jsoncToJSON(t, removeFromWaybarConfig(got, inc)); len(back["modules-right"].([]any)) != 1 {
			t.Fatalf("%s: uninstall: %v", tc.name, back)
		}
	}
	if _, err := addToWaybarConfig(`{"modules-left": []}`, inc); err == nil {
		t.Fatal("a configuration without modules-right should fail")
	}
}

// install and uninstall in a Waybar folder: modules file, configuration, styles.
func TestWaybarInstallUninstall(t *testing.T) {
	dir := t.TempDir()
	raw, _ := os.ReadFile("testdata/omarchy-waybar.jsonc")
	cfg := filepath.Join(dir, "config.jsonc")
	_ = os.WriteFile(cfg, raw, 0o644)
	css := filepath.Join(dir, "style.css")
	_ = os.WriteFile(css, []byte("* { color: red; }\n"), 0o644)

	if changed, err := installWaybar(cfg, "/home/me/.local/bin/omamac", io.Discard); err != nil || !changed {
		t.Fatal(changed, err)
	}
	mods := jsoncToJSON(t, mustRead(t, filepath.Join(dir, waybarModulesFile)))
	if len(mods) != barSlots+1 {
		t.Fatalf("%d modules", len(mods))
	}
	m1 := mods["custom/omamac-1"].(map[string]any)
	if m1["exec"] != "'/home/me/.local/bin/omamac' waybar 1" || m1["on-click"] != "'/home/me/.local/bin/omamac' open 1" || m1["return-type"] != "json" {
		t.Fatalf("module 1: %v", m1)
	}
	if mods["custom/omamac"].(map[string]any)["exec"] != "'/home/me/.local/bin/omamac' waybar 0" {
		t.Fatalf("OmaMac's module: %v", mods["custom/omamac"])
	}
	if !strings.Contains(mustRead(t, css), "#custom-omamac-1") {
		t.Fatal("no styles")
	}
	backups, _ := filepath.Glob(cfg + ".omamac-backup-*")
	if len(backups) != 1 {
		t.Fatalf("backups: %v", backups)
	}
	// Again: nothing to change, so Waybar does not restart at every start.
	if changed, err := installWaybar(cfg, "/home/me/.local/bin/omamac", io.Discard); err != nil || changed {
		t.Fatal("a second install changed something", err)
	}
	// Another path of the program (an update, another build): only the modules change.
	if changed, _ := installWaybar(cfg, "/opt/omamac", io.Discard); !changed || !strings.Contains(mustRead(t, filepath.Join(dir, waybarModulesFile)), "'/opt/omamac' waybar 1") {
		t.Fatal("the new path is not in the modules")
	}
	if strings.Count(mustRead(t, cfg), "custom/omamac-1") != 1 || strings.Count(mustRead(t, css), waybarCSSStart) != 1 {
		t.Fatal("a second install added again")
	}

	if changed, err := uninstallWaybar(cfg, "", io.Discard); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if mustRead(t, cfg) != string(raw) || mustRead(t, css) != "* { color: red; }\n" {
		t.Fatalf("uninstall left:\n%s\n%s", mustRead(t, cfg), mustRead(t, css))
	}
	if _, err := os.Stat(filepath.Join(dir, waybarModulesFile)); !os.IsNotExist(err) {
		t.Fatal("the modules file stayed")
	}
}

// Without a configuration of the user's, Waybar's system one is copied into the user's
// folder, which uninstall removes again; one given with -c is changed in place, if it
// is the user's.
func TestWaybarTarget(t *testing.T) {
	user, sysDir := t.TempDir(), t.TempDir()
	userDir := filepath.Join(user, "waybar")
	raw, _ := os.ReadFile("testdata/omarchy-waybar.jsonc")
	_ = os.WriteFile(filepath.Join(sysDir, "config.jsonc"), raw, 0o644)

	cfg, err := waybarTarget(nil, []string{userDir}, []string{sysDir}, io.Discard)
	if err != nil || cfg != filepath.Join(userDir, "config.jsonc") || mustRead(t, cfg) != string(raw) {
		t.Fatal(cfg, err)
	}
	if again, _ := waybarTarget(nil, []string{userDir}, []string{sysDir}, io.Discard); again != cfg {
		t.Fatal("the user's copy should be taken now:", again)
	}
	if _, err := installWaybar(cfg, "omamac", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := uninstallWaybar(cfg, filepath.Join(sysDir, "config.jsonc"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Fatal("the copy stayed after uninstall")
	}

	if _, err := waybarTarget(nil, []string{userDir}, []string{t.TempDir()}, io.Discard); err == nil {
		t.Fatal("no configuration anywhere should fail")
	}
	if _, err := waybarTarget([]string{"waybar", "-c", "/usr/share/x/config.jsonc"}, []string{userDir}, nil, io.Discard); err == nil || !strings.Contains(err.Error(), "-c /usr/share/x/config.jsonc") {
		t.Fatal("a -c configuration outside the home folder is not changed:", err)
	}
	mine := homePath(".config", "waybar", "mine.jsonc")
	if got, err := waybarTarget([]string{"waybar", "--config=" + mine}, []string{userDir}, nil, io.Discard); err != nil || got != mine {
		t.Fatal(got, err)
	}
	if ownedPath(homePath(".local", "share", "omarchy", "config", "waybar", "config.jsonc")) {
		t.Fatal("Omarchy's own files are not OmaMac's to change")
	}
}

func TestFlagValue(t *testing.T) {
	args := []string{"waybar", "-l", "debug", "-c", "/a", "--style=/b"}
	if flagValue(args, "-c", "--config") != "/a" || flagValue(args, "-s", "--style") != "/b" || flagValue(args, "-x", "--nope") != "" {
		t.Fatal(args)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The modules show the menu bar items in order; slots beyond them are empty (hidden).
func TestBarState(t *testing.T) {
	panels := []*PanelSpec{
		{ID: "a", Title: "Agenten-Limits", Icon: "🤖", BadgeText: "5% · 67%", Status: "crit"},
		{ID: "b", Title: "Atlassian Status", Subtitle: "Cloud", Icon: "🌐", BadgeText: "8/8 Operational"},
	}
	st := barStateOf(42, panels)
	if got := barItemFor(1, &st); got.Text != "🔴 🤖 5% · 67%" || got.Class != "crit" {
		t.Fatalf("slot 1: %+v", got)
	}
	if got := barItemFor(2, &st); got.Text != "🌐 8/8 Operational" || got.Class != "ok" || !strings.Contains(got.Tooltip, "Cloud") {
		t.Fatalf("slot 2: %+v", got)
	}
	if got := barItemFor(3, &st); got.Text != "" {
		t.Fatalf("slot 3 should be empty: %+v", got)
	}
	if got := barItemFor(0, &st); got.Text != "⌘" {
		t.Fatalf("slot 0: %+v", got)
	}
	if got := barItemFor(0, nil); got.Class != "stopped" || barItemFor(1, nil).Text != "" {
		t.Fatal("without the app, only OmaMac's own module shows")
	}
	a := newTestApp(t)
	for _, p := range a.panels {
		p.Enabled = false
	}
	a.panels[2].Enabled, a.panels[0].Enabled = true, true
	if got := a.barPanels(); len(got) != 2 || got[0] != a.panels[0] || got[1] != a.panels[2] {
		t.Fatal("barPanels: the enabled panels in order")
	}
}

func TestPickShell(t *testing.T) {
	has := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, x := range paths {
				if x == p {
					return true
				}
			}
			return false
		}
	}
	if got := pickShell("darwin", has()); got != "/bin/zsh" {
		t.Fatal(got)
	}
	if got := pickShell("linux", has("/bin/bash", "/usr/bin/zsh")); got != "/usr/bin/zsh" {
		t.Fatal(got)
	}
	if got := pickShell("linux", has("/bin/bash")); got != "/bin/bash" { // Omarchy
		t.Fatal(got)
	}
	if got := pickShell("linux", has()); got != "/bin/sh" {
		t.Fatal(got)
	}
}

func TestLinuxTerminals(t *testing.T) {
	old := goos
	goos = "linux"
	defer func() { goos = old }()
	for term, want := range map[string]string{
		"Standard":       "xdg-terminal-exec /tmp/s",
		"ghostty":        "ghostty -e /tmp/s",
		"alacritty":      "alacritty -e /tmp/s",
		"kitty":          "kitty /tmp/s",
		"wezterm":        "wezterm start -- /tmp/s",
		"gnome-terminal": "gnome-terminal -- /tmp/s",
	} {
		if got := strings.Join(terminalCommand(term, "/tmp/s").Args, " "); got != want {
			t.Errorf("%s: %s", term, got)
		}
	}
	if terms := installedTerminals(); len(terms) == 0 {
		t.Fatal("always one terminal")
	}
}

// Linux converts images in Go: large ones shrink, and they become JPEGs.
func TestScaleToJPEG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 1000))
	for x := 0; x < 3000; x++ {
		img.Set(x, 500, color.NRGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	out, err := scaleToJPEG(buf.Bytes(), 1568)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 1568 || cfg.Height != 522 {
		t.Fatalf("%v %dx%d", err, cfg.Width, cfg.Height)
	}
	if _, err := scaleToJPEG([]byte("no image"), 100); err == nil {
		t.Fatal("not an image should fail")
	}
}

// Waybar's own environment decides where it reads its configuration.
func TestWaybarSearchDirs(t *testing.T) {
	env := map[string]string{"HOME": "/home/u", "XDG_CONFIG_HOME": "/home/u/cfg", "XDG_CONFIG_DIRS": "/opt/xdg:/etc/xdg"}
	user, system := waybarSearchDirs(func(k string) string { return env[k] })
	if strings.Join(user, " ") != "/home/u/cfg/waybar /home/u/.config/waybar /home/u/waybar" {
		t.Fatal(user)
	}
	if system[0] != "/opt/xdg/waybar" || system[1] != "/etc/xdg/waybar" {
		t.Fatal(system)
	}
}
