package omamac

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The omamac command for coding agents (and people) in the panels folder: check a panel,
// list the variables, read the log. AGENTS.md describes it. Secret values never show:
// what a command prints has them replaced by $NAME.

const cliUsage = `usage: omamac [-config file] <command>

  check <panel.json>      run the panel's command with the variables, show what it printed,
                          the result and a PNG preview; exit code 1 when the panel fails
  vars                    the variables panels get (names and descriptions, never values)
  log [-n 40] [panel-id]  OmaMac's recent log lines, of one panel when an id is given
  help                    this text

Without a command, OmaMac starts as the menu bar app.
`

// RunCLI runs one of the commands above and returns the exit code: 0 done, 1 failed,
// 2 wrong arguments.
func RunCLI(configPath string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, cliUsage)
		return 2
	}
	switch args[0] {
	case "check":
		if len(args) != 2 {
			fmt.Fprint(stderr, "usage: omamac check <panel.json>\n")
			return 2
		}
		return cliCheck(configPath, args[1], stdout, stderr)
	case "vars":
		a, err := newApp(configPath, true)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, strings.TrimSpace(credentialCatalogText(a.Vault, a.Jira)))
		return 0
	case "log":
		fs := flag.NewFlagSet("log", flag.ContinueOnError)
		fs.SetOutput(stderr)
		n := fs.Int("n", 40, "number of lines")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 1 {
			return 2
		}
		lines, err := tailLog(*n, fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, l := range lines {
			fmt.Fprintln(stdout, l)
		}
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, cliUsage)
		return 0
	}
	fmt.Fprintf(stderr, "omamac: unknown command %q\n\n%s", args[0], cliUsage)
	return 2
}

// maskingWriter writes with every secret value replaced by its variable's name.
type maskingWriter struct{ w io.Writer }

func (m maskingWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(m.w, maskSecrets(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// cliCheck runs a panel file once, as OmaMac would, and says what came out.
func cliCheck(configPath, file string, stdout, stderr io.Writer) int {
	stdout, stderr = maskingWriter{stdout}, maskingWriter{stderr} // an agent reads this
	raw, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(stderr, "FAILED:", err)
		return 1
	}
	var spec PanelSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		fmt.Fprintf(stderr, "FAILED: %s is not valid JSON: %v\n", file, err)
		return 1
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&PanelSpec{}); err != nil {
		fmt.Fprintf(stdout, "• warning: %v (OmaMac ignores it)\n", err)
	}
	if id := strings.TrimSuffix(filepath.Base(file), ".json"); spec.ID != "" && spec.ID != id {
		fmt.Fprintf(stdout, "• note: the id is the file name, %q (not %q)\n", id, spec.ID)
	}
	spec.ID = strings.TrimSuffix(filepath.Base(file), ".json")

	a, err := newApp(configPath, true)
	if err != nil {
		fmt.Fprintln(stderr, "FAILED:", err)
		return 1
	}
	// Only the variables the panel names: an agent in the background calls this without asking.
	env := onlyVariables(a.PanelEnv(), panelVariables(&spec))
	notes, checkErr := spec.CheckPanelSpec(context.Background(), env...)
	for _, n := range notes {
		fmt.Fprintln(stdout, "•", n)
	}
	if runs := panelRuns(spec.ID); len(runs) > 0 {
		r := runs[0]
		fmt.Fprintf(stdout, "\nCommand: exit %d after %.1f s\n", r.Exit, r.Duration.Seconds())
		if out := firstLines(spec.LastCommandOut, 40, 300); out != "" {
			fmt.Fprintf(stdout, "Output (secret values shown as $NAME):\n%s\n", out)
		}
	}
	fmt.Fprintf(stdout, "\nResult: status %q · menu bar %q · %d metrics · %d rows · %d actions\n",
		spec.Status, spec.BadgeText, len(spec.Metrics), len(spec.Rows), len(spec.Actions))

	if spec.Type == PanelTypeNative && spec.BuiltinKind == "" {
		dir := filepath.Join(filepath.Dir(file), ".previews")
		if path, err := writePanelPreview(a, &spec, dir); err == nil {
			fmt.Fprintf(stdout, "Preview: %s\n", path)
		} else {
			fmt.Fprintln(stdout, "• no preview:", err)
		}
	} else if spec.Type == PanelTypeHTML {
		fmt.Fprintln(stdout, "• HTML panels have no preview here: open the panel in OmaMac")
	}

	if checkErr != nil {
		fmt.Fprintln(stderr, "FAILED:", checkErr)
		return 1
	}
	fmt.Fprintln(stdout, "OK")
	return 0
}

// writePanelPreview renders the panel as its menu bar popover shows it.
func writePanelPreview(a *App, p *PanelSpec, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	shown := maskedTexts(p) // the agent looks at the picture, too
	img := ui.Render(func(c *ui.Context) { a.renderNativePanelPopover(c, shown, nil, nil) }, p.Width, p.Height, 2.0)
	path := filepath.Join(dir, p.ID+".png")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return "", err
	}
	return path, nil
}

// maskedTexts is a copy of p whose texts show secret values as $NAME.
func maskedTexts(p *PanelSpec) *PanelSpec {
	cp := *p
	cp.Title, cp.Subtitle, cp.BadgeText = maskSecrets(p.Title), maskSecrets(p.Subtitle), maskSecrets(p.BadgeText)
	cp.Metrics = append([]PanelMetric(nil), p.Metrics...)
	for i := range cp.Metrics {
		m := &cp.Metrics[i]
		m.Label, m.Value, m.Unit = maskSecrets(m.Label), maskSecrets(m.Value), maskSecrets(m.Unit)
	}
	cp.Rows = append([]PanelRow(nil), p.Rows...)
	for i := range cp.Rows {
		r := &cp.Rows[i]
		r.Left, r.Right, r.Badge = maskSecrets(r.Left), maskSecrets(r.Right), maskSecrets(r.Badge)
	}
	return &cp
}

// onlyVariables keeps the NAME=value entries of env whose NAME is in names.
func onlyVariables(env, names []string) []string {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []string
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && want[name] {
			out = append(out, kv)
		}
	}
	return out
}
