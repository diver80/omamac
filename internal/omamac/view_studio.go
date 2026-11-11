package omamac

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// RenderStudioView renders the main OmaMac window: sidebar, the chosen view,
// and an error line at the bottom while something failed.
func (a *App) RenderStudioView(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.renderTopBar(c)
		ui.Divider(c)

		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			if a.sidebarCollapsed() {
				a.renderSidebarRail(c)
			} else {
				a.renderStudioSidebar(c)
			}
			ui.Divider(c).Vertical()

			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				switch a.activeTab {
				case TabDashboard:
					a.renderTabDashboard(c)
				case TabStore:
					a.renderTabStore(c)
				case TabSettings:
					a.renderTabSettings(c)
				default:
					a.renderTabOverview(c)
				}
			})
		})

		if a.statusErr && a.statusMsg != "" {
			ui.Row(c).FillWidth().Padding(8, 16).Background(t.Danger.Alpha(0.10)).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, a.statusMsg).FontSize(12).TextColor(t.Danger).Grow(1).MinWidth(0)
				if a.aiRun != nil && ui.Button(c.Key("status-ai-log"), T("KI-Ausgabe")).Tooltip(T("Was die KI geantwortet hat")).Clicked() {
					a.showAIRun = true
				}
				if ui.Button(c.Key("status-dismiss"), "✕").Tooltip(T("Ausblenden")).Clicked() {
					a.clearStatus()
				}
			})
		}
	})

	if c.Shortcut(ui.Cmd, ui.KeyBackslash) {
		a.toggleSidebar()
	}
	a.renderHelpModal(c)
	a.renderCreateDialog(c)
	a.renderAIRunDialog(c)

	for _, msg := range a.toasts {
		c.Toast(msg)
	}
	a.toasts = a.toasts[:0]
}

func (a *App) renderTopBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Padding(10, 16).Gap(10).AlignItems(ui.Center).Background(t.Surface).Children(func() {
		tip := T("Seitenleiste einklappen (⌘\\)")
		if a.sidebarCollapsed() {
			tip = T("Seitenleiste ausklappen (⌘\\)")
		}
		if ui.Button(c.Key("hdr-sidebar"), "☰").Tooltip(tip).Clicked() {
			a.toggleSidebar()
		}
		ui.Text(c, "OmaMac").FontSize(15).Bold()
		ui.Text(c, versionLabel()).FontSize(11.5).TextColor(t.TextMuted).
			Tooltip(T("Installierte Version – Updates unter Einstellungen → Allgemein"))
		ui.Spacer(c)
		if a.busy {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.busyLabel).FontSize(12).TextColor(t.TextMuted)
				if a.aiRun != nil && !a.showAIRun && textButton(c, "hdr-ai-show", T("Ausgabe ansehen")) {
					a.showAIRun = true
				}
			})
		}
		if ui.Button(c.Key("hdr-help-btn"), "?").Tooltip(T("Kurzanleitung")).Clicked() {
			a.showHelpModal = true
		}
		if ui.PrimaryButton(c.Key("hdr-new-panel"), T("+ Neues Panel")).Disabled(a.busy).Clicked() {
			a.openCreate("")
		}
	})
}

var studioTabIDs = map[StudioTab]string{
	TabDashboard: "dashboard",
	TabOverview:  "panels",
	TabStore:     "store",
	TabSettings:  "settings",
}

func (a *App) renderStudioSidebar(c *ui.Context) {
	t := c.Theme()
	enabledCnt, _ := a.countEnabledPanels()
	sel := studioTabIDs[a.activeTab]

	ui.Column(c).Width(200).FillHeight().Background(t.Surface).Children(func() {
		if ui.Sidebar(c.Key("nav"), &sel, func() {
			ui.SidebarItem(c, "dashboard", nil, "Dashboard")
			ui.SidebarItem(c, "panels", nil, "Panels").Children(func() {
				ui.Badge(c, fmt.Sprintf("%d/%d", enabledCnt, len(a.panels)))
			})
			ui.SidebarItem(c, "store", nil, T("Vorlagen"))
			ui.SidebarItem(c, "settings", nil, T("Einstellungen"))
		}).FillWidth().Grow(1).Changed() {
			for tab, id := range studioTabIDs {
				if id == sel {
					a.setTab(tab)
				}
			}
		}

		// The launcher is the other half of OmaMac: say how to reach it.
		launch := ui.ButtonBase(c.Key("side-launcher")).FillWidth().Padding(10, 14).Cursor(ui.CursorPointer)
		if launch.Hovered() {
			launch.Background(t.SurfaceHover)
		}
		launch.Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, T("Schnellstart")).FontSize(12).TextColor(t.TextMuted).Grow(1)
				ui.Text(c, shortcutLabel(a.Config.LauncherShortcut)).FontSize(11.5).TextColor(t.TextMuted)
			})
		})
		if launch.Clicked() {
			a.ToggleWalPanel()
		}
	})
}

// renderHelpModal is the short guide: shown once on the first start and from the "?" button.
func (a *App) renderHelpModal(c *ui.Context) {
	t := c.Theme()
	open := a.showHelpModal
	if ui.Modal(c.Key("help"), &a.showHelpModal, func() {
		ui.Column(c).Width(520).Gap(14).Children(func() {
			ui.Text(c, T("So funktioniert OmaMac")).FontSize(18).Bold()
			steps := []struct{ title, text string }{
				{"Dashboard", T("Alle Panels als Kacheln in einem Fenster, unten die Leiste für Apps und KI. Im Vollbild ein eigener Space – mit drei Fingern hinwischen.")},
				{"Panels", T("Der Schalter „Menüleiste“ zeigt ein Panel oben rechts in der Menüleiste, „Widget“ legt es schwebend auf den Desktop, „Dashboard“ zeigt es als Kachel.")},
				{T("Neues Panel"), T("Beschreibe in einem Satz, was angezeigt werden soll. Die KI baut daraus Panel und Abfrage-Skript – auch Anpassungen gehen per Satz.")},
				{T("Ohne KI im Betrieb"), T("Danach aktualisieren sich Panels lokal per Skript. Dabei wird keine KI genutzt und es fallen keine Tokens an.")},
				{T("Schnellstart ") + shortcutLabel(a.Config.LauncherShortcut), T("Apps starten, Fenster anordnen und Panels schalten – von überall.")},
				{T("Tresor"), T("Suchbegriffe und Tokens liegen im Schlüsselbund. Die KI sieht nur den Namen der Variable.")},
			}
			for i, s := range steps {
				ui.Row(c.Key(fmt.Sprintf("help-step-%d", i))).FillWidth().Gap(12).Children(func() {
					ui.Text(c, fmt.Sprint(i+1)).FontSize(12).Bold().TextColor(t.Accent).Width(14)
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, s.title).FontSize(13).Bold()
						ui.Text(c, s.text).FontSize(12).TextColor(t.TextMuted)
					})
				})
			}
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c.Key("help-close"), T("Los geht's")).Clicked() {
					a.showHelpModal = false
				}
			})
		})
	}).Dismissed() || (open && !a.showHelpModal) {
		a.closeHelpModal()
	}
}

func (a *App) closeHelpModal() {
	a.showHelpModal = false
	if !a.Config.OnboardingDismissed {
		a.Config.OnboardingDismissed = true
		_ = SaveConfig(a.ConfigPath, a.Config)
	}
}

// sidebarCollapsed: the sidebar shows only symbols. On the dashboard it is so unless
// opened there; elsewhere it is open unless collapsed there (⌘\ or ☰).
func (a *App) sidebarCollapsed() bool {
	if a.activeTab == TabDashboard {
		return !a.Config.DashboardSidebarOpen
	}
	return a.Config.SidebarCollapsed
}

func (a *App) toggleSidebar() {
	if a.activeTab == TabDashboard {
		a.Config.DashboardSidebarOpen = !a.Config.DashboardSidebarOpen
	} else {
		a.Config.SidebarCollapsed = !a.Config.SidebarCollapsed
	}
	_ = SaveConfig(a.ConfigPath, a.Config)
}

// renderSidebarRail is the collapsed sidebar: a symbol for each view.
func (a *App) renderSidebarRail(c *ui.Context) {
	t := c.Theme()
	items := []struct {
		tab          StudioTab
		glyph, label string
	}{
		{TabDashboard, "▦", "Dashboard"},
		{TabOverview, "☷", "Panels"},
		{TabStore, "✦", T("Vorlagen")},
		{TabSettings, "⚙", T("Einstellungen")},
	}
	ui.Column(c).Width(52).FillHeight().Background(t.Surface).Padding(10, 6).Gap(4).AlignItems(ui.Center).Children(func() {
		for _, it := range items {
			btn := ui.ButtonBase(c.Key("rail-"+studioTabIDs[it.tab])).Size(40, 36).Radius(8).Cursor(ui.CursorPointer).
				Justify(ui.Center).AlignItems(ui.Center).Tooltip(it.label)
			switch {
			case a.activeTab == it.tab:
				btn.Background(t.Accent.Alpha(0.16))
			case btn.Hovered():
				btn.Background(t.SurfaceHover)
			}
			glyph := it.glyph
			btn.Children(func() { ui.Text(c, glyph).FontSize(17) })
			if btn.Clicked() {
				a.setTab(it.tab)
			}
		}
		ui.Spacer(c)
		launch := ui.ButtonBase(c.Key("rail-launcher")).Size(40, 36).Radius(8).Cursor(ui.CursorPointer).
			Justify(ui.Center).AlignItems(ui.Center).Tooltip(T("Schnellstart ") + shortcutLabel(a.Config.LauncherShortcut))
		if launch.Hovered() {
			launch.Background(t.SurfaceHover)
		}
		launch.Children(func() { ui.Text(c, "⌥").FontSize(16).TextColor(t.TextMuted) })
		if launch.Clicked() {
			a.ToggleWalPanel()
		}
	})
}
