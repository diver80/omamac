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
	"strings"
	"testing"

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

	// Render Studio across all 5 tabs
	for _, tab := range []StudioTab{TabOverview, TabStore, TabPromptStudio, TabWindowManager, TabSettings} {
		app.activeTab = tab
		img := ui.Render(app.RenderStudioView, 1280, 800, 1.0)
		if img == nil || img.Bounds().Dx() != 1280 {
			t.Fatalf("failed to render studio tab %d", tab)
		}
	}

	// Render floating Wal-Panel in all 3 modes (Overview+AI, Store, Launcher)
	for _, mode := range []int{0, 1, 2} {
		app.walMode = mode
		img := ui.Render(app.RenderWalPanelView, 820, 580, 1.0)
		if img == nil || img.Bounds().Dx() != 820 {
			t.Fatalf("failed to render Wal-Panel mode %d", mode)
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

	// Default Main View: Übersicht & Schalter (with ON/OFF switches + Live Preview)
	app.showHelpModal = false
	app.activeTab = TabOverview
	savePNG("01-overview-switches.png", app.RenderStudioView, 1320, 820)
	savePNG("02-panels-manager.png", app.RenderStudioView, 1320, 820)

	// Store View (with direct ON/OFF switches)
	app.activeTab = TabStore
	savePNG("02b-store-switches.png", app.RenderStudioView, 1320, 760)

	// KI-Studio View
	app.activeTab = TabPromptStudio
	savePNG("01-studio-2prompt.png", app.RenderStudioView, 1320, 820)

	// Settings & Keychain Vault View
	app.activeTab = TabSettings
	savePNG("03-settings-gemini-claude.png", app.RenderStudioView, 1320, 880)

	// Kurzanleitung Modal
	app.showHelpModal = true
	savePNG("00-first-start-help.png", app.RenderStudioView, 1280, 760)
	app.showHelpModal = false

	// Floating Wal-Panel (Alt+Space) with Quick Overview Switches
	app.walMode = 0
	savePNG("04-wal-panel-ai.png", app.RenderWalPanelView, 820, 580)

	app.walMode = 1
	savePNG("06-wal-panel-store.png", app.RenderWalPanelView, 820, 580)

	app.walMode = 2
	savePNG("05-wal-panel-launcher.png", app.RenderWalPanelView, 820, 580)
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
