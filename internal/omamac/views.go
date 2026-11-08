package omamac

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

func pillBadge(c *ui.Context, label string, bg, fg ui.Color) ui.Element {
	return ui.Text(c, label).FontSize(10.5).Bold().Padding(2, 8).Radius(10).Background(bg).TextColor(fg).SingleLine()
}

func statusColors(t *ui.Theme, status string) (ui.Color, ui.Color) {
	switch strings.ToLower(status) {
	case "crit", "error", "danger":
		return t.Danger.Alpha(0.18), t.Danger
	case "warn", "warning":
		return t.Warning.Alpha(0.20), t.Warning
	case "info":
		return t.Accent.Alpha(0.18), t.Accent
	default:
		return t.Success.Alpha(0.18), t.Success
	}
}

// RenderWalPanelView renders the floating Walker/OmaLauncher-inspired HUD panel summoned via Alt+Space.
func (a *App) RenderWalPanelView(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Padding(14, 16).Gap(10).Children(func() {
		// Top drag & mode bar
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Text(c, "⌘ OmaMac Schnellstart").FontSize(15).Bold().TextColor(t.Accent)
			pillBadge(c, "0 TOKENS BEIM POLLING", t.Success.Alpha(0.18), t.Success)
			ui.Spacer(c)

			ui.Segmented(c.Key("wal-mode-seg"), &a.walMode, "🎛️ Übersicht & KI", "🏪 Vorlagen-Store", "🪟 Fenster & Apps")

			if ui.Button(c.Key("wal-open-studio"), "⚙ Hauptfenster ↗").Clicked() {
				if a.studioWin != nil {
					a.studioWin.Show()
					a.studioWin.Focus()
				}
				if a.walWin != nil {
					a.walWin.Hide()
				}
			}
			if ui.Button(c.Key("wal-close"), "✕").Clicked() {
				if a.walWin != nil {
					a.walWin.Hide()
				}
			}
		})

		if a.busy {
			ui.Row(c).FillWidth().Padding(6, 10).Radius(6).Background(t.Accent.Alpha(0.12)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.busyLabel).FontSize(12).Bold().TextColor(t.Accent)
			})
		}

		switch a.walMode {
		case 0:
			a.renderWalPromptMode(c)
		case 1:
			a.renderWalStoreMode(c)
		default:
			a.renderWalLauncherMode(c)
		}

		// Bottom Unified Omni-Bar
		a.renderOmaLauncherOmniBar(c)
	})
}

func (a *App) renderWalPromptMode(c *ui.Context) {
	t := c.Theme()
	provName := "Gemini"
	if a.Config.ActiveProvider == ProviderClaude {
		provName = "Claude"
	}
	enabledCnt, floatingCnt := a.countEnabledPanels()

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			// Box 1: Schnell-Übersicht mit einfachem Switch zum Aktivieren/Deaktivieren!
			ui.Column(c).FillWidth().Padding(12, 14).Radius(8).Border(1.5, t.Accent.Alpha(0.75)).Background(t.Surface).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "🎛️ Meine Status-Panels (Schnell-Schalter An / Aus)").FontSize(13.5).Bold()
					pillBadge(c, fmt.Sprintf("%d in Menüleiste • %d Widget", enabledCnt, floatingCnt), t.Success.Alpha(0.18), t.Success)
					ui.Spacer(c)
					if ui.Button(c.Key("wal-all-on"), "Alle An").Clicked() {
						a.setAllPanelsEnabled(true)
					}
					if ui.Button(c.Key("wal-all-off"), "Alle Aus").Clicked() {
						a.setAllPanelsEnabled(false)
					}
				})

				for _, p := range a.panels {
					pCopy := p
					rowBg := t.Background
					borderCol := t.Border
					if pCopy.Enabled {
						borderCol = t.Success.Alpha(0.55)
					}
					ui.Row(c.Key("wal-ov-row-"+pCopy.ID)).FillWidth().Padding(7, 10).Radius(6).Border(1, borderCol).Background(rowBg).AlignItems(ui.Center).Gap(10).Children(func() {
						if pCopy.Enabled {
							pillBadge(c, "AKTIV", t.Success.Alpha(0.2), t.Success)
						} else {
							pillBadge(c, "AUS", t.Border.Alpha(0.5), t.TextMuted)
						}
						ui.Text(c, pCopy.Icon+" "+pCopy.Title).FontSize(12.5).Bold().Grow(1).SingleLine()
						sBg, sFg := statusColors(t, pCopy.Status)
						pillBadge(c, pCopy.TrayLabel(), sBg, sFg)

						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "Menüleiste").FontSize(11).TextColor(t.TextMuted)
							if ui.Switch(c.Key("wal-sw-tray-"+pCopy.ID), &pCopy.Enabled).Changed() {
								stateTxt := "deaktiviert"
								if pCopy.Enabled {
									stateTxt = "in Menüleiste aktiviert"
								}
								a.persistPanelToggle(pCopy, fmt.Sprintf("Panel '%s' %s", pCopy.Title, stateTxt))
							}
						})

						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "Widget").FontSize(11).TextColor(t.TextMuted)
							if ui.Switch(c.Key("wal-sw-float-"+pCopy.ID), &pCopy.PinnedFloating).Changed() {
								stateTxt := "Desktop-Widget ausgeblendet"
								if pCopy.PinnedFloating {
									stateTxt = "als Desktop-Widget angepinnt"
								}
								a.persistPanelToggle(pCopy, fmt.Sprintf("'%s' %s", pCopy.Title, stateTxt))
							}
						})
					})
				}
			})

			// Box 2: OmaLauncher Quick-Card (Neues Panel per KI + Theme-Auswahl)
			ui.Column(c).FillWidth().Padding(12, 14).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("✨ Neues Element mit %s erstellen (Eingabe unten)", provName)).FontSize(13).Bold()
					ui.Spacer(c)
					pillBadge(c, a.AI.ActiveProviderSummary(), t.Accent.Alpha(0.16), t.Accent)
				})

				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Art:").FontSize(11.5).TextColor(t.TextMuted)
					kinds := []string{"[ Widget ]", "[ Mini-App ]", "[ Menüleisten-Panel ]"}
					for idx, kLbl := range kinds {
						idxCopy := idx
						if a.createKindIdx == idxCopy {
							if ui.PrimaryButton(c.Key(fmt.Sprintf("wal-kind-%d", idxCopy)), kLbl).Clicked() {
								a.createKindIdx = idxCopy
							}
						} else {
							if ui.Button(c.Key(fmt.Sprintf("wal-kind-%d", idxCopy)), kLbl).Clicked() {
								a.createKindIdx = idxCopy
							}
						}
					}
					ui.Spacer(c)
					ui.Segmented(c.Key("wal-grid-seg"), &a.gridSizeIdx, "3×3 Kompakt", "6×2 Breit")
					if ui.Button(c.Key("wal-browse-store"), "🏪 Store ▸").Clicked() {
						a.walMode = 1
					}
				})

				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, "FARBSCHEMA (THEME)").FontSize(10.5).Bold().TextColor(t.Accent)
					ui.Spacer(c)
					ui.Text(c, a.Config.ThemeName).FontSize(11).TextColor(t.TextMuted)
				})
				a.renderThemeSwatchRow(c, "wal")
			})
		})
	})
}

func (a *App) renderThemeSwatchRow(c *ui.Context, prefix string) {
	t := c.Theme()
	swatches := []struct {
		name string
		bg   ui.Color
		fg   ui.Color
		bars [5]ui.Color
	}{
		{
			name: "TokyoNight",
			bg:   ui.RGB(26, 27, 38),
			fg:   ui.RGB(192, 202, 245),
			bars: [5]ui.Color{ui.RGB(122, 162, 247), ui.RGB(247, 118, 142), ui.RGB(224, 175, 104), ui.RGB(158, 206, 106), ui.RGB(187, 154, 247)},
		},
		{
			name: "Vantablack",
			bg:   ui.RGB(8, 8, 10),
			fg:   ui.RGB(210, 210, 215),
			bars: [5]ui.Color{ui.RGB(90, 90, 96), ui.RGB(120, 120, 128), ui.RGB(150, 150, 158), ui.RGB(180, 180, 188), ui.RGB(215, 215, 222)},
		},
		{
			name: "White",
			bg:   ui.RGB(245, 246, 250),
			fg:   ui.RGB(30, 32, 42),
			bars: [5]ui.Color{ui.RGB(110, 114, 128), ui.RGB(75, 80, 95), ui.RGB(55, 60, 74), ui.RGB(40, 44, 56), ui.RGB(24, 26, 34)},
		},
		{
			name: "Catppuccin Mocha",
			bg:   ui.RGB(30, 30, 46),
			fg:   ui.RGB(205, 214, 244),
			bars: [5]ui.Color{ui.RGB(137, 180, 250), ui.RGB(243, 139, 168), ui.RGB(249, 226, 175), ui.RGB(166, 227, 161), ui.RGB(203, 166, 247)},
		},
	}

	ui.Row(c).FillWidth().Gap(8).Children(func() {
		for idx, sw := range swatches {
			swCopy := sw
			selected := strings.EqualFold(a.Config.ThemeName, swCopy.name)
			borderCol := t.Border
			if selected {
				borderCol = t.Accent
			}
			card := ui.ButtonBase(c.Key(fmt.Sprintf("%s-sw-%d", prefix, idx))).
				Grow(1).Padding(8, 10).Radius(6).Border(1.5, borderCol).Background(swCopy.bg).Cursor(ui.CursorPointer)
			card.Children(func() {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					ui.Text(c, strings.ToLower(swCopy.name)).FontSize(11.5).Bold().TextColor(swCopy.fg).SingleLine()
					ui.Row(c).FillWidth().Gap(4).Children(func() {
						for _, bCol := range swCopy.bars {
							ui.Row(c).Grow(1).Height(5).Radius(2).Background(bCol)
						}
					})
				})
			})
			if card.Clicked() {
				a.Config.ThemeName = swCopy.name
				a.themeSelect = swCopy.name
				_ = SaveConfig(a.ConfigPath, a.Config)
				a.toast("Farbschema gewechselt zu " + swCopy.name)
			}
		}
	})
}

func (a *App) findMatchingPanelForStore(it StoreCategoryItem) *PanelSpec {
	if it.Spec == nil {
		return nil
	}
	for _, p := range a.panels {
		if p.ID == it.Spec.ID || strings.HasPrefix(p.ID, it.Spec.ID+"-") || strings.EqualFold(p.Title, it.Spec.Title) {
			return p
		}
	}
	return nil
}

func (a *App) renderStoreCatalogList(c *ui.Context, keyPrefix string) {
	t := c.Theme()
	catalog := StoreCatalog()

	for idx, item := range catalog {
		itCopy := item
		existing := a.findMatchingPanelForStore(itCopy)
		isEnabled := existing != nil && existing.Enabled

		borderCol := t.Border
		if isEnabled {
			borderCol = t.Success.Alpha(0.6)
		}

		ui.Row(c.Key(fmt.Sprintf("%s-store-item-%d", keyPrefix, idx))).
			FillWidth().Padding(12, 14).Radius(8).Border(1, borderCol).Background(t.Surface).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, itCopy.Icon).FontSize(22)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					pillBadge(c, strings.ToUpper(itCopy.Category), t.Accent.Alpha(0.18), t.Accent)
					pillBadge(c, fmt.Sprintf("%s • %s", itCopy.Kind, itCopy.GridSize), t.Border.Alpha(0.6), t.TextMuted)
					ui.Text(c, itCopy.Title).FontSize(13.5).Bold().SingleLine()
				})
				ui.Text(c, itCopy.Subtitle).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
			})

			// Direct Switch to activate/deactivate right from the Store!
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if isEnabled {
					pillBadge(c, "AKTIV", t.Success.Alpha(0.2), t.Success)
				} else {
					pillBadge(c, "AUS", t.Border.Alpha(0.5), t.TextMuted)
				}
				swVal := isEnabled
				if ui.Switch(c.Key(fmt.Sprintf("%s-store-sw-%d", keyPrefix, idx)), &swVal).Changed() {
					if existing != nil {
						existing.Enabled = swVal
						a.persistPanelToggle(existing, fmt.Sprintf("Vorlage '%s' %s", existing.Title, map[bool]string{true: "aktiviert", false: "deaktiviert"}[swVal]))
					} else if swVal && itCopy.Spec != nil {
						clone := *itCopy.Spec
						clone.Enabled = true
						_ = SavePanel(a.Config.PanelsDir, &clone)
						a.panels = append(a.panels, &clone)
						a.selectPanel(clone.ID)
						a.syncAllPanelRuntimes()
						a.toast("Vorlage '" + clone.Title + "' installiert & aktiviert!")
					}
				}
			})

			if ui.Button(c.Key(fmt.Sprintf("%s-store-prompt-%d", keyPrefix, idx)), "✨ Als KI-Vorlage").Clicked() {
				a.prompt1Text = itCopy.Prompt
				a.activeTab = TabPromptStudio
				a.walMode = 0
				a.toast("Prompt-Vorlage geladen — jetzt beliebig anpassen & starten")
			}
		})
	}
}

func (a *App) renderWalStoreMode(c *ui.Context) {
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "🏪 Fertige Panels & Widgets — Einfach per Switch einschalten").FontSize(13.5).Bold()
				ui.Spacer(c)
				if ui.Button(c.Key("wal-store-back"), "← Zurück zur Übersicht").Clicked() {
					a.walMode = 0
				}
			})
			a.renderStoreCatalogList(c, "wal")
		})
	})
}

func (a *App) renderOmaLauncherOmniBar(c *ui.Context) {
	t := c.Theme()
	provShort := "gemini ▾"
	if a.Config.ActiveProvider == ProviderClaude {
		provShort = "claude ▾"
	}

	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Padding(6, 10).Radius(8).Border(1.5, t.Accent.Alpha(0.65)).Background(t.Surface).AlignItems(ui.Center).Gap(8).Children(func() {
			if ui.Button(c.Key("omni-prov-switch"), provShort).
				Tooltip("Klicken, um zwischen Google Gemini (Antigravity) und Anthropic Claude zu wechseln").Clicked() {
				if a.Config.ActiveProvider == ProviderGemini {
					a.Config.ActiveProvider = ProviderClaude
					a.providerIdx = 1
				} else {
					a.Config.ActiveProvider = ProviderGemini
					a.providerIdx = 0
				}
				_ = SaveConfig(a.ConfigPath, a.Config)
			}

			ui.TextInput(c.Key("omni-bar-input"), &a.prompt1Text).
				Placeholder(fmt.Sprintf("Neues Panel per KI beschreiben (z. B. „mydealz 3D-Drucker Deals“ oder „Docker Container“)... (%s)", strings.TrimSuffix(provShort, " ▾"))).
				Grow(1)

			if ui.PrimaryButton(c.Key("omni-bar-create"), "✨ Erstellen ↵").
				Disabled(a.busy || strings.TrimSpace(a.prompt1Text) == "").Clicked() {
				a.triggerPrompt1Create()
			}
		})
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
				{"⬅ Links 50%", TileLeftHalf},
				{"➡ Rechts 50%", TileRightHalf},
				{"⬆ Vollbild", TileMaximize},
				{"🎯 Zentriert 80%", TileCenter80},
				{"⬇ Unten 50%", TileBottomHalf},
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
						if ui.Button(c.Key("launch-"+appCopy.ID), "Starten ↵").Clicked() {
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

// renderNativePanelPopover renders a PanelSpec of Type == "native" using GPU-accelerated mygo/ui widgets.
func (a *App) renderNativePanelPopover(c *ui.Context, p *PanelSpec, rt *panelRuntime) {
	if p.BuiltinKind == BuiltinJiraQuickAccess {
		a.renderJiraQuickAccessModule(c, p)
		return
	}
	t := c.Theme()
	sBg, sFg := statusColors(t, p.Status)

	ui.Column(c).Fill().Padding(14).Gap(10).Children(func() {
		// Header
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).DragWindow().Children(func() {
			ui.Text(c, p.Icon+" "+p.Title).FontSize(14).Bold().Grow(1).SingleLine()
			pillBadge(c, strings.ToUpper(p.Status)+" • "+p.BadgeText, sBg, sFg)
			if p.IsBuiltin {
				pillBadge(c, "🔒 SYSTEM-MODUL", t.Accent.Alpha(0.18), t.Accent)
			}
			if ui.Button(c.Key("pop-ref-"+p.ID), "↻").Tooltip("Daten jetzt aktualisieren").Clicked() {
				a.refreshPanelAsync(p, "")
			}
		})

		if p.Subtitle != "" {
			ui.Text(c, p.Subtitle).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}

		// Metrics Grid (2 per row)
		if len(p.Metrics) > 0 {
			ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
				for idx, m := range p.Metrics {
					mBg, mFg := statusColors(t, m.Status)
					ui.Column(c.Key(fmt.Sprintf("m-%s-%d", p.ID, idx))).
						Grow(1).MinWidth(150).Padding(10).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(4).Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
							ui.Text(c, m.Label).FontSize(10.5).Bold().TextColor(t.TextMuted).Grow(1).SingleLine()
							if m.Unit != "" {
								pillBadge(c, m.Unit, mBg, mFg)
							}
						})
						ui.Text(c, m.Value).FontSize(18).Bold().TextColor(mFg)
						if m.Progress > 0 {
							pct := m.Progress
							if pct > 100 {
								pct = 100
							}
							ui.Row(c).FillWidth().Height(5).Radius(3).Background(t.Border).Children(func() {
								ui.Row(c).Width(float32(pct * 1.5)).FillHeight().Radius(3).Background(mFg)
							})
						}
					})
				}
			})
		}

		// Structured Rows (supports clickable URL per row for mydealz/Idealo deals!)
		if len(p.Rows) > 0 {
			ui.Column(c).FillWidth().Padding(8, 10).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(6).Children(func() {
				for idx, r := range p.Rows {
					rCopy := r
					rBg, rFg := statusColors(t, rCopy.Status)
					ui.Row(c.Key(fmt.Sprintf("r-%s-%d", p.ID, idx))).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, rCopy.Left).FontSize(11.5).TextColor(t.TextMuted).Grow(1).SingleLine()
						if rCopy.URL != "" {
							if ui.Link(c.Key(fmt.Sprintf("rl-%s-%d", p.ID, idx)), rCopy.Right, rCopy.URL).FontSize(11.5).Clicked() {
								_ = mygo.Shell.OpenExternal(rCopy.URL)
							}
						} else {
							ui.Text(c, rCopy.Right).FontSize(11.5).Bold().SingleLine()
						}
						if rCopy.Badge != "" {
							pillBadge(c, rCopy.Badge, rBg, rFg)
						}
					})
				}
			})
		}

		// Interactive Actions
		if len(p.Actions) > 0 {
			ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
				for idx, act := range p.Actions {
					actCopy := act
					key := c.Key(fmt.Sprintf("act-%s-%d", p.ID, idx))
					clicked := false
					if actCopy.Style == "primary" {
						clicked = ui.PrimaryButton(key, actCopy.Label).Clicked()
					} else {
						clicked = ui.Button(key, actCopy.Label).Clicked()
					}
					if clicked {
						if actCopy.Command != "" {
							cmd := exec.Command("/bin/zsh", "-c", actCopy.Command)
							if a.Vault != nil {
								cmd.Env = append(cmd.Environ(), a.Vault.EnvSlice()...)
							}
							_ = cmd.Start()
						}
						if actCopy.URL != "" {
							_ = mygo.Shell.OpenExternal(actCopy.URL)
						}
					}
				}
			})
		}

		ui.Spacer(c)

		// Inline Prompt #2 Quick-Refiner right inside the popover (not for pre-delivered system modules)
		if rt != nil && !p.IsBuiltin {
			ui.Divider(c)
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c.Key("pop-p2-"+p.ID), &rt.quickRefIn).
					Placeholder("✨ Mit KI anpassen (z. B. „Warnung rot färben“)...").
					Grow(1)
				if ui.PrimaryButton(c.Key("pop-p2-btn-"+p.ID), "Anpassen").
					Disabled(a.busy || strings.TrimSpace(rt.quickRefIn) == "").Clicked() {
					promptTxt := rt.quickRefIn
					rt.quickRefIn = ""
					a.triggerPrompt2Refine(p.ID, promptTxt)
				}
			})
		}
	})
}

// RenderStudioView renders the main OmaMac Control Center & Overview window.
func (a *App) RenderStudioView(c *ui.Context) {
	t := c.Theme()
	enabledCnt, floatingCnt := a.countEnabledPanels()

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		// Top Header Bar
		ui.Row(c).FillWidth().Padding(10, 16).Gap(10).AlignItems(ui.Center).Background(t.Surface).Children(func() {
			ui.Text(c, "⌘ OmaMac").FontSize(17).Bold().TextColor(t.Accent)
			pillBadge(c, fmt.Sprintf("🟢 %d IN MENÜLEISTE • %d WIDGET", enabledCnt, floatingCnt), t.Success.Alpha(0.18), t.Success)
			pillBadge(c, a.AI.ActiveProviderSummary(), t.Accent.Alpha(0.16), t.Accent)

			ui.Spacer(c)

			if a.busy {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, a.busyLabel).FontSize(12).TextColor(t.Accent)
				})
			}

			if ui.Button(c.Key("hdr-help-btn"), "❓ Kurzanleitung").Clicked() {
				a.showHelpModal = !a.showHelpModal
			}
			if ui.Button(c.Key("hdr-wal-toggle"), "Schnellstart-Panel ("+a.Config.LauncherShortcut+")").Clicked() {
				a.ToggleWalPanel()
			}
			if ui.PrimaryButton(c.Key("hdr-refresh-all"), "↻ Alle aktualisieren").Clicked() {
				go func() {
					a.refreshDuePanels(true)
					a.updateUI(func() {
						a.toast("Alle aktiven Panels aktualisiert (0 KI-Tokens verbraucht)")
					})
				}()
			}
		})

		ui.Divider(c)

		// Main Body: Sidebar + Content Area
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			a.renderStudioSidebar(c)
			ui.Divider(c).Vertical()

			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
				if a.showHelpModal {
					a.renderHelpOnboardingModal(c)
				} else {
					switch a.activeTab {
					case TabOverview:
						a.renderTabOverview(c)
					case TabStore:
						a.renderTabStore(c)
					case TabPromptStudio:
						a.renderTabPromptStudio(c)
					case TabWindowManager:
						a.renderTabWindowManager(c)
					case TabSettings:
						a.renderTabSettings(c)
					default:
						a.renderTabOverview(c)
					}
				}
			})
		})

		ui.Divider(c)
		// Status Footer
		ui.Row(c).FillWidth().Padding(6, 16).Background(t.Surface).AlignItems(ui.Center).Gap(8).Children(func() {
			col := t.TextMuted
			if a.statusErr {
				col = t.Danger
			}
			ui.Text(c, a.statusMsg).FontSize(11.5).TextColor(col).Grow(1).SingleLine()
			pillBadge(c, "⚡ DAUERBETRIEB = 0 KI-TOKENS", t.Success.Alpha(0.16), t.Success)
			ui.Text(c, fmt.Sprintf("Ordner: %s", a.Config.PanelsDir)).FontSize(11).TextColor(t.TextMuted)
		})
	})

	for _, msg := range a.toasts {
		c.Toast(msg)
	}
	a.toasts = a.toasts[:0]
}

func (a *App) renderStudioSidebar(c *ui.Context) {
	t := c.Theme()
	enabledCnt, _ := a.countEnabledPanels()
	vaultCnt := 0
	if a.Vault != nil {
		vaultCnt = len(a.Vault.Entries)
	}
	navItems := []struct {
		tab      StudioTab
		label    string
		subtitle string
		badge    string
	}{
		{TabOverview, "🎛️ Übersicht & Schalter", "Panels ein-/ausschalten", fmt.Sprintf("%d/%d an", enabledCnt, len(a.panels))},
		{TabStore, "🏪 Vorlagen-Store", "Fertige Panels & Widgets", "5 Kategorien"},
		{TabPromptStudio, "✨ KI-Studio", "Neu erstellen & anpassen", "2-Prompt"},
		{TabWindowManager, "🪟 Fenster & Apps", "Fenster-Tiling & Starter", fmt.Sprintf("%d Apps", len(a.apps))},
		{TabSettings, "🔐 KI & Tresor", "Gemini, Claude & Keys", fmt.Sprintf("%d Vars", vaultCnt)},
	}

	ui.Column(c).Width(245).FillHeight().Padding(12, 10).Gap(6).Background(t.Surface).Children(func() {
		ui.Text(c, "NAVIGATION").FontSize(10).Bold().TextColor(t.TextMuted).Padding(4, 8)
		for _, item := range navItems {
			it := item
			active := a.activeTab == it.tab && !a.showHelpModal
			btn := ui.ButtonBase(c.Key(fmt.Sprintf("side-%d", it.tab))).
				FillWidth().Padding(9, 10).Radius(8).Cursor(ui.CursorPointer)
			fg := t.Text
			if active {
				btn.Background(t.Accent.Alpha(0.16))
				fg = t.Accent
			} else if btn.Hovered() {
				btn.Background(t.SurfaceHover)
			}
			btn.Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Text(c, it.label).FontSize(12.5).Bold().TextColor(fg).Grow(1).SingleLine()
						pillBadge(c, it.badge, t.Border.Alpha(0.6), t.TextMuted)
					})
					ui.Text(c, it.subtitle).FontSize(10.5).TextColor(t.TextMuted).SingleLine()
				})
			})
			if btn.Clicked() {
				a.showHelpModal = false
				a.activeTab = it.tab
				if it.tab == TabSettings {
					a.AgyStatus = InspectAntigravityAuth(a.Config)
				}
			}
		}

		ui.Spacer(c)

		// Compact explanation box at bottom of sidebar
		ui.Column(c).FillWidth().Padding(10).Radius(8).Border(1, t.Border).Gap(4).Children(func() {
			ui.Text(c, "💡 Kurz erklärt").FontSize(11).Bold().TextColor(t.Accent)
			ui.Text(c, "• Schalter An = Erscheint oben in deiner macOS-Menüleiste\n• Widget An = Schwebt auf dem Desktop\n• 0 KI-Tokens beim Live-Polling!").
				FontSize(10.5).TextColor(t.TextMuted)
		})
	})
}

// renderTabOverview renders the main self-explanatory dashboard with simple On/Off Switches for each panel
// on the left and a live interactive preview + quick AI refiner on the right.
func (a *App) renderTabOverview(c *ui.Context) {
	t := c.Theme()
	enabledCnt, floatingCnt := a.countEnabledPanels()

	ui.Column(c).Grow(1).FillWidth().Padding(16).Gap(12).Children(func() {
		// Collapsible 3-Step Quick Guide so the UI is immediately understandable
		if !a.hideQuickGuide {
			ui.Row(c).FillWidth().Padding(12, 16).Radius(8).Border(1, t.Accent.Alpha(0.45)).Background(t.Surface).AlignItems(ui.Center).Gap(14).Children(func() {
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						pillBadge(c, "SO EINFACH GEHT'S", t.Accent.Alpha(0.2), t.Accent)
						ui.Text(c, "Willkommen in der OmaMac Übersicht — Steuere deine macOS Status-Panels mit einem Klick").FontSize(13).Bold()
					})
					ui.Text(c, "1️⃣ Schalter „Menüleiste“ umlegen → Das Panel erscheint sofort oben rechts in deiner macOS-Menüleiste (oder als „Desktop-Widget“).\n2️⃣ Auf eine Karte klicken → Rechts siehst du sofort die Live-Vorschau und kannst das Panel per KI-Satz anpassen.").
						FontSize(11.5).TextColor(t.TextMuted)
				})
				if ui.Button(c.Key("ov-hide-guide"), "Hinweis ausblenden ✕").Clicked() {
					a.hideQuickGuide = true
					a.Config.OnboardingDismissed = true
					_ = SaveConfig(a.ConfigPath, a.Config)
				}
			})
		}

		// Main Split: Left = Overview List with Switches | Right = Live Preview & Quick Refine
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Gap(16).Children(func() {
			// Left Column: Overview List of all Panels with Switches
			ui.Column(c).Grow(1).FillHeight().Gap(10).Children(func() {
				// List Header Bar (wraps so the action buttons never get clipped)
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
					ui.Column(c).Grow(1).MinWidth(280).Gap(2).Children(func() {
						ui.Text(c, "Meine Status-Panels & Desktop-Widgets").FontSize(15.5).Bold()
						ui.Text(c, fmt.Sprintf("%d von %d in der Menüleiste aktiv • %d als Desktop-Widget angepinnt", enabledCnt, len(a.panels), floatingCnt)).
							FontSize(11.5).TextColor(t.TextMuted)
					})
					if ui.Button(c.Key("ov-all-on"), "Alle aktivieren").Clicked() {
						a.setAllPanelsEnabled(true)
					}
					if ui.Button(c.Key("ov-all-off"), "Alle deaktivieren").Clicked() {
						a.setAllPanelsEnabled(false)
					}
					if ui.Button(c.Key("ov-go-store"), "🏪 + Aus Store").Clicked() {
						a.activeTab = TabStore
					}
					if ui.PrimaryButton(c.Key("ov-go-ai"), "✨ + Neues KI-Panel").Clicked() {
						a.activeTab = TabPromptStudio
					}
				})

				// Scrollable Panel Cards with clear Switches
				ui.Scroll(c).Grow(1).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						for _, p := range a.panels {
							pCopy := p
							isSelected := a.selectedPanelID == pCopy.ID
							sBg, sFg := statusColors(t, pCopy.Status)

							borderCol := t.Border
							borderW := float32(1.0)
							cardBg := t.Surface
							if isSelected {
								borderCol = t.Accent
								borderW = 2.0
								cardBg = t.Accent.Alpha(0.08)
							} else if pCopy.Enabled {
								borderCol = t.Success.Alpha(0.55)
							}

							ui.Column(c.Key("ov-card-"+pCopy.ID)).
								FillWidth().Padding(12, 14).Radius(10).Border(borderW, borderCol).Background(cardBg).Gap(8).
								Cursor(ui.CursorPointer).
								OnClick(func() {
									a.selectPanel(pCopy.ID)
								}).Children(func() {
								// Row 1: Status + Title + Live Badge + Simple ON/OFF Switches!
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
									if pCopy.Enabled {
										pillBadge(c, "🟢 AKTIV", t.Success.Alpha(0.20), t.Success)
									} else {
										pillBadge(c, "⚪ INAKTIV", t.Border.Alpha(0.5), t.TextMuted)
									}

									ui.Text(c, pCopy.Icon+" "+pCopy.Title).FontSize(14).Bold().Grow(1).MinWidth(160).SingleLine()

									// Switch 1: Menüleiste An/Aus
									ui.Row(c).Padding(4, 8).Radius(6).Background(t.Background).AlignItems(ui.Center).Gap(6).Children(func() {
										ui.Text(c, "Menüleiste").FontSize(11.5).Bold()
										if ui.Switch(c.Key("ov-sw-tray-"+pCopy.ID), &pCopy.Enabled).Changed() {
											a.selectPanel(pCopy.ID)
											stateTxt := "deaktiviert"
											if pCopy.Enabled {
												stateTxt = "in der macOS-Menüleiste aktiviert"
											}
											a.persistPanelToggle(pCopy, fmt.Sprintf("Panel '%s' %s", pCopy.Title, stateTxt))
										}
									})

									// Switch 2: Desktop-Widget An/Aus
									ui.Row(c).Padding(4, 8).Radius(6).Background(t.Background).AlignItems(ui.Center).Gap(6).Children(func() {
										ui.Text(c, "📌 Widget").FontSize(11.5).TextColor(t.TextMuted)
										if ui.Switch(c.Key("ov-sw-float-"+pCopy.ID), &pCopy.PinnedFloating).Changed() {
											a.selectPanel(pCopy.ID)
											stateTxt := "Desktop-Widget ausgeblendet"
											if pCopy.PinnedFloating {
												stateTxt = "als Desktop-Widget angepinnt"
											}
											a.persistPanelToggle(pCopy, fmt.Sprintf("'%s' %s", pCopy.Title, stateTxt))
										}
									})
								})

								// Row 2: what appears in the macOS menu bar, Subtitle & Update Interval
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
									pillBadge(c, "Menüleiste: "+pCopy.TrayLabel(), sBg, sFg)
									ui.Text(c, pCopy.Subtitle).FontSize(11.5).TextColor(t.TextMuted).Grow(1).SingleLine()
									ui.Text(c, fmt.Sprintf("Alle %ds • %s • Stand: %s", pCopy.RefreshSeconds, strings.ToUpper(string(pCopy.Type)), pCopy.LastUpdated)).
										FontSize(10.5).TextColor(t.TextMuted)
								})

								// Row 3: Clean Action Bar (clicking anywhere on the card already selects it for the preview)
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
									if pCopy.IsBuiltin {
										pillBadge(c, "🔒 SYSTEM-MODUL • nicht per KI anpassbar", t.Accent.Alpha(0.15), t.Accent)
									} else if ui.Button(c.Key("ov-ai-edit-"+pCopy.ID), "✨ Mit KI anpassen").Clicked() {
										a.selectPanel(pCopy.ID)
										a.activeTab = TabPromptStudio
									}

									if ui.Button(c.Key("ov-ref-"+pCopy.ID), "↻ Aktualisieren").Clicked() {
										a.refreshPanelAsync(pCopy, "'"+pCopy.Title+"' aktualisiert")
									}

									if pCopy.BuiltinKind == BuiltinJiraQuickAccess {
										if ui.Button(c.Key("ov-jira-board-"+pCopy.ID), "⤢ Board öffnen").Clicked() {
											a.OpenFullJiraBoardWindow(pCopy)
										}
									}

									modeBtnLbl := "Zu HTML-Ansicht"
									if pCopy.Type == PanelTypeHTML {
										modeBtnLbl = "Zu Native-Ansicht"
									}
									if !pCopy.IsBuiltin && ui.Button(c.Key("ov-mode-"+pCopy.ID), modeBtnLbl).Clicked() {
										if pCopy.Type == PanelTypeNative {
											pCopy.Type = PanelTypeHTML
											if strings.TrimSpace(pCopy.HTML) == "" {
												pCopy.HTML = synthesizeHTMLForPanel(pCopy)
											}
										} else {
											pCopy.Type = PanelTypeNative
										}
										a.persistPanelToggle(pCopy, fmt.Sprintf("'%s' auf %s umgestellt", pCopy.Title, strings.ToUpper(string(pCopy.Type))))
									}

									ui.Spacer(c)

									if len(a.panels) > 1 && !pCopy.IsBuiltin {
										if ui.Button(c.Key("ov-del-"+pCopy.ID), "🗑 Löschen").Clicked() {
											_ = DeletePanel(a.Config.PanelsDir, pCopy.ID)
											var next []*PanelSpec
											for _, item := range a.panels {
												if item.ID != pCopy.ID {
													next = append(next, item)
												}
											}
											a.panels = next
											if a.selectedPanelID == pCopy.ID && len(a.panels) > 0 {
												a.selectPanel(a.panels[0].ID)
											}
											a.syncAllPanelRuntimes()
											a.toast("Panel '" + pCopy.Title + "' gelöscht")
										}
									}
								})
							})
						}
					})
				})
			})

			// Right Column: Live Interactive Preview of Selected Panel + Direct Switch
			sel := a.findPanel(a.selectedPanelID)
			ui.Column(c).Width(420).FillHeight().Padding(12).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "LIVE-VORSCHAU").FontSize(11).Bold().TextColor(t.Accent)
					ui.Spacer(c)
					if sel != nil {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "Aktiv:").FontSize(11).TextColor(t.TextMuted)
							if ui.Switch(c.Key("prev-sw-tray-"+sel.ID), &sel.Enabled).Changed() {
								a.persistPanelToggle(sel, fmt.Sprintf("Panel '%s' umgeschaltet", sel.Title))
							}
						})
						if sel.Enabled {
							if ui.Button(c.Key("prev-pop-open"), "Popover ↗").Tooltip("Öffnet das echte Menüleisten-Popover").Clicked() {
								a.togglePanelFlyout(sel.ID)
							}
						}
					}
				})
				ui.Divider(c)

				if sel == nil {
					ui.Text(c, "Wähle links ein Panel aus, um die Live-Vorschau zu sehen.").TextColor(t.TextMuted)
				} else {
					rt := a.runtimes[sel.ID]
					a.renderNativePanelPopover(c, sel, rt)
				}
			})
		})
	})
}

func (a *App) renderTabStore(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(16).Gap(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "🏪 Vorlagen-Store — Fertige Panels & Widgets mit 1-Klick-Schalter").FontSize(16).Bold()
					ui.Text(c, "Schalte fertige Vorlagen aus 5 Kategorien direkt per Switch ein oder nutze sie als Startpunkt für das KI-Studio.").
						FontSize(12).TextColor(t.TextMuted)
				})
				if ui.Button(c.Key("store-to-ov"), "← Zurück zur Übersicht").Clicked() {
					a.activeTab = TabOverview
				}
			})

			a.renderStoreCatalogList(c, "tab")
		})
	})
}

// renderHelpOnboardingModal renders the Help View explaining OmaMac and 0-Token Polling.
func (a *App) renderHelpOnboardingModal(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(22, 28).Gap(16).Children(func() {
			// Hero Banner
			ui.Column(c).FillWidth().Padding(18).Radius(10).Border(1, t.Accent.Alpha(0.5)).Background(t.Surface).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					pillBadge(c, "KURZANLEITUNG", t.Accent.Alpha(0.22), t.Accent)
					pillBadge(c, "⚡ 0 KI-TOKENS IM LAUFENDEN BETRIEB", t.Success.Alpha(0.22), t.Success)
					ui.Spacer(c)
					if ui.PrimaryButton(c.Key("help-close-top"), "✓ Zurück zur Übersicht").Clicked() {
						a.closeHelpModal()
					}
				})
				ui.Text(c, "Wie OmaMac funktioniert: Einfache Schalter im Alltag, KI-Hilfe beim Bauen").FontSize(18).Bold()
				ui.Text(c, "OmaMac bringt Status-Panels für die macOS-Menüleiste, schwebende Desktop-Widgets und einen Schnellstart-Launcher (Alt+Space) zusammen.").
					FontSize(12.5).TextColor(t.TextMuted)
			})

			// Highlight Box: Why 0 Tokens during polling?
			ui.Column(c).FillWidth().Padding(16).Radius(10).Border(1, t.Success.Alpha(0.5)).Background(t.Success.Alpha(0.08)).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "💡 Wichtigstes Prinzip: KI nur beim Erstellen/Anpassen — 0 Tokens beim Live-Polling!").FontSize(14).Bold().TextColor(t.Success)
				})
				ui.Text(c, "• Wann wird die KI (Gemini / Claude) genutzt?\n  NUR wenn du aktiv im KI-Studio auf „Neues Panel erstellen“ oder „Panel anpassen“ klickst. Die KI erstellt dann einmalig das Panel und das Abfrage-Skript.\n\n• Was passiert danach im Dauerbetrieb (Polling alle 15s–180s)?\n  Eingeschaltete Panels führen im Hintergrund lokal ihr Shell-/Python-Skript aus (z. B. mydealz Deals, System-Load, Git-Status). Dabei werden 0,00 KI-Tokens verbraucht!").
					FontSize(12)
			})

			// 4 Architecture Pillars Grid
			ui.Row(c).FillWidth().Gap(12).Wrap().Children(func() {
				cards := []struct {
					badge string
					title string
					desc  string
				}{
					{
						"1 • ÜBERSICHT & SCHALTER",
						"Einfach An- und Ausschalten",
						"In der Übersicht siehst du alle vorhandenen Panels. Mit dem Schalter „Menüleiste“ legst du fest, welche Panels oben rechts in macOS angezeigt werden.",
					},
					{
						"2 • KI-STUDIO (2 SCHRITTE)",
						"Erstellen & Verfeinern",
						"Schritt 1 erzeugt aus einem Satz ein neues Panel (z. B. mydealz 3D-Drucker Deals). Schritt 2 passt Farben, Schwellenwerte oder Buttons live an.",
					},
					{
						"3 • SICHERER TRESOR",
						"Zentrale Variablen im macOS Keychain",
						"Suchbegriffe & API-Tokens ($MYDEALZ_KEYWORD, $IDEALO_TARGET_PRICE) liegen im macOS Keychain. Die KI sieht nur den Variablennamen ($VAR), nie das Geheimnis.",
					},
					{
						"4 • SCHNELLSTART (ALT+SPACE)",
						"Wal-Panel jederzeit öffnen",
						"Drücke Option+Space für das schwebende Schnellstart-Fenster: Panels per Switch umschalten, Fenster anordnen oder Programme starten.",
					},
				}
				for idx, cd := range cards {
					ui.Column(c.Key(fmt.Sprintf("help-card-%d", idx))).
						Grow(1).MinWidth(260).Padding(14).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(6).Children(func() {
						pillBadge(c, cd.badge, t.Accent.Alpha(0.18), t.Accent)
						ui.Text(c, cd.title).FontSize(13.5).Bold()
						ui.Text(c, cd.desc).FontSize(11.5).TextColor(t.TextMuted)
					})
				}
			})
		})
	})
}

func (a *App) closeHelpModal() {
	a.showHelpModal = false
	a.Config.OnboardingDismissed = true
	_ = SaveConfig(a.ConfigPath, a.Config)
}

func (a *App) renderTabPromptStudio(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).Grow(1).FillWidth().Padding(16).Gap(16).Children(func() {
		// Left Column: Step 1 (Create) + Step 2 (Refine)
		ui.Scroll(c).Grow(1).FillHeight().Children(func() {
			ui.Column(c).FillWidth().Gap(14).Children(func() {
				// Card 1: Prompt #1
				ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						pillBadge(c, "SCHRITT 1 • NEU ERSTELLEN", t.Accent.Alpha(0.2), t.Accent)
						ui.Text(c, "Neues Status-Panel mit KI generieren").FontSize(14).Bold()
					})
					ui.Text(c, "Beschreibe in einem Satz, was überwacht oder angezeigt werden soll. Variablen aus dem Tresor ($MYDEALZ_KEYWORD, $IDEALO_TARGET_PRICE) werden automatisch eingebunden.").
						FontSize(11.5).TextColor(t.TextMuted)

					ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Darstellung:").FontSize(12).Bold()
						ui.Segmented(c.Key("st-p1-type"), &a.prompt1TypeIdx, "Standard (Schnell / Native)", "Web-Design (HTML/CSS)", "Beide Varianten")
						ui.Spacer(c)
						ui.Text(c, "Anzahl:").FontSize(12).Bold()
						ui.Segmented(c.Key("st-p1-cnt"), &a.prompt1CountIdx, panelCountLabels...)
					})

					ui.TextArea(c.Key("st-p1-area"), &a.prompt1Text).
						Placeholder("Beispiel: Erstelle ein Status-Panel für aktuelle 3D-Drucker Deals auf mydealz und überwache den Idealo-Zielpreis ($IDEALO_TARGET_PRICE)...").
						Height(74).FillWidth()

					// Preset Quick-Prompts
					ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
						presets := []struct {
							label  string
							prompt string
						}{
							{"🖨️ Beispiel: mydealz 3D-Drucker & Idealo", "Create a status panel listing current 3D-Drucker deals from mydealz.de/gruppe/3d-drucker and monitoring Idealo target price using $MYDEALZ_KEYWORD and $IDEALO_TARGET_PRICE"},
							{"🐳 Beispiel: Docker & Container", "Create a Docker & Kubernetes container health status panel with running container count and restart action"},
							{"⚡ Beispiel: Akku & CPU-Last", "Create 2 panels: Battery health & charging wattage; CPU thermal & memory pressure"},
						}
						for idx, pr := range presets {
							prCopy := pr
							if ui.Button(c.Key(fmt.Sprintf("preset-%d", idx)), prCopy.label).Clicked() {
								a.prompt1Text = prCopy.prompt
							}
						}
						ui.Spacer(c)
						if ui.PrimaryButton(c.Key("st-p1-submit"), "✨ Neues Panel erstellen").
							Disabled(a.busy || strings.TrimSpace(a.prompt1Text) == "").Clicked() {
							a.triggerPrompt1Create()
						}
					})
				})

				// Card 2: Prompt #2 (Refine Selected Panel)
				sel := a.findPanel(a.selectedPanelID)
				ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						pillBadge(c, "SCHRITT 2 • ANPASSEN", t.Success.Alpha(0.2), t.Success)
						ui.Text(c, "Bestehendes Panel per Satz verfeinern").FontSize(14).Bold()
					})

					ui.Text(c, "1. Welches Panel möchtest du anpassen?").FontSize(11.5).Bold().TextColor(t.TextMuted)
					ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
						for _, p := range a.panels {
							if p.IsBuiltin {
								continue
							}
							pCopy := p
							lbl := fmt.Sprintf("%s %s", pCopy.Icon, pCopy.Title)
							if a.selectedPanelID == pCopy.ID {
								if ui.PrimaryButton(c.Key("st-sel-"+pCopy.ID), lbl).Clicked() {
									a.selectPanel(pCopy.ID)
								}
							} else {
								if ui.Button(c.Key("st-sel-"+pCopy.ID), lbl).Clicked() {
									a.selectPanel(pCopy.ID)
								}
							}
						}
					})

					ui.TextArea(c.Key("st-p2-area"), &a.prompt2Text).
						Placeholder("Beispiel: Hebe Bambu Lab Deals grün hervor, färbe den Status bei Warnung rot oder füge einen Button hinzu...").
						Height(70).FillWidth()

					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						if sel != nil && sel.IsBuiltin {
							ui.Text(c, fmt.Sprintf("'%s' ist ein vorgeliefertes System-Modul und kann nicht per KI angepasst werden — bitte oben ein eigenes Panel wählen.", sel.Title)).
								FontSize(11.5).TextColor(t.Warning)
						} else if sel != nil {
							ui.Text(c, fmt.Sprintf("Ausgewählt: %s (%d KI-Schritte bisher)", sel.Title, len(sel.PromptHistory))).
								FontSize(11.5).TextColor(t.TextMuted)
						}
						ui.Spacer(c)
						if ui.PrimaryButton(c.Key("st-p2-submit"), "⚡ Änderung anwenden & neu laden").
							Disabled(a.busy || sel == nil || sel.IsBuiltin || strings.TrimSpace(a.prompt2Text) == "").Clicked() {
							a.triggerPrompt2Refine(sel.ID, a.prompt2Text)
						}
					})

					// Prompt History Timeline
					if sel != nil && len(sel.PromptHistory) > 0 {
						ui.Divider(c)
						ui.Text(c, "BISHERIGE KI-SCHRITTE FÜR DIESES PANEL").FontSize(10.5).Bold().TextColor(t.TextMuted)
						for _, ph := range sel.PromptHistory {
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
								pillBadge(c, fmt.Sprintf("Schritt #%d", ph.Step), t.Accent.Alpha(0.15), t.Accent)
								ui.Text(c, ph.Prompt).FontSize(11.5).Grow(1).SingleLine()
								ui.Text(c, ph.Timestamp).FontSize(10.5).TextColor(t.TextMuted)
							})
						}
					}
				})
			})
		})

		// Right Column: Live Interactive Preview of Selected Panel
		sel := a.findPanel(a.selectedPanelID)
		ui.Column(c).Width(420).FillHeight().Padding(12).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "LIVE-VORSCHAU").FontSize(11).Bold().TextColor(t.Accent)
				ui.Spacer(c)
				if sel != nil {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Menüleiste:").FontSize(11).TextColor(t.TextMuted)
						if ui.Switch(c.Key("st-prev-sw-"+sel.ID), &sel.Enabled).Changed() {
							a.persistPanelToggle(sel, fmt.Sprintf("Panel '%s' umgeschaltet", sel.Title))
						}
					})
					if ui.Button(c.Key("st-prev-pop-open"), "Popover ↗").Clicked() {
						a.togglePanelFlyout(sel.ID)
					}
				}
			})
			ui.Divider(c)

			if sel == nil {
				ui.Text(c, "Wähle ein Panel aus, um die Vorschau zu sehen.").TextColor(t.TextMuted)
			} else {
				rt := a.runtimes[sel.ID]
				a.renderNativePanelPopover(c, sel, rt)
			}
		})
	})
}

func (a *App) renderTabWindowManager(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).FillWidth().Padding(16).Gap(12).Children(func() {
		ui.Text(c, "🪟 macOS Fenster-Tiling & Programm-Starter").FontSize(16).Bold()
		ui.Text(c, "Ordne das aktive Fenster per Klick an oder nutze jederzeit den globalen Shortcut ("+a.Config.LauncherShortcut+").").
			FontSize(12).TextColor(t.TextMuted)
		a.renderWalLauncherMode(c)
	})
}

func (a *App) renderTabSettings(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(16, 20).Gap(16).Children(func() {
			// Header + Save / Test Bar
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "🔐 KI-Anbieter (Gemini / Claude) & Zentraler Keychain-Tresor").FontSize(16).Bold()
					ui.Text(c, "Konfiguriere deine KI-Verbindung und speichere Suchbegriffe oder API-Tokens sicher im macOS-Schlüsselbund ($VAR_NAME).").
						FontSize(12).TextColor(t.TextMuted)
				})

				if ui.Button(c.Key("cfg-check-update"), "⬆ Nach Updates suchen (v"+mygo.App.Version()+")").
					Tooltip("Prüft GitHub-Releases (diver80/omamac) auf eine neue Version").Clicked() {
					updater.CheckForUpdates()
				}

				if ui.Button(c.Key("cfg-test-ai"), "🔌 KI-Verbindung testen").Disabled(a.busy).Clicked() {
					a.saveSettingsFromDraft()
					a.busy = true
					a.busyLabel = "Teste KI-Verbindung (" + a.AI.ActiveProviderSummary() + ")..."
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						msg, err := a.AI.TestConnection(ctx)
						a.updateUI(func() {
							a.busy = false
							if err != nil {
								a.statusErr = true
								a.statusMsg = "KI-Verbindungstest fehlgeschlagen: " + err.Error()
								a.toast(a.statusMsg)
							} else {
								a.statusErr = false
								a.statusMsg = msg
								a.toast("✅ " + msg)
							}
						})
					}()
				}

				if ui.PrimaryButton(c.Key("cfg-save-btn"), "💾 Einstellungen speichern").Disabled(a.busy).Clicked() {
					a.saveSettingsFromDraft()
					a.toast("Einstellungen gespeichert")
				}
			})

			// Card 0: Central Credentials & Secrets Vault (macOS Keychain via mygo.Secrets)
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "🔐 Zentraler Variablen- & Passwort-Tresor (macOS Keychain)").FontSize(14).Bold().Grow(1)
					pillBadge(c, "ZERO-LEAK SCHUTZ", t.Success.Alpha(0.18), t.Success)
				})
				ui.Text(c, "Werte werden im macOS Keychain gespeichert. Die KI sieht beim Generieren nur den Variablennamen (z. B. $MYDEALZ_KEYWORD), niemals den geheimen Wert.").
					FontSize(11.5).TextColor(t.TextMuted)

				if a.Vault != nil && len(a.Vault.Entries) > 0 {
					for _, entry := range a.Vault.Entries {
						eCopy := entry
						ui.Row(c.Key("vault-row-"+eCopy.Name)).FillWidth().Padding(6, 10).Radius(6).Background(t.Background).AlignItems(ui.Center).Gap(10).Children(func() {
							pillBadge(c, "$"+eCopy.Name, t.Accent.Alpha(0.18), t.Accent)
							ui.Text(c, eCopy.Description).FontSize(11.5).Grow(1).SingleLine()
							ui.Text(c, "••••••••").FontSize(11.5).TextColor(t.TextMuted)
							if ui.Button(c.Key("vault-del-"+eCopy.Name), "Entfernen").Clicked() {
								_ = a.Vault.Delete(eCopy.Name)
								a.toast("$" + eCopy.Name + " aus Tresor entfernt")
							}
						})
					}
				}

				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c.Key("vault-new-key"), &a.vaultNewKey).
						Placeholder("VARIABLEN_NAME (z. B. MYDEALZ_KEYWORD)").Width(240)
					ui.TextInput(c.Key("vault-new-val"), &a.vaultNewVal).
						Placeholder("Wert / API-Token / Filter").Width(210)
					ui.TextInput(c.Key("vault-new-desc"), &a.vaultNewDesc).
						Placeholder("Kurzbeschreibung für die KI (z. B. Suchbegriff für 3D-Drucker)").Grow(1)
					if ui.PrimaryButton(c.Key("vault-add-btn"), "+ Im Tresor speichern").
						Disabled(strings.TrimSpace(a.vaultNewKey) == "" || strings.TrimSpace(a.vaultNewVal) == "").Clicked() {
						if a.Vault != nil {
							_ = a.Vault.Set(a.vaultNewKey, a.vaultNewVal, a.vaultNewDesc)
							a.toast("$" + NormalizeEnvVarName(a.vaultNewKey) + " im macOS Keychain gespeichert")
							a.vaultNewKey, a.vaultNewVal, a.vaultNewDesc = "", "", ""
						}
					}
				})
			})

			// Card 1: Active AI Engine Selector
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
				ui.Text(c, "1. Aktiver KI-Anbieter für Panel-Erstellung & Anpassung").FontSize(14).Bold()
				ui.Segmented(c.Key("cfg-active-prov"), &a.providerIdx, "Google Gemini (Antigravity Login oder API-Key)", "Anthropic Claude (API-Key, Antigravity oder CLI)")
			})

			// Card 2: Gemini Configuration (Antigravity OAuth Login + API Key Fallback)
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(12).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "2. Google Gemini (Antigravity Login oder API-Key)").FontSize(14).Bold().Grow(1)
					if a.AgyStatus.Available {
						pillBadge(c, "✅ ANTIGRAVITY OAUTH VERBUNDEN", t.Success.Alpha(0.18), t.Success)
					} else {
						pillBadge(c, "⚠ ANTIGRAVITY NICHT ERKANNT", t.Warning.Alpha(0.18), t.Warning)
					}
				})

				ui.Text(c, a.AgyStatus.StatusSummary).FontSize(11.5).TextColor(t.TextMuted)

				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Anmeldung:").FontSize(12).Bold()
					ui.Segmented(c.Key("cfg-gem-auth"), &a.geminiAuthIdx, "Antigravity Login (Empfohlen)", "Direkter Gemini API-Key")
					ui.Spacer(c)
					if ui.Button(c.Key("cfg-agy-reauth"), "🔑 Antigravity Login öffnen (agy)").Clicked() {
						_ = TriggerAntigravityLogin(a.Config)
						a.toast("Antigravity Login im macOS Terminal gestartet")
					}
					if ui.Button(c.Key("cfg-agy-check"), "↻ Status prüfen").Clicked() {
						a.AgyStatus = InspectAntigravityAuth(a.Config)
						a.toast(a.AgyStatus.StatusSummary)
					}
				})

				ui.Form(c, func() {
					ui.Field(c, "Gemini Modell", func() {
						ui.Select(c.Key("cfg-gem-model"), &a.geminiModel, geminiModelList).Width(320)
					})
					ui.Field(c, "Gemini API-Key (Optionaler Fallback)", func() {
						ui.TextInput(c.Key("cfg-gem-key"), &a.geminiKey).
							Placeholder("AIzaSy... (optional wenn Antigravity Login aktiv ist)").
							FillWidth()
					})
				})
			})

			// Card 3: Claude Integration
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(12).Children(func() {
				ui.Text(c, "3. Anthropic Claude").FontSize(14).Bold()
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Anmeldung:").FontSize(12).Bold()
					ui.Segmented(c.Key("cfg-cl-auth"), &a.claudeAuthIdx,
						"Anthropic API-Key",
						"Über Antigravity Login (claude-sonnet/opus)",
						"Lokales Claude CLI (claude -p)",
					)
				})
				ui.Form(c, func() {
					ui.Field(c, "Claude Modell", func() {
						ui.Select(c.Key("cfg-cl-model"), &a.claudeModel, claudeModelList).Width(320)
					})
					ui.Field(c, "Anthropic API-Key", func() {
						ui.TextInput(c.Key("cfg-cl-key"), &a.claudeKey).
							Placeholder("sk-ant-api03-... (oder Antigravity / Claude CLI Modus wählen)").
							FillWidth()
					})
				})
			})

			// Card 4: Wal-Panel Hotkey & Omarchy Theme
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(12).Children(func() {
				ui.Text(c, "4. Tastenkürzel & Farbschema (Theme)").FontSize(14).Bold()
				ui.Form(c, func() {
					ui.Field(c, "Globaler Shortcut für Schnellstart-Panel", func() {
						ui.TextInput(c.Key("cfg-shortcut"), &a.shortcutText).Placeholder("Alt+Space").Width(240)
					})
					ui.Field(c, "Farbschema (Theme)", func() {
						ui.Select(c.Key("cfg-theme"), &a.themeSelect, omarchyThemes).Width(240)
					})
				})
			})
		})
	})
}

func (a *App) saveSettingsFromDraft() {
	c := a.Config
	if a.providerIdx == 1 {
		c.ActiveProvider = ProviderClaude
	} else {
		c.ActiveProvider = ProviderGemini
	}
	if a.geminiAuthIdx == 1 {
		c.GeminiAuthMode = GeminiAuthAPIKey
	} else {
		c.GeminiAuthMode = GeminiAuthAntigravity
	}
	c.GeminiModel = a.geminiModel
	c.GeminiAPIKey = strings.TrimSpace(a.geminiKey)

	switch a.claudeAuthIdx {
	case 1:
		c.ClaudeAuthMode = ClaudeAuthAntigravity
	case 2:
		c.ClaudeAuthMode = ClaudeAuthCLI
	default:
		c.ClaudeAuthMode = ClaudeAuthAPIKey
	}
	c.ClaudeModel = a.claudeModel
	c.ClaudeAPIKey = strings.TrimSpace(a.claudeKey)

	oldShortcut := c.LauncherShortcut
	c.LauncherShortcut = strings.TrimSpace(a.shortcutText)
	c.ThemeName = a.themeSelect
	c.Normalize()

	if oldShortcut != c.LauncherShortcut {
		mygo.GlobalShortcut.UnregisterAll()
		if c.LauncherShortcut != "" {
			_ = mygo.GlobalShortcut.Register(c.LauncherShortcut, func() {
				a.ToggleWalPanel()
			})
		}
	}

	_ = SaveConfig(a.ConfigPath, c)
	a.AgyStatus = InspectAntigravityAuth(c)
	a.statusErr = false
	a.statusMsg = "Einstellungen gespeichert — Aktive KI: " + a.AI.ActiveProviderSummary()
}
