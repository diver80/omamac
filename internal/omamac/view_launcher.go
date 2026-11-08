package omamac

import (
	"sort"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The launcher (global shortcut, ⌥ Space by default) is one search field over
// everything OmaMac can do from anywhere: switch panels, arrange the active
// window, start apps, open the main window, or describe a new panel for the AI.
// ↑↓ choose, ↵ runs, esc closes.

type launchKind int

const (
	launchPanel launchKind = iota
	launchWindow
	launchOmaMac
	launchApp
	launchAI
)

var launchKindLabels = map[launchKind]string{
	launchPanel:  "Panel",
	launchWindow: "Fenster",
	launchOmaMac: "OmaMac",
	launchApp:    "App",
	launchAI:     "KI",
}

var launchSectionTitles = map[launchKind]string{
	launchPanel:  "Panels",
	launchWindow: "Aktives Fenster anordnen",
	launchOmaMac: "OmaMac",
	launchApp:    "Programme",
}

type launchItem struct {
	kind     launchKind
	key      string
	title    string
	subtitle string
	glyph    string     // shown when there is no app icon
	appPath  string     // for the app's own icon
	panel    *PanelSpec // panel items: Enter switches its menu bar entry
	run      func()
}

var windowTiles = []struct {
	title, glyph string
	layout       TileLayout
}{
	{"Links 50 %", "◧", TileLeftHalf},
	{"Rechts 50 %", "◨", TileRightHalf},
	{"Vollbild", "▣", TileMaximize},
	{"Zentriert 80 %", "⊡", TileCenter80},
	{"Unten 50 %", "⬓", TileBottomHalf},
}

// launcherItems lists what matches query: everything grouped by kind while it
// is empty, else the matches by quality, then the AI's offer to build it.
func (a *App) launcherItems(query string) []launchItem {
	var all []launchItem
	for _, p := range a.panels {
		p := p
		all = append(all, launchItem{
			kind: launchPanel, key: "panel-" + p.ID, title: p.Title, subtitle: panelStatusText(p),
			glyph: p.Icon, panel: p,
			run: func() {
				p.Enabled = !p.Enabled
				msg := "„" + p.Title + "“ aus der Menüleiste genommen"
				if p.Enabled {
					msg = "„" + p.Title + "“ in der Menüleiste"
				}
				a.persistPanelToggle(p, msg)
			},
		})
	}
	for _, tl := range windowTiles {
		tl := tl
		all = append(all, launchItem{
			kind: launchWindow, key: "tile-" + string(tl.layout), title: tl.title, glyph: tl.glyph,
			run: func() {
				a.hideLauncher()
				disp := mygo.Screen.PrimaryDisplay()
				_ = ApplyWindowTile(tl.layout, disp.WorkArea.X, disp.WorkArea.Y, disp.WorkArea.Width, disp.WorkArea.Height)
			},
		})
	}
	all = append(all,
		launchItem{kind: launchOmaMac, key: "omamac-main", title: "Hauptfenster öffnen", glyph: "⌘", run: a.showStudio},
		launchItem{kind: launchOmaMac, key: "omamac-new", title: "Neues Panel …", glyph: "+", run: func() { a.openCreate("") }},
		launchItem{kind: launchOmaMac, key: "omamac-settings", title: "Einstellungen", glyph: "⚙", run: func() {
			a.activeTab = TabSettings
			a.showStudio()
		}},
	)
	for _, app := range a.apps {
		app := app
		all = append(all, launchItem{
			kind: launchApp, key: app.ID, title: app.Title, appPath: app.Subtitle, glyph: "◻",
			run: func() {
				a.hideLauncher()
				_ = app.Run()
			},
		})
	}

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return all
	}

	type scored struct {
		item  launchItem
		score int
	}
	var hits []scored
	for _, it := range all {
		if s := matchScore(strings.ToLower(it.title), q); s >= 0 {
			hits = append(hits, scored{it, s})
		}
	}
	// Better matches first; apps before the rest at the same quality, as they are what is typed most.
	rank := map[launchKind]int{launchApp: 0, launchPanel: 1, launchWindow: 2, launchOmaMac: 3}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		return rank[hits[i].item.kind] < rank[hits[j].item.kind]
	})
	items := make([]launchItem, 0, len(hits)+1)
	for _, h := range hits {
		items = append(items, h.item)
	}
	prompt := strings.TrimSpace(query)
	items = append(items, launchItem{
		kind: launchAI, key: "ai-create", title: "Neues Panel mit KI: „" + prompt + "“", glyph: "✨",
		subtitle: "Öffnet den Dialog im Hauptfenster",
		run:      func() { a.openCreate(prompt) },
	})
	return items
}

// matchScore rates how title matches q: 0 starts with it, 1 a word starts with
// it, 2 contains it, 3 has its letters in order (from three letters on, as
// shorter queries would match almost everything); -1 no match.
func matchScore(title, q string) int {
	switch {
	case strings.HasPrefix(title, q):
		return 0
	case strings.Contains(title, " "+q) || strings.Contains(title, "-"+q):
		return 1
	case strings.Contains(title, q):
		return 2
	case len(q) < 3:
		return -1
	}
	i := 0
	for _, r := range title {
		if i < len(q) && strings.HasPrefix(q[i:], string(r)) {
			i += len(string(r))
		}
	}
	if i == len(q) {
		return 3
	}
	return -1
}

// RenderWalPanelView renders the launcher window.
func (a *App) RenderWalPanelView(c *ui.Context) {
	t := c.Theme()
	items := a.launcherItems(a.launcherQuery)
	if a.launchSel >= len(items) {
		a.launchSel = len(items) - 1
	}
	if a.launchSel < 0 {
		a.launchSel = 0
	}
	grouped := strings.TrimSpace(a.launcherQuery) == ""

	ui.Column(c).Fill().Padding(12, 12, 8, 12).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).DragWindow().Children(func() {
			in := ui.SearchField(c.Key("wal-q"), &a.launcherQuery).AutoFocus().
				Placeholder("Programme, Panels, Fenster – oder ein neues Panel beschreiben").
				FontSize(15).Grow(1)
			if in.Changed() {
				a.launchSel = 0
				a.launchList.ScrollTo(0, ui.Start)
			}
			if in.Shortcut(0, ui.KeyDown) && a.launchSel < len(items)-1 {
				a.launchSel++
				a.launchList.ScrollIntoView(a.launchSel)
			}
			if in.Shortcut(0, ui.KeyUp) && a.launchSel > 0 {
				a.launchSel--
				a.launchList.ScrollIntoView(a.launchSel)
			}
			if in.Submitted() && a.launchSel < len(items) {
				items[a.launchSel].run()
			}
		})
		if c.Shortcut(0, ui.KeyEscape) {
			a.hideLauncher()
		}

		if a.busy {
			ui.Row(c).FillWidth().Padding(0, 6).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(12, 12)
				ui.Text(c, a.busyLabel).FontSize(11.5).TextColor(t.TextMuted)
			})
		}

		a.launchList.Key = func(i int) any { return items[i].key }
		ui.List(c.Key("wal-list"), &a.launchList, len(items), func(i int) {
			it := items[i]
			if grouped && (i == 0 || items[i-1].kind != it.kind) {
				ui.Text(c, launchSectionTitles[it.kind]).FontSize(10.5).Bold().TextColor(t.TextMuted).Padding(10, 10, 4, 10)
			}
			a.renderLaunchRow(c, it, i == a.launchSel, !grouped)
		}).Grow(1).FillWidth()

		// What the keys do, for the row chosen
		ui.Row(c).FillWidth().Padding(4, 6, 0, 6).Gap(14).AlignItems(ui.Center).Children(func() {
			action := "öffnen"
			if a.launchSel < len(items) {
				switch items[a.launchSel].kind {
				case launchPanel:
					action = "Menüleiste an/aus"
				case launchWindow:
					action = "anordnen"
				case launchApp:
					action = "starten"
				}
			}
			ui.Spacer(c)
			for _, hint := range []string{"↑↓ auswählen", "↵ " + action, "esc schließen"} {
				ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted)
			}
		})
	})
}

func (a *App) renderLaunchRow(c *ui.Context, it launchItem, selected, showKind bool) {
	t := c.Theme()
	row := ui.Row(c.Key("launch-"+it.key)).FillWidth().Padding(7, 10).Radius(8).AlignItems(ui.Center).Gap(10).
		Cursor(ui.CursorPointer).OnClick(it.run)
	switch {
	case selected:
		row.Background(t.Accent.Alpha(0.16))
	case row.Hovered():
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		if icon := a.appIcon(it.appPath); icon != nil {
			ui.Image(c, icon).Size(24, 24)
		} else {
			ui.Row(c).Size(24, 24).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, it.glyph).FontSize(16).TextColor(t.TextMuted)
			})
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, it.title).FontSize(13.5).SingleLine()
			if it.panel != nil {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					statusDot(c, it.panel.Status)
					ui.Text(c, it.subtitle).FontSize(11).TextColor(t.TextMuted).SingleLine()
				})
			} else if it.subtitle != "" {
				ui.Text(c, it.subtitle).FontSize(11).TextColor(t.TextMuted).SingleLine()
			}
		})
		if showKind {
			ui.Text(c, launchKindLabels[it.kind]).FontSize(11).TextColor(t.TextMuted)
		}
		if p := it.panel; p != nil {
			if ui.Switch(c.Key("launch-sw-"+p.ID), &p.Enabled).Label("Menüleiste").Changed() {
				msg := "„" + p.Title + "“ aus der Menüleiste genommen"
				if p.Enabled {
					msg = "„" + p.Title + "“ in der Menüleiste"
				}
				a.persistPanelToggle(p, msg)
			}
		}
	})
}

// appIconCache holds the apps' icons, loaded from the system as their rows first show.
type appIconCache struct {
	mu      sync.Mutex
	icons   map[string]ui.ImageSource
	loading map[string]bool
}

// appIcon returns the icon of the app at path, nil until it has loaded (or
// outside the running app, as in tests).
func (a *App) appIcon(path string) ui.ImageSource {
	if path == "" || a.walWin == nil {
		return nil
	}
	ic := &a.iconCache
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if src, ok := ic.icons[path]; ok {
		return src
	}
	if ic.loading == nil {
		ic.loading = map[string]bool{}
		ic.icons = map[string]ui.ImageSource{}
	}
	if !ic.loading[path] {
		ic.loading[path] = true
		go func() {
			var src ui.ImageSource
			if png, err := mygo.Shell.FileIcon(path, 64); err == nil {
				src, _ = ui.DecodeImage(png)
			}
			ic.mu.Lock()
			ic.icons[path] = src // nil when it failed: the glyph stays
			ic.mu.Unlock()
			if a.walWin != nil {
				a.walWin.Invalidate()
			}
		}()
	}
	return nil
}

func (a *App) hideLauncher() {
	if a.walWin != nil {
		a.walWin.Hide()
	}
}

func (a *App) showStudio() {
	a.hideLauncher()
	if a.studioWin != nil {
		a.studioWin.Show()
		a.studioWin.Focus()
	}
}
