package omamac

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// RenderWalPanelView renders the floating launcher summoned with the global shortcut.
func (a *App) RenderWalPanelView(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Padding(14, 16).Gap(12).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, "OmaMac").FontSize(15).Bold().SingleLine()
			ui.Spacer(c)
			ui.Segmented(c.Key("wal-mode-seg"), &a.walMode, "Panels", "Vorlagen", "Fenster & Apps")
			if ui.Button(c.Key("wal-open-studio"), "Hauptfenster").Clicked() {
				if a.studioWin != nil {
					a.studioWin.Show()
					a.studioWin.Focus()
				}
				if a.walWin != nil {
					a.walWin.Hide()
				}
			}
			if ui.Button(c.Key("wal-close"), "✕").Tooltip("Schließen").Clicked() {
				if a.walWin != nil {
					a.walWin.Hide()
				}
			}
		})

		if a.busy {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.busyLabel).FontSize(12).TextColor(t.TextMuted)
			})
		}

		switch a.walMode {
		case 0:
			a.renderPanelList(c, "wal", false)
			a.renderWalCreateBar(c)
		case 1:
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				a.renderStoreCatalogList(c, "wal")
			})
		default:
			a.renderWalLauncherMode(c)
		}
	})
}

// renderWalCreateBar creates a new panel from one sentence.
func (a *App) renderWalCreateBar(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.TextInput(c.Key("omni-bar-input"), &a.prompt1Text).
			Placeholder("Neues Panel beschreiben, z. B. „Docker-Container“ …").
			Grow(1)
		if ui.PrimaryButton(c.Key("omni-bar-create"), "Erstellen").
			Disabled(a.busy || strings.TrimSpace(a.prompt1Text) == "").Clicked() {
			a.triggerPrompt1Create()
		}
	})
}

func (a *App) renderWalLauncherMode(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Grow(1).FillWidth().Gap(10).Children(func() {
		// Quick Window Tiling Row
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Fenster anordnen:").FontSize(11.5).Bold().TextColor(t.TextMuted)
			tiles := []struct {
				label  string
				layout TileLayout
			}{
				{"Links 50 %", TileLeftHalf},
				{"Rechts 50 %", TileRightHalf},
				{"Vollbild", TileMaximize},
				{"Zentriert 80 %", TileCenter80},
				{"Unten 50 %", TileBottomHalf},
			}
			for _, tl := range tiles {
				tlCopy := tl
				if ui.Button(c.Key("wal-tile-"+string(tlCopy.layout)), tlCopy.label).Clicked() {
					disp := mygo.Screen.PrimaryDisplay()
					if a.walWin != nil {
						a.walWin.Hide()
					}
					_ = ApplyWindowTile(tlCopy.layout, disp.WorkArea.X, disp.WorkArea.Y, disp.WorkArea.Width, disp.WorkArea.Height)
				}
			}
		})

		ui.TextInput(c.Key("wal-app-search"), &a.launcherQuery).
			Placeholder("macOS-Programm suchen (/Applications)...").
			FillWidth()

		q := strings.ToLower(strings.TrimSpace(a.launcherQuery))
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				shown := 0
				for _, app := range a.apps {
					if q != "" && !strings.Contains(strings.ToLower(app.Title), q) {
						continue
					}
					appCopy := app
					ui.Row(c.Key("wal-app-"+appCopy.ID)).FillWidth().Padding(6, 10).Radius(6).Background(t.Surface).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, appCopy.Icon).FontSize(14)
						ui.Column(c).Grow(1).Children(func() {
							ui.Text(c, appCopy.Title).FontSize(12.5).Bold()
							ui.Text(c, appCopy.Subtitle).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
						})
						if ui.Button(c.Key("launch-"+appCopy.ID), "Starten").Clicked() {
							if a.walWin != nil {
								a.walWin.Hide()
							}
							_ = appCopy.Run()
						}
					})
					shown++
					if shown >= 25 {
						break
					}
				}
			})
		})
	})
}
