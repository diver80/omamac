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
			a.renderStudioSidebar(c)
			ui.Divider(c).Vertical()

			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				switch a.activeTab {
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
				if a.aiRun != nil && ui.Button(c.Key("status-ai-log"), "KI-Ausgabe").Tooltip("Was die KI geantwortet hat").Clicked() {
					a.showAIRun = true
				}
				if ui.Button(c.Key("status-dismiss"), "✕").Tooltip("Ausblenden").Clicked() {
					a.clearStatus()
				}
			})
		}
	})

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
		ui.Text(c, "OmaMac").FontSize(15).Bold()
		ui.Text(c, "v"+appVersion()).FontSize(11.5).TextColor(t.TextMuted).
			Tooltip("Installierte Version – Updates unter Einstellungen → Allgemein")
		ui.Spacer(c)
		if a.busy {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.busyLabel).FontSize(12).TextColor(t.TextMuted)
				if a.aiRun != nil && !a.showAIRun && textButton(c, "hdr-ai-show", "Ausgabe ansehen") {
					a.showAIRun = true
				}
			})
		}
		if ui.Button(c.Key("hdr-help-btn"), "?").Tooltip("Kurzanleitung").Clicked() {
			a.showHelpModal = true
		}
		if ui.PrimaryButton(c.Key("hdr-new-panel"), "+ Neues Panel").Disabled(a.busy).Clicked() {
			a.openCreate("")
		}
	})
}

var studioTabIDs = map[StudioTab]string{
	TabOverview: "panels",
	TabStore:    "store",
	TabSettings: "settings",
}

func (a *App) renderStudioSidebar(c *ui.Context) {
	t := c.Theme()
	enabledCnt, _ := a.countEnabledPanels()
	sel := studioTabIDs[a.activeTab]

	ui.Column(c).Width(200).FillHeight().Background(t.Surface).Children(func() {
		if ui.Sidebar(c.Key("nav"), &sel, func() {
			ui.SidebarItem(c, "panels", nil, "Panels").Children(func() {
				ui.Badge(c, fmt.Sprintf("%d/%d", enabledCnt, len(a.panels)))
			})
			ui.SidebarItem(c, "store", nil, "Vorlagen")
			ui.SidebarItem(c, "settings", nil, "Einstellungen")
		}).FillWidth().Grow(1).Changed() {
			for tab, id := range studioTabIDs {
				if id == sel {
					a.activeTab = tab
				}
			}
			if a.activeTab == TabSettings {
				a.AgyStatus = InspectAntigravityAuth(a.Config)
			}
		}

		// The launcher is the other half of OmaMac: say how to reach it.
		launch := ui.ButtonBase(c.Key("side-launcher")).FillWidth().Padding(10, 14).Cursor(ui.CursorPointer)
		if launch.Hovered() {
			launch.Background(t.SurfaceHover)
		}
		launch.Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Schnellstart").FontSize(12).TextColor(t.TextMuted).Grow(1)
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
			ui.Text(c, "So funktioniert OmaMac").FontSize(18).Bold()
			steps := []struct{ title, text string }{
				{"Panels", "Der Schalter „Menüleiste“ zeigt ein Panel oben rechts in der Menüleiste, „Widget“ legt es schwebend auf den Desktop."},
				{"Neues Panel", "Beschreibe in einem Satz, was angezeigt werden soll. Die KI baut daraus Panel und Abfrage-Skript – auch Anpassungen gehen per Satz."},
				{"Ohne KI im Betrieb", "Danach aktualisieren sich Panels lokal per Skript. Dabei wird keine KI genutzt und es fallen keine Tokens an."},
				{"Schnellstart " + shortcutLabel(a.Config.LauncherShortcut), "Apps starten, Fenster anordnen und Panels schalten – von überall."},
				{"Tresor", "Suchbegriffe und Tokens liegen im Schlüsselbund. Die KI sieht nur den Namen der Variable."},
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
				if ui.PrimaryButton(c.Key("help-close"), "Los geht's").Clicked() {
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
