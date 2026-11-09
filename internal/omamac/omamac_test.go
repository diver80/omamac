package omamac

import (
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestConfigAndAntigravityDetection(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp) // never import the real ~/.jira-quick-access instances in tests
	cfgPath := filepath.Join(tmp, "config.json")

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.ActiveProvider != ProviderGemini {
		t.Fatalf("expected default provider gemini, got %s", cfg.ActiveProvider)
	}

	st := InspectAntigravityAuth(cfg)
	if st.StatusSummary == "" {
		t.Fatalf("expected non-empty Antigravity status summary")
	}
}

// offlineGeminiConfig selects Gemini with an API key that is missing.
func offlineGeminiConfig(t *testing.T) *Config {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	cfg.ActiveProvider = ProviderGemini
	cfg.GeminiAuthMode = GeminiAuthAPIKey
	cfg.GeminiAPIKey = ""
	cfg.AntigravityTokenPath = filepath.Join(tmp, "nonexistent-token")
	return cfg
}

// A failing AI must not produce placeholder panels or silently rewrite a panel.
func TestAIFailureCreatesAndChangesNothing(t *testing.T) {
	ai := NewAIEngine(offlineGeminiConfig(t))

	panels, _, err := ai.GeneratePanels(context.Background(), "Jira audit log", "native", 1)
	if err == nil || len(panels) != 0 {
		t.Fatalf("GeneratePanels offline = %d panels, err %v; want 0 panels and an error", len(panels), err)
	}

	existing := StarterPanels()[0]
	before, _ := json.Marshal(existing)
	refined, _, err := ai.RefinePanel(context.Background(), existing, "Färbe den Status rot")
	if err == nil || refined != nil {
		t.Fatalf("RefinePanel offline = %v, err %v; want nil and an error", refined, err)
	}
	if after, _ := json.Marshal(existing); string(after) != string(before) {
		t.Fatalf("RefinePanel changed the existing panel although the AI failed")
	}
}

// rewriteTransport sends every request to target (a test server).
type rewriteTransport struct{ target *url.URL }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = rt.target.Scheme, rt.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

func TestTwoPromptWorkflowWithAI(t *testing.T) {
	var reply string
	var gotKeyHeader, gotKeyQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeyHeader, gotKeyQuery = r.Header.Get("x-goog-api-key"), r.URL.Query().Get("key")
		body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": reply}}}}}})
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	target, _ := url.Parse(srv.URL)

	cfg := offlineGeminiConfig(t)
	cfg.GeminiAPIKey = "test-key"
	ai := NewAIEngine(cfg)
	ai.HTTPClient = &http.Client{Transport: rewriteTransport{target}}

	// Prompt #1: two panels, "both" → native + html
	reply = "```json\n[{\"title\":\"Docker\",\"status\":\"ok\"},{\"title\":\"Battery\",\"status\":\"ok\"}]\n```"
	panels, _, err := ai.GeneratePanels(context.Background(), "Docker; Battery", "both", 2)
	if err != nil || len(panels) != 2 {
		t.Fatalf("GeneratePanels = %d panels, err %v", len(panels), err)
	}
	if gotKeyHeader != "test-key" || gotKeyQuery != "" {
		t.Fatalf("API key must be sent as header only (header %q, query %q)", gotKeyHeader, gotKeyQuery)
	}
	if panels[0].Type != PanelTypeNative || panels[1].Type != PanelTypeHTML || panels[1].HTML == "" {
		t.Fatalf("types = %s, %s (html %d bytes)", panels[0].Type, panels[1].Type, len(panels[1].HTML))
	}

	// Prompt #2: refine keeps the ID and records the step
	reply = `{"title":"Docker","status":"crit"}`
	refined, _, err := ai.RefinePanel(context.Background(), panels[0], "Status rot bei Alarm")
	if err != nil {
		t.Fatalf("RefinePanel: %v", err)
	}
	if refined.ID != panels[0].ID || refined.Status != "crit" || len(refined.PromptHistory) != 2 {
		t.Fatalf("refined = id %q status %q steps %d", refined.ID, refined.Status, len(refined.PromptHistory))
	}

	// A reply without panel JSON is an error, not a placeholder panel.
	reply = "Usage Limit Reached"
	if panels, _, err := ai.GeneratePanels(context.Background(), "x", "native", 1); err == nil || len(panels) != 0 {
		t.Fatalf("non-JSON reply = %d panels, err %v; want an error", len(panels), err)
	}
}

func TestUIRenderAllViews(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp) // never import the real ~/.jira-quick-access instances in tests
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	_ = SaveConfig(cfgPath, cfg)

	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}

	if app.activeTab != TabOverview {
		t.Fatalf("expected default tab to be TabOverview, got %d", app.activeTab)
	}

	// Render the main window's three views
	for _, tab := range []StudioTab{TabOverview, TabStore, TabSettings} {
		app.activeTab = tab
		img := ui.Render(app.RenderStudioView, 1280, 800, 1.0)
		if img == nil || img.Bounds().Dx() != 1280 {
			t.Fatalf("failed to render studio tab %d", tab)
		}
	}

	// Render the launcher empty and with a query
	for _, q := range []string{"", "te", "zzzz"} {
		app.launcherQuery = q
		img := ui.Render(app.RenderWalPanelView, 680, 480, 1.0)
		if img == nil || img.Bounds().Dx() != 680 {
			t.Fatalf("failed to render the launcher for %q", q)
		}
	}
}

func TestVaultZeroLeak(t *testing.T) {
	tmp := t.TempDir()
	v, err := LoadVault(filepath.Join(tmp, "vault.json"))
	if err != nil {
		t.Fatalf("LoadVault failed: %v", err)
	}
	if err := v.Set("idealo_api_secret", "super-secret-value-999", "Idealo Partner API Secret"); err != nil {
		t.Fatalf("Vault.Set failed: %v", err)
	}
	if got := v.Get("IDEALO_API_SECRET"); got != "super-secret-value-999" {
		t.Fatalf("expected secret value, got %q", got)
	}
	cat := v.AIPromptCatalog()
	if filepath.Base(cat) == "super-secret-value-999" {
		t.Fatalf("catalog leaked secret value")
	}
}

// TestGenerateScreenshots renders every view. The PNGs go to a temp dir unless
// OMAMAC_SCREENSHOTS=1 is set, which refreshes docs/omamac-screenshots, or
// OMAMAC_SCREENSHOT_DIR names another directory:
//
//	OMAMAC_SCREENSHOTS=1 go test ./internal/omamac -run TestGenerateScreenshots
func TestGenerateScreenshots(t *testing.T) {
	outDir := t.TempDir()
	if dir := os.Getenv("OMAMAC_SCREENSHOT_DIR"); dir != "" {
		outDir = dir
	} else if os.Getenv("OMAMAC_SCREENSHOTS") != "" {
		outDir = filepath.Join("..", "..", "docs", "omamac-screenshots")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp) // never import the real ~/.jira-quick-access instances in tests
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	_ = SaveConfig(cfgPath, cfg)

	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}
	// Show 3 active and 1 disabled panel in the overview so the ON/OFF switch states are clearly visible
	if len(app.panels) >= 3 {
		app.panels[2].Enabled = false
	}
	// The same apps on every machine, not the ones installed here
	app.apps = nil
	for _, name := range []string{"Safari", "Terminal", "TextEdit", "Notes", "Keynote", "Microsoft Teams", "Calendar"} {
		app.apps = append(app.apps, QuickCommand{ID: "app-" + Slugify(name), Title: name, Subtitle: "/Applications/" + name + ".app"})
	}
	sort.Slice(app.apps, func(i, j int) bool { return app.apps[i].Title < app.apps[j].Title })
	app.selectPanel("04-mydealz-3d")
	app.prompt1Text = "Create a status panel listing current 3D-Drucker deals from mydealz.de/gruppe/3d-drucker and monitoring Idealo target price ($IDEALO_TARGET_PRICE)"
	app.prompt1TypeIdx = 0 // Native GPU
	app.prompt1CountIdx = 0
	app.prompt2Text = "Highlight Bambu Lab deals in green and alert red when Idealo price drops below $IDEALO_TARGET_PRICE"

	savePNG := func(name string, view func(*ui.Context), w, h int) {
		img := ui.Render(view, w, h, 2.0)
		f, err := os.Create(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatalf("encode %s: %v", name, err)
		}
	}

	app.showHelpModal = false
	app.activeTab = TabOverview
	savePNG("01-panels.png", app.RenderStudioView, 1320, 820)

	app.showCreate = true
	savePNG("02-neues-panel.png", app.RenderStudioView, 1320, 820)
	app.showCreate = false

	app.activeTab = TabStore
	savePNG("03-vorlagen.png", app.RenderStudioView, 1320, 760)

	app.activeTab = TabSettings
	savePNG("04-einstellungen.png", app.RenderStudioView, 1320, 880)

	app.activeTab = TabOverview
	app.showHelpModal = true
	savePNG("00-kurzanleitung.png", app.RenderStudioView, 1280, 760)
	app.showHelpModal = false

	// Launcher (⌥ Space): grouped while empty, ranked matches while typing
	app.launcherQuery = ""
	savePNG("05-schnellstart.png", app.RenderWalPanelView, 680, 480)
	app.launcherQuery = "te"
	app.launchSel = 0
	savePNG("06-schnellstart-suche.png", app.RenderWalPanelView, 680, 480)
}

// A CLI that prints its error to stdout and exits non-zero ("Usage Limit Reached")
// must count as a failure, not as the AI's answer.
func TestClaudeCLIFailureIsAnError(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'Usage Limit Reached'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := offlineGeminiConfig(t)
	cfg.ClaudeCLIPath = bin
	_, err := NewAIEngine(cfg).runClaudeCLI(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "Usage Limit Reached") {
		t.Fatalf("err = %v; want an error that names the CLI output", err)
	}
}

// A hanging CLI must end in a readable timeout, not "signal: killed" plus unrelated stderr.
func TestCLITimeoutIsNamed(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'Warning: sign-in ends soon' >&2\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := cliTimeout
	cliTimeout = 200 * time.Millisecond
	defer func() { cliTimeout = orig }()
	cfg := offlineGeminiConfig(t)
	cfg.ClaudeCLIPath = bin
	_, err := NewAIEngine(cfg).runClaudeCLI(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "Zeitüberschreitung") || strings.Contains(err.Error(), "Warning") {
		t.Fatalf("err = %v; want a plain timeout error", err)
	}
}

func TestClaudeCLIArgs(t *testing.T) {
	got := strings.Join(claudeCLIArgs("claude-sonnet-5-5-medium", "PROMPT"), " ")
	for _, want := range []string{"--tools  ", "--strict-mcp-config", "--model sonnet", "--effort medium", "-p PROMPT"} {
		if !strings.Contains(got, want) {
			t.Fatalf("args %q lack %q", got, want)
		}
	}
	if !strings.HasSuffix(got, "-p PROMPT") {
		t.Fatalf("prompt must be the value of -p: %q", got)
	}
}

// The launcher is used from the keyboard: typing filters, Enter runs the chosen row.
func TestLauncherKeyboard(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	_ = SaveConfig(cfgPath, cfg)
	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	app.apps = nil // no installed apps in the results
	atl := app.findPanel("00-atlassian-status")
	if atl == nil {
		t.Fatalf("no Atlassian Status panel")
	}
	before := atl.Enabled

	tt := ui.NewTester(app.RenderWalPanelView, 680, 480)
	tt.Type("atlassian")
	items := app.launcherItems(app.launcherQuery)
	if len(items) < 2 || items[0].panel != atl || items[len(items)-1].kind != launchAI {
		t.Fatalf("results for %q: first %q, %d items", app.launcherQuery, items[0].title, len(items))
	}
	tt.Key(0, ui.KeyEnter)
	if atl.Enabled == before {
		t.Fatalf("Enter did not switch the panel's menu bar entry")
	}

	tt.Key(0, ui.KeyDown)
	if app.launchSel != 1 {
		t.Fatalf("Down chose row %d, want 1", app.launchSel)
	}
	tt.Type("x") // a new query chooses the first row again
	if app.launchSel != 0 {
		t.Fatalf("typing kept row %d chosen", app.launchSel)
	}
}

func TestMatchScore(t *testing.T) {
	for _, tc := range []struct {
		title, q string
		want     int
	}{
		{"terminal", "ter", 0},
		{"atlassian status", "sta", 1},
		{"textedit", "edit", 2},
		{"visual studio code", "vsc", 3},
		{"safari", "xyz", -1},
		{"zentriert 80 %", "zt", -1}, // two letters: no letters-in-order match
	} {
		if got := matchScore(tc.title, tc.q); got != tc.want {
			t.Errorf("matchScore(%q, %q) = %d, want %d", tc.title, tc.q, got, tc.want)
		}
	}
}

// agy's -p takes the prompt as its value, so the prompt must follow it directly.
func TestAntigravityArgs(t *testing.T) {
	got := strings.Join(antigravityArgs("gemini-4-argon-medium", "Hallo Welt"), "|")
	want := "--disable-slash-commands|--model|gemini-4-argon-medium|-p|Hallo Welt"
	if got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

// A panel is deleted with ⌘⌫ or the visible button, always after a confirmation;
// system modules cannot be deleted.
func TestDeletePanelFlow(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	cfg.OnboardingDismissed = true
	_ = SaveConfig(cfgPath, cfg)
	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	const id = "04-mydealz-3d"
	app.selectPanel(id)
	tt := ui.NewTester(app.RenderStudioView, 1320, 820)

	tt.Key(ui.Cmd, ui.KeyBackspace)
	if !app.confirmDelete || app.confirmDeleteID != id {
		t.Fatalf("⌘⌫ did not ask to delete %s", id)
	}
	if err := tt.Click("Löschen"); err != nil {
		t.Fatalf("confirm: %v (texts %q)", err, tt.Texts())
	}
	if app.findPanel(id) != nil {
		t.Fatalf("panel %s still there after confirming", id)
	}
	if _, err := os.Stat(filepath.Join(cfg.PanelsDir, id+".json")); !os.IsNotExist(err) {
		t.Fatalf("panel file still on disk: %v", err)
	}

	// The visible button asks too
	app.selectPanel("03-git-radar")
	tt.Frame()
	if err := tt.Click("Panel löschen …"); err != nil {
		t.Fatal(err)
	}
	if !app.confirmDelete || app.confirmDeleteID != "03-git-radar" {
		t.Fatalf("the button did not ask to delete")
	}
	tt.Key(0, ui.KeyEscape) // cancel
	if app.findPanel("03-git-radar") == nil {
		t.Fatalf("cancelling deleted the panel")
	}

	// System modules stay
	app.selectPanel("00-atlassian-status")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyBackspace)
	if app.confirmDelete {
		t.Fatalf("⌘⌫ offered to delete a system module")
	}
}
