package omamac

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The launcher's bar hands a sentence to an agent, as Omalauncher's does: OmaMac's own
// AI (Gemini or Claude) builds panels in the "Neues Panel" dialog, with screenshots and
// mockups; a coding agent (Claude Code, Codex, …) starts in a terminal, in the panels
// folder, where AGENTS.md tells it how panels work and which variables there are.

const agentBuiltin = "omamac" // the app's own panel AI

// agentDef is a coding agent the bar can start.
type agentDef struct {
	ID, Name string
	Bin      string                       // program name
	Args     func(prompt string) []string // starts it interactively with prompt as the first message
	ImageRef func(path string) string     // how a prompt points it at an image file
}

func plainImageRef(path string) string { return path }

var agentDefs = []agentDef{
	{ID: "claude", Name: "Claude Code", Bin: "claude", Args: func(p string) []string { return []string{p} }, ImageRef: plainImageRef},
	{ID: "codex", Name: "Codex", Bin: "codex", Args: func(p string) []string { return []string{p} }, ImageRef: plainImageRef},
	{ID: "gemini", Name: "Gemini CLI", Bin: "gemini", Args: func(p string) []string { return []string{"-i", p} }, ImageRef: func(path string) string { return "@" + path }},
	{ID: "agy", Name: "Antigravity", Bin: "agy", Args: func(p string) []string { return []string{"-i", p} }, ImageRef: plainImageRef},
	{ID: "opencode", Name: "opencode", Bin: "opencode", Args: func(p string) []string { return []string{"--prompt", p} }, ImageRef: plainImageRef},
}

// agentInfo is an installed agent: where its program is.
type agentInfo struct {
	agentDef
	Path string
}

// agentChoice is what the bar can send a sentence to: the app's AI first, then the
// installed agents.
type agentChoice struct {
	ID, Name string
	agent    *agentInfo // nil for OmaMac's own AI
}

type agentRegistry struct {
	mu       sync.Mutex
	found    []agentInfo
	detected bool
}

// detectAgents looks for the agents' programs: in the PATH of a login shell (an app
// started from the Finder gets a short one) and where installers put them.
func (a *App) detectAgents() {
	home, _ := os.UserHomeDir()
	dirs := strings.Split(loginShellPath(), ":")
	dirs = append(dirs, strings.Split(os.Getenv("PATH"), ":")...)
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin", ".local/bin", ".bun/bin", ".npm-global/bin", ".opencode/bin", ".cargo/bin", "bin"} {
		if !filepath.IsAbs(d) {
			d = filepath.Join(home, d)
		}
		dirs = append(dirs, d)
	}
	var found []agentInfo
	for _, def := range agentDefs {
		path := ""
		switch def.ID { // what the AI settings already point at
		case "claude":
			path = executableFile(a.Config.ClaudeCLIPath)
		case "agy":
			path = executableFile(a.Config.AntigravityCLIPath)
		}
		for _, d := range dirs {
			if path != "" {
				break
			}
			if d != "" {
				path = executableFile(filepath.Join(d, def.Bin))
			}
		}
		if path != "" {
			found = append(found, agentInfo{def, path})
		}
	}
	a.agents.mu.Lock()
	a.agents.found, a.agents.detected = found, true
	a.agents.mu.Unlock()
	logf("agent", "found: %s", agentNames(found))
}

func agentNames(list []agentInfo) string {
	if len(list) == 0 {
		return "none"
	}
	var names []string
	for _, ag := range list {
		names = append(names, ag.Name)
	}
	return strings.Join(names, ", ")
}

// executableFile returns path when it is an executable file (an absolute path), else "".
func executableFile(path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
		return ""
	}
	return path
}

var (
	loginPathOnce sync.Once
	loginPath     string
)

// loginShellPath is the PATH the user's login shell sets up (Homebrew's, say).
func loginShellPath() string {
	loginPathOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/bin/zsh", "-lc", `printf %s "$PATH"`).Output()
		if err == nil {
			loginPath = strings.TrimSpace(string(out))
		}
	})
	return loginPath
}

func (r *agentRegistry) isDetected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.detected
}

// installedAgentNames names the coding agents found ("keine" when there are none).
func (a *App) installedAgentNames() string {
	a.agents.mu.Lock()
	defer a.agents.mu.Unlock()
	if len(a.agents.found) == 0 {
		return T("kein Coding-Agent (Claude Code, Codex, Gemini CLI, Antigravity, opencode)")
	}
	return agentNames(a.agents.found)
}

// agentChoices lists the bar's targets: OmaMac's AI, then the installed agents.
func (a *App) agentChoices() []agentChoice {
	out := []agentChoice{{ID: agentBuiltin, Name: T("OmaMac-KI")}}
	a.agents.mu.Lock()
	defer a.agents.mu.Unlock()
	for i := range a.agents.found {
		ag := &a.agents.found[i]
		out = append(out, agentChoice{ID: ag.ID, Name: ag.Name, agent: ag})
	}
	return out
}

// currentAgent is the bar's target; OmaMac's AI when the chosen agent is not installed.
func (a *App) currentAgent() agentChoice {
	choices := a.agentChoices()
	for _, ch := range choices {
		if ch.ID == a.Config.LauncherAgent {
			return ch
		}
	}
	return choices[0]
}

// setAgent makes id the bar's target and saves it.
func (a *App) setAgent(id string) {
	if id == agentBuiltin {
		id = ""
	}
	if a.Config.LauncherAgent == id {
		return
	}
	a.Config.LauncherAgent = id
	_ = SaveConfig(a.ConfigPath, a.Config)
}

// nextAgent switches the bar to the next target (Tab in the launcher).
func (a *App) nextAgent() {
	choices := a.agentChoices()
	cur := a.currentAgent()
	for i, ch := range choices {
		if ch.ID == cur.ID {
			a.setAgent(choices[(i+1)%len(choices)].ID)
			return
		}
	}
}

// askAgent sends the bar's sentence and its images to the current target.
func (a *App) askAgent(prompt string, images []string) {
	ch := a.currentAgent()
	if ch.agent == nil {
		a.createImages = append(a.createImages, images...)
		a.openCreate(prompt)
		return
	}
	a.hideLauncher()
	fail := func(err error) {
		logf("agent", "%s did not start: %v", ch.Name, err)
		a.statusErr = true
		a.statusMsg = fmt.Sprintf(T("%s ließ sich nicht starten: %v"), ch.Name, err)
		a.toast("⚠️ " + a.statusMsg)
		a.showStudio()
	}
	if err := a.writeAgentWorkspace(); err != nil { // here: it reads the vault's catalog
		fail(err)
		return
	}
	ag, term := *ch.agent, a.terminal()
	go func() { // opening the terminal takes a moment
		if err := launchAgent(ag, term, a.Config.PanelsDir, a.cliBinDir(), prompt, images); err != nil {
			a.updateUI(func() { fail(err) })
		}
	}()
}

// Terminals the agents can run in, by the name of their app.
var terminalApps = []string{"Ghostty", "iTerm", "Terminal"}

// installedTerminals lists the terminals there are; Terminal always is.
func installedTerminals() []string {
	var out []string
	for _, name := range terminalApps {
		if name == "Terminal" || terminalInstalled(name) {
			out = append(out, name)
		}
	}
	return out
}

func terminalInstalled(name string) bool {
	for _, dir := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
		if _, err := os.Stat(filepath.Join(dir, name+".app")); err == nil {
			return true
		}
	}
	return false
}

// terminal is the one chosen in the settings, else the first installed one.
func (a *App) terminal() string {
	list := installedTerminals()
	for _, name := range list {
		if name == a.Config.Terminal {
			return name
		}
	}
	return list[0]
}

// launchAgent starts ag in a window of the terminal term, in the folder ws, with prompt;
// binDir, with the omamac command, goes first in its PATH.
func launchAgent(ag agentInfo, term, ws, binDir, prompt string, images []string) error {
	if len(images) > 0 {
		refs := make([]string, len(images))
		for i, img := range images {
			refs[i] = ag.ImageRef(img)
		}
		prompt += "\n\nAttached images (look at them): " + strings.Join(refs, " ")
	}
	script := agentScript(ws, binDir, ag.Path, ag.Args(prompt), "OmaMac · "+ag.Name)
	f, err := os.CreateTemp("", "omamac-agent-*.command")
	if err != nil {
		return err
	}
	_, werr := f.WriteString(script)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(f.Name())
		return fmt.Errorf("%v %v", werr, cerr)
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		return err
	}
	if out, err := terminalCommand(term, f.Name()).CombinedOutput(); err != nil {
		_ = os.Remove(f.Name())
		return fmt.Errorf("%s: %v %s", term, err, strings.TrimSpace(string(out)))
	}
	logf("agent", "%s started in %s (%d characters, %d images)", ag.Name, term, len(prompt), len(images))
	return nil
}

// terminalCommand opens a window of term that runs script. Ghostty gets it through its
// AppleScript (a script opened with it would ask first); Terminal and iTerm run a
// .command file opened with them. macOS asks once whether OmaMac may control Ghostty.
func terminalCommand(term, script string) *exec.Cmd {
	if term == "Ghostty" {
		// The limit covers macOS's first question whether OmaMac may control Ghostty, too.
		as := `with timeout of 90 seconds
	tell application "Ghostty"
		set cfg to new surface configuration
		set command of cfg to ` + appleScriptString(script) + `
		new window with configuration cfg
		activate
	end tell
end timeout`
		return exec.Command("osascript", "-e", as)
	}
	return exec.Command("open", "-a", term, script)
}

// appleScriptString quotes s for AppleScript.
func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// agentScript is the script the terminal runs: it removes itself, goes to the panels
// folder, puts the omamac command on the PATH and replaces itself with the agent.
func agentScript(workspace, binDir, program string, args []string, title string) string {
	var b strings.Builder
	b.WriteString("#!/bin/zsh -l\n")
	b.WriteString("# Written by OmaMac to start a coding agent; it deletes itself.\n")
	b.WriteString(`rm -f -- "$0"` + "\n")
	b.WriteString("cd -- " + shellQuote(workspace) + " || exit 1\n")
	b.WriteString("export PATH=" + shellQuote(binDir) + `:"$PATH"` + "\n")
	b.WriteString(`printf '\e]0;%s\a' ` + shellQuote(title) + "\n")
	b.WriteString("exec " + shellQuote(program))
	for _, arg := range args {
		b.WriteString(" " + shellQuote(arg))
	}
	b.WriteString("\n")
	return b.String()
}

// shellQuote quotes s for zsh: in single quotes, each ' written as '\”.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// cliBinDir holds the omamac command for the agents, next to the configuration.
func (a *App) cliBinDir() string {
	cfg := a.ConfigPath
	if cfg == "" {
		cfg = DefaultConfigPath()
	}
	return filepath.Join(filepath.Dir(cfg), "bin")
}

// writeAgentWorkspace writes what an agent in the panels folder reads: AGENTS.md (and the
// CLAUDE.md and GEMINI.md that point to it), and the omamac command it runs.
func (a *App) writeAgentWorkspace() error {
	ws := a.Config.PanelsDir
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(ws, "AGENTS.md"), []byte(a.agentsMarkdown()), 0o644); err != nil {
		return err
	}
	for _, name := range []string{"CLAUDE.md", "GEMINI.md"} {
		path := filepath.Join(ws, name)
		if _, err := os.Stat(path); os.IsNotExist(err) { // the user's own notes stay
			_ = os.WriteFile(path, []byte("@AGENTS.md\n"), 0o644)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	cfg := a.ConfigPath
	if cfg == "" {
		cfg = DefaultConfigPath()
	}
	wrapper := "#!/bin/sh\n# Written by OmaMac: its command line for coding agents (omamac help).\nexec " +
		shellQuote(exe) + " -config " + shellQuote(cfg) + ` "$@"` + "\n"
	bin := a.cliBinDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(bin, "omamac"), []byte(wrapper), 0o755)
}

func writeIfChanged(path string, data []byte, mode os.FileMode) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	return os.WriteFile(path, data, mode)
}

// agentsMarkdown is AGENTS.md: what a coding agent needs to write OmaMac panels.
func (a *App) agentsMarkdown() string {
	vars := strings.TrimSpace(credentialCatalogText(a.Vault, a.Jira))
	lang := "German"
	if Language() == "en" {
		lang = "English"
	}
	return fmt.Sprintf(`# OmaMac panels

This folder holds the panels of OmaMac, a macOS menu bar app. Each `+"`*.json`"+` file is one
panel, and the file name is its id. OmaMac watches the folder: a file you write shows up
in the app right away. OmaMac %s wrote this file and rewrites it when it starts an agent
here, so changes to it are lost.

## Rules

- Work only in this folder. Never read files outside it: `+"`../config.json`"+`, the vault and
  the Jira settings there hold secrets.
- Never put a secret value into a panel. Use the variables below: OmaMac sets them when
  the command runs. Never print their values (no `+"`echo $TOKEN`"+`, no `+"`env`"+`).
- A new panel arrives switched off, and a new or changed command does not run until the
  user allows it in OmaMac. Tell the user to check the panel there and switch it on.
- Run `+"`omamac check <id>.json`"+` after every change and fix what it reports.
- Write the panel's texts (title, labels, badge) in %s.

## A panel

`+"```json"+`
{
  "title": "Docker",
  "subtitle": "Running containers",
  "icon": "🐳",
  "type": "native",
  "refresh_seconds": 60,
  "requires_brew": ["jq"],
  "command": "docker ps --format json | jq -s '{badge_text: \"\\(length) up\", status: \"ok\", rows: [.[] | {left: .Names, right: .Status, status: \"ok\"}]}'",
  "actions": [{"label": "Open Docker", "command": "open -a Docker", "style": "primary"}]
}
`+"```"+`

- `+"`command`"+` runs in `+"`/bin/zsh -c`"+` with the user's environment and the variables below,
  every `+"`refresh_seconds`"+` (15 at least). It is stopped after %d seconds. It prints ONE JSON
  object to stdout:
  `+"`{\"badge_text\": \"…\", \"status\": \"ok|warn|crit|info\", \"subtitle\": \"…\", \"metrics\": […], \"rows\": […]}`"+`.
  `+"`badge_text`"+` is the text in the menu bar: keep it short. Lines `+"`BADGE=…`"+`, `+"`STATUS=…`"+` and
  `+"`KEY=VALUE`"+` (a row each) work too.
- On an error, write it to stderr and exit non-zero: OmaMac shows the panel as failed and logs it.
- `+"`metrics`"+`: `+"`label`, `value`, `unit`, `status`, `progress`"+` (0–100). `+"`rows`"+`: `+"`left`, `right`, `badge`, `status`, `url`"+`.
- `+"`actions`"+`: buttons, each with `+"`label`"+` and a `+"`command`"+` or `+"`url`"+`; `+"`style`"+` is `+"`primary`, `danger` or `default`"+`.
- `+"`type: \"html\"`"+` panels have a self-contained page in `+"`html`"+`; it gets the panel with its
  data in `+"`window.onOmaMacUpdate = function (panel) { … }`"+`.
- `+"`requires_brew`"+`: the Homebrew tools the command needs (`+"`omamac check`"+` looks for them).
- `+"`dashboard_size`"+`: the panel's tile on OmaMac's dashboard, `+"`small`, `medium`, `tall` or `wide`"+`.
- Leave these to OmaMac: `+"`enabled`, `pinned_floating`, `dashboard_off`, `menubar_text`, `prompt_history`, `last_*`"+`.
- Files with `+"`builtin_kind`"+` are OmaMac's own modules (Jira, Atlassian status): don't change them.

## Variables

%s

## The omamac command

- `+"`omamac check <id>.json`"+`: runs the command with the variables, prints what it printed (secret
  values shown as $NAME), the result and a PNG preview; exit code 1 when the panel fails.
- `+"`omamac vars`"+`: the variables (names and descriptions, never values).
- `+"`omamac log [-n 40] [id]`"+`: OmaMac's recent log lines, of one panel when an id is given.
`, versionLabel(), lang, int(commandTimeout.Seconds()), vars)
}
