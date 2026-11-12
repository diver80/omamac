package omamac

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The answer of the usage endpoint as it came for a Team plan (many null fields left out).
const usageAnswer = `{"five_hour":{"utilization":31.0,"resets_at":"2026-11-12T16:20:00.272403+00:00"},
"seven_day":{"utilization":59.0,"resets_at":"2026-11-16T05:00:00.272429+00:00"},
"seven_day_opus":null,"seven_day_sonnet":null,"pace":null,
"extra_usage":{"is_enabled":false,"monthly_limit":null,"used_credits":null,"currency":"EUR"},
"limits":[
 {"kind":"session","group":"session","percent":31,"severity":"normal","resets_at":"2026-11-12T16:20:00.272403+00:00","scope":null,"is_active":false},
 {"kind":"weekly_all","group":"weekly","percent":59,"severity":"normal","resets_at":"2026-11-16T05:00:00.272429+00:00","scope":null,"is_active":true},
 {"kind":"weekly_scoped","group":"weekly","percent":37,"severity":"normal","resets_at":"2026-11-16T05:00:00.272920+00:00","scope":{"model":{"id":null,"display_name":"Fable"},"surface":null},"is_active":false},
 {"kind":"broken","group":"weekly","percent":"n/a"}],
"spend":{"used":{"amount_minor":0,"currency":"EUR","exponent":2},"limit":null,"percent":0,"enabled":false}}`

func TestParseClaudeUsage(t *testing.T) {
	limits, extra := parseClaudeUsage([]byte(usageAnswer))
	if len(limits) != 3 || extra != "" {
		t.Fatalf("limits %+v extra %q", limits, extra)
	}
	want := []struct {
		label   string
		percent float64
		window  time.Duration
	}{{"Session", 31, sessionWindow}, {"Woche", 59, weekWindow}, {"Woche · Fable", 37, weekWindow}}
	for i, w := range want {
		l := limits[i]
		if l.Label != w.label || l.Percent != w.percent || l.Window != w.window || l.ResetsAt.IsZero() {
			t.Errorf("limit %d = %+v, want %+v", i, l, w)
		}
	}

	// Without the list: the older windows
	limits, _ = parseClaudeUsage([]byte(`{"five_hour":{"utilization":12,"resets_at":"2026-11-12T16:20:00Z"},
		"seven_day":{"utilization":40,"resets_at":null},"seven_day_opus":{"utilization":5,"resets_at":"2026-11-16T05:00:00Z"},"seven_day_sonnet":null}`))
	if len(limits) != 3 || limits[0].Label != "Session" || limits[1].Percent != 40 || !limits[1].ResetsAt.IsZero() || limits[2].Label != "Woche · Opus" {
		t.Fatalf("older windows: %+v", limits)
	}

	// Extra usage, when it is on
	_, extra = parseClaudeUsage([]byte(`{"limits":[],"spend":{"enabled":true,"used":{"amount_minor":1230,"currency":"EUR","exponent":2},"limit":{"amount_minor":5000,"currency":"EUR","exponent":2}}}`))
	if extra != "Extra-Nutzung: 12,30 € / 50,00 €" {
		t.Fatalf("extra = %q", extra)
	}
	if l, _ := parseClaudeUsage([]byte(`not json`)); l != nil {
		t.Fatal("garbage gave limits")
	}
}

func TestParseClaudeLogin(t *testing.T) {
	plain := `{"claudeAiOauth":{"accessToken":"tok","refreshToken":"r","expiresAt":1767225600000,"subscriptionType":"team"}}`
	for _, raw := range []string{plain, plain + "\n", hex.EncodeToString([]byte(plain))} {
		l, err := parseClaudeLogin([]byte(raw))
		if err != nil || l.AccessToken != "tok" || l.SubscriptionType != "team" || l.ExpiresAt != 1767225600000 {
			t.Fatalf("login from %.20q: %+v %v", raw, l, err)
		}
	}
	for _, raw := range []string{"", "{}", `{"claudeAiOauth":{"accessToken":""}}`, "zz"} {
		if _, err := parseClaudeLogin([]byte(raw)); err == nil {
			t.Errorf("%q gave a login", raw)
		}
	}
	if planName("team") != "Team" || planName("max") != "Max" || planName("") != "" {
		t.Error("plan names")
	}
}

func TestFetchClaudeUsage(t *testing.T) {
	status := http.StatusOK
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(usageAnswer))
	}))
	defer srv.Close()
	oldURL, oldLogin := claudeUsageURL, readClaudeLogin
	defer func() { claudeUsageURL, readClaudeLogin = oldURL, oldLogin }()
	claudeUsageURL = srv.URL
	login := &claudeLogin{AccessToken: "tok", SubscriptionType: "team", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	readClaudeLogin = func(context.Context) (*claudeLogin, error) { return login, nil }
	ctx := context.Background()

	u := fetchClaudeUsage(ctx, agentUsage{})
	if u.Problem != "" || u.Plan != "Team" || len(u.Limits) != 3 || u.At.IsZero() || !u.Loaded {
		t.Fatalf("usage = %+v", u)
	}
	if st, badge := usageStatus(u); st != "ok" || badge != "31 % · 59 %" {
		t.Fatalf("status %q badge %q", st, badge)
	}

	// A failed refresh keeps the last numbers and says why
	status = http.StatusUnauthorized
	again := fetchClaudeUsage(ctx, u)
	if len(again.Limits) != 3 || !strings.Contains(again.Problem, "neu anmelden") {
		t.Fatalf("after 401: %+v", again)
	}
	status = http.StatusTooManyRequests
	if again = fetchClaudeUsage(ctx, u); !strings.Contains(again.Problem, "bremst") || len(again.Limits) != 3 {
		t.Fatalf("after 429: %+v", again)
	}

	// An expired login is not sent
	n := requests
	login.ExpiresAt = time.Now().Add(-time.Minute).UnixMilli()
	if again = fetchClaudeUsage(ctx, u); requests != n || !strings.Contains(again.Problem, "abgelaufen") || len(again.Limits) != 3 {
		t.Fatalf("expired login: %d requests, %+v", requests-n, again)
	}

	// Not logged in: no numbers
	readClaudeLogin = func(context.Context) (*claudeLogin, error) { return nil, errNoClaudeLogin }
	if again = fetchClaudeUsage(ctx, u); !again.NoLogin || len(again.Limits) != 0 {
		t.Fatalf("no login: %+v", again)
	}
	if st, badge := usageStatus(again); st != "info" || badge != "nicht angemeldet" {
		t.Fatalf("no login: %q %q", st, badge)
	}
}

func TestUsagePaceAndTexts(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	week := usageLimit{Percent: 59, ResetsAt: now.Add(84 * time.Hour), Window: weekWindow}
	if p, ok := week.pace(now); !ok || p != 0.5 {
		t.Fatalf("pace = %v %v", p, ok)
	}
	if _, ok := (usageLimit{Percent: 5}).pace(now); ok {
		t.Fatal("pace without a reset time")
	}
	if _, ok := (usageLimit{Window: sessionWindow, ResetsAt: now.Add(6 * time.Hour)}).pace(now); ok {
		t.Fatal("pace for a reset beyond the window")
	}
	for d, want := range map[time.Duration]string{
		30 * time.Second:                              "gleich",
		45 * time.Minute:                              "45 min",
		2*time.Hour + 27*time.Minute:                  "2 h 27 min",
		87*time.Hour + 10*time.Minute:                 "3 T 15 h",
		72 * time.Hour:                                "3 T",
		2*time.Hour + 59*time.Minute + 50*time.Second: "3 h",
	} {
		if got := untilText(d); got != want {
			t.Errorf("untilText(%v) = %q, want %q", d, got, want)
		}
	}
	for _, c := range []struct {
		l    usageLimit
		want string
	}{
		{usageLimit{Percent: 79}, "ok"},
		{usageLimit{Percent: 80}, "warn"},
		{usageLimit{Percent: 20, Severity: "warning"}, "warn"},
		{usageLimit{Percent: 100}, "crit"},
	} {
		if got := c.l.status(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.l, got, c.want)
		}
	}
	u := agentUsage{Limits: []usageLimit{{Percent: 12}, {Percent: 85}, {Percent: 100}}}
	if st, badge := usageStatus(u); st != "crit" || badge != "12 % · 85 %" {
		t.Fatalf("status %q badge %q", st, badge)
	}
}

// The module draws every state: loading, numbers, a problem, no login.
func TestAgentUsageModuleRenders(t *testing.T) {
	app := newTestApp(t)
	p := app.findPanel("00-agent-usage")
	if p == nil || p.BuiltinKind != BuiltinAgentUsage || tileSize(p).ID != "small" {
		t.Fatalf("system panel: %+v", p)
	}
	limits, _ := parseClaudeUsage([]byte(usageAnswer))
	app.agents.found = []agentInfo{{agentDefs[0], "/bin/claude"}, {agentDefs[3], "/bin/agy"}}
	agy, _ := parseAgyUsage([]byte(agyUsageAnswer))
	app.setAgyUsage(agentUsage{Loaded: true, Limits: agy, Extra: "AI-Credits: 17", At: time.Now()})
	for _, u := range []agentUsage{
		{},
		{Loaded: true, Plan: "Team", Limits: limits, At: time.Now()},
		{Loaded: true, Plan: "Team", Limits: limits, At: time.Now(), Problem: "Anthropic bremst"},
		{Loaded: true, NoLogin: true, Problem: "nicht angemeldet"},
	} {
		app.setClaudeUsage(u)
		ui.Render(func(c *ui.Context) { app.renderNativePanelPopover(c, p, nil, func() {}) }, 420, 250, 1)
	}
}

// What agy -p "/usage" --output-format json printed (descriptions shortened).
const agyUsageAnswer = `{"conversation_id":"","status":"SUCCESS","response":"Gemini Models\tWeekly Limit Remaining\t98%\n","duration_seconds":0,"num_turns":0,
"command":{"name":"usage","data":{"description":"Within each group, models share a weekly limit and a 5-hour limit.","groups":[
 {"name":"Gemini Models","description":"Gemini Flash, Gemini Pro","buckets":[
  {"id":"gemini-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":0.9760345220565796,"reset_time":"2026-11-18T08:17:26Z"},
  {"id":"gemini-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":1,"reset_time":"2026-11-12T19:08:11Z"}]},
 {"name":"Claude and GPT models","buckets":[
  {"id":"3p-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":0.980131983757019,"reset_time":"2026-11-16T05:59:41Z"},
  {"id":"3p-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":1,"reset_time":"2026-11-12T19:11:55Z"}]},
 {"name":"Gemini 4 Argon","buckets":[
  {"id":"gemini-argon-wtu-weekly","name":"Gemini 4 Argon Weekly Limit","window":"weekly","remaining_fraction":0,"reset_time":"2026-11-14T04:49:13Z"}]}]}}}`

func TestParseAgyUsage(t *testing.T) {
	limits, err := parseAgyUsage([]byte(agyUsageAnswer))
	if err != nil || len(limits) != 5 {
		t.Fatalf("%v %+v", err, limits)
	}
	want := []struct {
		group, label string
		percent      float64
	}{{"Gemini", "Session", 0}, {"Gemini", "Woche", 2.4}, {"Claude & GPT", "Session", 0}, {"Claude & GPT", "Woche", 2}, {"Gemini 4 Argon", "Woche", 100}}
	for i, w := range want {
		if l := limits[i]; l.Group != w.group || l.Label != w.label || l.Percent != w.percent || l.ResetsAt.IsZero() || l.Window == 0 {
			t.Errorf("limit %d = %+v, want %+v", i, l, w)
		}
	}
	if limits[4].status() != "crit" {
		t.Error("a used-up limit is not critical")
	}
	if n, ok := parseAgyCredits([]byte(`{"status":"SUCCESS","command":{"name":"credits","data":{"remaining_credits":17,"upgrade_uri":"x"}}}`)); !ok || n != 17 {
		t.Fatalf("credits %d %v", n, ok)
	}
	for _, bad := range []string{"", "Please log in", `{"status":"ERROR","response":"not logged in"}`, `{"status":"SUCCESS","command":{"data":{"groups":[]}}}`} {
		if _, err := parseAgyUsage([]byte(bad)); err == nil {
			t.Errorf("%q gave limits", bad)
		}
	}
}

func TestFetchAgyUsage(t *testing.T) {
	old := runAgy
	defer func() { runAgy = old }()
	var commands []string
	fail := false
	runAgy = func(_ context.Context, ag agentInfo, dir, command string) ([]byte, error) {
		commands = append(commands, command)
		if fail {
			return nil, context.DeadlineExceeded
		}
		if command == "/credits" {
			return []byte(`{"status":"SUCCESS","command":{"data":{"remaining_credits":17}}}`), nil
		}
		return []byte(agyUsageAnswer), nil
	}
	ag := agentInfo{agentDefs[3], "/bin/agy"}
	u := fetchAgyUsage(context.Background(), ag, t.TempDir(), agentUsage{})
	if len(u.Limits) != 5 || u.Extra != "AI-Credits: 17" || u.Problem != "" || strings.Join(commands, " ") != "/usage /credits" {
		t.Fatalf("usage %+v after %v", u, commands)
	}
	fail = true
	if again := fetchAgyUsage(context.Background(), ag, t.TempDir(), u); len(again.Limits) != 5 || again.Problem == "" {
		t.Fatalf("a failed refresh: %+v", again)
	}

	// The menu bar shows the first agent with numbers; the status is the worst limit's
	if st, badge := usageStatus(agentUsage{NoLogin: true, Loaded: true}, u); st != "crit" || badge != "0 % · 2 %" {
		t.Fatalf("status %q badge %q", st, badge)
	}
	if st, badge := usageStatus(agentUsage{NoLogin: true}); st != "info" || badge != "nicht angemeldet" {
		t.Fatalf("status %q badge %q", st, badge)
	}
}
