package omamac

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConfigAndAntigravityDetection(t *testing.T) {
	tmp := t.TempDir()
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

func TestTwoPromptMultiPanelWorkflow(t *testing.T) {
	tmp := t.TempDir()
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	// Force offline fallback synthesizer so unit test runs deterministically in <50ms
	cfg.ActiveProvider = ProviderGemini
	cfg.GeminiAuthMode = GeminiAuthAPIKey
	cfg.GeminiAPIKey = ""
	cfg.AntigravityTokenPath = filepath.Join(tmp, "nonexistent-token")

	ai := NewAIEngine(cfg)

	// Prompt #1: Create 3 panels of "both" types (Native + HTML)
	panels, _, err := ai.GeneratePanels(context.Background(), "Docker containers; Battery health; Jira sprint queue", "both", 3)
	if err != nil {
		t.Fatalf("GeneratePanels failed: %v", err)
	}
	if len(panels) != 3 {
		t.Fatalf("expected 3 panels created, got %d", len(panels))
	}
	if panels[0].Type != PanelTypeNative {
		t.Errorf("expected panel[0] to be native, got %s", panels[0].Type)
	}
	if panels[1].Type != PanelTypeHTML || panels[1].HTML == "" {
		t.Errorf("expected panel[1] to be html with non-empty HTML, got type=%s", panels[1].Type)
	}

	// Prompt #2: Refine panel[0] with warning threshold and action button
	refined, _, err := ai.RefinePanel(context.Background(), panels[0], "Färbe den Status rot bei Alarm und füge einen Action Button hinzu")
	if err != nil {
		t.Fatalf("RefinePanel failed: %v", err)
	}
	if refined.Status != "crit" {
		t.Errorf("expected refined status 'crit', got %q", refined.Status)
	}
	if len(refined.PromptHistory) != 2 {
		t.Errorf("expected 2 prompt steps in history, got %d", len(refined.PromptHistory))
	}
}

func TestUIRenderAllViews(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	_ = SaveConfig(cfgPath, cfg)

	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
	}

	// Render Studio across all 4 tabs
	for _, tab := range []StudioTab{TabPromptStudio, TabPanelsManager, TabWindowManager, TabSettings} {
		app.activeTab = tab
		img := ui.Render(app.RenderStudioView, 1280, 800, 1.0)
		if img == nil || img.Bounds().Dx() != 1280 {
			t.Fatalf("failed to render studio tab %d", tab)
		}
	}

	// Render floating Wal-Panel in both AI Prompt Mode and Launcher Mode
	for _, mode := range []int{0, 1} {
		app.walMode = mode
		img := ui.Render(app.RenderWalPanelView, 760, 520, 1.0)
		if img == nil || img.Bounds().Dx() != 760 {
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

func TestGenerateScreenshots(t *testing.T) {
	outDir := "/Users/fhess/data/avono/dev/omarc/docs/omamac-screenshots"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	_ = SaveConfig(cfgPath, cfg)

	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatalf("NewApp failed: %v", err)
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

	// First-Start Onboarding & 0-Token Help Dialog
	app.showHelpModal = true
	savePNG("00-first-start-help.png", app.RenderStudioView, 1280, 760)

	app.showHelpModal = false
	app.activeTab = TabPromptStudio
	savePNG("01-studio-2prompt.png", app.RenderStudioView, 1280, 800)

	app.activeTab = TabPanelsManager
	savePNG("02-panels-manager.png", app.RenderStudioView, 1280, 740)

	app.activeTab = TabSettings
	savePNG("03-settings-gemini-claude.png", app.RenderStudioView, 1280, 880)

	app.walMode = 0
	savePNG("04-wal-panel-ai.png", app.RenderWalPanelView, 820, 560)

	app.walMode = 1
	savePNG("06-wal-panel-store.png", app.RenderWalPanelView, 820, 560)

	app.walMode = 2
	savePNG("05-wal-panel-launcher.png", app.RenderWalPanelView, 820, 560)
}



