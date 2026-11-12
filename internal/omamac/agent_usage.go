package omamac

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// The agents' plan limits: how much of the Claude Code subscription's session (5 hours)
// and week is used, as Claude Code's /usage shows it. OmaMac reads Claude Code's login with
// the security tool (Claude Code keeps it in the keychain with that tool, so no prompt) and
// asks Anthropic for the numbers. The token goes nowhere else and is not kept.

const BuiltinAgentUsage = "agent-usage"

// claudeUsageURL is where Claude Code's /usage gets its numbers.
var claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

// claudeLogin is the part of Claude Code's login the limits need.
type claudeLogin struct {
	AccessToken      string `json:"accessToken"`
	ExpiresAt        int64  `json:"expiresAt"` // Unix milliseconds
	SubscriptionType string `json:"subscriptionType"`
}

var errNoClaudeLogin = errors.New("no Claude Code subscription login")

// readClaudeLogin reads Claude Code's login; tests replace it.
var readClaudeLogin = claudeLoginFromSystem

// claudeLoginFromSystem reads Claude Code's login: from the keychain, else from its
// credentials file.
func claudeLoginFromSystem(ctx context.Context) (*claudeLogin, error) {
	raw, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password",
		"-s", "Claude Code-credentials", "-w").Output()
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		home, _ := os.UserHomeDir()
		if raw, err = os.ReadFile(filepath.Join(home, ".claude", ".credentials.json")); err != nil {
			return nil, errNoClaudeLogin
		}
	}
	return parseClaudeLogin(raw)
}

func parseClaudeLogin(raw []byte) (*claudeLogin, error) {
	raw = bytes.TrimSpace(raw)
	if dec, err := hex.DecodeString(string(raw)); err == nil { // older versions stored it hex-encoded
		raw = dec
	}
	var v struct {
		ClaudeAiOauth *claudeLogin `json:"claudeAiOauth"`
	}
	if json.Unmarshal(raw, &v) != nil || v.ClaudeAiOauth == nil || v.ClaudeAiOauth.AccessToken == "" {
		return nil, errNoClaudeLogin
	}
	return v.ClaudeAiOauth, nil
}

// usageLimit is one limit of a plan: the session's, the week's, a model's week.
type usageLimit struct {
	Label    string
	Percent  float64
	ResetsAt time.Time
	Window   time.Duration // its period: 5 hours, 7 days; 0 when unknown
	Severity string        // Anthropic's: "normal" while there is room
}

// agentUsage is what the module shows of one agent.
type agentUsage struct {
	Plan    string // "Team", "Max"
	Limits  []usageLimit
	Extra   string    // the extra usage bought beyond the plan, when it is on
	At      time.Time // when the numbers came
	Problem string    // why they are missing or old
	NoLogin bool      // Claude Code is not logged in with a subscription
	Loaded  bool      // a refresh has run
}

type usageState struct {
	mu     sync.Mutex
	claude agentUsage
}

func (a *App) claudeUsage() agentUsage {
	a.usage.mu.Lock()
	defer a.usage.mu.Unlock()
	return a.usage.claude
}

func (a *App) setClaudeUsage(u agentUsage) {
	a.usage.mu.Lock()
	a.usage.claude = u
	a.usage.mu.Unlock()
}

// refreshAgentUsage fetches the limits and sets the panel's status and menu bar text.
func (a *App) refreshAgentUsage(ctx context.Context, p *PanelSpec) {
	u := fetchClaudeUsage(ctx, a.claudeUsage())
	a.setClaudeUsage(u)
	p.Status, p.BadgeText = usageStatus(u)
	p.LastUpdated = time.Now().Format("15:04:05")
}

// fetchClaudeUsage asks for the current numbers; when that fails, prev's stay, with why.
func fetchClaudeUsage(ctx context.Context, prev agentUsage) agentUsage {
	const key = "Claude Code limits"
	login, err := readClaudeLogin(ctx)
	if err != nil {
		logOutcome("agent", key, err)
		return agentUsage{NoLogin: true, Loaded: true, Problem: T("Claude Code ist nicht mit einem Claude-Abo angemeldet.")}
	}
	u := prev
	u.Plan, u.NoLogin, u.Loaded = planName(login.SubscriptionType), false, true
	if login.ExpiresAt > 0 && time.UnixMilli(login.ExpiresAt).Before(time.Now()) {
		u.Problem = T("Die Anmeldung von Claude Code ist abgelaufen – starte Claude Code einmal, dann geht es hier weiter.")
		logOutcome("agent", key, errors.New("login expired"))
		return u
	}
	limits, extra, err := getClaudeUsage(ctx, login.AccessToken)
	logOutcome("agent", key, err)
	if err != nil {
		u.Problem = err.Error()
		return u
	}
	return agentUsage{Plan: u.Plan, Limits: limits, Extra: extra, At: time.Now(), Loaded: true}
}

// getClaudeUsage asks Anthropic for the limits of the login's plan.
func getClaudeUsage(ctx context.Context, token string) ([]usageLimit, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "OmaMac/"+appVersion())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf(T("Anthropic nicht erreichbar: %v"), errors.Unwrap(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, "", errors.New(T("Claude Code muss sich neu anmelden – starte es einmal (oder /login)."))
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, "", errors.New(T("Anthropic bremst die Abfrage gerade – der nächste Versuch kommt von selbst."))
	case resp.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf(T("Anthropic antwortet mit HTTP %d"), resp.StatusCode)
	}
	limits, extra := parseClaudeUsage(body)
	if len(limits) == 0 {
		return nil, "", errors.New(T("Anthropic nennt für dieses Konto keine Limits."))
	}
	return limits, extra, nil
}

const (
	sessionWindow = 5 * time.Hour
	weekWindow    = 7 * 24 * time.Hour
)

// parseClaudeUsage reads the answer of the usage endpoint: its list of limits, else the
// older windows (five_hour, seven_day …). Each part is read on its own, so one that changes
// its shape does not take the others with it.
func parseClaudeUsage(body []byte) ([]usageLimit, string) {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return nil, ""
	}
	var limits []usageLimit
	var list []json.RawMessage
	_ = json.Unmarshal(top["limits"], &list)
	for _, raw := range list {
		var l struct {
			Kind     string   `json:"kind"`
			Group    string   `json:"group"`
			Percent  *float64 `json:"percent"`
			Severity string   `json:"severity"`
			ResetsAt string   `json:"resets_at"`
			Scope    struct {
				Model   json.RawMessage `json:"model"`
				Surface json.RawMessage `json:"surface"`
			} `json:"scope"`
		}
		if json.Unmarshal(raw, &l) != nil || l.Percent == nil {
			continue
		}
		lim := usageLimit{Percent: *l.Percent, ResetsAt: parseResetTime(l.ResetsAt), Severity: l.Severity}
		switch l.Group {
		case "session":
			lim.Window = sessionWindow
		case "weekly":
			lim.Window = weekWindow
		}
		switch l.Kind {
		case "session":
			lim.Label = T("Session")
		case "weekly_all":
			lim.Label = T("Woche")
		default:
			lim.Label = humanKind(l.Kind)
			if name := displayName(l.Scope.Model); name != "" {
				lim.Label = T("Woche") + " · " + name
			} else if name := displayName(l.Scope.Surface); name != "" {
				lim.Label = T("Woche") + " · " + name
			}
		}
		limits = append(limits, lim)
	}
	if len(limits) == 0 {
		for _, w := range []struct {
			key, label string
			window     time.Duration
		}{
			{"five_hour", T("Session"), sessionWindow},
			{"seven_day", T("Woche"), weekWindow},
			{"seven_day_opus", T("Woche") + " · Opus", weekWindow},
			{"seven_day_sonnet", T("Woche") + " · Sonnet", weekWindow},
		} {
			var v struct {
				Utilization *float64 `json:"utilization"`
				ResetsAt    string   `json:"resets_at"`
			}
			if json.Unmarshal(top[w.key], &v) != nil || v.Utilization == nil {
				continue
			}
			limits = append(limits, usageLimit{Label: w.label, Percent: *v.Utilization,
				ResetsAt: parseResetTime(v.ResetsAt), Window: w.window})
		}
	}
	return limits, spendText(top["spend"])
}

func parseResetTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// displayName is the display_name of a scope part ({"display_name": "Fable"}), "" otherwise.
func displayName(raw json.RawMessage) string {
	var v struct {
		DisplayName string `json:"display_name"`
	}
	_ = json.Unmarshal(raw, &v)
	return strings.TrimSpace(v.DisplayName)
}

// humanKind turns an unknown kind ("weekly_design") into a label ("Weekly design").
func humanKind(kind string) string {
	s := strings.ReplaceAll(kind, "_", " ")
	if s == "" {
		return T("Limit")
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// spendText says what extra usage, bought beyond the plan, has cost this month.
func spendText(raw json.RawMessage) string {
	type money struct {
		AmountMinor *int64 `json:"amount_minor"`
		Currency    string `json:"currency"`
		Exponent    int    `json:"exponent"`
	}
	var s struct {
		Enabled bool            `json:"enabled"`
		Used    money           `json:"used"`
		Limit   json.RawMessage `json:"limit"`
	}
	if json.Unmarshal(raw, &s) != nil || !s.Enabled || s.Used.AmountMinor == nil {
		return ""
	}
	format := func(m money) string {
		v := float64(*m.AmountMinor) / math.Pow10(m.Exponent)
		n := strconv.FormatFloat(v, 'f', m.Exponent, 64)
		if Language() != "en" {
			n = strings.ReplaceAll(n, ".", ",")
		}
		sym := map[string]string{"EUR": "€", "USD": "$", "GBP": "£"}[m.Currency]
		if sym == "" {
			sym = m.Currency
		}
		return n + " " + sym
	}
	text := fmt.Sprintf(T("Extra-Nutzung: %s"), format(s.Used))
	var limit money
	if json.Unmarshal(s.Limit, &limit) == nil && limit.AmountMinor != nil {
		text += " / " + format(limit)
	}
	return text
}

// planName says Claude Code's subscriptionType as the plan is called: "team" is "Team".
func planName(sub string) string {
	switch sub = strings.TrimSpace(sub); sub {
	case "":
		return ""
	case "max":
		return "Max"
	case "pro":
		return "Pro"
	}
	return strings.ToUpper(sub[:1]) + sub[1:]
}

// pace is how far the limit's period has run (0..1): where its bar would be at an even use.
func (l usageLimit) pace(now time.Time) (float64, bool) {
	if l.Window <= 0 || l.ResetsAt.IsZero() {
		return 0, false
	}
	left := l.ResetsAt.Sub(now)
	if left < 0 || left > l.Window {
		return 0, false
	}
	return 1 - float64(left)/float64(l.Window), true
}

// status is how the limit is doing: "crit" when used up, "warn" from 80 %.
func (l usageLimit) status() string {
	sev := strings.ToLower(l.Severity)
	switch {
	case l.Percent >= 100 || sev == "critical" || sev == "exceeded" || sev == "blocked":
		return "crit"
	case l.Percent >= 80 || (sev != "" && sev != "normal"):
		return "warn"
	}
	return "ok"
}

// usageStatus is the panel's status and menu bar text: the session's and the week's percent.
func usageStatus(u agentUsage) (status, badge string) {
	if len(u.Limits) == 0 {
		if u.NoLogin {
			return "info", T("nicht angemeldet")
		}
		return "info", "Claude …"
	}
	rank := map[string]int{"ok": 0, "warn": 1, "crit": 2}
	status = "ok"
	var parts []string
	for i, l := range u.Limits {
		if s := l.status(); rank[s] > rank[status] {
			status = s
		}
		if i < 2 {
			parts = append(parts, pctText(l.Percent))
		}
	}
	return status, strings.Join(parts, " · ")
}

func pctText(v float64) string {
	if Language() == "en" {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%.0f %%", v)
}

// untilText says how long until a reset: "2 h 27 min", "3 T 15 h".
func untilText(d time.Duration) string {
	if d < time.Minute {
		return T("gleich")
	}
	m := int(d.Round(time.Minute).Minutes())
	days, hours, mins := m/1440, m%1440/60, m%60
	switch {
	case days > 0 && hours == 0:
		return fmt.Sprintf(T("%d T"), days)
	case days > 0:
		return fmt.Sprintf(T("%d T %d h"), days, hours)
	case hours > 0 && mins == 0:
		return fmt.Sprintf(T("%d h"), hours)
	case hours > 0:
		return fmt.Sprintf(T("%d h %d min"), hours, mins)
	}
	return fmt.Sprintf(T("%d min"), mins)
}

// resetText says when a limit starts anew: "Neu ab Do 17:20".
func resetText(t time.Time) string {
	t = t.Local()
	return fmt.Sprintf(T("Neu ab %s"), T(weekdayShort[(int(t.Weekday())+6)%7])+" "+t.Format("15:04"))
}

// claudeAgent is the installed Claude Code; nil when it is not.
func (a *App) claudeAgent() *agentInfo {
	a.agents.mu.Lock()
	defer a.agents.mu.Unlock()
	for i := range a.agents.found {
		if a.agents.found[i].ID == "claude" {
			ag := a.agents.found[i]
			return &ag
		}
	}
	return nil
}

// claudeOrange is Claude's color, for its mark in the module.
var claudeOrange = ui.Hex("#D97757")

// renderAgentUsageModule draws the limits: per limit a bar, where an even use would be,
// the percent used and the time until it starts anew.
func (a *App) renderAgentUsageModule(c *ui.Context, p *PanelSpec, onClose func()) {
	t := c.Theme()
	u := a.claudeUsage()
	now := time.Now()
	ui.Column(c).Fill().Padding(14).Gap(10).Children(func() {
		a.panelHeader(ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8), p).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, p.Icon+" "+T(p.Title)).FontSize(14).Bold().SingleLine()
				line := T("Abo-Limits")
				if !u.At.IsZero() {
					line += " · " + T("Stand ") + u.At.Format("15:04")
				}
				ui.Text(c, line).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
			if ui.Button(c.Key("usage-ref-"+p.ID), "↻").Tooltip(T("Jetzt aktualisieren")).Clicked() {
				a.refreshPanelAsync(p, "")
			}
			closeButton(c, "usage-close-"+p.ID, onClose)
		})
		ui.Row(c).FillWidth().Height(1).Background(t.Border)

		claude := a.claudeAgent()
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "✳").FontSize(16).Bold().TextColor(claudeOrange)
			ui.Text(c, "Claude Code").FontSize(13.5).Bold().SingleLine()
			if u.Plan != "" {
				ui.Text(c, u.Plan).FontSize(12).TextColor(t.TextMuted).SingleLine()
			}
			ui.Row(c).Grow(1)
			if claude != nil && ui.Button(c.Key("usage-term-"+p.ID), ">_").Tooltip(T("Claude Code im Terminal öffnen")).Clicked() {
				a.agentInTerminal(*claude, nil, nil)
			}
		})

		for i, l := range u.Limits {
			bar, fg := t.Accent, t.Text // colored only when it runs short
			if l.status() != "ok" {
				_, bar = statusColors(t, l.status())
				fg = bar
			}
			ui.Row(c.Key(fmt.Sprintf("usage-%s-%d", p.ID, i))).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, l.Label).FontSize(12).Width(88).SingleLine()
				usageBar(c, l, now, bar)
				ui.Text(c, pctText(l.Percent)).FontSize(12).Bold().TextColor(fg).Width(38).TextAlign(ui.End).SingleLine()
				if l.ResetsAt.IsZero() {
					ui.Row(c).Width(62)
					return
				}
				ui.Text(c, untilText(l.ResetsAt.Sub(now))).FontSize(12).TextColor(t.TextMuted).Width(62).
					TextAlign(ui.End).SingleLine().Tooltip(resetText(l.ResetsAt))
			})
		}
		if u.Extra != "" {
			ui.Text(c, u.Extra).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}

		switch {
		case !u.Loaded:
			ui.Text(c, T("Lädt …")).FontSize(11.5).TextColor(t.TextMuted)
		case claude == nil && u.NoLogin && a.agents.isDetected():
			ui.Text(c, T("Claude Code ist nicht installiert (brew install --cask claude-code).")).FontSize(11.5).TextColor(t.TextMuted).FillWidth()
		case u.NoLogin:
			ui.Text(c, u.Problem+" "+T("Starte Claude Code und melde dich mit /login an.")).FontSize(11.5).TextColor(t.TextMuted).FillWidth()
		case u.Problem != "":
			ui.Text(c, "⚠️ "+u.Problem).FontSize(11.5).TextColor(t.Warning).FillWidth()
		}
	})
}

// usageBar is a limit's bar; a mark shows how far its period has run.
func usageBar(c *ui.Context, l usageLimit, now time.Time, fill ui.Color) {
	t := c.Theme()
	used := math.Max(0, math.Min(1, l.Percent/100))
	pace, hasPace := l.pace(now)
	segment := func(from, to float64) { // the bar from..to (0..1), filled up to used
		if to <= from {
			return
		}
		filled := math.Max(from, math.Min(used, to)) - from
		ui.Row(c).Grow(float32(to - from)).Height(6).Radius(3).Background(t.Border).Clip().Children(func() {
			ui.Row(c).Grow(float32(filled)).FillHeight().Background(fill)
			ui.Row(c).Grow(float32(to - from - filled)).FillHeight()
		})
	}
	ui.Row(c).Grow(1).MinWidth(40).Height(12).AlignItems(ui.Center).Children(func() {
		if !hasPace {
			segment(0, 1)
			return
		}
		segment(0, pace)
		ui.Row(c).Width(2).Height(12).Radius(1).Background(t.Text).
			Tooltip(fmt.Sprintf(T("Bei gleichmäßiger Nutzung wären jetzt %s verbraucht"), pctText(pace*100)))
		segment(pace, 1)
	})
}
