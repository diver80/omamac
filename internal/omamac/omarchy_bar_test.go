package omamac

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// layoutIDs lists the ids of a shell.json's bar sections.
func layoutIDs(t *testing.T, path string) map[string][]string {
	t.Helper()
	var cfg struct {
		Bar struct {
			Layout map[string][]map[string]any `json:"layout"`
		} `json:"bar"`
	}
	if err := json.Unmarshal([]byte(mustRead(t, path)), &cfg); err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for sec, entries := range cfg.Bar.Layout {
		for _, e := range entries {
			out[sec] = append(out[sec], e["id"].(string))
		}
	}
	return out
}

func entryOf(t *testing.T, path, id string) map[string]any {
	t.Helper()
	var cfg struct {
		Bar struct {
			Layout map[string][]map[string]any `json:"layout"`
		} `json:"bar"`
	}
	_ = json.Unmarshal([]byte(mustRead(t, path)), &cfg)
	for _, entries := range cfg.Bar.Layout {
		for _, e := range entries {
			if e["id"] == id {
				return e
			}
		}
	}
	return nil
}

// OmaMac's command modules go onto Omarchy's bar from its defaults, keep the place and
// settings the user gave them, follow the number of menu bar items, and come off again.
func TestOmarchyBarModules(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("no jq")
	}
	user := filepath.Join(t.TempDir(), "omarchy", "shell.json")
	defaults := "testdata/omarchy-shell.json"

	changed, err := applyOmarchyMods(user, defaults, omarchyModules("/home/me/omamac", 2))
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	right := layoutIDs(t, user)["right"]
	if strings.Join(right[:4], " ") != "omamac-1 omamac-2 omamac omarchy.tray" || len(right) != 11 {
		t.Fatalf("right: %v", right)
	}
	m1 := entryOf(t, user, "omamac-1")
	if m1["type"] != "command" || m1["exec"] != "'/home/me/omamac' bar 1" || m1["onClick"] != "'/home/me/omamac' open 1" {
		t.Fatalf("omamac-1: %v", m1)
	}
	if entryOf(t, user, "omarchy.clock")["format"] != "dddd HH:mm" {
		t.Fatal("the clock lost its settings")
	}
	if changed, _ := applyOmarchyMods(user, defaults, omarchyModules("/home/me/omamac", 2)); changed {
		t.Fatal("the same modules again changed shell.json")
	}

	// The user drags omamac-1 to the left and gives it a setting.
	var cfg map[string]any
	_ = json.Unmarshal([]byte(mustRead(t, user)), &cfg)
	layout := cfg["bar"].(map[string]any)["layout"].(map[string]any)
	r := layout["right"].([]any)
	moved := r[0].(map[string]any)
	moved["fontSize"] = 14
	layout["right"] = r[1:]
	layout["left"] = append(layout["left"].([]any), moved)
	data, _ := json.Marshal(cfg)
	_ = os.WriteFile(user, data, 0o644)

	// An update moves OmaMac; one menu bar item fewer.
	if _, err := applyOmarchyMods(user, defaults, omarchyModules("/opt/omamac", 1)); err != nil {
		t.Fatal(err)
	}
	ids := layoutIDs(t, user)
	if ids["left"][len(ids["left"])-1] != "omamac-1" || strings.Contains(strings.Join(ids["right"], " "), "omamac-2") {
		t.Fatalf("layout: %v", ids)
	}
	if e := entryOf(t, user, "omamac-1"); e["exec"] != "'/opt/omamac' bar 1" || e["fontSize"] != float64(14) {
		t.Fatalf("omamac-1 after the update: %v", e)
	}
	if backups, _ := filepath.Glob(user + ".omamac-backup-*"); len(backups) == 0 {
		t.Fatal("no backup of the user's shell.json")
	}

	if _, err := applyOmarchyMods(user, "", nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mustRead(t, user), "omamac") {
		t.Fatal("uninstall left modules")
	}
	if len(layoutIDs(t, user)["right"]) != 8 {
		t.Fatalf("right after uninstall: %v", layoutIDs(t, user)["right"])
	}
}

// A module that printed lately counts as on the bar, so its panel has no tray icon.
func TestBarSlotHeartbeat(t *testing.T) {
	old := goos
	goos = "linux"
	defer func() { goos = old }()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if live := liveBarSlots(); live[2] {
		t.Fatal("nothing printed yet")
	}
	var out strings.Builder
	if cliBarOnce(2, &out) != 0 || !strings.Contains(out.String(), `"text"`) {
		t.Fatal(out.String())
	}
	if live := liveBarSlots(); !live[2] || live[1] {
		t.Fatalf("live: %v", live)
	}
	if got := omarchyModules("/x", 9); len(got) != barSlots+1 {
		t.Fatalf("%d modules for 9 items", len(got))
	}
}
