package omamac

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// StudioTab represents the navigation sections in the OmaMac main window.
type StudioTab int

const (
	TabOverview StudioTab = iota // Panels: switches, preview, AI refine (default)
	TabStore                     // Vorlagen: ready-made panels
	TabSettings                  // Einstellungen: AI provider, vault, general
)

type panelRuntime struct {
	spec     *PanelSpec
	tray     *mygo.Tray
	flyout   *mygo.Window
	floating *mygo.Window

	lastRefresh time.Time
	refreshing  bool

	// What the open windows were built for (type + HTML): a change rebuilds them.
	flyoutKey   string
	floatingKey string
}

// windowKey identifies what a panel window renders; HTML windows load their document once.
func windowKey(p *PanelSpec) string {
	if p.Type != PanelTypeHTML {
		return string(p.Type)
	}
	return string(p.Type) + ":" + fmt.Sprintf("%x", sha256.Sum256([]byte(p.HTML)))
}

// App manages the OmaMac Wal-Panel, Multi-Status-Panel Trays/Flyouts, Credentials Vault, and Settings UI.
type App struct {
	ConfigPath string
	Config     *Config
	Vault      *Vault
	AI         *AIEngine
	Jira       *JiraModuleState // Pre-delivered Jira Quick-Access module (JQL, Board, Tickets)
	AgyStatus  AntigravityStatus

	studioWin *mygo.Window
	walWin    *mygo.Window
	mainTray  *mygo.Tray
	desktop   bool      // StartDesktopServices ran: trays and windows may be created
	walBlurAt time.Time // when the launcher last hid because it lost the focus

	activeTab StudioTab

	panels   []*PanelSpec
	runtimes map[string]*panelRuntime
	apps     []QuickCommand

	// AI create (prompt 1) & refine (prompt 2) state
	showCreate      bool // the "Neues Panel" dialog
	createPlaceIdx  int  // 0 = menu bar, 1 = menu bar + desktop widget
	showCreateMore  bool // the dialog's further options
	showHistory     bool // the AI steps of the chosen panel
	prompt1Text     string
	prompt1TypeIdx  int // 0 = Native (mygo/ui), 1 = HTML (WebView), 2 = Both (Native + HTML)
	prompt1CountIdx int // 0 = 1 Panel, 1 = 2 Panels, 2 = 3 Panels, 3 = 4 Panels
	selectedPanelID string
	prompt2Text     string

	confirmDeleteID string // panel whose deletion awaits confirmation
	confirmDelete   bool

	// Launcher state
	launcherQuery string
	launchSel     int // the chosen row
	launchList    ui.ListState
	iconCache     appIconCache

	// Settings & Central Credentials Vault draft state
	providerIdx   int // 0 = Gemini, 1 = Claude
	geminiAuthIdx int // 0 = Antigravity OAuth Login, 1 = Gemini API Key
	geminiModel   string
	geminiKey     string
	claudeAuthIdx int // 0 = Anthropic API Key, 1 = Antigravity Claude, 2 = Claude CLI
	claudeModel   string
	claudeKey     string
	shortcutText  string

	vaultNewKey  string
	vaultNewVal  string
	vaultNewDesc string

	showHelpModal bool

	createImages []string // images attached in the "Neues Panel" dialog
	refineImages []string // images attached in the refine box
	thumbs       map[string]ui.ImageSource

	aiRun     *aiRun // the current or last AI run, shown in the "KI arbeitet" dialog
	showAIRun bool
	aiRunSeen int // output length the dialog last scrolled to

	quitting  bool // set when the app quits: windows closing then must not unpin widgets or hide
	busy      bool
	busyLabel string
	statusMsg string
	statusErr bool
	toasts    []string

	mu sync.Mutex
}

var (
	panelTypeLabels  = []string{"Native GPU (mygo/ui)", "Web-Design (HTML/CSS)", "Beide Varianten"}
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
		ConfigPath:    configPath,
		Config:        cfg,
		Vault:         vault,
		AI:            aiEngine,
		Jira:          NewJiraModuleState(filepath.Dir(cfg.PanelsDir)),
		AgyStatus:     InspectAntigravityAuth(cfg),
		activeTab:     TabOverview,
		panels:        panels,
		runtimes:      make(map[string]*panelRuntime),
		apps:          DiscoverMacApps(),
		showHelpModal: !cfg.OnboardingDismissed, // the short guide opens once, on the first start
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
}

func (a *App) selectPanel(id string) {
	a.selectedPanelID = id
}

func (a *App) findPanel(id string) *PanelSpec {
	for _, p := range a.panels {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (a *App) countEnabledPanels() (enabledCount int, floatingCount int) {
	for _, p := range a.panels {
		if p.Enabled {
			enabledCount++
		}
		if p.PinnedFloating {
			floatingCount++
		}
	}
	return enabledCount, floatingCount
}

func (a *App) persistPanelToggle(p *PanelSpec, actionDesc string) {
	if p == nil {
		return
	}
	_ = SavePanel(a.Config.PanelsDir, p)
	a.syncAllPanelRuntimes()
	if actionDesc != "" {
		a.toast(actionDesc)
	}
}

func (a *App) setAllPanelsEnabled(enabled bool) {
	for _, p := range a.panels {
		p.Enabled = enabled
		_ = SavePanel(a.Config.PanelsDir, p)
	}
	a.syncAllPanelRuntimes()
	if enabled {
		a.toast("Alle Panels in der macOS-Menüleiste aktiviert")
	} else {
		a.toast("Alle Panels in der macOS-Menüleiste deaktiviert")
	}
}

// StartDesktopServices mounts the main OmaMac tray, all enabled status panel trays,
// registers the global Wal-Panel shortcut, and starts periodic panel data refresh.
func (a *App) StartDesktopServices(studioWin *mygo.Window) {
	a.studioWin = studioWin
	a.desktop = true

	// 1. Create the floating Wal-Panel (hidden initially, summoned via global shortcut or tray)
	a.walWin = mygo.NewWindow(mygo.WindowOptions{
		Title:             "OmaMac Schnellstart",
		Width:             680,
		Height:            480,
		Frameless:         true,
		Panel:             true,
		AlwaysOnTop:       true,
		Hidden:            true,
		Vibrancy:          mygo.VibrancyGlass,
		HideWindowButtons: true,
		Content:           ui.View(a.RenderWalPanelView),
	})
	// Like Spotlight, the launcher goes away when another window takes the focus.
	a.walWin.OnBlur(func() {
		if !a.quitting && a.walWin.IsVisible() {
			a.walBlurAt = time.Now()
			a.walWin.Hide()
		}
	})

	// Escape / Cmd+W hide the Wal-Panel and the main window instead of destroying them,
	// so the hotkey, the tray icon and "Hauptfenster" keep working. Quitting still closes them.
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) { a.quitting = true })
	hideOnClose := func(w *mygo.Window) {
		w.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting {
				return
			}
			e.PreventDefault()
			w.Hide()
		})
	}
	hideOnClose(a.walWin)
	if studioWin != nil {
		hideOnClose(studioWin)
	}

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
		ToolTip: "OmaMac – Übersicht & Wal-Panel öffnen",
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
	// A click on the tray icon first takes the focus from the open launcher,
	// which hides it: that click means "close", not "open again".
	if time.Since(a.walBlurAt) < 400*time.Millisecond {
		return
	}
	a.launcherQuery = ""
	a.launchSel = 0
	a.launchList.ScrollTo(0, ui.Start)
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
		if !a.desktop {
			continue // tests and `omamac -check`: no menu bar or windows
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

		// A changed type or HTML (Prompt 2, "Zu HTML-/Native-Ansicht") needs new windows.
		key := windowKey(p)
		if rt.flyout != nil && rt.flyoutKey != key {
			if !rt.flyout.IsDestroyed() {
				rt.flyout.Destroy()
			}
			rt.flyout = nil
		}
		if rt.floating != nil && rt.floatingKey != key {
			if !rt.floating.IsDestroyed() {
				rt.floating.Destroy()
			}
			rt.floating = nil
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
				rt.floatingKey = key
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
	rt.flyoutKey = windowKey(p)
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
		// Render the runtime's current spec, so a panel replaced by Prompt 2 shows its new version.
		opts.Content = ui.View(func(c *ui.Context) {
			a.renderNativePanelPopover(c, rt.spec, rt, func() {
				if rt.flyout != nil && !rt.flyout.IsDestroyed() {
					rt.flyout.Hide()
				}
			})
		})
		rt.flyout = mygo.NewFlyout(opts)
	}
}

// createFloatingPanelWindow creates the desktop widget window of a pinned panel. Closing it
// (✕ in native panels, the window's close button in HTML panels) unpins the widget.
func (a *App) createFloatingPanelWindow(p *PanelSpec) *mygo.Window {
	panelID := p.ID
	opts := mygo.WindowOptions{
		Title:         p.Title,
		Width:         p.Width,
		Height:        p.Height,
		TitleBarStyle: mygo.TitleBarHidden,
		AlwaysOnTop:   true,
		Vibrancy:      mygo.VibrancyGlass,
	}
	var win *mygo.Window
	if p.Type == PanelTypeHTML {
		// HTML has no native header: keep the macOS window buttons for closing.
		win = mygo.NewWindow(opts)
		htmlDoc := p.HTML
		if strings.TrimSpace(htmlDoc) == "" {
			htmlDoc = synthesizeHTMLForPanel(p)
		}
		win.Page().LoadHTML(htmlDoc, "https://omamac.local/")
	} else {
		opts.HideWindowButtons = true
		opts.Content = ui.View(func(c *ui.Context) {
			rt := a.runtimes[panelID]
			spec := p
			if rt != nil && rt.spec != nil {
				spec = rt.spec
			}
			a.renderNativePanelPopover(c, spec, rt, func() {
				if win != nil && !win.IsDestroyed() {
					win.Close()
				}
			})
		})
		win = mygo.NewWindow(opts)
	}
	win.OnClose(func(e *mygo.CloseEvent) {
		a.unpinFloatingWidget(panelID)
	})
	return win
}

// unpinFloatingWidget is called when the user closes a desktop widget: it stays closed
// (PinnedFloating = false) instead of being recreated by the next syncAllPanelRuntimes.
func (a *App) unpinFloatingWidget(panelID string) {
	p := a.findPanel(panelID)
	if a.quitting || p == nil || !p.PinnedFloating {
		return // quitting closes every window: the widget stays pinned for the next launch
	}
	p.PinnedFloating = false
	_ = SavePanel(a.Config.PanelsDir, p)
	if rt, ok := a.runtimes[panelID]; ok {
		rt.floating = nil // the window is closing itself
	}
	a.toast("Widget „" + p.Title + "“ geschlossen – über den Schalter „Widget“ wieder anpinnen")
	a.updateUI(func() {})
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
	a.refreshDuePanels(true)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.refreshDuePanels(false)
	}
}

// refreshDuePanels refreshes every active panel whose RefreshSeconds interval has elapsed
// (or all active ones when force is true). Runs off the UI thread.
func (a *App) refreshDuePanels(force bool) {
	type job struct {
		p  *PanelSpec
		rt *panelRuntime
	}
	var jobs []job
	a.updateUISync(func() {
		now := time.Now()
		for _, p := range a.panels {
			if !p.Enabled && !p.PinnedFloating {
				continue
			}
			rt := a.runtimes[p.ID]
			if rt == nil || rt.refreshing {
				continue
			}
			interval := time.Duration(p.RefreshSeconds) * time.Second
			if interval < 15*time.Second {
				interval = 15 * time.Second
			}
			if !force && now.Sub(rt.lastRefresh) < interval {
				continue
			}
			rt.refreshing = true
			jobs = append(jobs, job{p, rt})
		}
	})
	if len(jobs) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, jb := range jobs {
		wg.Add(1)
		go func(jb job) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			a.refreshPanelData(ctx, jb.p)
		}(jb)
	}
	wg.Wait()
	a.updateUI(func() {
		now := time.Now()
		for _, jb := range jobs {
			jb.rt.refreshing = false
			jb.rt.lastRefresh = now
		}
		a.syncAllPanelRuntimes()
	})
}

// refreshPanelData updates a panel's live data: built-in system modules use their native Go clients,
// all other panels run their shell Command with Vault credentials injected.
func (a *App) refreshPanelData(ctx context.Context, p *PanelSpec) {
	if p == nil {
		return
	}
	switch p.BuiltinKind {
	case BuiltinJiraQuickAccess:
		if a.Jira != nil {
			a.Jira.RefreshJiraPanel(ctx, p)
		}
	case BuiltinAtlassianStatus:
		RefreshAtlassianStatusPanel(ctx, p)
	default:
		var env []string
		if a.Vault != nil {
			env = a.Vault.EnvSlice()
		}
		_ = p.RunDataRefresh(ctx, env...)
	}
}

// refreshPanelAsync refreshes one panel in the background and re-renders all trays/popovers afterwards.
func (a *App) refreshPanelAsync(p *PanelSpec, doneMsg string) {
	rt := a.runtimes[p.ID]
	if rt != nil {
		if rt.refreshing {
			return // a refresh of this panel is already running
		}
		rt.refreshing = true
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		a.refreshPanelData(ctx, p)
		a.updateUI(func() {
			if rt != nil {
				rt.refreshing = false
				rt.lastRefresh = time.Now()
			}
			a.syncAllPanelRuntimes()
			if doneMsg != "" {
				a.toast(doneMsg)
			}
		})
	}()
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
	if a.Jira != nil && a.Jira.boardWin != nil && !a.Jira.boardWin.IsDestroyed() {
		a.Jira.boardWin.Invalidate()
	}
}

// errText describes a failed AI call for the status bar.
func errText(err error) string {
	if err == nil {
		return "die KI hat kein Ergebnis geliefert."
	}
	return err.Error()
}

// updateUISync runs fn on the main thread and waits for it (used to snapshot UI state from goroutines).
func (a *App) updateUISync(fn func()) {
	mygo.RunOnMain(fn)
}

// clearStatus removes the error line at the bottom of the main window.
func (a *App) clearStatus() {
	a.statusErr = false
	a.statusMsg = ""
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
	asWidget := a.createPlaceIdx == 1
	count := a.prompt1CountIdx + 1

	var vaultEnv []string
	if a.Vault != nil {
		vaultEnv = a.Vault.EnvSlice()
	}

	imagePaths := append([]string(nil), a.createImages...)
	a.busy = true
	a.busyLabel = "KI erstellt das Panel …"
	ctx, run := a.startAIRun("Neues Panel: " + truncateStr(prompt, 60))
	go func() {
		var created []*PanelSpec
		var providerUsed string
		images, err := LoadAIImages(ctx, imagePaths)
		if err == nil {
			created, providerUsed, err = a.AI.GeneratePanels(ctx, prompt, reqType, count, images...)
		}
		if err == nil && len(created) == 0 {
			err = errors.New("die KI hat kein Panel geliefert")
		}
		for _, p := range created {
			p.Kind = "panel"
			if asWidget {
				p.Kind = "widget"
				p.PinnedFloating = true
			}
			progressStep(ctx, "Prüfe „%s“ …", p.Title)
			_, _ = p.CheckPanelSpec(ctx, vaultEnv...)
		}
		a.updateUI(func() {
			a.busy = false
			run.finish(err)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				a.statusErr = true
				a.statusMsg = "Kein Panel erstellt — " + errText(err)
				a.toast("⚠️ " + a.statusMsg)
				return
			}
			a.showAIRun = false
			a.createImages = nil
			for _, p := range created {
				_ = SavePanel(a.Config.PanelsDir, p)
				a.panels = append(a.panels, p)
			}
			if len(created) > 0 {
				a.selectPanel(created[len(created)-1].ID)
			}
			a.syncAllPanelRuntimes()
			a.clearStatus()
			if len(created) == 1 {
				a.toast(fmt.Sprintf("„%s“ erstellt (%s)", created[0].Title, providerUsed))
			} else {
				a.toast(fmt.Sprintf("%d Panels erstellt (%s)", len(created), providerUsed))
			}
		})
	}()
}

// triggerPrompt2Refine runs Prompt 2 on a selected panel, runs omamac-check, and hot-reloads its menubar tray & popover.
func (a *App) triggerPrompt2Refine(panelID, refinePrompt string) {
	refinePrompt = strings.TrimSpace(refinePrompt)
	target := a.findPanel(panelID)
	if target == nil || target.IsBuiltin || refinePrompt == "" || a.busy {
		return
	}
	var vaultEnv []string
	if a.Vault != nil {
		vaultEnv = a.Vault.EnvSlice()
	}

	imagePaths := append([]string(nil), a.refineImages...)
	a.busy = true
	a.busyLabel = fmt.Sprintf("KI passt „%s“ an …", target.Title)
	ctx, run := a.startAIRun(fmt.Sprintf("„%s“ anpassen", target.Title))
	go func() {
		var updated *PanelSpec
		var providerUsed string
		images, err := LoadAIImages(ctx, imagePaths)
		if err == nil {
			updated, providerUsed, err = a.AI.RefinePanel(ctx, target, refinePrompt, images...)
		}
		if err == nil && updated == nil {
			err = errors.New("die KI hat kein Panel geliefert")
		}
		if updated != nil {
			progressStep(ctx, "Prüfe „%s“ …", updated.Title)
			_, _ = updated.CheckPanelSpec(ctx, vaultEnv...)
		}
		a.updateUI(func() {
			a.busy = false
			run.finish(err)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				a.statusErr = true
				a.statusMsg = "Panel unverändert — " + errText(err)
				a.toast("⚠️ " + a.statusMsg)
				return
			}
			a.showAIRun = false
			replaced := false
			for i, p := range a.panels {
				if p.ID == panelID {
					a.panels[i] = updated
					replaced = true
					break
				}
			}
			if !replaced {
				return // the panel was deleted while the AI was working: don't bring it back
			}
			_ = SavePanel(a.Config.PanelsDir, updated)
			a.selectPanel(updated.ID)
			if rt, ok := a.runtimes[updated.ID]; ok && rt.flyout != nil && !rt.flyout.IsDestroyed() {
				rt.flyout.Destroy()
				rt.flyout = nil
			}
			a.syncAllPanelRuntimes()
			a.prompt2Text = ""
			a.refineImages = nil
			a.clearStatus()
			a.toast(fmt.Sprintf("„%s“ angepasst (%s)", updated.Title, providerUsed))
		})
	}()
}
