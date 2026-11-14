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

const actionTimeout = 30 * time.Second

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
	case BuiltinAgentUsage:
		a.renderAgentUsageModule(c, p, onClose)
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

		// Scrollable body for metrics & rows so long lists never clip inside tiles or popovers
		ui.Scroll(c.Key("pop-scroll-" + p.ID)).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
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
									ui.Row(c).FillWidth().Height(5).Radius(3).Background(t.Border).Clip().Children(func() {
										ui.Row(c).Grow(float32(pct)).Basis(0).FillHeight().Radius(3).Background(mFg)
										if rem := 100 - pct; rem > 0 {
											ui.Row(c).Grow(float32(rem)).Basis(0).FillHeight()
										}
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
			})
		})

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

// runPanelAction runs an action button's command (with a timeout and error feedback) and opens its URL.
func (a *App) runPanelAction(p *PanelSpec, act PanelAction) {
	if act.Command != "" && a.isHeld(p) {
		a.toast(fmt.Sprintf(T("„%s“ wartet auf deine Freigabe im Hauptfenster"), p.Title))
		return
	}
	if act.Command != "" {
		logf("panel", "%s: action %q", p.ID, act.Label)
		env := panelEnv(a.Vault, a.Jira)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, scriptShell(), "-c", act.Command)
			cmd.Env = append(cmd.Environ(), env...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				detail := maskSecrets(firstLines(strings.TrimSpace(string(out)), 1, 140))
				if ctx.Err() == context.DeadlineExceeded {
					detail = fmt.Sprintf(T("nach %d s abgebrochen"), int(actionTimeout.Seconds()))
				} else if detail == "" {
					detail = err.Error()
				}
				logf("panel", "%s: action %q failed: %v (%s)", p.ID, act.Label, err, detail)
				a.updateUI(func() {
					a.toast(fmt.Sprintf(T("Aktion „%s“ fehlgeschlagen: %s"), act.Label, detail))
				})
			}
		}()
	}
	if act.URL != "" {
		_ = mygo.Shell.OpenExternal(act.URL)
	}
}
