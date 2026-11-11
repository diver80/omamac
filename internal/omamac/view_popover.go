package omamac

import (
	"fmt"
	"os/exec"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// renderNativePanelPopover renders a PanelSpec of Type == "native": in the menu bar
// popover, the desktop widget and the preview of the main window.
// onClose, when not nil, adds a ✕ button to the header (menu bar popover and desktop widget).
func (a *App) renderNativePanelPopover(c *ui.Context, p *PanelSpec, _ *panelRuntime, onClose func()) {
	if p == nil {
		return
	}
	switch p.BuiltinKind {
	case BuiltinJiraQuickAccess:
		a.renderJiraQuickAccessModule(c, p, onClose)
		return
	case BuiltinJiraActivity:
		a.renderJiraActivityModule(c, p, onClose)
		return
	case BuiltinJiraWeek:
		a.renderJiraWeekModule(c, p, onClose)
		return
	}
	t := c.Theme()

	ui.Column(c).Fill().Padding(14).Gap(10).Children(func() {
		// Header: what it is and how it is doing
		a.panelHeader(ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8), p).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, p.Icon+" "+T(p.Title)).FontSize(14).Bold().SingleLine()
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					status, text := a.panelStatusLine(p)
					statusDot(c, status)
					ui.Text(c, text).FontSize(11).TextColor(t.TextMuted).SingleLine()
				})
			})
			if ui.Button(c.Key("pop-ref-"+p.ID), "↻").Tooltip(T("Jetzt aktualisieren")).Clicked() {
				a.refreshPanelAsync(p, "")
			}
			closeButton(c, "pop-close-"+p.ID, onClose)
		})

		if p.Subtitle != "" {
			ui.Text(c, T(p.Subtitle)).FontSize(11).TextColor(t.TextMuted).SingleLine()
		}

		// Metrics (two per row)
		if len(p.Metrics) > 0 {
			ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
				for idx, m := range p.Metrics {
					mBg, mFg := statusColors(t, m.Status)
					ui.Column(c.Key(fmt.Sprintf("m-%s-%d", p.ID, idx))).
						Grow(1).MinWidth(150).Padding(10).Radius(8).Border(1, t.Border).Background(t.Background).Gap(4).Children(func() {
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

		// Rows (each may link to a URL, as the mydealz deals do)
		if len(p.Rows) > 0 {
			ui.Column(c).FillWidth().Padding(8, 10).Radius(8).Border(1, t.Border).Background(t.Background).Gap(6).Children(func() {
				for idx, r := range p.Rows {
					rCopy := r
					rBg, rFg := statusColors(t, rCopy.Status)
					ui.Row(c.Key(fmt.Sprintf("r-%s-%d", p.ID, idx))).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, rCopy.Left).FontSize(11.5).TextColor(t.TextMuted).Grow(1).SingleLine()
						if rCopy.URL != "" {
							ui.Link(c.Key(fmt.Sprintf("rl-%s-%d", p.ID, idx)), rCopy.Right, rCopy.URL).FontSize(11.5) // opens the URL itself
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

		// Actions of the panel
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
						a.runPanelAction(p, actCopy)
					}
				}
			})
		}
	})
}

// runPanelAction runs an action button's command (with the vault's variables) and opens its URL.
func (a *App) runPanelAction(p *PanelSpec, act PanelAction) {
	if act.Command != "" && a.isHeld(p) {
		a.toast(fmt.Sprintf(T("„%s“ wartet auf deine Freigabe im Hauptfenster"), p.Title))
		return
	}
	if act.Command != "" {
		logf("panel", "%s: action %q", p.ID, act.Label)
		cmd := exec.Command("/bin/zsh", "-c", act.Command)
		cmd.Env = append(cmd.Environ(), panelEnv(a.Vault, a.Jira)...)
		if cmd.Start() == nil {
			go func() { _ = cmd.Wait() }() // reap the process (no zombies)
		}
	}
	if act.URL != "" {
		_ = mygo.Shell.OpenExternal(act.URL)
	}
}
