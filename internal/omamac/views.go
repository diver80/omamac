package omamac

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/egoist/mygo"
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
			ui.Text(c, "⌘ OmaMac Wal-Panel").FontSize(15).Bold().TextColor(t.Accent)
			pillBadge(c, "0-TOKEN POLLING", t.Success.Alpha(0.18), t.Success)
			ui.Spacer(c)

			ui.Segmented(c.Key("wal-mode-seg"), &a.walMode, "✨ OmaLauncher Studio", "🏪 Store (5 Kategorien)", "🚀 Tiling & Apps")

			if ui.Button(c.Key("wal-open-studio"), "⚙ Studio ↗").Clicked() {
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

		// Bottom OmaLauncher-style Unified Dock + Omni-Bar ("claude/gemini ▾ | app öffnen oder ki fragen...")
		a.renderOmaLauncherOmniBar(c)
	})
}

func (a *App) renderWalPromptMode(c *ui.Context) {
	t := c.Theme()
	provName := "gemini code"
	if a.Config.ActiveProvider == ProviderClaude {
		provName = "claude code"
	}

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			// OmaLauncher Quick-Card (inspired by Screenshot 1: "mit claude/gemini code erstellen", [widget] [app] [panel], store durchsuchen, THEME swatches)
			ui.Column(c).FillWidth().Padding(14, 16).Radius(8).Border(1.5, t.Accent).Background(t.Surface).Gap(10).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("mit %s erstellen", provName)).FontSize(14).Bold()
					ui.Spacer(c)
					pillBadge(c, a.AI.ActiveProviderSummary(), t.Accent.Alpha(0.16), t.Accent)
				})

				// Bracketed selector: [ widget ]  [ app ]  [ panel ]  +  Grid Size [ 3×3 ] [ 6×2 ]
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					kinds := []string{"[ widget ]", "[ app ]", "[ panel ]"}
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
					ui.Text(c, "Grid:").FontSize(11.5).TextColor(t.TextMuted)
					ui.Segmented(c.Key("wal-grid-seg"), &a.gridSizeIdx, "3×3 (Tile)", "6×2 (Wide)")
					ui.Segmented(c.Key("wal-p1-type-seg"), &a.prompt1TypeIdx, "Native", "HTML", "Both")
				})

				// "store durchsuchen" button row + Prompt #1 input
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					if ui.Button(c.Key("wal-browse-store"), "🏪 store durchsuchen (System · Mac · Web/mydealz · Personal · Pixel-Owl) ▸").Clicked() {
						a.walMode = 1
					}
					ui.Spacer(c)
					ui.Segmented(c.Key("wal-p1-cnt-seg"), &a.prompt1CountIdx, panelCountLabels...)
				})

				// Theme Swatch Cards ("tokyo night", "vantablack", "white", "catppuccin") like Screenshot 1
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, "THEME").FontSize(11).Bold().TextColor(t.Accent)
					ui.Spacer(c)
					ui.Text(c, strings.ToLower(a.Config.ThemeName)).FontSize(11).TextColor(t.TextMuted)
				})
				a.renderThemeSwatchRow(c, "wal")
			})

			// Box 2: Prompt #2 (Refine / Iterate any existing Widget/App/Panel + owl-check status)
			ui.Column(c).FillWidth().Padding(12, 14).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					pillBadge(c, "PROMPT #2 • REFINE + OWL-CHECK", t.Success.Alpha(0.2), t.Success)
					ui.Text(c, "Bestehendes Widget / Panel live anpassen").FontSize(12.5).Bold()
					ui.Spacer(c)
					ui.Text(c, fmt.Sprintf("%d aktiv", len(a.panels))).FontSize(11).TextColor(t.TextMuted)
				})

				ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
					for _, p := range a.panels {
						pCopy := p
						active := a.selectedPanelID == pCopy.ID
						lbl := fmt.Sprintf("%s %s [%s]", pCopy.Icon, pCopy.Title, pCopy.GridSize)
						if active {
							if ui.PrimaryButton(c.Key("wal-sel-"+pCopy.ID), lbl).Clicked() {
								a.selectPanel(pCopy.ID)
							}
						} else {
							if ui.Button(c.Key("wal-sel-"+pCopy.ID), lbl).Clicked() {
								a.selectPanel(pCopy.ID)
							}
						}
					}
				})

				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c.Key("wal-p2-input"), &a.prompt2Text).
						Placeholder("Prompt #2: z. B. Badge rot bei Warnung, Button ergänzen oder auf 6×2 HTML-Sparkline wechseln...").
						Grow(1)
					if ui.PrimaryButton(c.Key("wal-p2-go"), "⚡ Apply Prompt #2").
						Disabled(a.busy || a.selectedPanelID == "" || strings.TrimSpace(a.prompt2Text) == "").Clicked() {
						a.triggerPrompt2Refine(a.selectedPanelID, a.prompt2Text)
					}
				})
			})
		})
	})
}

func (a *App) renderThemeSwatchRow(c *ui.Context, prefix string) {
	t := c.Theme()
	swatches := []struct {
		name   string
		bg     ui.Color
		fg     ui.Color
		bars   [5]ui.Color
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
				a.toast("Theme switched to " + swCopy.name)
			}
		}
	})
}

func (a *App) renderWalStoreMode(c *ui.Context) {
	t := c.Theme()
	catalog := StoreCatalog()

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "🏪 OmaLauncher Widget & Panel Store (5 Kategorien)").FontSize(13.5).Bold()
				ui.Spacer(c)
				if ui.Button(c.Key("wal-store-back"), "← Zurück").Clicked() {
					a.walMode = 0
				}
			})

			for idx, item := range catalog {
				itCopy := item
				ui.Row(c.Key(fmt.Sprintf("store-item-%d", idx))).
					FillWidth().Padding(10, 12).Radius(8).Border(1, t.Border).Background(t.Surface).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, itCopy.Icon).FontSize(20)
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							pillBadge(c, strings.ToUpper(itCopy.Category), t.Accent.Alpha(0.18), t.Accent)
							pillBadge(c, fmt.Sprintf("[%s • %s]", itCopy.Kind, itCopy.GridSize), t.Border.Alpha(0.6), t.TextMuted)
							ui.Text(c, itCopy.Title).FontSize(13).Bold().SingleLine()
						})
						ui.Text(c, itCopy.Subtitle).FontSize(11).TextColor(t.TextMuted).SingleLine()
					})
					if ui.Button(c.Key(fmt.Sprintf("store-prompt-%d", idx)), "Prompt anpassen").Clicked() {
						a.prompt1Text = itCopy.Prompt
						a.walMode = 0
					}
					if ui.PrimaryButton(c.Key(fmt.Sprintf("store-inst-%d", idx)), "+ Installieren").Clicked() {
						clone := *itCopy.Spec
						clone.ID = fmt.Sprintf("%s-%d", itCopy.Spec.ID, time.Now().Unix()%1000)
						clone.Enabled = true
						_ = SavePanel(a.Config.PanelsDir, &clone)
						a.panels = append(a.panels, &clone)
						a.selectPanel(clone.ID)
						a.syncAllPanelRuntimes()
						a.toast("Installed '" + clone.Title + "'!")
					}
				})
			}
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
		// Unified Omni-Bar at the bottom (exact OmaLauncher Screenshot 1 style: "claude ▾ | app öffnen oder claude fragen...")
		ui.Row(c).FillWidth().Padding(6, 10).Radius(8).Border(1.5, t.Accent.Alpha(0.65)).Background(t.Surface).AlignItems(ui.Center).Gap(8).Children(func() {
			if ui.Button(c.Key("omni-prov-switch"), provShort).
				Tooltip("Click to toggle between Google Gemini (Antigravity) and Anthropic Claude").Clicked() {
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
				Placeholder(fmt.Sprintf("app öffnen oder %s nach neuem [widget] / [panel] fragen...", strings.TrimSuffix(provShort, " ▾"))).
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
			ui.Text(c, "Tile Window:").FontSize(11.5).Bold().TextColor(t.TextMuted)
			tiles := []struct {
				label  string
				layout TileLayout
			}{
				{"⬅ Left 50%", TileLeftHalf},
				{"➡ Right 50%", TileRightHalf},
				{"⬆ Maximize", TileMaximize},
				{"🎯 Center 80%", TileCenter80},
				{"⬇ Bottom 50%", TileBottomHalf},
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
			Placeholder("Search macOS Applications (/Applications) or type a shell command...").
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
						if ui.Button(c.Key("launch-"+appCopy.ID), "Launch ↵").Clicked() {
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
	t := c.Theme()
	sBg, sFg := statusColors(t, p.Status)

	ui.Column(c).Fill().Padding(14).Gap(10).Children(func() {
		// Header
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).DragWindow().Children(func() {
			ui.Text(c, p.Icon+" "+p.Title).FontSize(14).Bold().Grow(1).SingleLine()
			pillBadge(c, strings.ToUpper(p.Status)+" • "+p.BadgeText, sBg, sFg)
			if ui.Button(c.Key("pop-ref-"+p.ID), "↻").Tooltip("Refresh panel telemetry now").Clicked() {
				go func() {
					_ = p.RunDataRefresh(context.Background())
					a.updateUI(func() {
						a.syncAllPanelRuntimes()
					})
				}()
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

		// Inline Prompt #2 Quick-Refiner right inside the popover!
		if rt != nil {
			ui.Divider(c)
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c.Key("pop-p2-"+p.ID), &rt.quickRefIn).
					Placeholder("✨ Prompt #2: Refine this panel live...").
					Grow(1)
				if ui.PrimaryButton(c.Key("pop-p2-btn-"+p.ID), "Refine").
					Disabled(a.busy || strings.TrimSpace(rt.quickRefIn) == "").Clicked() {
					promptTxt := rt.quickRefIn
					rt.quickRefIn = ""
					a.triggerPrompt2Refine(p.ID, promptTxt)
				}
			})
		}
	})
}

// RenderStudioView renders the main OmaMac Control Center & Settings window.
func (a *App) RenderStudioView(c *ui.Context) {
	t := c.Theme()

	ui.Column(c).Fill().Background(t.Background).Children(func() {
		// Top Header Bar
		ui.Row(c).FillWidth().Padding(10, 16).Gap(10).AlignItems(ui.Center).Background(t.Surface).Children(func() {
			ui.Text(c, "⌘ OmaMac").FontSize(17).Bold().TextColor(t.Accent)
			pillBadge(c, "WAL-PANEL + MULTI-STATUS STUDIO", t.Accent.Alpha(0.16), t.Accent)
			pillBadge(c, a.AI.ActiveProviderSummary(), t.Success.Alpha(0.16), t.Success)
			if a.Vault != nil {
				pillBadge(c, fmt.Sprintf("🔐 VAULT: %d VARS", len(a.Vault.Entries)), t.Accent.Alpha(0.16), t.Accent)
			}

			ui.Spacer(c)

			if a.busy {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, a.busyLabel).FontSize(12).TextColor(t.Accent)
				})
			}

			if ui.Button(c.Key("hdr-help-btn"), "❓ Hilfe & 0-Token Prinzip").Clicked() {
				a.showHelpModal = !a.showHelpModal
			}
			if ui.Button(c.Key("hdr-wal-toggle"), "Toggle Wal-Panel ("+a.Config.LauncherShortcut+")").Clicked() {
				a.ToggleWalPanel()
			}
			if ui.PrimaryButton(c.Key("hdr-refresh-all"), "↻ Refresh All Panels").Clicked() {
				go func() {
					var env []string
					if a.Vault != nil {
						env = a.Vault.EnvSlice()
					}
					for _, p := range a.panels {
						_ = p.RunDataRefresh(context.Background(), env...)
					}
					a.updateUI(func() {
						a.syncAllPanelRuntimes()
						a.toast("Refreshed all active status panels (0 AI tokens used)")
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
					case TabPromptStudio:
						a.renderTabPromptStudio(c)
					case TabPanelsManager:
						a.renderTabPanelsManager(c)
					case TabWindowManager:
						a.renderTabWindowManager(c)
					case TabSettings:
						a.renderTabSettings(c)
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
			pillBadge(c, "⚡ LIVE POLLING = 0 AI TOKENS", t.Success.Alpha(0.16), t.Success)
			ui.Text(c, fmt.Sprintf("Panels Dir: %s", a.Config.PanelsDir)).FontSize(11).TextColor(t.TextMuted)
		})
	})

	for _, msg := range a.toasts {
		c.Toast(msg)
	}
	a.toasts = a.toasts[:0]
}

// renderHelpOnboardingModal renders the First-Start & Architecture Help View explaining OmaMac and 0-Token Polling.
func (a *App) renderHelpOnboardingModal(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(22, 28).Gap(16).Children(func() {
			// Hero Banner
			ui.Column(c).FillWidth().Padding(18).Radius(10).Border(1, t.Accent.Alpha(0.5)).Background(t.Surface).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					pillBadge(c, "WILLKOMMEN BEI OMAMAC", t.Accent.Alpha(0.22), t.Accent)
					pillBadge(c, "⚡ 0 AI TOKENS IM LAUFENDEN BETRIEB", t.Success.Alpha(0.22), t.Success)
					ui.Spacer(c)
					if ui.PrimaryButton(c.Key("help-close-top"), "✓ Verstanden – Zum Studio").Clicked() {
						a.closeHelpModal()
					}
				})
				ui.Text(c, "Wie OmaMac funktioniert: KI-Power beim Bauen, 0 Tokens beim Live-Polling").FontSize(18).Bold()
				ui.Text(c, "OmaMac bringt das Omarchy-Prinzip (Wal-Panel Launcher + maßgeschneiderte Menubar- & Desktop-Status-Panels) nativ auf macOS.").
					FontSize(12.5).TextColor(t.TextMuted)
			})

			// Highlight Box: Why 0 Tokens during polling?
			ui.Column(c).FillWidth().Padding(16).Radius(10).Border(1, t.Success.Alpha(0.5)).Background(t.Success.Alpha(0.08)).Gap(8).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "💡 Wichtigstes Prinzip: KI nur beim Erstellen/Anpassen — 0 Tokens beim Polling!").FontSize(14).Bold().TextColor(t.Success)
				})
				ui.Text(c, "• Wann wird die KI (Gemini / Claude) genutzt?\n  NUR wenn du aktiv im Studio oder Wal-Panel auf 'Run Prompt #1 (Create)' oder 'Run Prompt #2 (Refine)' klickst. Die KI schreibt dann einmalig die Panel-Definition (JSON/HTML) und das lokale Abfrage-Skript (Shell/Python/curl).\n\n• Was passiert danach im Dauerbetrieb (Polling alle 15s–180s)?\n  Das fertige Panel führt im Hintergrund lokal dein generiertes Shell-/Python-Skript aus (z. B. mydealz RSS/Web-Abfrage, Idealo Preis-Check, System-Load, Git-Status, Jira-API). Dabei wird KEINE KI aufgerufen und es werden 0,00 Tokens verbraucht!").
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
						"1 • 2-PROMPT STUDIO",
						"Mit 2 Prompts zum Live-Panel",
						"Prompt #1 erzeugt 1 bis 4 Panels gleichzeitig (z. B. mydealz 3D-Drucker Deals oder Docker-Monitor). Prompt #2 passt Schwellenwerte, Farben, Badges oder Action-Buttons live per Hot-Reload an.",
					},
					{
						"2 • BEIDE PANEL-TYPEN",
						"Native GPU (mygo/ui) & HTML Glass",
						"Du kannst jedes Panel entweder als blitzschnelles GPU-gerendertes mygo/ui Panel (KPIs, Balken, Tabellen, Buttons) oder als freies HTML/CSS/JS WebView-Glas-Popover betreiben und jederzeit umschalten.",
					},
					{
						"3 • ZERO-LEAK VAULT",
						"Zentrale Credentials im macOS Keychain",
						"API-Keys & Filter ($MYDEALZ_KEYWORD, $IDEALO_TARGET_PRICE, $JIRA_TOKEN) liegen im macOS Keychain. Die KI sieht beim Generieren nur den Variablennamen ($VAR), niemals das Passwort!",
					},
					{
						"4 • WAL-PANEL & TILING",
						"Globaler Shortcut (Alt+Space)",
						"Drücke jederzeit Option+Space für das schwebende Wal-Panel: Apps aus /Applications starten, Fenster wie in Omarchy tilen (Left/Right 50%, Center 80%, Maximize) oder Panels nachprompten.",
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

			// Bottom Action Bar with "Don't show automatically on startup" switch
			ui.Row(c).FillWidth().Padding(12, 16).Radius(8).Border(1, t.Border).Background(t.Surface).AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Switch(c.Key("help-dont-show"), &a.dontShowHelpAgain)
				ui.Text(c, "Diesen Dialog beim nächsten App-Start nicht mehr automatisch einblenden (jederzeit über '❓ Hilfe & 0-Token Prinzip' oben rechts aufrufbar)").
					FontSize(12).TextColor(t.TextMuted).Grow(1)
				if ui.PrimaryButton(c.Key("help-close-bottom"), "🚀 Jetzt starten").Clicked() {
					a.closeHelpModal()
				}
			})
		})
	})
}

func (a *App) closeHelpModal() {
	a.showHelpModal = false
	a.Config.OnboardingDismissed = a.dontShowHelpAgain
	_ = SaveConfig(a.ConfigPath, a.Config)
}

func (a *App) renderStudioSidebar(c *ui.Context) {
	t := c.Theme()
	vaultCnt := 0
	if a.Vault != nil {
		vaultCnt = len(a.Vault.Entries)
	}
	navItems := []struct {
		tab   StudioTab
		label string
		badge string
	}{
		{TabPromptStudio, "✨ 2-Prompt Panel Studio", "AI"},
		{TabPanelsManager, "📊 Active Panels Manager", fmt.Sprintf("%d", len(a.panels))},
		{TabWindowManager, "🪟 Window Tiling & Apps", fmt.Sprintf("%d", len(a.apps))},
		{TabSettings, "🔐 AI & Credentials Vault", fmt.Sprintf("%d vars", vaultCnt)},
	}

	ui.Column(c).Width(235).FillHeight().Padding(12, 10).Gap(6).Background(t.Surface).Children(func() {
		ui.Text(c, "OMAMAC MODULES").FontSize(10).Bold().TextColor(t.TextMuted).Padding(4, 8)
		for _, item := range navItems {
			it := item
			active := a.activeTab == it.tab
			btn := ui.ButtonBase(c.Key(fmt.Sprintf("side-%d", it.tab))).
				FillWidth().Padding(9, 10).Radius(6).Cursor(ui.CursorPointer)
			fg := t.Text
			if active {
				btn.Background(t.Accent.Alpha(0.16))
				fg = t.Accent
			} else if btn.Hovered() {
				btn.Background(t.SurfaceHover)
			}
			btn.Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, it.label).FontSize(12.5).Bold().TextColor(fg).Grow(1).SingleLine()
					pillBadge(c, it.badge, t.Border.Alpha(0.6), t.TextMuted)
				})
			})
			if btn.Clicked() {
				a.activeTab = it.tab
				if it.tab == TabSettings {
					a.AgyStatus = InspectAntigravityAuth(a.Config)
				}
			}
		}

		ui.Spacer(c)

		ui.Column(c).FillWidth().Padding(10).Radius(8).Border(1, t.Border).Gap(4).Children(func() {
			ui.Text(c, "2-Prompt + Vault Workflow").FontSize(11).Bold().TextColor(t.Accent)
			ui.Text(c, "1. Store tokens/vars in Vault ($MYDEALZ_KEYWORD, $JIRA_TOKEN)\n2. Prompt #1 creates 1..N panels\n3. Prompt #2 refines live").
				FontSize(10.5).TextColor(t.TextMuted)
		})
	})
}

func (a *App) renderTabPromptStudio(c *ui.Context) {
	t := c.Theme()

	ui.Row(c).Grow(1).FillWidth().Padding(16).Gap(16).Children(func() {
		// Left Column: Prompt #1 (Multi-Create) + Prompt #2 (Refine)
		ui.Scroll(c).Grow(1).FillHeight().Children(func() {
			ui.Column(c).FillWidth().Gap(14).Children(func() {
				// Card 1: Prompt #1
				ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						pillBadge(c, "STEP 1 • PROMPT #1", t.Accent.Alpha(0.2), t.Accent)
						ui.Text(c, "Generate One or Multiple Status Panels").FontSize(14).Bold()
					})
					ui.Text(c, "Choose whether to generate GPU-rendered Native mygo/ui panels, HTML/CSS/JS WebView panels, or both side-by-side. Vault variables ($VAR) are automatically available to the AI.").
						FontSize(11.5).TextColor(t.TextMuted)

					ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Panel Type:").FontSize(12).Bold()
						ui.Segmented(c.Key("st-p1-type"), &a.prompt1TypeIdx, "Native (mygo/ui)", "HTML (WebView)", "Both Types")
					})
					ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "Panel Count:").FontSize(12).Bold()
						ui.Segmented(c.Key("st-p1-cnt"), &a.prompt1CountIdx, panelCountLabels...)
					})

					ui.TextArea(c.Key("st-p1-area"), &a.prompt1Text).
						Placeholder("Prompt #1: e.g. Create a panel listing current 3D printer deals from mydealz and monitoring Idealo target price ($IDEALO_TARGET_PRICE)...").
						Height(76).FillWidth()

					// Preset Quick-Prompts
					ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
						presets := []struct {
							label  string
							prompt string
						}{
							{"🖨️ + mydealz 3D-Drucker & Idealo", "Create a status panel listing current 3D-Drucker deals from mydealz.de/gruppe/3d-drucker and monitoring Idealo target price using $MYDEALZ_KEYWORD and $IDEALO_TARGET_PRICE"},
							{"🐳 + Docker & K8s", "Create a Docker & Kubernetes container health status panel with running container count and restart action"},
							{"⚡ + Dual: Battery + CPU", "Create 2 panels: Battery health & charging wattage; CPU thermal & memory pressure"},
						}
						for idx, pr := range presets {
							prCopy := pr
							if ui.Button(c.Key(fmt.Sprintf("preset-%d", idx)), prCopy.label).Clicked() {
								a.prompt1Text = prCopy.prompt
							}
						}
						ui.Spacer(c)
						if ui.PrimaryButton(c.Key("st-p1-submit"), "✨ Run Prompt #1 (Create Panels)").
							Disabled(a.busy || strings.TrimSpace(a.prompt1Text) == "").Clicked() {
							a.triggerPrompt1Create()
						}
					})
				})

				// Card 2: Prompt #2 (Refine Selected Panel)
				sel := a.findPanel(a.selectedPanelID)
				ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						pillBadge(c, "STEP 2 • PROMPT #2", t.Success.Alpha(0.2), t.Success)
						ui.Text(c, "Refine & Iterate Selected Status Panel").FontSize(14).Bold()
					})

					// Select which panel to refine
					ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
						for _, p := range a.panels {
							pCopy := p
							lbl := fmt.Sprintf("%s %s (%s)", pCopy.Icon, pCopy.Title, strings.ToUpper(string(pCopy.Type)))
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
						Placeholder("Prompt #2: e.g. Highlight Bambu Lab deals in green, alert red when price < $IDEALO_TARGET_PRICE, or convert to HTML chart...").
						Height(72).FillWidth()

					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						if sel != nil {
							ui.Text(c, fmt.Sprintf("Target: %s (%d prompt steps so far)", sel.Title, len(sel.PromptHistory))).
								FontSize(11.5).TextColor(t.TextMuted)
						}
						ui.Spacer(c)
						if ui.PrimaryButton(c.Key("st-p2-submit"), "⚡ Run Prompt #2 (Refine & Hot-Reload)").
							Disabled(a.busy || sel == nil || strings.TrimSpace(a.prompt2Text) == "").Clicked() {
							a.triggerPrompt2Refine(sel.ID, a.prompt2Text)
						}
					})

					// Prompt History Timeline
					if sel != nil && len(sel.PromptHistory) > 0 {
						ui.Divider(c)
						ui.Text(c, "PROMPT HISTORY FOR THIS PANEL").FontSize(10.5).Bold().TextColor(t.TextMuted)
						for _, ph := range sel.PromptHistory {
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
								pillBadge(c, fmt.Sprintf("Prompt #%d", ph.Step), t.Accent.Alpha(0.15), t.Accent)
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
		ui.Column(c).Width(430).FillHeight().Padding(12).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "LIVE PANEL PREVIEW").FontSize(11).Bold().TextColor(t.Accent)
				ui.Spacer(c)
				if sel != nil {
					pillBadge(c, "TYPE: "+strings.ToUpper(string(sel.Type)), t.Accent.Alpha(0.18), t.Accent)
					if ui.Button(c.Key("prev-pop-open"), "Open Popover ↗").Clicked() {
						a.togglePanelFlyout(sel.ID)
					}
				}
			})
			ui.Divider(c)

			if sel == nil {
				ui.Text(c, "Select or create a panel to preview it live.").TextColor(t.TextMuted)
			} else {
				rt := a.runtimes[sel.ID]
				a.renderNativePanelPopover(c, sel, rt)
			}
		})
	})
}

func (a *App) renderTabPanelsManager(c *ui.Context) {
	t := c.Theme()

	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(16).Gap(14).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "Multi-Panel Status Bar & Floating Desktop Manager").FontSize(16).Bold()
					ui.Text(c, "Enable/disable macOS Menubar Trays, pin panels as floating desktop HUDs, or switch any panel between Native (mygo/ui) and HTML (WebView).").
						FontSize(12).TextColor(t.TextMuted)
				})
			})

			for _, p := range a.panels {
				pCopy := p
				sBg, sFg := statusColors(t, pCopy.Status)
				ui.Column(c.Key("mgr-card-"+pCopy.ID)).FillWidth().Padding(14).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Text(c, pCopy.Icon+" "+pCopy.Title).FontSize(14).Bold()
						pillBadge(c, "TYPE: "+strings.ToUpper(string(pCopy.Type)), t.Accent.Alpha(0.16), t.Accent)
						pillBadge(c, pCopy.TrayLabel(), sBg, sFg)
						ui.Spacer(c)

						trayLbl := "Menubar Tray: OFF"
						if pCopy.Enabled {
							trayLbl = "✅ Menubar Tray: ON"
						}
						if ui.Button(c.Key("tog-tray-"+pCopy.ID), trayLbl).Clicked() {
							pCopy.Enabled = !pCopy.Enabled
							_ = SavePanel(a.Config.PanelsDir, pCopy)
							a.syncAllPanelRuntimes()
						}

						floatLbl := "Pin Desktop HUD: OFF"
						if pCopy.PinnedFloating {
							floatLbl = "📌 Pinned Desktop HUD: ON"
						}
						if ui.Button(c.Key("tog-float-"+pCopy.ID), floatLbl).Clicked() {
							pCopy.PinnedFloating = !pCopy.PinnedFloating
							_ = SavePanel(a.Config.PanelsDir, pCopy)
							a.syncAllPanelRuntimes()
						}

						switchTypeLbl := "Switch to HTML"
						if pCopy.Type == PanelTypeHTML {
							switchTypeLbl = "Switch to Native UI"
						}
						if ui.Button(c.Key("tog-type-"+pCopy.ID), switchTypeLbl).Clicked() {
							if pCopy.Type == PanelTypeNative {
								pCopy.Type = PanelTypeHTML
								if strings.TrimSpace(pCopy.HTML) == "" {
									pCopy.HTML = synthesizeHTMLForPanel(pCopy)
								}
							} else {
								pCopy.Type = PanelTypeNative
							}
							_ = SavePanel(a.Config.PanelsDir, pCopy)
							a.syncAllPanelRuntimes()
						}

						if ui.Button(c.Key("dup-"+pCopy.ID), "Duplicate").Clicked() {
							clone := *pCopy
							clone.ID = fmt.Sprintf("%s-copy-%d", pCopy.ID, time.Now().Unix()%1000)
							clone.Title = pCopy.Title + " (Copy)"
							_ = SavePanel(a.Config.PanelsDir, &clone)
							a.panels = append(a.panels, &clone)
							a.syncAllPanelRuntimes()
						}

						if len(a.panels) > 1 {
							if ui.Button(c.Key("del-"+pCopy.ID), "Delete").Clicked() {
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
							}
						}
					})

					ui.Text(c, fmt.Sprintf("ID: %s  •  Refresh: %ds  •  Command: %s", pCopy.ID, pCopy.RefreshSeconds, truncateStr(pCopy.Command, 90))).
						FontSize(11).TextColor(t.TextMuted).SingleLine()
				})
			}
		})
	})
}

func (a *App) renderTabWindowManager(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Grow(1).FillWidth().Padding(16).Gap(12).Children(func() {
		ui.Text(c, "Omarchy-Style macOS Window Tiling & App Launcher").FontSize(16).Bold()
		ui.Text(c, "Use the Wal-Panel ("+a.Config.LauncherShortcut+") or trigger window tiling presets directly below.").
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
					ui.Text(c, "AI Providers (Gemini Antigravity / Claude) & Central Credentials Vault").FontSize(16).Bold()
					ui.Text(c, "Configure AI engines and central macOS Keychain credentials ($VAR_NAME) shared safely with generated status panels.").
						FontSize(12).TextColor(t.TextMuted)
				})

				if ui.Button(c.Key("cfg-test-ai"), "🔌 Test Active AI Connection").Disabled(a.busy).Clicked() {
					a.saveSettingsFromDraft()
					a.busy = true
					a.busyLabel = "Testing AI connection (" + a.AI.ActiveProviderSummary() + ")..."
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						msg, err := a.AI.TestConnection(ctx)
						a.updateUI(func() {
							a.busy = false
							if err != nil {
								a.statusErr = true
								a.statusMsg = "AI Connection Test failed: " + err.Error()
								a.toast(a.statusMsg)
							} else {
								a.statusErr = false
								a.statusMsg = msg
								a.toast("✅ " + msg)
							}
						})
					}()
				}

				if ui.PrimaryButton(c.Key("cfg-save-btn"), "💾 Save Settings").Disabled(a.busy).Clicked() {
					a.saveSettingsFromDraft()
					a.toast("Settings saved to " + a.ConfigPath)
				}
			})

			// Card 0: Central Credentials & Secrets Vault (macOS Keychain via mygo.Secrets)
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "🔐 Central Panel & Agent Credentials Vault (macOS Keychain)").FontSize(14).Bold().Grow(1)
					pillBadge(c, "ZERO-LEAK AI PROMPT INJECTION", t.Success.Alpha(0.18), t.Success)
				})
				ui.Text(c, "Secrets stored here are saved in macOS Keychain (mygo.Secrets). The AI agent only sees variable names & descriptions (never values!), and panels receive them automatically as $ENV_VAR at runtime.").
					FontSize(11.5).TextColor(t.TextMuted)

				if a.Vault != nil && len(a.Vault.Entries) > 0 {
					for _, entry := range a.Vault.Entries {
						eCopy := entry
						ui.Row(c.Key("vault-row-"+eCopy.Name)).FillWidth().Padding(6, 10).Radius(6).Background(t.Background).AlignItems(ui.Center).Gap(10).Children(func() {
							pillBadge(c, "$"+eCopy.Name, t.Accent.Alpha(0.18), t.Accent)
							ui.Text(c, eCopy.Description).FontSize(11.5).Grow(1).SingleLine()
							ui.Text(c, "••••••••").FontSize(11.5).TextColor(t.TextMuted)
							if ui.Button(c.Key("vault-del-"+eCopy.Name), "Remove").Clicked() {
								_ = a.Vault.Delete(eCopy.Name)
								a.toast("Removed $" + eCopy.Name + " from Vault")
							}
						})
					}
				}

				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.TextInput(c.Key("vault-new-key"), &a.vaultNewKey).
						Placeholder("ENV_VAR_NAME (e.g. JIRA_TOKEN, MYDEALZ_KEYWORD)").Width(230)
					ui.TextInput(c.Key("vault-new-val"), &a.vaultNewVal).
						Placeholder("Secret Value / API Token / Filter").Width(210)
					ui.TextInput(c.Key("vault-new-desc"), &a.vaultNewDesc).
						Placeholder("Description for AI Agent (e.g. Atlassian Cloud API token)").Grow(1)
					if ui.PrimaryButton(c.Key("vault-add-btn"), "+ Save to Vault").
						Disabled(strings.TrimSpace(a.vaultNewKey) == "" || strings.TrimSpace(a.vaultNewVal) == "").Clicked() {
						if a.Vault != nil {
							_ = a.Vault.Set(a.vaultNewKey, a.vaultNewVal, a.vaultNewDesc)
							a.toast("Saved $" + NormalizeEnvVarName(a.vaultNewKey) + " to macOS Keychain Vault")
							a.vaultNewKey, a.vaultNewVal, a.vaultNewDesc = "", "", ""
						}
					}
				})
			})

			// Card 1: Active AI Engine Selector
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(10).Children(func() {
				ui.Text(c, "1. Primary AI Provider for Status Panel Generation & Refinement").FontSize(14).Bold()
				ui.Segmented(c.Key("cfg-active-prov"), &a.providerIdx, "Google Gemini (Antigravity Login or API Key)", "Anthropic Claude (API Key, Antigravity or CLI)")
			})

			// Card 2: Gemini Configuration (Antigravity OAuth Login + API Key Fallback)
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(12).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, "2. Google Gemini Integration (Antigravity Login or API Key)").FontSize(14).Bold().Grow(1)
					if a.AgyStatus.Available {
						pillBadge(c, "✅ ANTIGRAVITY OAUTH CONNECTED", t.Success.Alpha(0.18), t.Success)
					} else {
						pillBadge(c, "⚠ ANTIGRAVITY NOT DETECTED", t.Warning.Alpha(0.18), t.Warning)
					}
				})

				ui.Text(c, a.AgyStatus.StatusSummary).FontSize(11.5).TextColor(t.TextMuted)

				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Gemini Auth Mode:").FontSize(12).Bold()
					ui.Segmented(c.Key("cfg-gem-auth"), &a.geminiAuthIdx, "Antigravity Login (Recommended)", "Direct Gemini API Key")
					ui.Spacer(c)
					if ui.Button(c.Key("cfg-agy-reauth"), "🔑 Antigravity Login / Refresh (agy)").Clicked() {
						_ = TriggerAntigravityLogin(a.Config)
						a.toast("Launched Antigravity login in macOS Terminal")
					}
					if ui.Button(c.Key("cfg-agy-check"), "↻ Re-Check Token").Clicked() {
						a.AgyStatus = InspectAntigravityAuth(a.Config)
						a.toast(a.AgyStatus.StatusSummary)
					}
				})

				ui.Form(c, func() {
					ui.Field(c, "Gemini Model", func() {
						ui.Select(c.Key("cfg-gem-model"), &a.geminiModel, geminiModelList).Width(320)
					})
					ui.Field(c, "Gemini API Key (Fallback / Direct Mode)", func() {
						ui.TextInput(c.Key("cfg-gem-key"), &a.geminiKey).
							Placeholder("AIzaSy... (optional when Antigravity Login is active)").
							FillWidth()
					})
				})
			})

			// Card 3: Claude Integration (Anthropic API Key, Antigravity Claude, or Claude CLI)
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(12).Children(func() {
				ui.Text(c, "3. Anthropic Claude Integration").FontSize(14).Bold()
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Claude Auth Mode:").FontSize(12).Bold()
					ui.Segmented(c.Key("cfg-cl-auth"), &a.claudeAuthIdx,
						"Anthropic API Key",
						"Via Antigravity Login (claude-sonnet/opus)",
						"Local Claude CLI (claude -p)",
					)
				})
				ui.Form(c, func() {
					ui.Field(c, "Claude Model", func() {
						ui.Select(c.Key("cfg-cl-model"), &a.claudeModel, claudeModelList).Width(320)
					})
					ui.Field(c, "Anthropic API Key", func() {
						ui.TextInput(c.Key("cfg-cl-key"), &a.claudeKey).
							Placeholder("sk-ant-api03-... (or use Antigravity / Claude CLI mode)").
							FillWidth()
					})
				})
			})

			// Card 4: Wal-Panel Hotkey & Omarchy Theme
			ui.Column(c).FillWidth().Padding(16).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(12).Children(func() {
				ui.Text(c, "4. Wal-Panel Global Shortcut & Omarchy Theme").FontSize(14).Bold()
				ui.Form(c, func() {
					ui.Field(c, "Global Wal-Panel Shortcut", func() {
						ui.TextInput(c.Key("cfg-shortcut"), &a.shortcutText).Placeholder("Alt+Space").Width(240)
					})
					ui.Field(c, "Omarchy Theme Preset", func() {
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
	a.statusMsg = "Saved settings — Active AI: " + a.AI.ActiveProviderSummary()
}
