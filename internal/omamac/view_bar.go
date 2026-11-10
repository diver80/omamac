package omamac

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The launcher's bar as a desktop widget (Config.LauncherWidget), like Omalauncher's bar
// at the bottom of the home screen: a click opens the launcher right above it, images
// dropped on it go along to the agent. It floats above other windows, as OmaMac's
// widgets do; ⌥ Space stays the quickest way in.

const (
	barWidth, barHeight           = 560, 50
	launcherWidth, launcherHeight = 680, 480
)

// syncLauncherBar shows or removes the bar as the setting says.
func (a *App) syncLauncherBar() {
	if !a.desktop {
		return
	}
	if !a.Config.LauncherWidget {
		if a.barWin != nil && !a.barWin.IsDestroyed() {
			a.barWin.Destroy()
		}
		a.barWin = nil
		return
	}
	if a.barWin != nil && !a.barWin.IsDestroyed() {
		return
	}
	wa := mygo.Screen.PrimaryDisplay().WorkArea
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             T("OmaMac-Leiste"),
		Width:             barWidth,
		Height:            barHeight,
		X:                 wa.X + (wa.Width-barWidth)/2,
		Y:                 wa.Y + wa.Height - barHeight - 28,
		Frameless:         true,
		AlwaysOnTop:       true,
		Vibrancy:          mygo.VibrancyGlass,
		CornerRadius:      14,
		HideWindowButtons: true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		StateKey:          "omamac-launcher-bar",
		Content:           ui.View(a.renderLauncherBar),
	})
	win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting && a.Config.LauncherWidget { // ⌘W: the bar stays off
			a.Config.LauncherWidget = false
			_ = SaveConfig(a.ConfigPath, a.Config)
		}
		a.barWin = nil
	})
	a.barWin = win
}

// setLauncherWidget puts the bar on the desktop or takes it away.
func (a *App) setLauncherWidget(on bool) {
	a.Config.LauncherWidget = on
	_ = SaveConfig(a.ConfigPath, a.Config)
	a.syncLauncherBar()
	if on {
		a.toast(T("Leiste auf dem Desktop – ein Klick öffnet den Schnellstart"))
	}
}

// openLauncherFromBar opens the launcher above the bar (below it at the top of the screen).
func (a *App) openLauncherFromBar() {
	if a.walWin == nil {
		return
	}
	if a.walWin.IsVisible() || time.Since(a.walBlurAt) < 400*time.Millisecond {
		a.hideLauncher() // the click that took the focus from the open launcher closes it
		return
	}
	if a.barWin == nil || a.barWin.IsDestroyed() {
		a.showLauncher(nil)
		return
	}
	b := a.barWin.Bounds()
	at := mygo.Point{X: b.X + (b.Width-launcherWidth)/2, Y: b.Y - launcherHeight - 10}
	if wa := mygo.Screen.PrimaryDisplay().WorkArea; at.Y < wa.Y {
		at.Y = b.Y + b.Height + 10
	}
	a.showLauncher(&at)
}

// renderLauncherBar is the bar: a handle to move it, the field that opens the launcher,
// where sentences go, and ✕.
func (a *App) renderLauncherBar(c *ui.Context) {
	t := c.Theme()
	row := ui.Row(c).Fill().Padding(7, 8, 7, 8).Gap(8).AlignItems(ui.Center)
	if files := row.DroppedFiles(); files != nil {
		a.addImages(&a.launchImages, files)
		a.openLauncherFromBar()
	}
	if row.FileDragOver() {
		row.Border(2, t.Accent).Radius(14)
	}
	row.Children(func() {
		ui.Row(c).Width(14).FillHeight().AlignItems(ui.Center).Justify(ui.Center).DragWindow().Children(func() {
			ui.Text(c, "⠿").FontSize(14).TextColor(t.TextMuted)
		})
		field := ui.Row(c.Key("bar-field")).Grow(1).MinWidth(0).FillHeight().Padding(0, 12).Radius(9).
			Background(t.Background).AlignItems(ui.Center).Gap(8).Cursor(ui.CursorPointer).
			OnClick(a.openLauncherFromBar).Tooltip(T("Schnellstart öffnen – Bilder hierher ziehen"))
		if field.Hovered() {
			field.Background(t.SurfaceHover)
		}
		field.Children(func() {
			ui.Text(c, "🔍").FontSize(12)
			ui.Text(c, T("Apps öffnen oder die KI fragen …")).FontSize(13).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
			if n := len(a.launchImages); n > 0 {
				ui.Text(c, fmt.Sprintf("🖼 %d", n)).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Text(c, shortcutLabel(a.Config.LauncherShortcut)).FontSize(11).TextColor(t.TextMuted)
		})
		a.renderAgentPill(c, "bar-agent")
		closeButton(c, "bar-close", func() { a.setLauncherWidget(false) })
	})
}
