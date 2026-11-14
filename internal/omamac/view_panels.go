package omamac

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	trayColWidth   = 50
	widgetColWidth = 50
	dashColWidth   = 44
)

// renderTabOverview is the main view: the panels on the left, one line each with
// its two switches, and the chosen panel's preview and actions on the right.
func (a *App) renderTabOverview(c *ui.Context) {
	enabledCnt, floatingCnt := a.countEnabledPanels()

	ui.Row(c).Grow(1).MinHeight(0).FillWidth().Padding(20).Gap(20).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).FillHeight().Gap(14).Children(func() {
			pageHeader(c, "Panels",
				fmt.Sprintf(T("%d von %d in der Menüleiste · %d als Widget · %d auf dem Dashboard"), enabledCnt, len(a.panels), floatingCnt, len(a.dashboardPanels())),
				func() {
					ui.Button(c.Key("ov-more"), "⋯").Tooltip(T("Weitere Aktionen")).Menu(func(m *ui.Menu) {
						if m.Item(T("Alle in der Menüleiste zeigen")).Chosen() {
							a.setAllPanelsEnabled(true)
						}
						if m.Item(T("Alle aus der Menüleiste nehmen")).Chosen() {
							a.setAllPanelsEnabled(false)
						}
						m.Separator()
						if m.Item(T("Alle jetzt aktualisieren")).Chosen() {
							a.refreshAllNow()
						}
						m.Separator()
						if m.Item(T("Eigene Panels teilen …")).Chosen() {
							a.openExport(a.panels)
						}
						if m.Item(T("Panels importieren …")).Chosen() {
							a.pickImportFile()
						}
						if m.Item(T("Aus Zwischenablage importieren")).Chosen() {
							a.importFromClipboard()
						}
					})
				})
			a.renderPanelList(c, "ov", true)
		})
		a.renderPanelDetail(c)
	})
	// ⌘⌫ deletes the chosen panel (a focused text field keeps the keys for itself).
	if c.Shortcut(ui.Cmd, ui.KeyBackspace) {
		a.askDeletePanel(a.findPanel(a.selectedPanelID))
	}
	a.renderDeleteConfirm(c)
	a.renderRenameDialog(c)
	a.renderEditCommandDialog(c)
	a.renderExportDialog(c)
	a.renderImportDialog(c)
}

func (a *App) filteredPanels() []*PanelSpec {
	q := strings.ToLower(strings.TrimSpace(a.panelFilter))
	if q == "" {
		return a.panels
	}
	var out []*PanelSpec
	for _, p := range a.panels {
		if strings.Contains(strings.ToLower(p.Title), q) ||
			strings.Contains(strings.ToLower(T(p.Title)), q) ||
			strings.Contains(strings.ToLower(p.Subtitle), q) ||
			strings.Contains(strings.ToLower(p.BadgeText), q) ||
			strings.Contains(strings.ToLower(p.ID), q) {
			out = append(out, p)
		}
	}
	return out
}

// renderPanelList shows every panel in one line with compact toggles for menu bar,
// widget and dashboard. selectable rows choose the panel for the detail column.
func (a *App) renderPanelList(c *ui.Context, prefix string, selectable bool) {
	t := c.Theme()
	panels := a.filteredPanels()
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Gap(6).Children(func() {
		if selectable && len(a.panels) > 4 {
			ui.SearchField(c.Key(prefix+"-filter"), &a.panelFilter).
				Placeholder(T("Panels filtern …")).FillWidth()
		}
		ui.Row(c).FillWidth().Padding(0, 12).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Row(c).Grow(1)
			fixedCell(c, trayColWidth, func() {
				ui.Text(c, T("Leiste")).FontSize(10.5).TextColor(t.TextMuted).Tooltip(T("Menüleiste"))
			})
			fixedCell(c, widgetColWidth, func() {
				ui.Text(c, "Widget").FontSize(10.5).TextColor(t.TextMuted)
			})
			fixedCell(c, dashColWidth, func() {
				ui.Text(c, "Dash").FontSize(10.5).TextColor(t.TextMuted).Tooltip("Dashboard")
			})
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				if len(panels) == 0 && strings.TrimSpace(a.panelFilter) != "" {
					ui.Column(c).FillWidth().Padding(24).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, T("Kein Panel passt zum Filter.")).FontSize(12).TextColor(t.TextMuted)
						if ui.Button(c.Key(prefix+"-filter-reset"), T("Filter zurücksetzen")).Clicked() {
							a.panelFilter = ""
						}
					})
					return
				}
				for _, p := range panels {
					a.renderPanelRow(c, prefix, p, selectable)
				}
			})
		})
	})
}

func compactTogglePill(c *ui.Context, key string, active bool, glyph, tooltip string) bool {
	t := c.Theme()
	btn := ui.ButtonBase(c.Key(key)).Size(34, 22).Radius(6).Cursor(ui.CursorPointer).
		Justify(ui.Center).AlignItems(ui.Center).Tooltip(tooltip)
	labelColor := t.TextMuted.Alpha(0.6)
	label := "·"
	switch {
	case active:
		btn.Background(t.Accent.Alpha(0.18)).Border(1, t.Accent.Alpha(0.5))
		labelColor = t.Accent
		label = glyph
	case btn.Hovered():
		btn.Background(t.SurfaceHover).Border(1, t.Border)
		label = glyph
	default:
		btn.Background(t.Background).Border(1, t.Border)
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(11).Bold().TextColor(labelColor)
	})
	return btn.Clicked()
}

func (a *App) renderPanelRow(c *ui.Context, prefix string, p *PanelSpec, selectable bool) {
	t := c.Theme()
	row := ui.Row(c.Key(prefix+"-row-"+p.ID)).FillWidth().Padding(8, 12).Radius(8).AlignItems(ui.Center).Gap(6)
	if selectable {
		row.Cursor(ui.CursorPointer).OnClick(func() { a.selectPanel(p.ID) })
		row.ContextMenu(func(m *ui.Menu) { a.panelMenuItems(m, p) })
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
			title := ui.Text(c, T(p.Title)).FontSize(13).Bold().SingleLine() // delivered panels keep their German default name on disk
			if !p.Enabled && !p.PinnedFloating {
				title.TextColor(t.TextMuted)
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				status, text := a.panelStatusLine(p)
				statusDot(c, status)
				ui.Text(c, text).FontSize(11).TextColor(t.TextMuted).SingleLine()
				if p.NotifyOnStatusChange {
					ui.Text(c, "🔔").FontSize(10).Tooltip(T("Benachrichtigung bei Statuswechsel aktiv"))
				}
			})
		})
		fixedCell(c, trayColWidth, func() {
			if compactTogglePill(c, prefix+"-sw-tray-"+p.ID, p.Enabled, "⌃", T("In der Menüleiste anzeigen")) {
				p.Enabled = !p.Enabled
				msg := T("„") + p.Title + T("“ aus der Menüleiste genommen")
				if p.Enabled {
					msg = T("„") + p.Title + T("“ in der Menüleiste")
				}
				a.persistPanelToggle(p, msg)
			}
		})
		fixedCell(c, widgetColWidth, func() {
			if compactTogglePill(c, prefix+"-sw-float-"+p.ID, p.PinnedFloating, "❐", T("Als schwebendes Widget auf dem Desktop")) {
				p.PinnedFloating = !p.PinnedFloating
				msg := T("Widget „") + p.Title + T("“ ausgeblendet")
				if p.PinnedFloating {
					msg = T("„") + p.Title + T("“ als Widget auf dem Desktop")
				}
				a.persistPanelToggle(p, msg)
			}
		})
		fixedCell(c, dashColWidth, func() {
			on := !p.DashboardOff
			if compactTogglePill(c, prefix+"-sw-dash-"+p.ID, on, "▦", T("Auf dem Dashboard anzeigen")) {
				p.DashboardOff = on
				msg := fmt.Sprintf(T("„%s“ vom Dashboard genommen"), p.Title)
				if !p.DashboardOff {
					msg = fmt.Sprintf(T("„%s“ auf dem Dashboard"), p.Title)
				}
				a.persistPanelToggle(p, msg)
			}
		})
	})
}

func (a *App) dropPreviewWeb() {
	if a.previewWeb != nil {
		if a.previewWeb.view != nil {
			a.previewWeb.view.Destroy()
		}
		a.previewWeb = nil
		a.previewWebID = ""
	}
}

func (a *App) htmlPreviewView(p *PanelSpec) *mygo.WebView {
	if !a.desktop || a.studioWin == nil || a.studioWin.IsDestroyed() || p == nil {
		return nil
	}
	key := windowKey(p)
	if a.previewWeb != nil && a.previewWebID == p.ID && a.previewWeb.key == key {
		return a.previewWeb.view
	}
	a.dropPreviewWeb()
	wv, err := a.studioWin.NewWebView(mygo.WebViewOptions{Transparent: true})
	if err != nil {
		logf("app", "%s: no preview web view: %v", p.ID, err)
		return nil
	}
	doc := p.HTML
	if strings.TrimSpace(doc) == "" {
		doc = synthesizeHTMLForPanel(p)
	}
	id := p.ID
	wv.Page().LoadHTML(doc, "https://omamac.local/")
	wv.Page().OnDOMReady(func() {
		if rt := a.runtimes[id]; rt != nil {
			a.pushHTMLPanelUpdate(rt)
		}
	})
	a.previewWeb = &dashWeb{view: wv, key: key}
	a.previewWebID = p.ID
	return wv
}

// renderPanelDetail is the right column: the live preview of the chosen panel and what can be done with it.
func (a *App) renderPanelDetail(c *ui.Context) {
	t := c.Theme()
	sel := a.findPanel(a.selectedPanelID)

	ui.Column(c).Width(420).FillHeight().Gap(10).Children(func() {
		if sel == nil {
			a.dropPreviewWeb()
			ui.Column(c).Grow(1).FillWidth().Radius(10).Border(1, t.Border).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, T("Wähle links ein Panel aus.")).TextColor(t.TextMuted)
			})
			return
		}
		if sel.Type != PanelTypeHTML {
			a.dropPreviewWeb()
		}

		ui.Scroll(c.Key("detail-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				if a.isHeld(sel) {
					a.renderReviewBox(c, sel)
				}

				// The preview shows the panel as it opens from the menu bar (or its HTML WebView).
				ui.Column(c.Key("detail-preview")).FillWidth().Height(270).Radius(10).Border(1, t.Border).Background(t.Surface).Clip().Children(func() {
					if sel.Type == PanelTypeHTML && a.desktop {
						ui.Row(c).FillWidth().Padding(6, 10).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, sel.Icon+" "+T(sel.Title)).FontSize(12).Bold().SingleLine().Grow(1).MinWidth(0)
							ui.Segmented(c.Key("detail-html-mode"), &a.detailPreviewMode, "HTML", T("Daten"))
						})
						ui.Divider(c)
						if a.detailPreviewMode == 0 {
							if wv := a.htmlPreviewView(sel); wv != nil {
								ui.WebView(c, wv).Grow(1)
								return
							}
						}
					}
					a.renderNativePanelPopover(c, sel, nil, nil)
				})

				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, panelMetaLine(sel)).FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
					if ui.Button(c.Key("detail-rename"), "✎").Tooltip(T("Name, Symbol und Menüleisten-Text ändern")).Clicked() {
						a.openRename(sel)
					}
					if sel.BuiltinKind == BuiltinJiraQuickAccess {
						if ui.Button(c.Key("detail-jira-board"), T("Board öffnen")).Clicked() {
							a.OpenFullJiraBoardWindow(sel)
						}
					}
					if sel.Enabled {
						if ui.Button(c.Key("detail-popover"), T("Popover öffnen")).Tooltip(T("Öffnet das Panel wie aus der Menüleiste")).Clicked() {
							a.togglePanelFlyout(sel.ID)
						}
					}
					a.renderPanelMoreMenu(c, sel)
				})

				if sel.LastRunError != "" && !a.isHeld(sel) {
					ui.Text(c, T("Skript fehlgeschlagen: ")+sel.LastRunError).FontSize(11.5).TextColor(t.Danger).FillWidth()
				}

				a.renderPanelRefine(c, sel)
				a.renderPanelRuns(c, sel)
			})
		})

		if sel.Deletable() {
			ui.Row(c).FillWidth().Children(func() {
				ui.Spacer(c)
				del := ui.ButtonBase(c.Key("detail-delete-"+sel.ID)).Cursor(ui.CursorPointer).Padding(2, 4)
				hovered := del.Hovered()
				del.Children(func() {
					txt := ui.Text(c, T("Panel löschen …")).FontSize(11.5).TextColor(t.Danger.Alpha(0.8))
					if hovered {
						txt.TextColor(t.Danger)
					}
				})
				if del.Tooltip(T("Löscht das Panel nach einer Rückfrage (⌘⌫)")).Clicked() {
					a.askDeletePanel(sel)
				}
			})
		}
	})
}

// renderPanelRefine changes the chosen panel with one sentence to the AI.
func (a *App) renderPanelRefine(c *ui.Context, sel *PanelSpec) {
	t := c.Theme()
	if sel.Configurable() {
		ui.Column(c.Key("module-cfg")).FillWidth().Padding(12, 14).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, T("Konfiguration")).FontSize(13).Bold().Grow(1)
				if ui.Button(c.Key("module-dup"), T("Duplizieren")).Tooltip(T("Ein weiteres Panel dieses Moduls mit eigenen Einstellungen")).Clicked() {
					a.duplicatePanel(sel)
				}
			})
			a.renderModuleConfig(c, sel)
		})
		return
	}
	if sel.IsBuiltin {
		ui.Text(c, T("System-Modul: wird mit OmaMac geliefert und lässt sich nicht per KI ändern.")).
			FontSize(11.5).TextColor(t.TextMuted)
		return
	}
	box := ui.Column(c.Key("refine")).FillWidth().Padding(12, 14).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(8)
	if files := box.DroppedFiles(); files != nil {
		a.addImages(&a.refineImages, files)
	}
	if box.FileDragOver() {
		box.Border(2, t.Accent)
	}
	box.Children(func() {
		ui.Text(c, T("Mit KI anpassen")).FontSize(13).Bold()
		submit := ui.TextArea(c.Key("refine-prompt-"+sel.ID), &a.prompt2Text).
			Placeholder(T("z. B. „Färbe den Status rot, wenn der Preis unter 400 € fällt“")).
			Height(56).FillWidth().
			OnPaste(func(text string) bool { return text == "" && a.pasteImage(&a.refineImages) }).
			Shortcut(ui.Cmd, ui.KeyEnter)
		a.renderImageAttachments(c, "refine-images", &a.refineImages)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			if n := len(sel.PromptHistory); n > 0 {
				label := fmt.Sprintf(T("Verlauf (%d)"), n)
				if a.showHistory {
					label = T("Verlauf ausblenden")
				}
				if textButton(c, "refine-history", label) {
					a.showHistory = !a.showHistory
				}
			}
			ui.Spacer(c)
			busy := a.busy || strings.TrimSpace(a.prompt2Text) == ""
			if (ui.PrimaryButton(c.Key("refine-submit"), T("Anpassen")).Tooltip("⌘↵").Disabled(busy).Clicked() || submit) && !busy {
				a.triggerPrompt2Refine(sel.ID, a.prompt2Text)
			}
		})
		if a.showHistory {
			for i, ph := range sel.PromptHistory {
				stepCopy := ph
				ui.Row(c.Key(fmt.Sprintf("refine-step-%d", i))).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("%d.", stepCopy.Step)).FontSize(11).TextColor(t.TextMuted).Width(18)
					ui.Text(c, stepCopy.Prompt).FontSize(11).Grow(1).MinWidth(0)
					ui.Text(c, stepCopy.Timestamp).FontSize(10.5).TextColor(t.TextMuted)
					if stepCopy.Snapshot != nil && i < len(sel.PromptHistory)-1 {
						if textButton(c, fmt.Sprintf("refine-undo-%d", i), T("Wiederherstellen")) {
							a.restorePromptSnapshot(sel, stepCopy)
						}
					}
				})
			}
		}
	})
}

// renderPanelMoreMenu is the ⋯ menu of a panel; a right-click on its row opens the same items.
func (a *App) renderPanelMoreMenu(c *ui.Context, p *PanelSpec) {
	ui.Button(c.Key("detail-more-"+p.ID), "⋯").Tooltip(T("Weitere Aktionen")).Menu(func(m *ui.Menu) {
		a.panelMenuItems(m, p)
	})
}

// panelMenuItems are a panel's actions in its ⋯ and right-click menus, deleting last.
func (a *App) panelMenuItems(m *ui.Menu, p *PanelSpec) {
	if m.Item(T("Jetzt aktualisieren")).Chosen() {
		a.refreshPanelAsync(p, T("„")+p.Title+T("“ aktualisiert"))
	}
	if m.Item(T("Umbenennen …")).Chosen() {
		a.openRename(p)
	}
	if !p.IsBuiltin && m.Item(T("Befehl & Intervall …")).Chosen() {
		a.openEditCommand(p)
	}
	notifyLabel := T("Bei Statuswechsel benachrichtigen")
	if p.NotifyOnStatusChange {
		notifyLabel = "✓ " + notifyLabel
	}
	if m.Item(notifyLabel).Chosen() {
		p.NotifyOnStatusChange = !p.NotifyOnStatusChange
		msg := fmt.Sprintf(T("Benachrichtigungen für „%s“ deaktiviert"), p.Title)
		if p.NotifyOnStatusChange {
			msg = fmt.Sprintf(T("Benachrichtigungen für „%s“ aktiviert"), p.Title)
		}
		_ = SavePanel(a.Config.PanelsDir, p)
		a.toast(msg)
	}
	if !p.IsBuiltin && m.Item(T("Teilen …")).Chosen() {
		a.openExport([]*PanelSpec{p})
	}
	if p.Configurable() && m.Item(T("Duplizieren")).Chosen() {
		a.duplicatePanel(p)
	}
	if p.IsBuiltin {
		if p.Deletable() {
			m.Separator()
			if m.Item(T("Löschen …")).Shortcut(ui.Cmd, ui.KeyBackspace).Chosen() {
				a.askDeletePanel(p)
			}
		}
		return
	}
	label := T("Als HTML-Ansicht darstellen")
	if p.Type == PanelTypeHTML {
		label = T("Als native Ansicht darstellen")
	}
	if m.Item(label).Chosen() {
		a.togglePanelType(p)
	}
	m.Separator()
	if m.Item(T("Löschen …")).Shortcut(ui.Cmd, ui.KeyBackspace).Chosen() {
		a.askDeletePanel(p)
	}
}

// askDeletePanel asks before deleting p; the delivered system modules cannot be deleted.
func (a *App) askDeletePanel(p *PanelSpec) {
	if p == nil || !p.Deletable() {
		return
	}
	a.confirmDeleteID = p.ID
	a.confirmDelete = true
}

func (a *App) renderDeleteConfirm(c *ui.Context) {
	title := T("Panel löschen?")
	if p := a.findPanel(a.confirmDeleteID); p != nil {
		title = fmt.Sprintf(T("„%s“ löschen?"), p.Title)
	}
	if ui.AlertDialog(c, &a.confirmDelete, title, T("Das Panel und sein Abfrage-Skript werden entfernt."), T("Abbrechen"), T("Löschen")) == 1 {
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
	kind := T("nativ")
	if p.Type == PanelTypeHTML {
		kind = T("als HTML")
	}
	a.persistPanelToggle(p, T("„")+p.Title+T("“ wird jetzt ")+kind+T(" dargestellt"))
}

func (a *App) deletePanel(id string) {
	p := a.findPanel(id)
	if p == nil || !p.Deletable() {
		return
	}
	_ = DeletePanel(a.Config.PanelsDir, id)
	a.removePanel(id)
	a.approved.forget(id)
	logf("app", "%s deleted", id)
	a.toast(T("„") + p.Title + T("“ gelöscht"))
}

// refreshAllNow refreshes every enabled panel at once, whatever their intervals.
func (a *App) refreshAllNow() {
	go func() {
		a.refreshDuePanels(true)
		a.updateUI(func() {
			a.toast(T("Alle Panels aktualisiert"))
		})
	}()
}

// openRename opens the dialog for p's name, symbol and menu bar text.
func (a *App) openRename(p *PanelSpec) {
	if p == nil {
		return
	}
	a.renameID = p.ID
	a.renameTitle, a.renameIcon, a.renameMenu = p.Title, p.Icon, p.MenuBarText
	a.showRename = true
}

// renderRenameDialog edits what the user calls a panel: its name in OmaMac,
// its symbol and the text in the menu bar, with a live preview of the latter.
func (a *App) renderRenameDialog(c *ui.Context) {
	p := a.findPanel(a.renameID)
	if p == nil {
		return
	}
	t := c.Theme()
	ui.Modal(c.Key("rename"), &a.showRename, func() {
		ui.Column(c).Width(520).Gap(14).Children(func() {
			ui.Text(c, T("Panel umbenennen")).FontSize(18).Bold()
			submit := false
			ui.Form(c, func() {
				ui.Field(c, "Name", func() {
					submit = ui.TextInput(c.Key("rename-title"), &a.renameTitle).AutoFocus().FillWidth().Shortcut(0, ui.KeyEnter) || submit
				})
				ui.Field(c, T("Symbol"), func() {
					ui.TextInput(c.Key("rename-icon"), &a.renameIcon).Placeholder(T("Emoji, z. B. 🛡️")).Width(120)
				})
				ui.Field(c, T("Menüleiste"), func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						submit = ui.TextInput(c.Key("rename-menu"), &a.renameMenu).
							Placeholder(T("automatisch: „")+strings.TrimSpace(p.BadgeText)+T("“")).FillWidth().Shortcut(0, ui.KeyEnter) || submit
						ui.Text(c, T("Leer: Text des Skripts. ")+MenuBarStatusToken+T(" fügt ihn ein – z. B. „Kunde A ")+MenuBarStatusToken+T("“")).
							FontSize(11.5).TextColor(t.TextMuted)
					})
				})
				ui.Field(c, T("Vorschau"), func() {
					preview := *p
					preview.Icon, preview.MenuBarText = strings.TrimSpace(a.renameIcon), a.renameMenu
					ui.Text(c, preview.TrayLabel()).FontSize(12.5).Padding(3, 10).Radius(6).Background(t.Background).SingleLine()
				})
			})
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c.Key("rename-cancel"), T("Abbrechen")).Clicked() {
					a.showRename = false
				}
				if ui.PrimaryButton(c.Key("rename-save"), T("Speichern")).Disabled(strings.TrimSpace(a.renameTitle) == "").Clicked() || submit {
					a.applyRename(p)
				}
			})
		})
	})
}

// applyRename stores the dialog's values and updates the menu bar at once.
func (a *App) applyRename(p *PanelSpec) {
	title := strings.TrimSpace(a.renameTitle)
	if title == "" {
		return
	}
	p.Title = title
	if icon := strings.TrimSpace(a.renameIcon); icon != "" {
		p.Icon = icon
	}
	p.MenuBarText = strings.TrimSpace(a.renameMenu)
	_ = SavePanel(a.Config.PanelsDir, p)
	a.showRename = false
	a.syncAllPanelRuntimes()
	a.toast(T("„") + p.Title + T("“ gespeichert"))
}

// panelStatusLine is a panel's dot and short status in lists: waiting for the user's OK,
// failed, or what its script reports.
func (a *App) panelStatusLine(p *PanelSpec) (status, text string) {
	switch {
	case a.isHeld(p):
		return "warn", T("wartet auf deine Freigabe")
	case p.LastRunError != "":
		return "crit", T("Skript fehlgeschlagen")
	}
	return p.Status, panelStatusText(p)
}

// renderReviewBox shows the commands of a panel written outside OmaMac (by a coding agent,
// say) until the user allows them.
func (a *App) renderReviewBox(c *ui.Context, p *PanelSpec) {
	t := c.Theme()
	ui.Column(c.Key("review-"+p.ID)).FillWidth().Padding(12, 14).Radius(10).Border(1, t.Warning).
		Background(t.Warning.Alpha(0.08)).Gap(8).Children(func() {
		ui.Text(c, T("Befehl prüfen")).FontSize(13).Bold()
		ui.Text(c, T("Dieses Panel kommt von außerhalb von OmaMac, z. B. von einem Coding-Agenten. Seine Befehle laufen erst, wenn du sie freigibst.")).
			FontSize(11.5).TextColor(t.TextMuted).FillWidth()
		ui.Scroll(c).FillWidth().MaxHeight(150).Children(func() {
			ui.Column(c).FillWidth().Padding(8, 10).Radius(6).Background(t.Background).Gap(6).Children(func() {
				if cmd := strings.TrimSpace(p.Command); cmd != "" {
					ui.Text(c, cmd).Font("monospace").FontSize(11).FillWidth().Selectable()
				}
				for _, act := range p.Actions {
					if cmd := strings.TrimSpace(act.Command); cmd != "" {
						ui.Text(c, fmt.Sprintf(T("Aktion „%s“: %s"), act.Label, cmd)).Font("monospace").FontSize(11).FillWidth().Selectable()
					}
				}
			})
		})
		if vars := panelVariables(p); len(vars) > 0 {
			ui.Text(c, T("Nutzt: ")+"$"+strings.Join(vars, ", $")).FontSize(11).TextColor(t.TextMuted).FillWidth()
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spacer(c)
			if p.Deletable() && ui.Button(c.Key("review-delete-"+p.ID), T("Löschen …")).Clicked() {
				a.askDeletePanel(p)
			}
			if ui.PrimaryButton(c.Key("review-allow-"+p.ID), T("Freigeben")).Tooltip(T("Der Befehl läuft ab jetzt bei jeder Aktualisierung")).Clicked() {
				a.allowPanel(p)
			}
		})
	})
}

// renderPanelRuns lists the last runs of a panel's script, as the log has them.
func (a *App) renderPanelRuns(c *ui.Context, p *PanelSpec) {
	if strings.TrimSpace(p.Command) == "" || p.IsBuiltin {
		return
	}
	runs := panelRuns(p.ID)
	if len(runs) == 0 {
		return
	}
	t := c.Theme()
	ui.Collapsible(c, fmt.Sprintf(T("Läufe (%d)"), len(runs)), &a.showRuns, func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			for i, r := range runs {
				ui.Column(c.Key(fmt.Sprintf("run-%s-%d", p.ID, i))).FillWidth().Gap(2).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						status := "ok"
						if r.failed() {
							status = "crit"
						}
						statusDot(c, status)
						ui.Text(c, r.At.Format("15:04:05")).FontSize(11)
						ui.Text(c, fmt.Sprintf(T("%.1f s · Exit %d"), r.Duration.Seconds(), r.Exit)).FontSize(11).TextColor(t.TextMuted)
					})
					if r.Err != "" {
						ui.Text(c, r.Err).FontSize(11).TextColor(t.Danger).FillWidth()
					} else if r.Output != "" && i == 0 {
						ui.Text(c, firstLines(r.Output, 3, 120)).Font("monospace").FontSize(10.5).TextColor(t.TextMuted).FillWidth()
					}
				})
			}
			ui.Row(c).FillWidth().Children(func() {
				ui.Spacer(c)
				if textButton(c, "runs-log", T("Protokoll öffnen")) {
					a.openLogFile()
				}
			})
		})
	})
}

// openLogFile shows OmaMac's log in the Console app (or whatever opens .log files).
func (a *App) openLogFile() {
	path := LogPath()
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err != nil {
		logf("app", "log opened") // so there is a file to open
	}
	if err := mygo.Shell.OpenPath(path); err != nil {
		a.toast("⚠️ " + err.Error())
	}
}

func (a *App) restorePromptSnapshot(p *PanelSpec, step PromptStep) {
	if p == nil || step.Snapshot == nil {
		return
	}
	p.RestoreSnapshot(step.Snapshot)
	a.approved.allow(p)
	_ = SavePanel(a.Config.PanelsDir, p)
	if rt, ok := a.runtimes[p.ID]; ok && rt.flyout != nil && !rt.flyout.IsDestroyed() {
		rt.flyout.Destroy()
		rt.flyout = nil
	}
	a.syncAllPanelRuntimes()
	a.refreshPanelAsync(p, fmt.Sprintf(T("„%s“ auf Schritt %d zurückgesetzt"), p.Title, step.Step))
}

func (a *App) openEditCommand(p *PanelSpec) {
	if p == nil || p.IsBuiltin {
		return
	}
	a.editCmdID = p.ID
	a.editCmdText = p.Command
	a.editCmdRefresh = fmt.Sprint(max(15, p.RefreshSeconds))
	a.editCmdNotify = p.NotifyOnStatusChange
	a.showEditCmd = true
}

func (a *App) renderEditCommandDialog(c *ui.Context) {
	p := a.findPanel(a.editCmdID)
	if p == nil {
		return
	}
	t := c.Theme()
	ui.Modal(c.Key("edit-cmd"), &a.showEditCmd, func() {
		ui.Column(c).Width(560).Gap(14).Children(func() {
			ui.Text(c, T("Befehl & Intervall")).FontSize(18).Bold()
			ui.Form(c, func() {
				ui.Field(c, T("Intervall (Sekunden)"), func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.TextInput(c.Key("edit-cmd-refresh"), &a.editCmdRefresh).Width(100)
						ui.Text(c, T("mindestens 15 Sekunden")).FontSize(11.5).TextColor(t.TextMuted)
					})
				})
				ui.Field(c, T("Abfrage-Befehl"), func() {
					ui.TextArea(c.Key("edit-cmd-text"), &a.editCmdText).Height(150).FillWidth()
				})
				ui.Field(c, T("Benachrichtigung"), func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Switch(c.Key("edit-cmd-notify"), &a.editCmdNotify).Label(T("Bei Statuswechsel benachrichtigen"))
						ui.Text(c, T("macOS-Mitteilung senden, wenn sich der Status ändert")).FontSize(11.5).TextColor(t.TextMuted)
					})
				})
			})
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c.Key("edit-cmd-cancel"), T("Abbrechen")).Clicked() {
					a.showEditCmd = false
				}
				if ui.PrimaryButton(c.Key("edit-cmd-save"), T("Speichern")).Clicked() {
					a.applyEditCommand(p)
				}
			})
		})
	})
}

func (a *App) applyEditCommand(p *PanelSpec) {
	sec, err := strconv.Atoi(strings.TrimSpace(a.editCmdRefresh))
	if err != nil || sec < 15 {
		sec = 15
	}
	p.RefreshSeconds = sec
	p.Command = strings.TrimSpace(a.editCmdText)
	p.NotifyOnStatusChange = a.editCmdNotify
	a.approved.allow(p)
	_ = SavePanel(a.Config.PanelsDir, p)
	a.showEditCmd = false
	a.syncAllPanelRuntimes()
	a.refreshPanelAsync(p, fmt.Sprintf(T("„%s“ gespeichert"), p.Title))
}
