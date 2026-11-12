package omamac

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The dashboard is OmaMac's home screen, as Omalauncher's is: the panels as tiles in a
// grid, and the launcher's bar at the bottom. It is the main window's first view; full
// screen it is a Space of its own, a swipe away. Real macOS desktop widgets would need a
// WidgetKit extension in Swift; the floating widgets stay for single panels.

const dashColumns = 6 // a tile is 2 (small), 3 (medium) or 6 (wide) of them wide

// dashSize is how big a tile is.
type dashSize struct {
	ID, Label string
	Cols      int
	Height    float32
}

var dashSizes = []dashSize{
	{"small", "Klein", 2, 250},
	{"medium", "Mittel", 3, 330},
	{"tall", "Hoch", 3, 520},
	{"wide", "Breit", 6, 400},
}

// tileSize is the size of p's tile: the one chosen, else wide for the week board and
// medium for the rest.
func tileSize(p *PanelSpec) dashSize {
	id := p.DashboardSize
	if id == "" {
		id = "medium"
		switch p.BuiltinKind {
		case BuiltinJiraWeek:
			id = "wide"
		case BuiltinAgentUsage:
			id = "small"
		}
	}
	for _, s := range dashSizes {
		if s.ID == id {
			return s
		}
	}
	return dashSizes[1]
}

// dashDrag is a tile being dragged by its header, to put it elsewhere.
type dashDrag struct{ id string }

// dashboardPanels are the panels on the dashboard, in the order the user put them.
func (a *App) dashboardPanels() []*PanelSpec {
	var out []*PanelSpec
	seen := map[string]bool{}
	for _, id := range a.Config.DashboardOrder {
		if p := a.findPanel(id); p != nil && !seen[id] {
			seen[id] = true
			if !p.DashboardOff {
				out = append(out, p)
			}
		}
	}
	for _, p := range a.panels {
		if !seen[p.ID] && !p.DashboardOff {
			out = append(out, p)
		}
	}
	return out
}

// moveTile puts the tile of panel id where the tile of panel before is, and saves the order.
func (a *App) moveTile(id, before string) {
	if id == before {
		return
	}
	var order []string
	for _, p := range a.dashboardPanels() {
		if p.ID != id {
			order = append(order, p.ID)
		}
	}
	at := len(order)
	for i, other := range order {
		if other == before {
			at = i
		}
	}
	order = append(order[:at], append([]string{id}, order[at:]...)...)
	a.Config.DashboardOrder = order
	_ = SaveConfig(a.ConfigPath, a.Config)
}

// setTileSize gives p's tile another size.
func (a *App) setTileSize(p *PanelSpec, size string) {
	p.DashboardSize = size
	_ = SavePanel(a.Config.PanelsDir, p)
}

// dashboardShown reports whether the dashboard is on screen: its panels then refresh too.
func (a *App) dashboardShown() bool {
	return a.desktop && a.activeTab == TabDashboard && a.studioWin != nil && !a.studioWin.IsDestroyed() && a.studioWin.IsVisible()
}

// setTab shows another view of the main window.
func (a *App) setTab(tab StudioTab) {
	if a.activeTab == tab {
		return
	}
	a.activeTab = tab
	switch tab {
	case TabSettings:
		a.AgyStatus = InspectAntigravityAuth(a.Config)
	case TabDashboard:
		if a.desktop {
			go a.refreshDuePanels(false) // tiles only on the dashboard have not refreshed meanwhile
		}
	}
}

// toggleFullScreen puts the main window on a Space of its own, or back.
func (a *App) toggleFullScreen() {
	if a.studioWin == nil || a.studioWin.IsDestroyed() {
		return
	}
	a.studioWin.SetFullScreen(!a.studioWin.IsFullScreen())
}

// renderTabDashboard is the dashboard: the tiles, and the launcher's bar below them.
func (a *App) renderTabDashboard(c *ui.Context) {
	t := c.Theme()
	panels := a.dashboardPanels()
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Padding(14, 20, 6, 20).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "Dashboard").FontSize(18).Bold()
			ui.Text(c, fmt.Sprintf(T("%d Panels · Kopf ziehen zum Sortieren, Rechtsklick für die Größe"), len(panels))).
				FontSize(11.5).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
			full := T("Vollbild")
			if a.studioWin != nil && !a.studioWin.IsDestroyed() && a.studioWin.IsFullScreen() {
				full = T("Vollbild beenden")
			}
			if ui.Button(c.Key("dash-full"), "⛶ "+full).Tooltip(T("Das Dashboard als eigener Space – mit drei Fingern hin- und herwischen")).Clicked() {
				a.toggleFullScreen()
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(6, 20, 16, 20).Gap(14).Children(func() {
				if len(panels) == 0 {
					ui.Column(c).FillWidth().Padding(40).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, T("Noch keine Panels auf dem Dashboard.")).TextColor(t.TextMuted)
						if ui.Button(c.Key("dash-manage"), T("Panels verwalten")).Clicked() {
							a.setTab(TabOverview)
						}
					})
					return
				}
				for _, row := range packTiles(panels) {
					height := float32(0)
					cols := 0
					for _, p := range row {
						s := tileSize(p)
						height = max(height, s.Height)
						cols += s.Cols
					}
					ui.Row(c).FillWidth().Height(height).Gap(14).Children(func() {
						for _, p := range row {
							a.renderTile(c, p)
						}
						if cols < dashColumns {
							ui.Row(c).Grow(float32(dashColumns - cols)).Basis(0)
						}
					})
				}
			})
		})
		ui.Divider(c)
		a.renderDashboardBar(c)
	})
}

// packTiles fills rows of dashColumns with the tiles in order; a gap a tile leaves at the
// end of a row takes the next tile that fits, so a wide tile doesn't leave a hole.
func packTiles(panels []*PanelSpec) [][]*PanelSpec {
	var rows [][]*PanelSpec
	placed := make([]bool, len(panels))
	for start := 0; start < len(panels); start++ {
		if placed[start] {
			continue
		}
		row := []*PanelSpec{panels[start]}
		placed[start] = true
		used := tileSize(panels[start]).Cols
		for i := start + 1; i < len(panels) && used < dashColumns; i++ {
			if cols := tileSize(panels[i]).Cols; !placed[i] && used+cols <= dashColumns {
				row = append(row, panels[i])
				placed[i] = true
				used += cols
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// renderTile is one panel on the dashboard, as its popover shows it.
func (a *App) renderTile(c *ui.Context, p *PanelSpec) {
	t := c.Theme()
	s := tileSize(p)
	tile := ui.Column(c.Key("tile-"+p.ID)).Grow(float32(s.Cols)).Basis(0).MinWidth(0).FillHeight().
		Radius(12).Border(1, t.Border).Background(t.Surface).Clip()
	if d, ok := ui.Drop[dashDrag](tile); ok {
		a.moveTile(d.id, p.ID)
	}
	if d, ok := ui.DragOver[dashDrag](tile); ok && d.id != p.ID {
		tile.Border(2, t.Accent)
	}
	tile.ContextMenu(func(m *ui.Menu) {
		for _, size := range dashSizes {
			label := T(size.Label)
			if size.ID == s.ID {
				label = "✓ " + label
			}
			if m.Item(label).Chosen() {
				a.setTileSize(p, size.ID)
			}
		}
		m.Separator()
		if m.Item(T("Jetzt aktualisieren")).Chosen() {
			a.refreshPanelAsync(p, "")
		}
		if m.Item(T("Bearbeiten …")).Chosen() {
			a.selectPanel(p.ID)
			a.setTab(TabOverview)
		}
		if m.Item(T("Vom Dashboard nehmen")).Chosen() {
			p.DashboardOff = true
			a.persistPanelToggle(p, fmt.Sprintf(T("„%s“ vom Dashboard genommen – zurück über den Schalter „Dashboard“"), p.Title))
		}
	})
	tile.Children(func() {
		if p.Type == PanelTypeHTML {
			a.renderHTMLTile(c, p)
			return
		}
		a.tileRender = true // the header drags the tile, not the window
		a.renderNativePanelPopover(c, p, a.runtimes[p.ID], nil)
		a.tileRender = false
	})
}

// panelHeader makes a panel's header move what the panel is in: its window (popover,
// desktop widget), or on the dashboard its tile.
func (a *App) panelHeader(row ui.Element, p *PanelSpec) ui.Element {
	if a.tileRender {
		return row.Drag(dashDrag{p.ID}).Cursor(ui.CursorGrab)
	}
	return row.DragWindow()
}

// renderHTMLTile shows an HTML panel's page in its tile; outside the app (tests, the
// screenshots) its data, as natively drawn panels show it.
func (a *App) renderHTMLTile(c *ui.Context, p *PanelSpec) {
	t := c.Theme()
	wv := a.htmlTileView(p)
	if wv == nil {
		a.tileRender = true
		a.renderNativePanelPopover(c, p, a.runtimes[p.ID], nil)
		a.tileRender = false
		return
	}
	header := ui.Row(c).FillWidth().Padding(10, 14, 6, 14).AlignItems(ui.Center).Gap(8)
	a.tileRender = true
	a.panelHeader(header, p).Children(func() {
		status, text := a.panelStatusLine(p)
		ui.Text(c, p.Icon+" "+T(p.Title)).FontSize(13).Bold().SingleLine().Grow(1).MinWidth(0)
		statusDot(c, status)
		ui.Text(c, text).FontSize(11).TextColor(t.TextMuted).SingleLine()
	})
	a.tileRender = false
	ui.WebView(c, wv).Grow(1)
}

// dashWeb is the web view an HTML panel's tile shows, for the HTML it was made for.
type dashWeb struct {
	view *mygo.WebView
	key  string
}

// htmlTileView returns the web view of an HTML panel's tile; nil outside the app.
func (a *App) htmlTileView(p *PanelSpec) *mygo.WebView {
	if !a.desktop || a.studioWin == nil || a.studioWin.IsDestroyed() {
		return nil
	}
	key := windowKey(p)
	if w, ok := a.dashWebs[p.ID]; ok {
		if w.key == key {
			return w.view
		}
		w.view.Destroy()
		delete(a.dashWebs, p.ID)
	}
	wv, err := a.studioWin.NewWebView(mygo.WebViewOptions{Transparent: true})
	if err != nil {
		logf("app", "%s: no web view for the dashboard: %v", p.ID, err)
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
	if a.dashWebs == nil {
		a.dashWebs = map[string]*dashWeb{}
	}
	a.dashWebs[p.ID] = &dashWeb{view: wv, key: key}
	return wv
}

// dropDashWebs closes the web views of tiles that are gone.
func (a *App) dropDashWebs() {
	for id, w := range a.dashWebs {
		if p := a.findPanel(id); p == nil || p.DashboardOff || p.Type != PanelTypeHTML {
			w.view.Destroy()
			delete(a.dashWebs, id)
		}
	}
}

// renderDashboardBar is the launcher's bar under the tiles: a name opens an app, a
// sentence goes to the agent; the best matches show above it while typing.
func (a *App) renderDashboardBar(c *ui.Context) {
	t := c.Theme()
	var items []launchItem
	if strings.TrimSpace(a.dashQuery) != "" {
		items = a.launcherItems(a.dashQuery)
		if len(items) > 6 {
			items = items[:6]
		}
	}
	if a.dashSel >= len(items) {
		a.dashSel = max(len(items)-1, 0)
	}
	run := func(it launchItem) {
		a.dashQuery, a.dashSel = "", 0
		it.run()
	}
	bar := ui.Column(c).FillWidth().Padding(10, 20, 12, 20).Gap(6).Background(t.Surface)
	if files := bar.DroppedFiles(); files != nil {
		a.addImages(&a.launchImages, files)
	}
	if bar.FileDragOver() {
		bar.Border(2, t.Accent)
	}
	bar.Children(func() {
		for i, it := range items {
			row := it
			row.run = func() { run(it) }
			a.renderLaunchRow(c, row, i == a.dashSel, true)
		}
		if len(a.launchImages) > 0 {
			a.renderImageAttachments(c, "dash-images", &a.launchImages)
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			in := ui.SearchField(c.Key("dash-q"), &a.dashQuery).
				Placeholder(T("App öffnen oder die KI fragen …")).FontSize(14).Grow(1).
				OnPaste(func(text string) bool { return text == "" && a.pasteImage(&a.launchImages) })
			if in.Changed() {
				a.dashSel = 0
			}
			if in.Shortcut(0, ui.KeyDown) && a.dashSel < len(items)-1 {
				a.dashSel++
			}
			if in.Shortcut(0, ui.KeyUp) && a.dashSel > 0 {
				a.dashSel--
			}
			if in.Shortcut(0, ui.KeyTab) {
				a.nextAgent()
			}
			if in.Submitted() && a.dashSel < len(items) {
				run(items[a.dashSel])
			}
			a.renderAgentPill(c, "dash-agent")
		})
	})
}
