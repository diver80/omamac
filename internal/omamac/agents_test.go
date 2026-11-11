package omamac

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// newTestApp makes an app on a temporary home, config and panels folder.
func newTestApp(t *testing.T) *App {
	t.Helper()
	useMemSecrets(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	if err := SaveConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	app.apps = nil
	return app
}

func writePanelFile(t *testing.T, dir, name string, p map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(p)
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A script that fails shows as failed, with its exit code and stderr; a warning on
// stderr no longer breaks the JSON on stdout.
func TestPanelRunFailureAndStderr(t *testing.T) {
	p := &PanelSpec{ID: "fails", Command: `echo "no network" >&2; exit 3`, Status: "ok"}
	if err := p.RunDataRefresh(context.Background()); err == nil {
		t.Fatal("a failing script is not an error")
	}
	if p.Status != "crit" || !strings.Contains(p.LastRunError, "3") || !strings.Contains(p.LastRunError, "no network") {
		t.Fatalf("failed run: status %q, error %q", p.Status, p.LastRunError)
	}
	if runs := panelRuns("fails"); len(runs) != 1 || runs[0].Exit != 3 || !runs[0].failed() {
		t.Fatalf("runs: %+v", runs)
	}

	ok := &PanelSpec{ID: "warns", Command: `echo "deprecated" >&2; echo '{"badge_text":"42","status":"ok"}'`}
	if err := ok.RunDataRefresh(context.Background()); err != nil || ok.BadgeText != "42" || ok.LastRunError != "" {
		t.Fatalf("JSON with a warning: err %v, badge %q, error %q", err, ok.BadgeText, ok.LastRunError)
	}

	// JSON printed before a non-zero exit still counts: the script told its state
	soft := &PanelSpec{ID: "soft", Command: `echo '{"badge_text":"0 found","status":"warn"}'; exit 1`}
	if err := soft.RunDataRefresh(context.Background()); err != nil || soft.Status != "warn" {
		t.Fatalf("JSON then exit 1: err %v, status %q", err, soft.Status)
	}
}

// The log never holds a secret value: it shows the variable's name.
func TestLogMasksSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "omamac.log")
	prev := setLogPath(path)
	t.Cleanup(func() { setLogPath(prev); setLogSecrets(nil) })
	setLogSecrets([]string{"MY_TOKEN=tok-1234567890", "SHORT=abc"})
	logf("panel", "curl -H 'Authorization: tok-1234567890' failed\nsecond line")
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "tok-1234567890") || !strings.Contains(string(raw), "$MY_TOKEN") {
		t.Fatalf("log line: %q", raw)
	}
	if strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("a message is one line: %q", raw)
	}
	lines, _ := tailLog(10, "panel")
	if len(lines) != 1 {
		t.Fatalf("tail: %q", lines)
	}
}

// A panel an agent writes into the folder arrives switched off, and its command waits
// for the user's OK; a changed command waits again; OmaMac's own writes are no change.
func TestExternalPanelWaitsForApproval(t *testing.T) {
	app := newTestApp(t)
	dir := app.Config.PanelsDir
	marker := filepath.Join(t.TempDir(), "ran")
	writePanelFile(t, dir, "agent-made.json", map[string]any{
		"id": "something-else", "title": "Agent made", "enabled": true,
		"command": "touch " + marker + "; echo BADGE=ran",
	})
	app.reloadPanelFile("agent-made.json")
	p := app.findPanel("agent-made")
	if p == nil {
		t.Fatal("the file's panel was not added (the file name is its id)")
	}
	if p.Enabled || !app.isHeld(p) {
		t.Fatalf("new panel: enabled %v, held %v", p.Enabled, app.isHeld(p))
	}
	if status, _ := app.panelStatusLine(p); status != "warn" {
		t.Fatalf("held panel shows %q", status)
	}
	app.refreshPanelData(context.Background(), p)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a held command ran")
	}
	app.runPanelAction(p, PanelAction{Command: "touch " + marker})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an action of a held panel ran")
	}
	img := ui.Render(app.RenderStudioView, 1280, 800, 1) // with the review box
	if img == nil {
		t.Fatal("no render")
	}

	app.approved.allow(p)
	app.refreshPanelData(context.Background(), p)
	if _, err := os.Stat(marker); err != nil || p.BadgeText != "ran" {
		t.Fatalf("an allowed command did not run (badge %q)", p.BadgeText)
	}
	if again := loadApprovals(approvalsPath(app.ConfigPath), nil); !again.allowed(p) {
		t.Fatal("the approval was not saved")
	}

	// OmaMac's own save is not a change from outside
	p.Enabled = true
	_ = SavePanel(dir, p)
	app.reloadPanelFile("agent-made.json")
	if app.findPanel("agent-made") != p {
		t.Fatal("OmaMac's own write replaced the panel")
	}

	// the agent changes the command: it waits again, the switches stay the user's
	writePanelFile(t, dir, "agent-made.json", map[string]any{"title": "Agent made", "enabled": false, "command": "echo BADGE=changed"})
	app.reloadPanelFile("agent-made.json")
	p = app.findPanel("agent-made")
	if !p.Enabled || !app.isHeld(p) {
		t.Fatalf("changed panel: enabled %v, held %v", p.Enabled, app.isHeld(p))
	}

	// the file goes: so does the panel
	_ = os.Remove(filepath.Join(dir, "agent-made.json"))
	app.reloadPanelFile("agent-made.json")
	if app.findPanel("agent-made") != nil {
		t.Fatal("the panel of a deleted file stayed")
	}

	// a broken file changes nothing
	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644)
	n := len(app.panels)
	app.reloadPanelFile("broken.json")
	if len(app.panels) != n {
		t.Fatal("a broken file added a panel")
	}
}

// Upgrading from a version without approvals: every panel there is ran before and stays allowed.
func TestApprovalsAllowExistingPanelsOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approved-commands.json")
	old := &PanelSpec{ID: "mine", Command: "echo BADGE=1"}
	ap := loadApprovals(path, []*PanelSpec{old})
	if !ap.allowed(old) {
		t.Fatal("an existing panel is held after the update")
	}
	later := &PanelSpec{ID: "later", Command: "echo BADGE=2"}
	if loadApprovals(path, []*PanelSpec{old, later}).allowed(later) {
		t.Fatal("a panel added after the first start was allowed without asking")
	}
	if !ap.allowed(&PanelSpec{ID: "static"}) {
		t.Fatal("a panel without a command needs no approval")
	}
	for _, p := range StarterPanels() {
		if !ap.allowed(p) {
			t.Fatalf("the delivered template %q needs approval", p.ID)
		}
	}
}

// The terminal script starts the agent in the panels folder with the prompt as one
// argument, whatever quotes it holds, and deletes itself.
func TestAgentScriptQuotesThePrompt(t *testing.T) {
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "panels dir")
	_ = os.MkdirAll(ws, 0o755)
	out := filepath.Join(tmp, "out.txt")
	fake := filepath.Join(tmp, "agent")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > '"+out+"'\nprintf '%s' \"$PATH\" >> '"+out+"'\n"), 0o755)
	prompt := "it's a \"test\" with $HOME and `date` and \\n"
	script := filepath.Join(tmp, "run.command")
	_ = os.WriteFile(script, []byte(agentScript(ws, "/opt/omamac bin", fake, []string{"-i", prompt}, "OmaMac · test")), 0o700)
	if b, err := exec.Command("/bin/zsh", script).CombinedOutput(); err != nil {
		t.Fatalf("script: %v %s", err, b)
	}
	raw, _ := os.ReadFile(out)
	lines := strings.Split(string(raw), "\n")
	realWS, _ := filepath.EvalSymlinks(ws)
	if len(lines) < 4 || (lines[0] != ws && lines[0] != realWS) || lines[1] != "-i" || lines[2] != prompt || !strings.HasPrefix(lines[3], "/opt/omamac bin:") {
		t.Fatalf("agent got %q", lines)
	}
	if _, err := os.Stat(script); err == nil {
		t.Fatal("the script did not delete itself")
	}
}

// AGENTS.md names the variables, never their values; the omamac command is next to the config.
func TestAgentWorkspace(t *testing.T) {
	app := newTestApp(t)
	if err := app.Vault.Set("SHOP_TOKEN", "value-not-for-agents", "the shop's API token"); err != nil {
		t.Fatal(err)
	}
	if err := app.writeAgentWorkspace(); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(app.Config.PanelsDir, "AGENTS.md"))
	if !strings.Contains(string(md), "$SHOP_TOKEN") || strings.Contains(string(md), "value-not-for-agents") {
		t.Fatalf("AGENTS.md variables:\n%s", md)
	}
	if claude, _ := os.ReadFile(filepath.Join(app.Config.PanelsDir, "CLAUDE.md")); string(claude) != "@AGENTS.md\n" {
		t.Fatalf("CLAUDE.md: %q", claude)
	}
	wrapper := filepath.Join(app.cliBinDir(), "omamac")
	if st, err := os.Stat(wrapper); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("no omamac command: %v", err)
	}
	// LoadPanels and the watch don't take the Markdown files for panels
	panels, _ := LoadPanels(app.Config.PanelsDir)
	for _, p := range panels {
		if strings.HasSuffix(p.ID, ".md") || p.ID == "AGENTS" {
			t.Fatalf("a Markdown file became panel %q", p.ID)
		}
	}
}

// omamac check runs a panel, masks secret values and fails with the script.
func TestCLICheck(t *testing.T) {
	app := newTestApp(t)
	_ = app.Vault.Set("SHOP_TOKEN", "value-not-for-agents", "")
	dir := app.Config.PanelsDir
	writePanelFile(t, dir, "shop.json", map[string]any{
		"title": "Shop", "titel": "typo",
		"command": `echo '{"badge_text":"token '"$SHOP_TOKEN"'","status":"ok","rows":[{"left":"a","right":"b"}]}'`,
	})
	var out, errb bytes.Buffer
	if code := RunCLI(app.ConfigPath, []string{"check", filepath.Join(dir, "shop.json")}, &out, &errb); code != 0 {
		t.Fatalf("check failed (%d): %s %s", code, out.String(), errb.String())
	}
	text := out.String()
	for _, want := range []string{"$SHOP_TOKEN", "1 rows", "Preview:", "unknown field \"titel\"", "OK"} {
		if !strings.Contains(text, want) {
			t.Errorf("check output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "value-not-for-agents") {
		t.Fatalf("check printed a secret value:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, ".previews", "shop.png")); err != nil {
		t.Fatalf("no preview: %v", err)
	}

	writePanelFile(t, dir, "broken.json", map[string]any{"title": "Broken", "command": "echo oops >&2; exit 2"})
	out.Reset()
	errb.Reset()
	if code := RunCLI(app.ConfigPath, []string{"check", filepath.Join(dir, "broken.json")}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "oops") {
		t.Fatalf("a failing panel: code %d, %q", code, errb.String())
	}
	if code := RunCLI(app.ConfigPath, []string{"nope"}, &out, &errb); code != 2 {
		t.Fatalf("unknown command: %d", code)
	}
	out.Reset()
	if code := RunCLI(app.ConfigPath, []string{"vars"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "SHOP_TOKEN") || strings.Contains(out.String(), "value-not-for-agents") {
		t.Fatalf("vars: %d %q", code, out.String())
	}
}

// In the launcher a sentence goes to the agent first, a name opens what it names; Tab
// switches the agent.
func TestLauncherSendsSentencesToTheAgent(t *testing.T) {
	app := newTestApp(t)
	app.agents.found = []agentInfo{{agentDefs[0], "/bin/echo"}}
	app.agents.detected = true
	app.Config.LauncherAgent = "claude"

	items := app.launcherItems("zeige mir die offenen Pull Requests")
	if items[0].kind != launchAI || !strings.Contains(items[0].title, "Claude Code") {
		t.Fatalf("a sentence: first item %q", items[0].title)
	}
	items = app.launcherItems("atlassian")
	if items[0].kind == launchAI || items[len(items)-1].kind != launchAI {
		t.Fatalf("a name: first item %q", items[0].title)
	}

	tt := ui.NewTester(app.RenderWalPanelView, 680, 480)
	tt.Key(0, ui.KeyTab)
	if app.currentAgent().ID != agentBuiltin {
		t.Fatalf("Tab chose %q", app.currentAgent().ID)
	}
	items = app.launcherItems("ein Panel mit der CPU-Last bitte")
	if !strings.HasPrefix(items[0].title, "Neues Panel mit KI") || !strings.Contains(items[0].subtitle, "Hauptfenster") {
		t.Fatalf("OmaMac's AI: %q", items[0].subtitle)
	}

	// pasted images go with the sentence and are then gone
	app.launchImages = []string{"/tmp/shot.png"}
	items = app.launcherItems("so soll es aussehen")
	items[0].run()
	if len(app.launchImages) != 0 || len(app.createImages) != 1 || !app.showCreate {
		t.Fatalf("images: launcher %v, dialog %v, open %v", app.launchImages, app.createImages, app.showCreate)
	}

	if img := ui.Render(app.renderLauncherBar, barWidth, barHeight, 1); img == nil {
		t.Fatal("bar does not render")
	}
}

// Ghostty gets the script through AppleScript, quoted; the others open the .command file.
func TestTerminalCommand(t *testing.T) {
	cmd := terminalCommand("Ghostty", `/tmp/a "b"\c.command`)
	if cmd.Args[0] != "osascript" || !strings.Contains(cmd.Args[2], `set command of cfg to "/tmp/a \"b\"\\c.command"`) {
		t.Fatalf("Ghostty: %q", cmd.Args)
	}
	if cmd := terminalCommand("Terminal", "/tmp/x.command"); strings.Join(cmd.Args, " ") != "open -a Terminal /tmp/x.command" {
		t.Fatalf("Terminal: %q", cmd.Args)
	}
	if out, err := exec.Command("osacompile", "-o", filepath.Join(t.TempDir(), "x.scpt"), "-e", terminalCommand("Ghostty", "/tmp/x").Args[2]).CombinedOutput(); err != nil {
		t.Fatalf("the AppleScript does not compile: %v %s", err, out)
	}
}

// The dashboard: tiles in rows of six columns, in the order the user dragged them, and
// only the panels switched on for it.
func TestDashboard(t *testing.T) {
	app := newTestApp(t)
	panels := app.dashboardPanels()
	if len(panels) != len(app.panels) {
		t.Fatalf("%d of %d panels on the dashboard", len(panels), len(app.panels))
	}
	week := app.findPanel("00-jira-week")
	if week == nil || tileSize(week).ID != "wide" {
		t.Fatalf("the week board is not a wide tile")
	}
	n := 0
	for _, row := range packTiles(panels) {
		cols := 0
		for _, p := range row {
			cols += tileSize(p).Cols
			n++
		}
		if cols > dashColumns {
			t.Fatalf("a row of %d columns", cols)
		}
	}
	if n != len(panels) {
		t.Fatalf("%d of %d tiles placed", n, len(panels))
	}
	// medium, wide, medium: the second medium fills the first row's gap
	m1, w, m2 := &PanelSpec{ID: "m1"}, &PanelSpec{ID: "w", DashboardSize: "wide"}, &PanelSpec{ID: "m2"}
	if rows := packTiles([]*PanelSpec{m1, w, m2}); len(rows) != 2 || len(rows[0]) != 2 || rows[0][1] != m2 || rows[1][0] != w {
		t.Fatalf("packing: %v", rows)
	}

	last := panels[len(panels)-1]
	app.moveTile(last.ID, panels[0].ID)
	if got := app.dashboardPanels(); got[0] != last {
		t.Fatalf("the moved tile is at %q", got[0].ID)
	}
	if again, _ := LoadConfig(app.ConfigPath); len(again.DashboardOrder) == 0 || again.DashboardOrder[0] != last.ID {
		t.Fatal("the order was not saved")
	}

	panels[0].DashboardOff = true
	for _, p := range app.dashboardPanels() {
		if p == panels[0] {
			t.Fatal("a panel switched off for the dashboard is a tile")
		}
	}
	app.setTileSize(last, "small")
	if tileSize(last).Cols != 2 {
		t.Fatal("the size was not taken")
	}

	// the bar under the tiles: a sentence goes to OmaMac's AI
	app.dashQuery = "zeige mir die Wetterlage für Berlin"
	items := app.launcherItems(app.dashQuery)
	if items[0].kind != launchAI {
		t.Fatalf("the bar's first suggestion is %q", items[0].title)
	}
	if img := ui.Render(app.RenderStudioView, 1320, 900, 1); img == nil {
		t.Fatal("the dashboard does not render")
	}
	if app.sidebarCollapsed() != true {
		t.Fatal("the sidebar is open on the dashboard by default")
	}
}

// Background mode: each agent's non-interactive arguments.
func TestHeadlessArgs(t *testing.T) {
	claude := agentInfo{agentDefs[0], "/x/claude"}
	args := headlessArgs(claude, "ignored: on stdin", false)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--output-format stream-json", "--verbose", "--permission-mode acceptEdits"} {
		if !strings.Contains(joined, want) {
			t.Errorf("claude args lack %q: %q", want, args)
		}
	}
	if args[len(args)-2] != "--allowedTools" || strings.Contains(joined, "ignored") {
		t.Fatalf("claude: tools last, prompt on stdin: %q", args)
	}
	if !strings.Contains(strings.Join(headlessArgs(claude, "", true), " "), "--continue") {
		t.Fatal("claude does not continue")
	}
	for _, def := range agentDefs[1:] {
		ag := agentInfo{def, "/x/" + def.Bin}
		args := headlessArgs(ag, "the prompt", false)
		found := false
		for _, a := range args {
			found = found || a == "the prompt"
		}
		if !found {
			t.Errorf("%s: prompt missing in %q", def.ID, args)
		}
	}
}

// A background run streams what the agent says and does, and the files it writes land
// in the panels folder.
func TestRunAgentProcess(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	fake := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(fake, []byte(`#!/bin/sh
read -r msg
case "$PATH" in "`+bin+`":*) ;; *) echo '{"type":"result","is_error":true,"result":"omamac not first in PATH"}'; exit 1;; esac
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Ich baue das Panel."}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"'"$PWD"'/prs.json"}}]}}'
echo '{"title":"PRs","command":"echo BADGE=1"}' > prs.json
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"omamac check prs.json"}}]}}'
echo '{"type":"result","is_error":false,"result":"done","permission_denials":[{"tool_name":"Bash","tool_input":{"command":"curl example.com"}}]}'
`), 0o755)
	var steps []string
	var out strings.Builder
	ctx := WithAIProgress(context.Background(), &AIProgress{
		Step:   func(s string) { steps = append(steps, s) },
		Output: func(s string) { out.WriteString(s) },
	})
	ag := agentInfo{agentDefs[0], fake}
	if err := runAgentProcess(ctx, ag, dir, bin, agentPrompt("Mach ein PR-Panel", ag, nil), nil, false); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(steps, " | ")
	for _, want := range []string{"Schreibt prs.json", "Führt aus: omamac check prs.json", "Nicht erlaubt: Führt aus: curl example.com"} {
		if !strings.Contains(all, want) {
			t.Errorf("steps lack %q: %s", want, all)
		}
	}
	if !strings.Contains(out.String(), "Ich baue das Panel.") {
		t.Fatalf("output: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "prs.json")); err != nil {
		t.Fatal("the agent did not work in the panels folder")
	}

	// a failure the agent reports is the run's error
	bad := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(bad, []byte("#!/bin/sh\nread -r msg\necho '{\"type\":\"result\",\"is_error\":true,\"result\":\"Credit balance is too low\"}'\nexit 1\n"), 0o755)
	if err := runAgentProcess(ctx, agentInfo{agentDefs[0], bad}, dir, bin, "x", nil, false); err == nil || !strings.Contains(err.Error(), "Credit balance") {
		t.Fatalf("reported failure: %v", err)
	}

	// other agents: their text, line by line
	plain := filepath.Join(t.TempDir(), "opencode")
	_ = os.WriteFile(plain, []byte("#!/bin/sh\necho \"args: $*\"\n"), 0o755)
	out.Reset()
	oc := agentInfo{agentDefs[4], plain}
	if err := runAgentProcess(ctx, oc, dir, bin, "hallo", nil, true); err != nil || !strings.Contains(out.String(), "args: run --continue hallo") {
		t.Fatalf("opencode: %v %q", err, out.String())
	}
}

// The panels an agent wrote are the ones that came in through the folder during its run.
func TestPanelsWrittenSince(t *testing.T) {
	app := newTestApp(t)
	start := time.Now()
	writePanelFile(t, app.Config.PanelsDir, "from-agent.json", map[string]any{"title": "From agent", "enabled": true})
	app.reloadPanelFile("from-agent.json")
	got := app.panelsWrittenSince(start)
	if len(got) != 1 || got[0].ID != "from-agent" {
		t.Fatalf("written: %v", got)
	}
	if len(app.panelsWrittenSince(time.Now().Add(time.Second))) != 0 {
		t.Fatal("older changes count")
	}
	if env := onlyVariables([]string{"A_TOKEN=1", "B_TOKEN=2", "PATHLIKE"}, []string{"B_TOKEN"}); len(env) != 1 || env[0] != "B_TOKEN=2" {
		t.Fatalf("only the named variables: %q", env)
	}
}
