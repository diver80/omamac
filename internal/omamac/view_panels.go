package omamac

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	trayColWidth   = 84
	widgetColWidth = 64
)

// renderTabOverview is the main view: the panels on the left, one line each with
// its two switches, and the chosen panel's preview and actions on the right.
func (a *App) renderTabOverview(c *ui.Context) {
	enabledCnt, floatingCnt := a.countEnabledPanels()

	ui.Row(c).Grow(1).MinHeight(0).FillWidth().Padding(20).Gap(20).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Gap(14).Children(func() {
			pageHeader(c, "Panels",
				fmt.Sprintf("%d von %d in der Menüleiste · %d als Widget", enabledCnt, len(a.panels), floatingCnt),
				func() {
					ui.Button(c.Key("ov-more"), "⋯").Tooltip("Weitere Aktionen").Menu(func(m *ui.Menu) {
						if m.Item("Alle in der Menüleiste zeigen").Chosen() {
							a.setAllPanelsEnabled(true)
						}
						if m.Item("Alle aus der Menüleiste nehmen").Chosen() {
							a.setAllPanelsEnabled(false)
						}
						m.Separator()
						if m.Item("Alle jetzt aktualisieren").Chosen() {
							a.refreshAllNow()
						}
					})
				})
			a.renderPanelList(c, "ov", true)
		})
		a.renderPanelDetail(c)
	})
	a.renderDeleteConfirm(c)
}

// renderPanelList shows every panel in one line with its menu bar and widget
// switches. selectable rows choose the panel for the detail column.
func (a *App) renderPanelList(c *ui.Context, prefix string, selectable bool) {
	t := c.Theme()
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Gap(4).Children(func() {
		ui.Row(c).FillWidth().Padding(0, 12).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Row(c).Grow(1)
			fixedCell(c, trayColWidth, func() {
				ui.Text(c, "Menüleiste").FontSize(10.5).TextColor(t.TextMuted)
			})
			fixedCell(c, widgetColWidth, func() {
				ui.Text(c, "Widget").FontSize(10.5).TextColor(t.TextMuted)
			})
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				for _, p := range a.panels {
					a.renderPanelRow(c, prefix, p, selectable)
				}
			})
		})
	})
}

func (a *App) renderPanelRow(c *ui.Context, prefix string, p *PanelSpec, selectable bool) {
	t := c.Theme()
	row := ui.Row(c.Key(prefix+"-row-"+p.ID)).FillWidth().Padding(8, 12).Radius(8).AlignItems(ui.Center).Gap(10)
	if selectable {
		row.Cursor(ui.CursorPointer).OnClick(func() { a.selectPanel(p.ID) })
	}
	switch {
	case selectable && a.selectedPanelID == p.ID:
		row.Background(t.Accent.Alpha(0.14))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		ui.Text(c, p.Icon).FontSize(17).Width(26)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			title := ui.Text(c, p.Title).FontSize(13).Bold().SingleLine()
			if !p.Enabled && !p.PinnedFloating {
				title.TextColor(t.TextMuted)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				statusDot(c, p.Status)
				ui.Text(c, panelStatusText(p)).FontSize(11).TextColor(t.TextMuted).SingleLine()
			})
		})
		fixedCell(c, trayColWidth, func() {
			if ui.Switch(c.Key(prefix+"-sw-tray-"+p.ID), &p.Enabled).Label("Menüleiste").Changed() {
				msg := "„" + p.Title + "“ aus der Menüleiste genommen"
				if p.Enabled {
					msg = "„" + p.Title + "“ in der Menüleiste"
				}
				a.persistPanelToggle(p, msg)
			}
		})
		fixedCell(c, widgetColWidth, func() {
			if ui.Switch(c.Key(prefix+"-sw-float-"+p.ID), &p.PinnedFloating).Label("Widget").Changed() {
				msg := "Widget „" + p.Title + "“ ausgeblendet"
				if p.PinnedFloating {
					msg = "„" + p.Title + "“ als Widget auf dem Desktop"
				}
				a.persistPanelToggle(p, msg)
			}
		})
	})
}

// renderPanelDetail is the right column: the live preview of the chosen panel and what can be done with it.
func (a *App) renderPanelDetail(c *ui.Context) {
	t := c.Theme()
	sel := a.findPanel(a.selectedPanelID)

	ui.Column(c).Width(400).FillHeight().Gap(12).Children(func() {
		if sel == nil {
			ui.Column(c).Grow(1).FillWidth().Radius(10).Border(1, t.Border).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Wähle links ein Panel aus.").TextColor(t.TextMuted)
			})
			return
		}

		// The preview shows the panel as it opens from the menu bar.
		ui.Column(c.Key("detail-preview")).FillWidth().Grow(1).MinHeight(0).Radius(10).Border(1, t.Border).Background(t.Surface).Clip().Children(func() {
			a.renderNativePanelPopover(c, sel, nil, nil)
		})

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, panelMetaLine(sel)).FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
			if sel.BuiltinKind == BuiltinJiraQuickAccess {
				if ui.Button(c.Key("detail-jira-board"), "Board öffnen").Clicked() {
					a.OpenFullJiraBoardWindow(sel)
				}
			}
			if sel.Enabled {
				if ui.Button(c.Key("detail-popover"), "Popover öffnen").Tooltip("Öffnet das Panel wie aus der Menüleiste").Clicked() {
					a.togglePanelFlyout(sel.ID)
				}
			}
			a.renderPanelMoreMenu(c, sel)
		})

		a.renderPanelRefine(c, sel)
	})
}

// renderPanelRefine changes the chosen panel with one sentence to the AI.
func (a *App) renderPanelRefine(c *ui.Context, sel *PanelSpec) {
	t := c.Theme()
	if sel.IsBuiltin {
		ui.Text(c, "System-Modul: wird mit OmaMac geliefert und lässt sich nicht per KI ändern.").
			FontSize(11.5).TextColor(t.TextMuted)
		return
	}
	ui.Column(c.Key("refine")).FillWidth().Padding(12, 14).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
		ui.Text(c, "Mit KI anpassen").FontSize(13).Bold()
		submit := ui.TextArea(c.Key("refine-prompt-"+sel.ID), &a.prompt2Text).
			Placeholder("z. B. „Färbe den Status rot, wenn der Preis unter 400 € fällt“").
			Height(56).FillWidth().Shortcut(ui.Cmd, ui.KeyEnter)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			if n := len(sel.PromptHistory); n > 0 {
				label := fmt.Sprintf("Verlauf (%d)", n)
				if a.showHistory {
					label = "Verlauf ausblenden"
				}
				if textButton(c, "refine-history", label) {
					a.showHistory = !a.showHistory
				}
			}
			ui.Spacer(c)
			busy := a.busy || strings.TrimSpace(a.prompt2Text) == ""
			if (ui.PrimaryButton(c.Key("refine-submit"), "Anpassen").Tooltip("⌘↵").Disabled(busy).Clicked() || submit) && !busy {
				a.triggerPrompt2Refine(sel.ID, a.prompt2Text)
			}
		})
		if a.showHistory {
			for i, ph := range sel.PromptHistory {
				ui.Row(c.Key(fmt.Sprintf("refine-step-%d", i))).FillWidth().Gap(8).Children(func() {
					ui.Text(c, fmt.Sprintf("%d.", ph.Step)).FontSize(11).TextColor(t.TextMuted).Width(18)
					ui.Text(c, ph.Prompt).FontSize(11).Grow(1).MinWidth(0)
					ui.Text(c, ph.Timestamp).FontSize(10.5).TextColor(t.TextMuted)
				})
			}
		}
	})
}

// renderPanelMoreMenu is the ⋯ menu of a panel: the rarer actions, deleting last.
func (a *App) renderPanelMoreMenu(c *ui.Context, p *PanelSpec) {
	ui.Button(c.Key("detail-more-"+p.ID), "⋯").Tooltip("Weitere Aktionen").Menu(func(m *ui.Menu) {
		if m.Item("Jetzt aktualisieren").Chosen() {
			a.refreshPanelAsync(p, "„"+p.Title+"“ aktualisiert")
		}
		if !p.IsBuiltin {
			label := "Als HTML-Ansicht darstellen"
			if p.Type == PanelTypeHTML {
				label = "Als native Ansicht darstellen"
			}
			if m.Item(label).Chosen() {
				a.togglePanelType(p)
			}
			if len(a.panels) > 1 {
				m.Separator()
				if m.Item("Löschen …").Chosen() {
					a.confirmDeleteID = p.ID
					a.confirmDelete = true
				}
			}
		}
	})
}

func (a *App) renderDeleteConfirm(c *ui.Context) {
	title := "Panel löschen?"
	if p := a.findPanel(a.confirmDeleteID); p != nil {
		title = "„" + p.Title + "“ löschen?"
	}
	if ui.AlertDialog(c, &a.confirmDelete, title, "Das Panel und sein Abfrage-Skript werden entfernt.", "Abbrechen", "Löschen") == 1 {
		a.deletePanel(a.confirmDeleteID)
	}
}

func (a *App) togglePanelType(p *PanelSpec) {
	if p.Type == PanelTypeNative {
		p.Type = PanelTypeHTML
		if strings.TrimSpace(p.HTML) == "" {
			p.HTML = synthesizeHTMLForPanel(p)
		}
	} else {
		p.Type = PanelTypeNative
	}
	kind := "nativ"
	if p.Type == PanelTypeHTML {
		kind = "als HTML"
	}
	a.persistPanelToggle(p, "„"+p.Title+"“ wird jetzt "+kind+" dargestellt")
}

func (a *App) deletePanel(id string) {
	p := a.findPanel(id)
	if p == nil || p.IsBuiltin {
		return
	}
	_ = DeletePanel(a.Config.PanelsDir, id)
	var next []*PanelSpec
	for _, item := range a.panels {
		if item.ID != id {
			next = append(next, item)
		}
	}
	a.panels = next
	if a.selectedPanelID == id && len(a.panels) > 0 {
		a.selectPanel(a.panels[0].ID)
	}
	a.syncAllPanelRuntimes()
	a.toast("„" + p.Title + "“ gelöscht")
}

// refreshAllNow refreshes every enabled panel at once, whatever their intervals.
func (a *App) refreshAllNow() {
	go func() {
		a.refreshDuePanels(true)
		a.updateUI(func() {
			a.toast("Alle Panels aktualisiert")
		})
	}()
}
