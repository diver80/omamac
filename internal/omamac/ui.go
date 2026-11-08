package omamac

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// StudioTab represents the navigation sections in the OmaMac Studio & Settings window.
type StudioTab int

const (
	TabPromptStudio StudioTab = iota
	TabPanelsManager
	TabWindowManager
	TabSettings
)

type panelRuntime struct {
	spec       *PanelSpec
	tray       *mygo.Tray
	flyout     *mygo.Window
	floating   *mygo.Window
	quickRefIn string
}

// App manages the OmaMac Wal-Panel, Multi-Status-Panel Trays/Flyouts, Credentials Vault, and Settings UI.
type App struct {
	ConfigPath string
	Config     *Config
	Vault      *Vault
	AI         *AIEngine
	AgyStatus  AntigravityStatus

	studioWin *mygo.Window
	walWin    *mygo.Window
	mainTray  *mygo.Tray

	activeTab StudioTab
	walMode   int // 0 = 2-Prompt AI Panel Studio, 1 = App & Window Launcher

	panels   []*PanelSpec
	runtimes map[string]*panelRuntime
	apps     []QuickCommand

	// 2-Prompt Studio & OmaLauncher state
	createKindIdx   int // 0 = [ widget ], 1 = [ app ], 2 = [ panel ]
	gridSizeIdx     int // 0 = 3x3 (Half), 1 = 6x2 (Full)
	showStoreView   bool
	omniBarQuery    string
	prompt1Text     string
	prompt1TypeIdx  int // 0 = Native (mygo/ui), 1 = HTML (WebView), 2 = Both (Native + HTML)
	prompt1CountIdx int // 0 = 1 Panel, 1 = 2 Panels, 2 = 3 Panels, 3 = 4 Panels
	selectedPanelID string
	prompt2Text     string
	editorCmdText   string
	editorBadgeText string
	editorHTMLText  string

	// Launcher state
	launcherQuery string

	// Settings & Central Credentials Vault draft state
	providerIdx   int // 0 = Gemini, 1 = Claude
	geminiAuthIdx int // 0 = Antigravity OAuth Login, 1 = Gemini API Key
	geminiModel   string
	geminiKey     string
	claudeAuthIdx int // 0 = Anthropic API Key, 1 = Antigravity Claude, 2 = Claude CLI
	claudeModel   string
	claudeKey     string
	shortcutText  string
	themeSelect   string

	vaultNewKey  string
	vaultNewVal  string
	vaultNewDesc string

	showHelpModal     bool
	dontShowHelpAgain bool

	busy      bool
	busyLabel string
	statusMsg string
	statusErr bool
	toasts    []string

	mu sync.Mutex
}

var (
	panelTypeLabels  = []string{"Native GPU (mygo/ui DSL)", "HTML/CSS/JS (WebView Glass)", "Both Types (Native + HTML)"}
	panelCountLabels = []string{"1 Panel", "2 Panels", "3 Panels", "4 Panels"}
	geminiModelList  = []string{
		"gemini-4-argon-medium",
		"gemini-4-argon-high",
		"gemini-3.8-flash-high",
		"gemini-3.8-flash-medium",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
	}
	claudeModelList = []string{
		"claude-sonnet-5-5-medium",
		"claude-sonnet-5-5-high",
		"claude-opus-5-5-medium",
		"claude-sonnet-4-20250514",
		"claude-3-7-sonnet-latest",
	}
	omarchyThemes = []string{
		"TokyoNight",
		"Catppuccin Mocha",
		"Gruvbox Dark",
		"Everforest",
		"Kanagawa",
		"Nord",
	}
)

// NewApp initializes OmaMac, loads config, vault, and saved panels, and prepares the AI engine.
func NewApp(configPath string) (*App, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	vaultPath := DefaultVaultPath()
	if configPath != "" {
		vaultPath = configPath + ".vault.json"
	}
	vault, _ := LoadVault(vaultPath)

	panels, err := LoadPanels(cfg.PanelsDir)
	if err != nil {
		return nil, err
	}

	aiEngine := NewAIEngine(cfg)
	aiEngine.Vault = vault

	a := &App{
		ConfigPath:        configPath,
		Config:            cfg,
		Vault:             vault,
		AI:                aiEngine,
		AgyStatus:         InspectAntigravityAuth(cfg),
		panels:            panels,
		runtimes:          make(map[string]*panelRuntime),
		apps:              DiscoverMacApps(),
		showHelpModal:     !cfg.OnboardingDismissed,
		dontShowHelpAgain: cfg.OnboardingDismissed,
		statusMsg:         "OmaMac ready. AI is only used to create/refine panels — live polling uses 0 AI tokens!",
	}
	if len(panels) > 0 {
		a.selectPanel(panels[0].ID)
	}
	a.loadSettingsDraft()
	return a, nil
}

func (a *App) loadSettingsDraft() {
	c := a.Config
	if c.ActiveProvider == ProviderClaude {
		a.providerIdx = 1
	} else {
		a.providerIdx = 0
	}
	if c.GeminiAuthMode == GeminiAuthAPIKey {
		a.geminiAuthIdx = 1
	} else {
		a.geminiAuthIdx = 0
	}
	a.geminiModel = c.GeminiModel
	a.geminiKey = c.GeminiAPIKey

	switch c.ClaudeAuthMode {
	case ClaudeAuthAntigravity:
		a.claudeAuthIdx = 1
	case ClaudeAuthCLI:
		a.claudeAuthIdx = 2
	default:
		a.claudeAuthIdx = 0
	}
	a.claudeModel = c.ClaudeModel
	a.claudeKey = c.ClaudeAPIKey
	a.shortcutText = c.LauncherShortcut
	a.themeSelect = c.ThemeName
}

func (a *App) selectPanel(id string) {
	a.selectedPanelID = id
	if p := a.findPanel(id); p != nil {
		a.editorCmdText = p.Command
		a.editorBadgeText = p.BadgeText
		a.editorHTMLText = p.HTML
	}
}

func (a *App) findPanel(id string) *PanelSpec {
	for _, p := range a.panels {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// StartDesktopServices mounts the main OmaMac tray, all enabled status panel trays,
// registers the global Wal-Panel shortcut, and starts periodic panel data refresh.
func (a *App) StartDesktopServices(studioWin *mygo.Window) {
	a.studioWin = studioWin

	// 1. Create the floating Wal-Panel (hidden initially, summoned via global shortcut or tray)
	a.walWin = mygo.NewWindow(mygo.WindowOptions{
		Title:             "OmaMac Wal-Panel",
		Width:             760,
		Height:            520,
		Frameless:         true,
		Panel:             true,
		AlwaysOnTop:       true,
		Hidden:            true,
		Vibrancy:          mygo.VibrancyGlass,
		HideWindowButtons: true,
		Content:           ui.View(a.RenderWalPanelView),
	})

	// 2. Register global hotkey (e.g. Alt+Space)
	if a.Config.LauncherShortcut != "" {
		_ = mygo.GlobalShortcut.Register(a.Config.LauncherShortcut, func() {
			a.ToggleWalPanel()
		})
	}

	// 3. Create main OmaMac control tray icon in macOS menu bar
	if tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "omamac-master-tray",
		Title:   "⌘ OmaMac",
		ToolTip: "OmaMac – Wal-Panel Launcher & AI Status Panels",
	}); err == nil {
		a.mainTray = tray
		tray.OnClick(func() {
			a.ToggleWalPanel()
		})
	}

	// 4. Mount all enabled status panels into the macOS menu bar (and floating windows if pinned)
	a.syncAllPanelRuntimes()

	// 5. Start background ticker to refresh panel data commands
	go a.backgroundRefreshLoop()
}

// ToggleWalPanel shows or hides the Spotlight/Walker-style floating Wal-Panel.
func (a *App) ToggleWalPanel() {
	if a.walWin == nil {
		return
	}
	if a.walWin.IsVisible() && a.walWin.IsFocused() {
		a.walWin.Hide()
		return
	}
	a.walWin.Center()
	a.walWin.Show()
	a.walWin.Focus()
	a.walWin.Invalidate()
}

func (a *App) syncAllPanelRuntimes() {
	activeIDs := make(map[string]bool)
	for _, p := range a.panels {
		activeIDs[p.ID] = true
		rt, ok := a.runtimes[p.ID]
		if !ok {
			rt = &panelRuntime{spec: p}
			a.runtimes[p.ID] = rt
		} else {
			rt.spec = p
		}

		// Sync Menubar Tray
		if p.Enabled {
			if rt.tray == nil {
				panelID := p.ID
				if t, err := mygo.NewTray(mygo.TrayOptions{
					ID:      "omamac-panel-" + p.ID,
					Title:   p.TrayLabel(),
					ToolTip: p.Title + " — " + p.Subtitle,
				}); err == nil {
					rt.tray = t
					t.OnClick(func() {
						a.togglePanelFlyout(panelID)
					})
				}
			} else {
				rt.tray.SetTitle(p.TrayLabel())
				rt.tray.SetToolTip(p.Title + " — " + p.Subtitle)
			}
		} else if rt.tray != nil {
			if rt.flyout != nil {
				rt.flyout.Destroy()
				rt.flyout = nil
			}
			rt.tray.Destroy()
			rt.tray = nil
		}

		// Update live HTML flyout/floating if open
		if p.Type == PanelTypeHTML {
			a.pushHTMLPanelUpdate(rt)
		} else {
			if rt.flyout != nil && !rt.flyout.IsDestroyed() {
				rt.flyout.Invalidate()
			}
			if rt.floating != nil && !rt.floating.IsDestroyed() {
				rt.floating.Invalidate()
			}
		}

		// Sync Floating Desktop HUD Window
		if p.PinnedFloating {
			if rt.floating == nil || rt.floating.IsDestroyed() {
				rt.floating = a.createFloatingPanelWindow(p)
			}
		} else if rt.floating != nil {
			if !rt.floating.IsDestroyed() {
				rt.floating.Destroy()
			}
			rt.floating = nil
		}
	}

	// Clean up deleted panels
	for id, rt := range a.runtimes {
		if !activeIDs[id] {
			if rt.flyout != nil && !rt.flyout.IsDestroyed() {
				rt.flyout.Destroy()
			}
			if rt.floating != nil && !rt.floating.IsDestroyed() {
				rt.floating.Destroy()
			}
			if rt.tray != nil {
				rt.tray.Destroy()
			}
			delete(a.runtimes, id)
		}
	}
}

func (a *App) togglePanelFlyout(panelID string) {
	rt, ok := a.runtimes[panelID]
	if !ok || rt.tray == nil {
		return
	}
	p := rt.spec
	if rt.flyout != nil && !rt.flyout.IsDestroyed() {
		if rt.flyout.IsVisible() {
			rt.flyout.Hide()
			return
		}
		rt.flyout.Destroy()
		rt.flyout = nil
	}

	opts := mygo.FlyoutOptions{
		Tray:         rt.tray,
		Width:        p.Width,
		Height:       p.Height,
		Popover:      true,
		Focusable:    true,
		Shadow:       true,
		Vibrancy:     mygo.VibrancyGlass,
		CornerRadius: 12,
	}
	if p.Type == PanelTypeHTML {
		rt.flyout = mygo.NewFlyout(opts)
		htmlDoc := p.HTML
		if strings.TrimSpace(htmlDoc) == "" {
			htmlDoc = synthesizeHTMLForPanel(p)
		}
		rt.flyout.Page().LoadHTML(htmlDoc, "https://omamac.local/")
		rt.flyout.Page().OnDOMReady(func() {
			a.pushHTMLPanelUpdate(rt)
		})
	} else {
		opts.Content = ui.View(func(c *ui.Context) {
			a.renderNativePanelPopover(c, p, rt)
		})
		rt.flyout = mygo.NewFlyout(opts)
	}
}

func (a *App) createFloatingPanelWindow(p *PanelSpec) *mygo.Window {
	opts := mygo.WindowOptions{
		Title:             p.Title,
		Width:             p.Width,
		Height:            p.Height,
		Frameless:         true,
		AlwaysOnTop:       true,
		Vibrancy:          mygo.VibrancyGlass,
		HideWindowButtons: true,
	}
	if p.Type == PanelTypeHTML {
		win := mygo.NewWindow(opts)
		htmlDoc := p.HTML
		if strings.TrimSpace(htmlDoc) == "" {
			htmlDoc = synthesizeHTMLForPanel(p)
		}
		win.Page().LoadHTML(htmlDoc, "https://omamac.local/")
		return win
	}
	opts.Content = ui.View(func(c *ui.Context) {
		rt := a.runtimes[p.ID]
		a.renderNativePanelPopover(c, p, rt)
	})
	return mygo.NewWindow(opts)
}

func (a *App) pushHTMLPanelUpdate(rt *panelRuntime) {
	if rt == nil || rt.spec == nil {
		return
	}
	payload, _ := json.Marshal(rt.spec)
	js := fmt.Sprintf("if (window.onOmaMacUpdate) { window.onOmaMacUpdate(%s); }", string(payload))
	if rt.flyout != nil && !rt.flyout.IsDestroyed() && rt.flyout.Page() != nil {
		_, _ = rt.flyout.Page().Eval(js)
	}
	if rt.floating != nil && !rt.floating.IsDestroyed() && rt.floating.Page() != nil {
		_, _ = rt.floating.Page().Eval(js)
	}
}

func (a *App) backgroundRefreshLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		var env []string
		if a.Vault != nil {
			env = a.Vault.EnvSlice()
		}
		for _, p := range a.panels {
			if !p.Enabled && !p.PinnedFloating {
				continue
			}
			_ = p.RunDataRefresh(context.Background(), env...)
		}
		a.updateUI(func() {
			a.syncAllPanelRuntimes()
		})
	}
}

func (a *App) updateUI(fn func()) {
	if a.studioWin != nil && !a.studioWin.IsDestroyed() {
		a.studioWin.Update(fn)
	} else {
		mygo.RunOnMain(fn)
	}
	if a.walWin != nil && !a.walWin.IsDestroyed() {
		a.walWin.Invalidate()
	}
}

func (a *App) toast(msg string) {
	a.toasts = append(a.toasts, msg)
}

// triggerPrompt1Create runs Prompt 1 to generate 1..N panels of the chosen type ("native", "html", or "both").
func (a *App) triggerPrompt1Create() {
	prompt := strings.TrimSpace(a.prompt1Text)
	if prompt == "" || a.busy {
		return
	}
	reqType := "native"
	switch a.prompt1TypeIdx {
	case 1:
		reqType = "html"
	case 2:
		reqType = "both"
	}
	kinds := []string{"widget", "app", "panel"}
	chosenKind := kinds[a.createKindIdx%len(kinds)]
	grids := []string{"3x3", "6x2"}
	chosenGrid := grids[a.gridSizeIdx%len(grids)]
	count := a.prompt1CountIdx + 1

	var vaultEnv []string
	if a.Vault != nil {
		vaultEnv = a.Vault.EnvSlice()
	}

	a.busy = true
	a.busyLabel = fmt.Sprintf("Prompt #1: Creating %d %s(s) [%s] via %s + running omamac-check...", count, chosenKind, chosenGrid, a.AI.ActiveProviderSummary())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		created, providerUsed, err := a.AI.GeneratePanels(ctx, prompt, reqType, count)
		for _, p := range created {
			p.Kind = chosenKind
			p.GridSize = chosenGrid
			if chosenKind == "widget" {
				p.PinnedFloating = true
			}
			_, _ = p.CheckPanelSpec(ctx, vaultEnv...)
		}
		a.updateUI(func() {
			a.busy = false
			if err != nil && len(created) == 0 {
				a.statusErr = true
				a.statusMsg = "Prompt #1 failed: " + err.Error()
				a.toast(a.statusMsg)
				return
			}
			for _, p := range created {
				_ = SavePanel(a.Config.PanelsDir, p)
				a.panels = append(a.panels, p)
			}
			if len(created) > 0 {
				a.selectPanel(created[len(created)-1].ID)
			}
			a.syncAllPanelRuntimes()
			a.statusErr = false
			a.statusMsg = fmt.Sprintf("Prompt #1 + omamac-check OK (%s): Created %d %s(s) [%s]!", providerUsed, len(created), chosenKind, chosenGrid)
			a.toast(fmt.Sprintf("Created & verified %d new %s(s)!", len(created), chosenKind))
		})
	}()
}

// triggerPrompt2Refine runs Prompt 2 on a selected panel, runs omamac-check, and hot-reloads its menubar tray & popover.
func (a *App) triggerPrompt2Refine(panelID, refinePrompt string) {
	refinePrompt = strings.TrimSpace(refinePrompt)
	target := a.findPanel(panelID)
	if target == nil || refinePrompt == "" || a.busy {
		return
	}
	var vaultEnv []string
	if a.Vault != nil {
		vaultEnv = a.Vault.EnvSlice()
	}

	a.busy = true
	a.busyLabel = fmt.Sprintf("Prompt #2: Refining '%s' + running omamac-check...", target.Title)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		updated, providerUsed, err := a.AI.RefinePanel(ctx, target, refinePrompt)
		if updated != nil {
			_, _ = updated.CheckPanelSpec(ctx, vaultEnv...)
		}
		a.updateUI(func() {
			a.busy = false
			if err != nil && updated == nil {
				a.statusErr = true
				a.statusMsg = "Prompt #2 failed: " + err.Error()
				a.toast(a.statusMsg)
				return
			}
			for i, p := range a.panels {
				if p.ID == panelID {
					a.panels[i] = updated
					break
				}
			}
			_ = SavePanel(a.Config.PanelsDir, updated)
			a.selectPanel(updated.ID)
			if rt, ok := a.runtimes[updated.ID]; ok && rt.flyout != nil && !rt.flyout.IsDestroyed() {
				rt.flyout.Destroy()
				rt.flyout = nil
			}
			a.syncAllPanelRuntimes()
			a.prompt2Text = ""
			a.statusErr = false
			a.statusMsg = fmt.Sprintf("Prompt #2 + omamac-check OK on '%s' (%s) — hot-reloaded!", updated.Title, providerUsed)
			a.toast("Refined & verified '" + updated.Title + "'")
		})
	}()
}
