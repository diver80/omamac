package omamac

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// BuiltinJiraWeek is a Kanban board of one week across all Jira instances: a column per day,
// paged week by week. A ticket sits on the day it was last changed, resolved or is due.
const BuiltinJiraWeek = "jira-week"

const defaultWeekJQL = "assignee = currentUser()"

// Week board date fields, in the order of the segmented control.
const (
	weekByUpdated = iota
	weekByResolved
	weekByDue
)

var weekFieldLabels = [3]string{"Geändert", "Erledigt", "Fällig"}

type weekState struct {
	Offset int // 0 = this week, -1 = last week, …
	Field  int // weekByUpdated, weekByResolved, weekByDue

	FilterInst string
	ShowConfig bool

	// The week on display and what it was loaded for (Offset/Field may already point elsewhere).
	Issues    []weekCard
	LoadedFor string
	Errors    map[string]string
	LastSync  string
	Loading   bool
}

// weekCard is a ticket on the board; Day is its column, before the week's Monday = overdue.
type weekCard struct {
	JiraIssue
	Day time.Time
}

// weekStart is the Monday 00:00 (local) of the week offset weeks from now's.
func weekStart(now time.Time, offset int) time.Time {
	y, m, d := now.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	return day.AddDate(0, 0, -((int(day.Weekday())+6)%7)+7*offset)
}

func weekKey(offset, field int) string { return fmt.Sprintf("%d/%d", offset, field) }

// weekTitle is "KW 46 · 09.–15.11.2026".
func weekTitle(start time.Time) string {
	_, kw := start.ISOWeek()
	end := start.AddDate(0, 0, 6)
	from := start.Format("02.01.")
	if start.Month() == end.Month() {
		from = start.Format("02.")
	}
	return fmt.Sprintf("KW %d · %s–%s", kw, from, end.Format("02.01.2006"))
}

// weekJQL limits an instance's week JQL to the week (and, for due dates this week, to what is overdue).
func weekJQL(base string, field int, start time.Time, overdue bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = defaultWeekJQL
	}
	if i := strings.Index(strings.ToUpper(base), "ORDER BY"); i >= 0 {
		base = strings.TrimSpace(base[:i])
	}
	name := [3]string{"updated", "resolutiondate", "duedate"}[field]
	from, to := start.Format("2006-01-02"), start.AddDate(0, 0, 7).Format("2006-01-02")
	cond := fmt.Sprintf(`%s >= "%s" AND %s < "%s"`, name, from, name, to)
	if overdue {
		cond = fmt.Sprintf(`(%s OR (duedate < "%s" AND resolution is EMPTY))`, cond, from)
	}
	return fmt.Sprintf("(%s) AND %s ORDER BY %s ASC", base, cond, name)
}

// weekSt is the runtime state of one Jira Week panel; it starts on the panel's date field.
func (j *JiraModuleState) weekSt(p *PanelSpec) *weekState {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.weeks == nil {
		j.weeks = map[string]*weekState{}
	}
	st := j.weeks[p.ID]
	if st == nil {
		st = &weekState{Field: weekField(p)}
		j.weeks[p.ID] = st
	}
	return st
}

// RefreshWeekPanel loads the week the board shows from the panel's instances.
func (j *JiraModuleState) RefreshWeekPanel(ctx context.Context, p *PanelSpec) {
	st := j.weekSt(p)
	j.mu.Lock()
	offset, field := st.Offset, st.Field
	st.Loading = true
	j.mu.Unlock()

	start := weekStart(time.Now(), offset)
	overdue := field == weekByDue && offset == 0
	srcs := j.sourcesFor(p)

	// Results in the order of the instances: of two instances on one site, the first names a ticket.
	type res struct {
		found []foundIssue
		err   error
	}
	results := make([]res, len(srcs))
	var wg sync.WaitGroup
	for i, src := range srcs {
		wg.Add(1)
		go func(i int, src panelSource) {
			defer wg.Done()
			f, err := j.searchJQL(ctx, src.JiraInstanceConfig, weekJQL(src.Filter, field, start, overdue), 100)
			results[i] = res{f, err}
		}(i, src)
	}
	wg.Wait()
	var cards []weekCard
	errs := map[string]string{}
	seen := map[string]bool{}
	for i, r := range results {
		if r.err != nil {
			errs[srcs[i].ID] = r.err.Error()
			continue
		}
		for _, f := range r.found {
			if seen[f.BaseURL+"|"+f.Key] {
				continue
			}
			seen[f.BaseURL+"|"+f.Key] = true
			day := [3]time.Time{f.Updated, f.Resolved, f.Due}[field]
			if day.IsZero() {
				continue
			}
			cards = append(cards, weekCard{JiraIssue: f.JiraIssue, Day: day})
		}
	}
	sort.SliceStable(cards, func(a, b int) bool { return cards[a].Day.Before(cards[b].Day) })

	j.mu.Lock()
	// A page turned while loading: keep the old result out, the newer refresh brings its own.
	stale := st.Offset != offset || st.Field != field
	if !stale {
		st.Issues = cards
		st.LoadedFor = weekKey(offset, field)
		st.Errors = errs
		st.LastSync = time.Now().Format("15:04:05")
		st.Loading = false
	}
	j.mu.Unlock()
	if !stale {
		syncWeekPanel(p, cards, start, field, len(srcs), len(errs))
	}
}

func syncWeekPanel(p *PanelSpec, cards []weekCard, start time.Time, field, asked, failed int) {
	if p == nil {
		return
	}
	_, kw := start.ISOWeek()
	open, overdue := 0, 0
	for _, cd := range cards {
		if cd.Day.Before(start) {
			overdue++
		} else if cd.ColumnCategory() != 2 {
			open++
		}
	}
	inWeek := len(cards) - overdue
	p.LastUpdated = time.Now().Format("15:04:05")
	p.BadgeText = fmt.Sprintf("KW%d · %d", kw, inWeek)
	p.Status = "ok"
	if overdue > 0 {
		p.BadgeText += fmt.Sprintf(" · %d überfällig", overdue)
		p.Status = "warn"
	}
	switch {
	case asked == 0:
		p.BadgeText, p.Status = "keine Instanz", "info"
	case failed == asked:
		p.BadgeText, p.Status = "Abruf ⚠", "crit"
	case failed > 0:
		p.Status = "warn"
	}
	p.Metrics = []PanelMetric{
		{Label: weekFieldLabels[field] + " " + weekTitle(start), Value: fmt.Sprint(inWeek), Unit: "Tickets", Status: "info"},
		{Label: "Noch offen", Value: fmt.Sprint(open), Unit: fmt.Sprintf("%d überfällig", overdue), Status: p.Status},
	}
	var rows []PanelRow
	for _, cd := range cards {
		if len(rows) == 5 {
			break
		}
		rows = append(rows, PanelRow{Left: fmt.Sprintf("[%s] %s", cd.Key, truncateStr(cd.Summary, 38)),
			Right: cd.InstanceName, Badge: weekdayShort[(int(cd.Day.Weekday())+6)%7], Status: "info", URL: cd.URL})
	}
	p.Rows = rows
}

// turnWeek pages the board (delta weeks; 0 = back to this week) and loads the new week.
func (a *App) turnWeek(p *PanelSpec, delta int) {
	j := a.Jira
	st := j.weekSt(p)
	j.mu.Lock()
	if delta == 0 {
		st.Offset = 0
	} else {
		st.Offset += delta
	}
	j.mu.Unlock()
	a.reloadWeek(p)
}

func (a *App) reloadWeek(p *PanelSpec) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		a.Jira.RefreshWeekPanel(ctx, p)
		a.updateUI(func() { a.syncAllPanelRuntimes() })
	}()
}

// renderJiraWeekModule draws the week board: navigation, then Mon–Sun (and overdue) as columns.
func (a *App) renderJiraWeekModule(c *ui.Context, p *PanelSpec, onClose func()) {
	t := c.Theme()
	j := a.Jira
	if j == nil {
		return
	}
	sBg, sFg := statusColors(t, p.Status)
	st := j.weekSt(p)
	j.mu.RLock()
	offset, field := st.Offset, st.Field
	current := st.LoadedFor == weekKey(offset, field)
	var cards []weekCard
	perInst := map[string]int{}
	if current {
		for _, cd := range st.Issues {
			perInst[cd.InstanceID]++
			if st.FilterInst == "" || cd.InstanceID == st.FilterInst {
				cards = append(cards, cd)
			}
		}
	}
	errs := map[string]string{}
	for k, v := range st.Errors {
		errs[k] = v
	}
	loading, lastSync := st.Loading, st.LastSync
	j.mu.RUnlock()
	instances := j.sourcesFor(p)

	start := weekStart(time.Now(), offset)
	overdueCol := field == weekByDue && offset == 0
	var cols [8][]weekCard // 0 = overdue, 1..7 = Mon..Sun
	for _, cd := range cards {
		if cd.Day.Before(start) {
			cols[0] = append(cols[0], cd)
			continue
		}
		if d := daysBetween(start, cd.Day); d >= 0 && d < 7 {
			cols[d+1] = append(cols[d+1], cd)
		}
	}

	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).DragWindow().Children(func() {
			ui.Text(c, p.Icon+" "+p.Title).FontSize(14).Bold().Grow(1).MinWidth(120).SingleLine()
			pillBadge(c, p.BadgeText, sBg, sFg)
			if ui.Button(c.Key("wk-refresh"), "↻").Tooltip("Woche neu laden").Clicked() {
				a.reloadWeek(p)
			}
			closeButton(c, "wk-close", onClose)
		})

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			if ui.Button(c.Key("wk-prev"), "◀").Tooltip("Vorige Woche").Clicked() {
				a.turnWeek(p, -1)
			}
			if ui.Button(c.Key("wk-today"), "Heute").Tooltip("Zur aktuellen Woche").Disabled(offset == 0).Clicked() {
				a.turnWeek(p, 0)
			}
			if ui.Button(c.Key("wk-next"), "▶").Tooltip("Nächste Woche").Clicked() {
				a.turnWeek(p, 1)
			}
			ui.Text(c, weekTitle(start)).FontSize(13).Bold()
			if loading {
				ui.Text(c, "lädt …").FontSize(10.5).TextColor(t.TextMuted)
			}
			ui.Spacer(c)
			if lastSync != "" && current {
				ui.Text(c, "Stand "+lastSync).FontSize(10.5).TextColor(t.TextMuted)
			}
			if ui.Segmented(c.Key("wk-field"), &st.Field, weekFieldLabels[:]...).Changed() {
				p.moduleConfig().DateField = weekFields[st.Field]
				a.saveModuleConfig(p)
				a.reloadWeek(p)
			}
			if ui.Button(c.Key("wk-cfg"), "⚙️").Tooltip("Welche Tickets je Instanz (JQL)").Clicked() {
				st.ShowConfig = !st.ShowConfig
			}
		})

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			instancePill(c, "wk-inst-all", "Alle Instanzen", st.FilterInst == "", func() { st.FilterInst = "" })
			for _, inst := range instances {
				id := inst.ID
				lbl := fmt.Sprintf("%s (%d)", inst.Name, perInst[id])
				if errs[id] != "" {
					lbl = inst.Name + " (⚠)"
				}
				instancePill(c, "wk-inst-"+id, lbl, st.FilterInst == id, func() {
					if st.FilterInst == id {
						st.FilterInst = ""
					} else {
						st.FilterInst = id
					}
				})
			}
		})

		if st.ShowConfig {
			ui.Scroll(c).Grow(1).FillWidth().Children(func() { a.renderModuleConfig(c, p) })
			return
		}
		if len(instances) == 0 {
			ui.Text(c, "Keine Jira-Instanz für diese Woche – unter ⚙️ auswählen.").FontSize(12).TextColor(t.TextMuted)
			return
		}

		now := time.Now()
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().Gap(6).Children(func() {
			for col := 0; col < 8; col++ {
				if col == 0 && !overdueCol {
					continue
				}
				ci := col
				colCards := cols[ci]
				title, accent, border, grow := "Überfällig", t.Danger, t.Danger.Alpha(0.6), float32(1)
				if ci > 0 {
					day := start.AddDate(0, 0, ci-1)
					title = weekdayShort[ci-1] + " " + day.Format("02.01.")
					accent, border = t.TextMuted, t.Border
					if sameDay(day, now) {
						accent, border = t.Accent, t.Accent.Alpha(0.6)
					}
					if ci >= 6 && len(colCards) == 0 {
						grow = 0.6 // an empty weekend
					}
				}
				ui.Column(c.Key(fmt.Sprintf("wk-col-%d", ci))).
					Grow(grow).MinWidth(0).FillHeight().Padding(6).Radius(8).Border(1, border).Background(t.Surface).Gap(5).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
						ui.Text(c, title).FontSize(11.5).Bold().TextColor(accent).Grow(1).MinWidth(0).SingleLine()
						if len(colCards) > 0 {
							pillBadge(c, fmt.Sprint(len(colCards)), accent.Alpha(0.18), accent)
						}
					})
					ui.Divider(c)
					ui.Scroll(c).Grow(1).FillWidth().Children(func() {
						ui.Column(c).FillWidth().Gap(5).Children(func() {
							for _, cd := range colCards {
								a.renderWeekCard(c, cd, field)
							}
						})
					})
				})
			}
		})
	})
}

// renderWeekCard is one ticket: key in its status color, instance, summary, status.
func (a *App) renderWeekCard(c *ui.Context, cd weekCard, field int) {
	t := c.Theme()
	col := [3]ui.Color{t.Accent, t.Warning, t.Success}[cd.ColumnCategory()]
	iss := cd.JiraIssue
	ui.Column(c.Key("wk-card-"+cd.BaseURL+cd.Key)).
		FillWidth().Padding(6, 7).Radius(6).Border(1, col.Alpha(0.45)).Background(t.Background).Gap(3).
		Cursor(ui.CursorPointer).OnClick(func() { a.OpenEmbeddedTicketWindow(iss) }).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, cd.Key).FontSize(10.5).Bold().TextColor(col).Grow(1).MinWidth(0).SingleLine()
			ui.Link(c.Key("wk-ext-"+cd.BaseURL+cd.Key), "↗", iss.URL).FontSize(11).Tooltip("Im Browser öffnen")
		})
		ui.Text(c, cd.Summary).FontSize(11).MaxLines(3)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, truncateStr(cd.StatusName, 16)).FontSize(9.5).Bold().TextColor(col).Grow(1).MinWidth(0).SingleLine()
			when := cd.InstanceName
			if field != weekByDue {
				when += " · " + cd.Day.Format("15:04")
			}
			ui.Text(c, when).FontSize(9.5).TextColor(t.TextMuted).SingleLine()
		})
	})
}

// daysBetween counts calendar days from start to t (a DST change makes a day 23 or 25 hours).
func daysBetween(start, t time.Time) int {
	y, m, d := t.Date()
	return int(math.Round(time.Date(y, m, d, 0, 0, 0, 0, time.Local).Sub(start).Hours() / 24))
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
