package omamac

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// moduleInstanceSettings describes the ⚙️ view of Jira Activity and Jira Week: a switch per
// instance (is it part of this module) and one text field (projects or JQL).
type moduleInstanceSettings struct {
	key, intro, placeholder, saved string
	off                            func(*JiraInstanceConfig) *bool
	text                           func(*JiraInstanceConfig) *string
	reload                         func()
}

func (a *App) renderModuleInstances(c *ui.Context, p *PanelSpec, errs map[string]string, m moduleInstanceSettings) {
	t := c.Theme()
	j := a.Jira
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, m.intro).FontSize(11.5).TextColor(t.TextMuted).Grow(1).MinWidth(0)
				if ui.Button(c.Key(m.key+"-manage"), "Instanzen verwalten …").
					Tooltip("Instanzen anlegen, Zugänge ändern, ganz abschalten (Jira Quick-Access)").Clicked() {
					a.openJiraInstanceSettings()
				}
			})
			shown := 0
			for idx := range j.Instances {
				i := idx
				inst := &j.Instances[i]
				if !inst.Enabled {
					continue // switched off for every module in Jira Quick-Access
				}
				shown++
				on := !*m.off(inst)
				border := t.Border
				if on {
					border = t.Accent.Alpha(0.45)
				}
				ui.Column(c.Key(fmt.Sprintf("%s-cfg-%d", m.key, i))).FillWidth().Padding(8, 10).Radius(8).Border(1, border).Background(t.Surface).Gap(6).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						if ui.Switch(c.Key(fmt.Sprintf("%s-cfg-on-%d", m.key, i)), &on).Changed() {
							*m.off(inst) = !on
							_ = j.Save()
							m.reload()
						}
						ui.Text(c, inst.Name).FontSize(12).Bold()
						ui.Text(c, inst.BaseURL).FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
						if e := errs[inst.ID]; e != "" && on {
							pillBadge(c, "FEHLER: "+truncateStr(e, 30), t.Danger.Alpha(0.2), t.Danger)
						}
					})
					if !on {
						return
					}
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
						ui.TextInput(c.Key(fmt.Sprintf("%s-cfg-txt-%d", m.key, i)), m.text(inst)).Placeholder(m.placeholder).Grow(1)
						if ui.Button(c.Key(fmt.Sprintf("%s-cfg-save-%d", m.key, i)), "Speichern").Clicked() {
							_ = j.Save()
							m.reload()
							a.toast(m.saved + " („" + inst.Name + "“)")
						}
					})
				})
			}
			if shown == 0 {
				ui.Text(c, "Noch keine Jira-Instanz – über „Instanzen verwalten …“ anlegen.").FontSize(12).TextColor(t.TextMuted)
			}
		})
	})
}

// openJiraInstanceSettings opens Jira Quick-Access in its window on the ⚙️ JQL tab, where instances are managed.
func (a *App) openJiraInstanceSettings() {
	for _, p := range a.panels {
		if p.BuiltinKind == BuiltinJiraQuickAccess {
			a.OpenFullJiraBoardWindow(p)
			a.Jira.ViewMode = 2
			if a.Jira.boardWin != nil && !a.Jira.boardWin.IsDestroyed() {
				a.Jira.boardWin.Invalidate()
			}
			return
		}
	}
}
