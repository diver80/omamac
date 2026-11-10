package omamac

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// OmaMac's log: what the panels' scripts, the AI, Jira and the coding agents did, one line
// each, in ~/Library/Logs/OmaMac/omamac.log. Secret values never reach it: every value
// panels get as a variable is replaced by its name ($NAME) before a line is written.

const (
	logMaxSize  = 2 << 20 // then omamac.log becomes omamac.log.1 and a new one starts
	runsPerPane = 10      // runs kept per panel for its "Läufe" list
)

type appLogger struct {
	mu      sync.Mutex
	path    string // "" logs nothing (tests)
	f       *os.File
	size    int64
	secrets []secretValue // longest value first, so a value inside another is not split
}

type secretValue struct{ name, value string }

var appLog = &appLogger{path: defaultLogPath()}

func defaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", "OmaMac", "omamac.log")
}

// LogPath is the file OmaMac writes its log to ("" when it logs nothing).
func LogPath() string {
	appLog.mu.Lock()
	defer appLog.mu.Unlock()
	return appLog.path
}

// setLogPath points the log at path; "" turns it off. It returns the previous path.
func setLogPath(path string) string {
	appLog.mu.Lock()
	defer appLog.mu.Unlock()
	prev := appLog.path
	if appLog.f != nil {
		_ = appLog.f.Close()
		appLog.f = nil
	}
	appLog.path, appLog.size = path, 0
	return prev
}

// setLogSecrets takes the environment panels get (NAME=value): its values are masked.
func setLogSecrets(env []string) {
	var s []secretValue
	for _, kv := range env {
		name, val, ok := strings.Cut(kv, "=")
		if !ok || len(strings.TrimSpace(val)) < 6 { // shorter values would mask ordinary words
			continue
		}
		s = append(s, secretValue{name, val})
	}
	sort.Slice(s, func(i, j int) bool { return len(s[i].value) > len(s[j].value) })
	appLog.mu.Lock()
	appLog.secrets = s
	appLog.mu.Unlock()
}

// maskSecrets replaces every secret value in s with its variable's name.
func maskSecrets(s string) string {
	appLog.mu.Lock()
	defer appLog.mu.Unlock()
	return appLog.mask(s)
}

func (l *appLogger) mask(s string) string {
	for _, sv := range l.secrets {
		s = strings.ReplaceAll(s, sv.value, "$"+sv.name)
	}
	return s
}

// logf writes one line of the kind "panel", "ai", "jira", "agent", "watch" or "app".
func logf(kind, format string, args ...any) {
	appLog.write(kind, fmt.Sprintf(format, args...))
}

func (l *appLogger) write(kind, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.path == "" {
		return
	}
	msg = strings.Join(strings.Fields(l.mask(msg)), " ") // one line, whatever the message holds
	line := fmt.Sprintf("%s %-5s %s\n", time.Now().Format("2006-01-02 15:04:05"), kind, msg)
	if l.f != nil && l.size+int64(len(line)) > logMaxSize {
		_ = l.f.Close()
		l.f = nil
		_ = os.Rename(l.path, l.path+".1")
	}
	if l.f == nil {
		if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
			return
		}
		f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		l.f, l.size = f, 0
		if st, err := f.Stat(); err == nil {
			l.size = st.Size()
		}
	}
	n, _ := l.f.WriteString(line)
	l.size += int64(n)
}

// tailLog returns the last n lines of the log (with those of omamac.log.1 before them),
// only the ones containing filter when it is not empty.
func tailLog(n int, filter string) ([]string, error) {
	path := LogPath()
	if path == "" {
		return nil, nil
	}
	var lines []string
	for _, p := range []string{path + ".1", path} {
		f, err := os.Open(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if filter == "" || strings.Contains(sc.Text(), filter) {
				lines = append(lines, sc.Text())
			}
		}
		_ = f.Close()
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// panelRun is one run of a panel's script, for the panel's "Läufe" list.
type panelRun struct {
	At       time.Time
	Duration time.Duration
	Exit     int    // the script's exit code; -1 when it did not start or was stopped
	Err      string // what went wrong; "" when it worked
	Output   string // the start of what it printed, secret values masked
}

func (r panelRun) failed() bool { return r.Err != "" }

var runLog = struct {
	mu   sync.Mutex
	runs map[string][]panelRun // newest last
}{runs: map[string][]panelRun{}}

// recordRun keeps r for panel id and logs it when it failed or when the panel's state
// changed (its first run, failing again after working, working again after failing):
// a panel refreshing every 30 seconds would otherwise fill the log.
func recordRun(id string, r panelRun) {
	runLog.mu.Lock()
	prev := runLog.runs[id]
	changed := len(prev) == 0 || prev[len(prev)-1].failed() != r.failed() || prev[len(prev)-1].Err != r.Err
	next := append(prev, r)
	if len(next) > runsPerPane {
		next = next[len(next)-runsPerPane:]
	}
	runLog.runs[id] = next
	runLog.mu.Unlock()

	switch {
	case !changed: // the same as last time: in the panel's list, not again in the log
	case r.failed():
		logf("panel", "%s failed after %.1f s: %s", id, r.Duration.Seconds(), r.Err)
	default:
		logf("panel", "%s ok (%.1f s, exit %d)", id, r.Duration.Seconds(), r.Exit)
	}
}

var outcomes = struct {
	mu      sync.Mutex
	failing map[string]string
}{failing: map[string]string{}}

// logOutcome logs a failure of what key names (a Jira instance's search, say) when it
// starts or its message changes, and that it works again: not every refresh's repeat.
func logOutcome(kind, key string, err error) {
	outcomes.mu.Lock()
	prev, was := outcomes.failing[key]
	if err != nil {
		outcomes.failing[key] = err.Error()
	} else {
		delete(outcomes.failing, key)
	}
	outcomes.mu.Unlock()
	switch {
	case err != nil && (!was || prev != err.Error()):
		logf(kind, "%s: %v", key, err)
	case err == nil && was:
		logf(kind, "%s: works again", key)
	}
}

// panelRuns returns the panel's recent runs, newest first.
func panelRuns(id string) []panelRun {
	runLog.mu.Lock()
	defer runLog.mu.Unlock()
	src := runLog.runs[id]
	out := make([]panelRun, len(src))
	for i, r := range src {
		out[len(src)-1-i] = r
	}
	return out
}

// firstLines returns up to n lines of s, each cut to max characters.
func firstLines(s string, n, max int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	for i, l := range lines {
		lines[i] = truncateStr(l, max)
	}
	return strings.Join(lines, "\n")
}
