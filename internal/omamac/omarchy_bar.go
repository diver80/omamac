package omamac

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Omarchy's own bar (omarchy-shell, Quickshell; it replaced Waybar) runs "command"
// modules of ~/.config/omarchy/shell.json every few seconds and shows what they print
// in Waybar's JSON. OmaMac puts one there for each item of its menu bar (`omamac bar N`,
// which prints the item once) and one for itself, as omarchy's own `omarchy bar` does:
// jq on the user's shell.json, or on Omarchy's defaults when there is none yet, then
// `omarchy-shell shell reloadConfig`. Entries the user moved stay where they are.

// omarchyShell reports whether Omarchy's shell draws the bar here.
func omarchyShell() bool {
	if goos != "linux" {
		return false
	}
	_, err := exec.LookPath("omarchy-shell")
	return err == nil
}

// omarchyShellRuns asks the running shell for an answer.
func omarchyShellRuns() bool {
	return exec.Command("omarchy-shell", "shell", "ping").Run() == nil
}

// omarchyShellConfig is the user's shell.json, and what it starts from without one.
func omarchyShellConfig() (user, defaults string) {
	user = homePath(".config", "omarchy", "shell.json")
	root := os.Getenv("OMARCHY_PATH")
	if root == "" {
		for _, dir := range []string{"/usr/share/omarchy", homePath(".local", "share", "omarchy")} {
			if _, err := os.Stat(filepath.Join(dir, "config", "omarchy", "shell.json")); err == nil {
				root = dir
				break
			}
		}
	}
	if root != "" {
		defaults = filepath.Join(root, "config", "omarchy", "shell.json")
	}
	return user, defaults
}

// omarchyModules are OmaMac's entries for n menu bar items: "omamac-1" … and "omamac".
func omarchyModules(bin string, n int) []map[string]any {
	run := shellQuote(bin)
	var mods []map[string]any
	for slot := 1; slot <= min(n, barSlots); slot++ {
		mods = append(mods, map[string]any{
			"id": fmt.Sprintf("omamac-%d", slot), "type": "command",
			"exec": fmt.Sprintf("%s bar %d", run, slot), "interval": 3,
			"onClick": fmt.Sprintf("%s open %d", run, slot), "tooltip": "OmaMac",
		})
	}
	return append(mods, map[string]any{
		"id": "omamac", "type": "command",
		"exec": run + " bar 0", "interval": 3,
		"onClick": run + " open", "onRightClick": run + " open studio", "tooltip": "OmaMac",
	})
}

// omarchyJQ puts $mods into the layout: entries that are there keep their place and
// the user's own settings, ours beyond $mods go, new ones come first on the right.
// With $mods empty it takes all of OmaMac's out.
const omarchyJQ = `
def ours: ((.id // "") | tostring | test("^omamac(-[0-9]+)?$"));
def arr: if type == "array" then . else [] end;
($mods | map({key: .id, value: .}) | from_entries) as $want
| (if type == "object" then . else {} end)
| .version = 1
| .bar = (if (.bar | type) == "object" then .bar else {} end)
| .bar.layout = (if (.bar.layout | type) == "object" then .bar.layout else {} end)
| ([.bar.layout[] | arr[] | select(ours) | .id]) as $have
| .bar.layout |= with_entries(.value |= (arr | map(if ours then (if $want[.id] then . + $want[.id] else empty end) else . end)))
| .bar.layout.right = ([$mods[] | select(.id as $i | ($have | index($i)) == null)] + (.bar.layout.right | arr))
`

// applyOmarchyMods writes shell.json with mods applied; changed: it is different now.
func applyOmarchyMods(user, defaults string, mods []map[string]any) (changed bool, err error) {
	src := user
	if st, err := os.Stat(user); err != nil || st.Size() == 0 {
		src = defaults
	}
	if src == "" {
		return false, errors.New("no shell.json of Omarchy found")
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}
	if mods == nil {
		mods = []map[string]any{} // jq's $mods: none, not null
	}
	modsJSON, _ := json.Marshal(mods)
	cmd := exec.Command("jq", "--argjson", "mods", string(modsJSON), omarchyJQ)
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("jq: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	if src == user && sameJSON(raw, out) {
		return false, nil
	}
	if src == user {
		backup := fmt.Sprintf("%s.omamac-backup-%s", user, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, raw, 0o644); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		return false, err
	}
	return true, writeFileAtomic(user, out)
}

// sameJSON reports whether a and b hold the same JSON, however written.
func sameJSON(a, b []byte) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	ja, _ := json.Marshal(va)
	jb, _ := json.Marshal(vb)
	return bytes.Equal(ja, jb)
}

// setUpOmarchyBar puts n menu bar items (and OmaMac's own) on Omarchy's bar.
func setUpOmarchyBar(bin string, n int, out io.Writer) (bool, error) {
	user, defaults := omarchyShellConfig()
	changed, err := applyOmarchyMods(user, defaults, omarchyModules(bin, n))
	if err == nil && changed {
		fmt.Fprintf(out, "✓ %s: OmaMac's %d modules (running %s)\n", user, min(n, barSlots)+1, bin)
		reloadOmarchyShell()
	}
	return changed, err
}

// takeDownOmarchyBar takes them off again.
func takeDownOmarchyBar(out io.Writer) (bool, error) {
	user, _ := omarchyShellConfig()
	if _, err := os.Stat(user); err != nil {
		return false, nil // the defaults never had them
	}
	changed, err := applyOmarchyMods(user, "", nil)
	if err == nil && changed {
		fmt.Fprintf(out, "✓ %s: OmaMac's modules taken out\n", user)
		reloadOmarchyShell()
	}
	return changed, err
}

func reloadOmarchyShell() {
	if exec.Command("omarchy-shell", "shell", "reloadConfig").Run() != nil {
		_ = exec.Command("omarchy-shell", "-q", "shell", "rescanPlugins").Run()
	}
}

// cliBarOnce is `omamac bar N`: the Nth item once, for Omarchy's command modules,
// which also tells the app that the module is there (touchBarSlot).
func cliBarOnce(slot int, stdout io.Writer) int {
	touchBarSlot(slot)
	line, _ := json.Marshal(barItemFor(slot, readBarState()))
	fmt.Fprintf(stdout, "%s\n", line)
	return 0
}
