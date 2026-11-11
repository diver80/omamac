package omamac

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// ModuleConfig holds the settings of a built-in module in its panel, so one module can run as
// several panels (say, a week board per customer). AI panels keep theirs in their command.
type ModuleConfig struct {
	Sources   []JiraSource `json:"sources,omitempty"`    // per Jira instance; one not listed is on, unfiltered
	Scope     string       `json:"scope,omitempty"`      // jira-activity: "mine" (default) or "all"
	DateField string       `json:"date_field,omitempty"` // jira-week: "updated" (default), "resolved" or "due"
}

// JiraSource is what a module panel takes from one Jira instance.
type JiraSource struct {
	Instance string `json:"instance"` // JiraInstanceConfig.ID
	Off      bool   `json:"off,omitempty"`
	Filter   string `json:"filter,omitempty"` // jira-activity: project keys; jira-week: JQL
}

// Configurable reports whether p is a built-in module with settings of its own.
func (p *PanelSpec) Configurable() bool {
	return p.BuiltinKind == BuiltinJiraActivity || p.BuiltinKind == BuiltinJiraWeek
}

// Deletable: AI panels and copies of built-in modules; the delivered modules stay.
func (p *PanelSpec) Deletable() bool {
	if !p.IsBuiltin {
		return true
	}
	for _, b := range BuiltinSystemPanels() {
		if b.ID == p.ID {
			return false
		}
	}
	return true
}

func (p *PanelSpec) moduleConfig() *ModuleConfig {
	if p.Config == nil {
		p.Config = &ModuleConfig{}
	}
	return p.Config
}

// source is the setting for one instance (on and unfiltered when there is none).
func (m *ModuleConfig) source(id string) JiraSource {
	if m != nil {
		for _, s := range m.Sources {
			if s.Instance == id {
				return s
			}
		}
	}
	return JiraSource{Instance: id}
}

// sourcePtr is the setting for one instance, added when missing, to bind to a switch or field.
func (m *ModuleConfig) sourcePtr(id string) *JiraSource {
	for i := range m.Sources {
		if m.Sources[i].Instance == id {
			return &m.Sources[i]
		}
	}
	m.Sources = append(m.Sources, JiraSource{Instance: id})
	return &m.Sources[len(m.Sources)-1]
}

// panelSource is an instance a module panel reads, with that panel's filter for it.
type panelSource struct {
	JiraInstanceConfig
	Filter string
}

// sourcesFor lists the instances of a module panel: enabled in Jira Quick-Access, not switched off in the panel.
func (j *JiraModuleState) sourcesFor(p *PanelSpec) []panelSource {
	var out []panelSource
	for _, inst := range j.activeInstances() {
		s := p.Config.source(inst.ID)
		if !s.Off {
			out = append(out, panelSource{inst, strings.TrimSpace(s.Filter)})
		}
	}
	return out
}

var (
	activityScopes = [2]string{"mine", "all"}
	weekFields     = [3]string{"updated", "resolved", "due"}
)

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return 0
}

func activityScope(p *PanelSpec) int {
	if p.Config == nil {
		return activityMine
	}
	return indexOf(activityScopes[:], p.Config.Scope)
}

func weekField(p *PanelSpec) int {
	if p.Config == nil {
		return weekByUpdated
	}
	return indexOf(weekFields[:], p.Config.DateField)
}

// saveModuleConfig stores the panel after a change of its settings.
func (a *App) saveModuleConfig(p *PanelSpec) {
	if err := SavePanel(a.Config.PanelsDir, p); err != nil {
		a.toast(T("Speichern fehlgeschlagen: ") + err.Error())
	}
}

// renderModuleConfig is the settings form of a module panel: per instance a switch and its
// filter. It shows in the panel (⚙️) and in the main window under the preview.
func (a *App) renderModuleConfig(c *ui.Context, p *PanelSpec) {
	t := c.Theme()
	j := a.Jira
	if j == nil {
		return
	}
	cfg := p.moduleConfig()
	hint, placeholder := T("Projekte je Instanz, z. B. „ABC, XYZ“ – leer = alle Projekte."), T("alle Projekte")
	reload := func() {
		j.resetActivity(p.ID)
		a.refreshPanelAsync(p, "")
	}
	if p.BuiltinKind == BuiltinJiraWeek {
		hint, placeholder = T("JQL je Instanz, ohne Datum – OmaMac grenzt auf die Woche ein."), defaultWeekJQL
		reload = func() { a.reloadWeek(p) }
	}
	key := "mc-" + p.ID
	ui.Column(c.Key(key)).FillWidth().Gap(6).Children(func() {
		ui.Text(c, T("Instanzen: ")+hint).FontSize(11).TextColor(t.TextMuted)
		for _, inst := range j.Instances {
			cfg.sourcePtr(inst.ID) // all first: the pointers below stay valid
		}
		shown := 0
		for idx := range j.Instances {
			inst := j.Instances[idx]
			if !inst.Enabled {
				continue // off for every module, in Einstellungen → Jira-Instanzen
			}
			shown++
			src := cfg.sourcePtr(inst.ID)
			on := !src.Off
			ui.Row(c.Key(fmt.Sprintf("%s-%d", key, idx))).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				if ui.Switch(c.Key(fmt.Sprintf("%s-on-%d", key, idx)), &on).Tooltip(inst.BaseURL).Changed() {
					src.Off = !on
					a.saveModuleConfig(p)
					reload()
				}
				name := ui.Text(c, inst.Name).FontSize(12).Bold().Width(96).SingleLine().Tooltip(inst.BaseURL)
				if !on {
					name.TextColor(t.TextMuted)
				}
				ui.TextInput(c.Key(fmt.Sprintf("%s-f-%d", key, idx)), &src.Filter).Placeholder(placeholder).Grow(1).MinWidth(0).Disabled(!on)
			})
		}
		if shown == 0 {
			ui.Text(c, T("Noch keine Jira-Instanz eingerichtet.")).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			if ui.Button(c.Key(key+"-manage"), T("Instanzen verwalten …")).
				Tooltip(T("Einstellungen → Jira-Instanzen: anlegen, Zugänge ändern, für alle Module abschalten")).Clicked() {
				a.openJiraSettings()
			}
			ui.Spacer(c)
			if ui.PrimaryButton(c.Key(key+"-apply"), T("Übernehmen")).Tooltip(T("Filter speichern und neu laden")).Clicked() {
				a.saveModuleConfig(p)
				reload()
				a.toast(T("„") + p.Title + T("“: Einstellungen gespeichert"))
			}
		})
	})
}

// openJiraSettings shows the main window's settings, where the Jira instances are managed.
func (a *App) openJiraSettings() {
	a.setTab(TabSettings)
	if a.studioWin != nil {
		a.studioWin.Show()
		a.studioWin.Focus()
	}
}

// duplicatePanel adds a copy of a module panel with its settings, to configure differently.
func (a *App) duplicatePanel(p *PanelSpec) {
	cp := *p
	if p.Config != nil {
		cfg := *p.Config
		cfg.Sources = append([]JiraSource(nil), p.Config.Sources...)
		cp.Config = &cfg
	}
	base := p.ID
	for _, b := range BuiltinSystemPanels() {
		if b.BuiltinKind == p.BuiltinKind {
			base = b.ID
		}
	}
	for n := 2; ; n++ {
		if a.findPanel(fmt.Sprintf("%s-%d", base, n)) == nil {
			cp.ID = fmt.Sprintf("%s-%d", base, n)
			cp.Title = fmt.Sprintf("%s %d", strings.TrimRight(strings.TrimRight(p.Title, "0123456789"), " "), n)
			break
		}
	}
	cp.MenuBarText, cp.Enabled, cp.PinnedFloating = "", true, false
	a.saveModuleConfig(&cp)
	a.panels = append(a.panels, &cp)
	a.selectPanel(cp.ID)
	a.syncAllPanelRuntimes()
	a.refreshPanelAsync(&cp, "")
	a.openRename(&cp)
}
