package omamac

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestQuotaErrorDetectionAndClaudeFallback(t *testing.T) {
	if !IsQuotaError(errors.New("429 RESOURCE_EXHAUSTED: Your AI credits balance is too low")) {
		t.Fatal("expected quota error to be detected")
	}
	if IsQuotaError(errors.New("connection refused")) {
		t.Fatal("ordinary network error flagged as quota error")
	}
	if !strings.Contains(friendlyAIError(errors.New("RESOURCE_EXHAUSTED")), "429") {
		t.Fatalf("unexpected friendly message: %s", friendlyAIError(errors.New("RESOURCE_EXHAUSTED")))
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	fakeClaude := filepath.Join(tmp, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	cfg.ActiveProvider = ProviderGemini
	cfg.GeminiAuthMode = GeminiAuthAntigravity
	cfg.ClaudeCLIPath = fakeClaude
	cfg.OnboardingDismissed = true
	_ = SaveConfig(cfgPath, cfg)

	if !CanFallbackToClaudeCLI(cfg) {
		t.Fatal("expected CanFallbackToClaudeCLI to be true")
	}

	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	retried := false
	app.lastAIRetry = func() { retried = true }
	app.showAIRun = true
	app.switchToClaudeCLIAndRetry()

	if !retried {
		t.Fatal("expected lastAIRetry to be invoked")
	}
	if app.Config.ActiveProvider != ProviderClaude || app.Config.ClaudeAuthMode != ClaudeAuthCLI {
		t.Fatalf("config not switched: provider=%s auth=%s", app.Config.ActiveProvider, app.Config.ClaudeAuthMode)
	}
	if CanFallbackToClaudeCLI(app.Config) {
		t.Fatal("CanFallbackToClaudeCLI should be false when already using Claude CLI")
	}
}

func TestJSONRepairRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		reply := `[{"title":"Broken","status":"ok"` // malformed JSON containing '[' and '{'
		if calls >= 2 {
			reply = `[{"title":"Repaired","status":"ok","badge_text":"OK"}]`
		}
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

	panels, _, err := ai.GeneratePanels(context.Background(), "Test repair", "native", 1)
	if err != nil || len(panels) != 1 {
		t.Fatalf("GeneratePanels repair failed: %v (calls=%d)", err, calls)
	}
	if panels[0].Title != "Repaired" || calls != 2 {
		t.Fatalf("got title=%q calls=%d; want Repaired, 2", panels[0].Title, calls)
	}

	// RefinePanel repair too
	calls = 0
	refined, _, err := ai.RefinePanel(context.Background(), panels[0], "change title")
	if err != nil || refined == nil || refined.Title != "Repaired" || calls != 2 {
		t.Fatalf("RefinePanel repair failed: refined=%v err=%v calls=%d", refined, err, calls)
	}
}

func TestPromptHistorySnapshotAndRestore(t *testing.T) {
	replies := []string{
		`[{"title":"Disk v1","status":"ok","badge_text":"10 GB","command":"echo 'BADGE=10 GB'"}]`,
		`{"title":"Disk v2","status":"crit","badge_text":"99%","command":"echo 'BADGE=99%'"}`,
	}
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rep := replies[min(idx, len(replies)-1)]
		idx++
		body, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": rep}}}}}})
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	target, _ := url.Parse(srv.URL)

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfgPath := filepath.Join(tmp, "config.json")
	cfg := DefaultConfig()
	cfg.PanelsDir = filepath.Join(tmp, "panels")
	cfg.ActiveProvider = ProviderGemini
	cfg.GeminiAuthMode = GeminiAuthAPIKey
	cfg.GeminiAPIKey = "test-key"
	cfg.OnboardingDismissed = true
	_ = SaveConfig(cfgPath, cfg)

	app, err := NewApp(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	app.AI.HTTPClient = &http.Client{Transport: rewriteTransport{target}}

	panels, _, err := app.AI.GeneratePanels(context.Background(), "Disk", "native", 1)
	if err != nil || len(panels) != 1 {
		t.Fatalf("create failed: %v", err)
	}
	v1 := panels[0]
	if len(v1.PromptHistory) != 1 || v1.PromptHistory[0].Snapshot == nil {
		t.Fatalf("expected step 1 snapshot, got %+v", v1.PromptHistory)
	}

	v2, _, err := app.AI.RefinePanel(context.Background(), v1, "make crit")
	if err != nil || v2.Title != "Disk v2" || len(v2.PromptHistory) != 2 {
		t.Fatalf("refine failed: v2=%+v err=%v", v2, err)
	}
	app.panels = append(app.panels, v2)
	app.approved.allow(v2)
	_ = SavePanel(cfg.PanelsDir, v2)

	app.restorePromptSnapshot(v2, v2.PromptHistory[0])
	if v2.Title != "Disk v1" || v2.Command != "echo 'BADGE=10 GB'" {
		t.Fatalf("restore failed: title=%q cmd=%q", v2.Title, v2.Command)
	}
}

func TestEditCommandDialogAndPanelFilter(t *testing.T) {
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
	app.activeTab = TabOverview
	app.panelFilter = "mydealz"
	if got := app.filteredPanels(); len(got) != 1 || got[0].ID != "04-mydealz-3d" {
		t.Fatalf("filteredPanels = %d panels, want 04-mydealz-3d", len(got))
	}
	app.panelFilter = ""

	target := app.findPanel("04-mydealz-3d")
	if target == nil {
		t.Fatal("missing starter panel")
	}
	app.selectPanel(target.ID)
	app.openEditCommand(target)
	app.editCmdRefresh = "45"
	app.editCmdText = "echo 'BADGE=Updated'"
	app.editCmdNotify = true

	tt := ui.NewTester(app.RenderStudioView, 1320, 820)
	if err := tt.Click("Speichern"); err != nil {
		t.Fatalf("click Speichern: %v", err)
	}
	if target.RefreshSeconds != 45 || target.Command != "echo 'BADGE=Updated'" || !target.NotifyOnStatusChange {
		t.Fatalf("edit not applied: refresh=%d cmd=%q notify=%v", target.RefreshSeconds, target.Command, target.NotifyOnStatusChange)
	}
}

func TestLaunchAtLoginPlist(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	oldGOOS := goos
	goos = "darwin"
	defer func() { goos = oldGOOS }()

	plistPath := launchAgentPath()
	if err := writeLaunchAgentPlist(plistPath, "/Applications/OmaMac.app/Contents/MacOS/OmaMac"); err != nil {
		t.Fatal(err)
	}
	if !IsLaunchAtLoginEnabled() {
		t.Fatal("expected IsLaunchAtLoginEnabled to be true")
	}
	raw, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "/usr/bin/open") || !strings.Contains(s, "/Applications/OmaMac.app") || !strings.Contains(s, "de.diver80.omamac") {
		t.Fatalf("unexpected plist contents:\n%s", s)
	}
	if err := SetLaunchAtLogin(false); err != nil {
		t.Fatal(err)
	}
	if IsLaunchAtLoginEnabled() {
		t.Fatal("expected IsLaunchAtLoginEnabled to be false after removal")
	}
}

func TestDiscoverMacAppsIncludesUserAppsAndExtra(t *testing.T) {
	tmp := t.TempDir()
	sysApps := filepath.Join(tmp, "sys")
	userApps := filepath.Join(tmp, "user")
	finder := filepath.Join(tmp, "CoreServices", "Finder.app")
	for _, d := range []string{
		filepath.Join(sysApps, "Safari.app"),
		filepath.Join(userApps, "CustomTool.app"),
		filepath.Join(userApps, "Safari.app"), // duplicate should be skipped
		finder,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	apps := discoverMacAppsFrom([]string{sysApps, userApps}, finder)
	if len(apps) != 3 {
		t.Fatalf("expected 3 unique apps, got %d (%+v)", len(apps), apps)
	}
	if apps[0].Title != "CustomTool" || apps[1].Title != "Finder" || apps[2].Title != "Safari" {
		t.Fatalf("unexpected order/names: %+v", apps)
	}
}

func TestRenderLongPopoverAndStatusNotification(t *testing.T) {
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
	p := &PanelSpec{
		ID:                   "long-test",
		Title:                "Long Panel",
		Icon:                 "📦",
		Status:               "ok",
		BadgeText:            "15 items",
		NotifyOnStatusChange: true,
		Metrics: []PanelMetric{
			{Label: "Usage", Value: "65%", Progress: 65, Status: "warn"},
		},
		Actions: []PanelAction{{Label: "Open", URL: "https://example.com", Style: "primary"}},
	}
	for i := 0; i < 15; i++ {
		p.Rows = append(p.Rows, PanelRow{Left: "Row", Right: "Val", Badge: "OK", Status: "ok"})
	}
	img := ui.Render(func(c *ui.Context) {
		app.renderNativePanelPopover(c, p, nil, nil)
	}, 380, 270, 1.0)
	if img == nil || img.Bounds().Dx() != 380 {
		t.Fatal("failed to render scrollable popover")
	}

	// Ensure status transition notification helper runs cleanly without panic
	p.Status = "crit"
	app.maybeNotifyStatusChange(p, "ok")
}
