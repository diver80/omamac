package omamac

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Coding agents run in the background, in the panels folder, without a terminal: each
// agent's own non-interactive mode, its output streamed into the "KI arbeitet" dialog.
// They may edit the files of that folder and run the omamac command, nothing else; what
// they write arrives through the folder watch, switched off and held for the user's OK.
// A reply in the dialog continues the same session; "Im Terminal fortsetzen" opens it
// interactively in Terminal (or the terminal chosen in the settings).

// agentRunTimeout bounds one background run of a coding agent.
const agentRunTimeout = 15 * time.Minute

// claudeAgentTools are what Claude Code may do without asking in a background run.
const claudeAgentTools = "Read,Write,Edit,MultiEdit,Glob,Grep,Bash(omamac check:*),Bash(omamac vars),Bash(omamac log:*)"

// headlessArgs are the arguments of ag's non-interactive mode; cont continues its last
// session in this folder. Claude Code reads the prompt (and images) from stdin.
func headlessArgs(ag agentInfo, prompt string, cont bool) []string {
	var args []string
	switch ag.ID {
	case "claude":
		args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
			"--permission-mode", "acceptEdits"}
		if cont {
			args = append(args, "--continue")
		}
		return append(args, "--allowedTools", claudeAgentTools) // last: it takes several values
	case "codex":
		args = []string{"exec", "--skip-git-repo-check", "--sandbox", "workspace-write",
			"-c", "sandbox_workspace_write.network_access=true"} // omamac check calls the panels' APIs
		if cont {
			args = append(args, "resume", "--last")
		}
		return append(args, prompt)
	case "gemini":
		return []string{"-p", prompt, "--approval-mode", "auto_edit"} // no session to continue
	case "agy":
		args = []string{"-p", prompt, "--mode", "accept-edits"}
		if cont {
			args = append(args, "--continue")
		}
		return args
	case "opencode":
		args = []string{"run"}
		if cont {
			args = append(args, "--continue")
		}
		return append(args, prompt)
	}
	return []string{prompt}
}

// continueArgs open ag's last session in this folder interactively, in a terminal.
func continueArgs(ag agentInfo) []string {
	switch ag.ID {
	case "claude", "opencode":
		return []string{"--continue"}
	case "codex":
		return []string{"resume", "--last"}
	case "agy":
		return []string{"-c"}
	}
	return nil
}

// agentPrompt is what the agent gets: the user's sentence, where it is and how to answer.
func agentPrompt(prompt string, ag agentInfo, images []string) string {
	var b strings.Builder
	b.WriteString(prompt)
	if len(images) > 0 && ag.ID != "claude" { // Claude Code gets them in the message itself
		refs := make([]string, len(images))
		for i, img := range images {
			refs[i] = ag.ImageRef(img)
		}
		b.WriteString("\n\nAttached images (look at them): " + strings.Join(refs, " "))
	}
	lang := "German"
	if Language() == "en" {
		lang = "English"
	}
	b.WriteString("\n\n(OmaMac started you in the background in its panels folder: follow AGENTS.md there. " +
		"Nobody can answer you while you work, so decide sensibly and say what you assumed. " +
		"End with a short summary in " + lang + ": which panel files you wrote and what to check.)")
	return b.String()
}

// runAgentBackground starts ag on prompt in the background and shows it in the dialog.
func (a *App) runAgentBackground(ag agentInfo, prompt string, images []string, cont bool) {
	if a.busy {
		a.toast(T("Die KI arbeitet noch – bitte warten oder abbrechen."))
		return
	}
	if err := a.writeAgentWorkspace(); err != nil {
		a.toast("⚠️ " + err.Error())
		return
	}
	title := fmt.Sprintf("%s: %s", ag.Name, truncateStr(prompt, 60))
	ctx, run := a.startAIRunFor(title, agentRunTimeout)
	run.agent = &ag
	a.busy = true
	a.busyLabel = fmt.Sprintf(T("%s arbeitet …"), ag.Name)
	a.showStudio()

	dir, binDir := a.Config.PanelsDir, a.cliBinDir()
	full := agentPrompt(prompt, ag, images)
	go func() {
		start := time.Now()
		logf("agent", "%s started in the background (%d characters, %d images, continue %v)", ag.Name, len(prompt), len(images), cont)
		err := runAgentProcess(ctx, ag, dir, binDir, full, images, cont)
		logf("agent", "%s finished after %.0f s: %v", ag.Name, time.Since(start).Seconds(), err)
		time.Sleep(600 * time.Millisecond) // the folder watch takes the last files over
		a.updateUI(func() {
			a.busy = false
			run.finish(err)
			if changed := a.panelsWrittenSince(start); len(changed) > 0 {
				run.setResult(T("geschrieben"), changed, nil)
				a.selectPanel(changed[len(changed)-1].ID)
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				a.statusErr = true
				a.statusMsg = fmt.Sprintf("%s: %v", ag.Name, err)
			}
		})
	}()
}

// panelsWrittenSince lists the panels that came in through the folder since t: the agent's.
func (a *App) panelsWrittenSince(t time.Time) []*PanelSpec {
	var out []*PanelSpec
	for _, p := range a.panels {
		if at, ok := a.externalAt[p.ID]; ok && !at.Before(t) {
			out = append(out, p)
		}
	}
	return out
}

// runAgentProcess runs the agent in dir until it is done, reporting to ctx's progress.
func runAgentProcess(ctx context.Context, ag agentInfo, dir, binDir, prompt string, images []string, cont bool) error {
	cmd := exec.CommandContext(ctx, ag.Path, headlessArgs(ag, prompt, cont)...)
	cmd.Dir = dir
	path := binDir
	if lp := loginShellPath(); lp != "" { // the app's own PATH is short; agents need node, git …
		path += ":" + lp
	}
	cmd.Env = append(os.Environ(), "PATH="+path+":"+os.Getenv("PATH"), "OMAMAC_AGENT=1")
	cmd.WaitDelay = 3 * time.Second
	if ag.ID == "claude" {
		imgs, err := LoadAIImages(ctx, images)
		if err != nil {
			return err
		}
		cmd.Stdin = bytes.NewReader(claudeStreamInput(prompt, imgs))
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	progressStep(ctx, T("Anfrage an %s gesendet (%d Zeichen)"), ag.Name, len(prompt))
	if err := cmd.Start(); err != nil {
		return err
	}
	var stream agentStream
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 64*1024), 32<<20)
	for sc.Scan() {
		if ag.ID == "claude" {
			stream.claudeLine(ctx, sc.Text())
		} else {
			progressOutput(ctx, sc.Text()+"\n")
			stream.text.WriteString(sc.Text() + "\n")
		}
	}
	err = cmd.Wait()
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf(T("%s: abgebrochen: %w"), ag.Name, context.Canceled)
	case ctx.Err() != nil:
		return fmt.Errorf(T("%s: nach %d min abgebrochen"), ag.Name, int(agentRunTimeout.Minutes()))
	case stream.failure != "":
		return errors.New(stream.failure)
	case err != nil:
		return fmt.Errorf("%v: %s", err, cliErrorDetail(stream.text.String(), stderr.String()))
	}
	return nil
}

// agentStream reads an agent's output.
type agentStream struct {
	text    strings.Builder
	failure string // the agent's own report of a failure
}

// claudeAgentEvent is the part of a Claude Code stream-json line a background run shows.
type claudeAgentEvent struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Message *struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	PermissionDenials []struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	} `json:"permission_denials"`
}

// claudeLine shows what Claude Code says and does: its text as output, its tools as steps.
func (s *agentStream) claudeLine(ctx context.Context, line string) {
	var ev claudeAgentEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type == "" {
		progressOutput(ctx, line+"\n")
		s.text.WriteString(line + "\n")
		return
	}
	switch ev.Type {
	case "assistant":
		if ev.Message == nil {
			return
		}
		for _, part := range ev.Message.Content {
			switch part.Type {
			case "text":
				if t := strings.TrimSpace(part.Text); t != "" {
					progressOutput(ctx, t+"\n\n")
					s.text.WriteString(t + "\n")
				}
			case "tool_use":
				progressStep(ctx, "%s", describeTool(part.Name, part.Input))
			}
		}
	case "result":
		for _, d := range ev.PermissionDenials {
			progressStep(ctx, T("Nicht erlaubt: %s"), describeTool(d.ToolName, d.ToolInput))
		}
		if ev.IsError {
			s.failure = strings.TrimSpace(ev.Result)
			if s.failure == "" {
				s.failure = T("Claude Code meldet einen Fehler.")
			}
		}
	}
}

// describeTool says in a few words what a tool call does: "Schreibt shop.json".
func describeTool(name string, input json.RawMessage) string {
	var in struct {
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
		Pattern  string `json:"pattern"`
	}
	_ = json.Unmarshal(input, &in)
	file := filepath.Base(in.FilePath)
	switch name {
	case "Write":
		return fmt.Sprintf(T("Schreibt %s"), file)
	case "Edit", "MultiEdit":
		return fmt.Sprintf(T("Ändert %s"), file)
	case "Read":
		return fmt.Sprintf(T("Liest %s"), file)
	case "Bash":
		return fmt.Sprintf(T("Führt aus: %s"), truncateStr(strings.Join(strings.Fields(in.Command), " "), 90))
	case "Glob", "Grep":
		return fmt.Sprintf(T("Sucht %s"), in.Pattern)
	}
	return name
}

// continueInTerminal opens the agent's last session in this folder in a terminal.
func (a *App) continueInTerminal(ag agentInfo) {
	if err := a.writeAgentWorkspace(); err != nil {
		a.toast("⚠️ " + err.Error())
		return
	}
	term := a.terminal()
	go func() {
		err := openInTerminal(ag, term, a.Config.PanelsDir, a.cliBinDir(), continueArgs(ag))
		a.updateUI(func() {
			if err != nil {
				logf("agent", "%s did not open in %s: %v", ag.Name, term, err)
				a.toast("⚠️ " + fmt.Sprintf(T("%s ließ sich nicht starten: %v"), ag.Name, err))
				return
			}
			logf("agent", "%s continued in %s", ag.Name, term)
			a.showAIRun = false
		})
	}()
}
