package omamac

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// BuiltinJiraActivity is the activity stream of all Jira instances, like the
// Activity Stream gadget of Jira Data Center: who did what to which ticket.
const BuiltinJiraActivity = "jira-activity"

// JiraActivity is one entry of an instance's activity stream.
type JiraActivity struct {
	ID           string
	InstanceID   string
	InstanceName string
	BaseURL      string
	When         time.Time
	Author       string
	Action       string // what the author did, e.g. "changed the status to In Progress on"
	IssueKey     string
	IssueTitle   string // "KEY - summary" as the stream names the ticket
	URL          string
	Detail       string // comment text or the changed fields, as plain text
}

// Issue makes the entry openable in the ticket window.
func (e JiraActivity) Issue() JiraIssue {
	return JiraIssue{InstanceID: e.InstanceID, InstanceName: e.InstanceName, BaseURL: e.BaseURL,
		Key: e.IssueKey, Summary: e.IssueTitle, URL: e.URL}
}

type activityState struct {
	Entries    []JiraActivity
	Errors     map[string]string // instance ID → error
	LastSync   string
	ViewMode   int    // 0 = stream, 1 = filter settings
	Scope      int    // activityMine or activityAll
	FilterInst string // "" = all instances
	Search     string
}

const activityPerInstance = 40

// Activity scopes, in the order of the segmented control.
const (
	activityMine = iota // tickets I am assigned to, reported or watch – big sites drown everything else
	activityAll
)

// myActivityJQL picks "my tickets" for the stream: those the stream then reports on.
const myActivityJQL = "(assignee = currentUser() OR reporter = currentUser() OR watcher = currentUser()) AND updated >= -14d ORDER BY updated DESC"

// activityHTTP has more patience than the JQL client: big sites answer the stream slowly.
var activityHTTP = &http.Client{Timeout: 25 * time.Second}

// RefreshActivityPanel loads the activity stream of every enabled instance and fills the panel's summary.
func (j *JiraModuleState) RefreshActivityPanel(ctx context.Context, p *PanelSpec) {
	j.mu.RLock()
	scope := j.act.Scope
	j.mu.RUnlock()
	type res struct {
		inst    JiraInstanceConfig
		entries []JiraActivity
		err     error
	}
	// Instances on the same site with the same filter ask once (e.g. two JQL views of one site).
	var targets []JiraInstanceConfig
	seen := map[string]bool{}
	for _, inst := range j.activeInstances() {
		k := strings.TrimRight(inst.BaseURL, "/") + "|" + normalizeProjectKeys(inst.ActivityProjects)
		if !seen[k] {
			seen[k] = true
			targets = append(targets, inst)
		}
	}
	ch := make(chan res, len(targets))
	for _, inst := range targets {
		go func(inst JiraInstanceConfig) {
			var keys []string
			if scope == activityMine {
				found, err := j.searchJQL(ctx, inst, myActivityJQL, 60)
				if err != nil || len(found) == 0 {
					ch <- res{inst, nil, err}
					return
				}
				for _, f := range found {
					keys = append(keys, f.Key)
				}
			}
			e, err := fetchActivity(ctx, inst, activityPerInstance, keys)
			ch <- res{inst, e, err}
		}(inst)
	}
	var all []JiraActivity
	errs := map[string]string{}
	ids := map[string]bool{}
	for range targets {
		r := <-ch
		if r.err != nil {
			errs[r.inst.ID] = r.err.Error()
			continue
		}
		for _, e := range r.entries {
			if !ids[e.BaseURL+e.ID] {
				ids[e.BaseURL+e.ID] = true
				all = append(all, e)
			}
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].When.After(all[b].When) })

	j.mu.Lock()
	j.act.Entries = all
	j.act.Errors = errs
	j.act.LastSync = time.Now().Format("15:04:05")
	j.mu.Unlock()

	syncActivityPanel(p, all, len(targets), len(errs))
}

// SetActivityScope switches between my tickets and the whole site.
func (j *JiraModuleState) setActivityScope(scope int) {
	j.mu.Lock()
	j.act.Scope = scope
	j.act.Entries, j.act.LastSync = nil, ""
	j.mu.Unlock()
}

func syncActivityPanel(p *PanelSpec, all []JiraActivity, asked, failed int) {
	if p == nil {
		return
	}
	now := time.Now()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	today, lastHour := 0, 0
	for _, e := range all {
		if !e.When.Before(midnight) {
			today++
		}
		if now.Sub(e.When) <= time.Hour {
			lastHour++
		}
	}
	p.LastUpdated = now.Format("15:04:05")
	p.BadgeText = fmt.Sprintf("%d heute", today)
	p.Status = "ok"
	switch {
	case asked == 0:
		p.BadgeText, p.Status = "keine Instanz", "info"
	case failed == asked:
		p.BadgeText, p.Status = "Abruf ⚠", "crit"
	case failed > 0:
		p.Status = "warn"
	}
	p.Metrics = []PanelMetric{
		{Label: "Heute", Value: fmt.Sprint(today), Unit: "Einträge", Status: "info"},
		{Label: "Letzte Stunde", Value: fmt.Sprint(lastHour), Unit: fmt.Sprintf("%d/%d Instanzen", asked-failed, asked), Status: p.Status},
	}
	var rows []PanelRow
	for _, e := range all {
		if len(rows) == 5 {
			break
		}
		rows = append(rows, PanelRow{Left: e.Author + " " + e.Action, Right: e.IssueKey, Badge: e.When.Format("15:04"), Status: "info", URL: e.URL})
	}
	p.Rows = rows
}

// activeInstances are the enabled instances that have a URL and a token.
func (j *JiraModuleState) activeInstances() []JiraInstanceConfig {
	j.mu.RLock()
	defer j.mu.RUnlock()
	var out []JiraInstanceConfig
	for _, inst := range j.Instances {
		if inst.Enabled && strings.TrimSpace(inst.BaseURL) != "" && strings.TrimSpace(inst.APIToken) != "" {
			out = append(out, inst)
		}
	}
	return out
}

// normalizeProjectKeys turns "abc, XYZ;def" into "ABC XYZ DEF".
func normalizeProjectKeys(s string) string {
	f := strings.FieldsFunc(strings.ToUpper(s), func(r rune) bool { return r == ',' || r == ';' || r == ' ' })
	return strings.Join(f, " ")
}

// activityURL is the Atom feed of the Activity Streams plugin, which Jira Cloud still serves
// (/activity) and Data Center has always had; providers=issues leaves out Confluence.
// With keys, only those tickets' activity.
func activityURL(inst JiraInstanceConfig, max int, keys []string) string {
	q := url.Values{}
	q.Set("maxResults", fmt.Sprint(max))
	q.Set("providers", "issues")
	if inst.Email != "" {
		q.Set("os_authType", "basic")
	}
	if projects := normalizeProjectKeys(inst.ActivityProjects); projects != "" {
		q.Add("streams", "key IS "+projects)
	}
	if len(keys) > 0 {
		q.Add("streams", "issue-key IS "+strings.Join(keys, " "))
	}
	return strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/") + "/activity?" + q.Encode()
}

func fetchActivity(ctx context.Context, inst JiraInstanceConfig, max int, keys []string) ([]JiraActivity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, activityURL(inst, max, keys), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", jiraAuthHeader(inst))
	req.Header.Set("Accept", "application/atom+xml")
	resp, err := activityHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return parseActivityFeed(body, inst)
}

type atomFeed struct {
	Entries []struct {
		ID      string `xml:"id"`
		Title   string `xml:"title"`
		Content string `xml:"content"`
		Summary string `xml:"summary"`
		Updated string `xml:"updated"`
		Author  struct {
			Name string `xml:"name"`
		} `xml:"author"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

var (
	reTag       = regexp.MustCompile(`<[^>]*>`)
	reBlock     = regexp.MustCompile(`(?i)</?(li|p|div|br|tr)[^>]*>`)
	reSpace     = regexp.MustCompile(`\s+`)
	reBrowseKey = regexp.MustCompile(`/browse/([A-Z][A-Z0-9_]+-\d+)`)
	reIssueLink = regexp.MustCompile(`(?s)<a[^>]*href="[^"]*/browse/[A-Z][A-Z0-9_]+-\d+[^"]*"[^>]*>(.*?)</a>`)
)

// plainText strips the feed's HTML: tags out, entities decoded, whitespace collapsed.
func plainText(s string) string {
	s = reBlock.ReplaceAllString(s, " · ")
	s = html.UnescapeString(reTag.ReplaceAllString(s, ""))
	s = strings.Trim(reSpace.ReplaceAllString(s, " "), " ·")
	for strings.Contains(s, "·  ·") || strings.Contains(s, "· ·") {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "·  ·", "·"), "· ·", "·")
	}
	return s
}

func parseActivityFeed(body []byte, inst JiraInstanceConfig) ([]JiraActivity, error) {
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("Activity-Feed unlesbar: %w", err)
	}
	base := strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/")
	out := make([]JiraActivity, 0, len(feed.Entries))
	for _, fe := range feed.Entries {
		e := JiraActivity{ID: fe.ID, InstanceID: inst.ID, InstanceName: inst.Name, BaseURL: base, Author: strings.TrimSpace(fe.Author.Name)}
		if t, err := time.Parse(time.RFC3339Nano, fe.Updated); err == nil {
			e.When = t.Local()
		}
		// The title names the ticket in its last link to /browse/KEY.
		if m := reIssueLink.FindAllStringSubmatch(fe.Title, -1); len(m) > 0 {
			e.IssueTitle = plainText(m[len(m)-1][1])
		}
		if m := reBrowseKey.FindAllStringSubmatch(fe.Title, -1); len(m) > 0 {
			e.IssueKey = m[len(m)-1][1]
			e.URL = base + "/browse/" + e.IssueKey
		} else {
			for _, l := range fe.Links {
				if l.Rel == "alternate" {
					e.URL = strings.SplitN(l.Href, "?", 2)[0]
				}
			}
		}
		title := plainText(fe.Title)
		title = strings.TrimSpace(strings.TrimPrefix(title, e.Author))
		if e.IssueTitle != "" {
			title = strings.TrimSpace(reSpace.ReplaceAllString(strings.Replace(title, e.IssueTitle, "", 1), " "))
		}
		e.Action = title
		detail := fe.Content
		if strings.TrimSpace(plainText(detail)) == "" {
			detail = fe.Summary
		}
		e.Detail = truncateStr(plainText(detail), 220)
		out = append(out, e)
	}
	return out, nil
}

// filteredActivity applies the instance pill and the search field.
func (j *JiraModuleState) filteredActivity() []JiraActivity {
	j.mu.RLock()
	defer j.mu.RUnlock()
	q := strings.ToLower(strings.TrimSpace(j.act.Search))
	var out []JiraActivity
	for _, e := range j.act.Entries {
		if j.act.FilterInst != "" && e.InstanceID != j.act.FilterInst {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Author+" "+e.Action+" "+e.IssueTitle+" "+e.Detail+" "+e.InstanceName), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// dayLabel is "Heute", "Gestern" or "Mo, 03.11.".
func dayLabel(t time.Time) string {
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	ty, tm, td := t.Date()
	day := time.Date(ty, tm, td, 0, 0, 0, 0, time.Local)
	switch {
	case day.Equal(today):
		return "Heute"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Gestern"
	}
	return weekdayShort[(int(t.Weekday())+6)%7] + ", " + t.Format("02.01.")
}

var weekdayShort = [7]string{"Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"}

// renderJiraActivityModule shows the activity stream of all instances, newest first and grouped by day.
func (a *App) renderJiraActivityModule(c *ui.Context, p *PanelSpec, onClose func()) {
	t := c.Theme()
	j := a.Jira
	if j == nil {
		return
	}
	sBg, sFg := statusColors(t, p.Status)
	entries := j.filteredActivity()
	j.mu.RLock()
	total := len(j.act.Entries)
	perInst := map[string]int{}
	for _, e := range j.act.Entries {
		perInst[e.InstanceID]++
	}
	errs := map[string]string{}
	for k, v := range j.act.Errors {
		errs[k] = v
	}
	lastSync := j.act.LastSync
	j.mu.RUnlock()
	instances := j.activeInstances()

	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).DragWindow().Children(func() {
			ui.Text(c, p.Icon+" "+p.Title).FontSize(14).Bold().Grow(1).MinWidth(120).SingleLine()
			pillBadge(c, p.BadgeText, sBg, sFg)
			if ui.Button(c.Key("act-refresh"), "↻").Tooltip("Activity Streams aller Instanzen neu laden").Clicked() {
				a.refreshPanelAsync(p, "")
			}
			closeButton(c, "act-close", onClose)
		})

		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			instancePill(c, "act-inst-all", fmt.Sprintf("Alle Instanzen (%d)", total), j.act.FilterInst == "", func() { j.act.FilterInst = "" })
			for _, inst := range instances {
				id := inst.ID
				n, has := perInst[id]
				_, failed := errs[id]
				if !has && !failed {
					continue // same site as another instance: its entries count there
				}
				lbl := fmt.Sprintf("%s (%d)", inst.Name, n)
				if failed {
					lbl = inst.Name + " (⚠)"
				}
				instancePill(c, "act-inst-"+id, lbl, j.act.FilterInst == id, func() {
					if j.act.FilterInst == id {
						j.act.FilterInst = ""
					} else {
						j.act.FilterInst = id
					}
				})
			}
			ui.Spacer(c)
			if lastSync != "" {
				ui.Text(c, "Stand "+lastSync).FontSize(10.5).TextColor(t.TextMuted)
			}
			scope := j.act.Scope
			if ui.Segmented(c.Key("act-scope"), &scope, "Meine Tickets", "Alle").
				Tooltip("Meine Tickets: zugewiesen, gemeldet oder beobachtet und in den letzten 14 Tagen geändert").Changed() {
				j.setActivityScope(scope)
				a.refreshPanelAsync(p, "")
			}
			ui.Segmented(c.Key("act-mode"), &j.act.ViewMode, "📰", "⚙️")
		})

		if j.act.ViewMode == 1 {
			a.renderActivitySettings(c, p, errs)
			return
		}

		ui.TextInput(c.Key("act-search"), &j.act.Search).Placeholder("Filtern nach Person, Ticket, Text …").FillWidth()

		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				if len(instances) == 0 {
					ui.Text(c, "Noch keine Jira-Instanz eingerichtet – im Modul „Jira Quick-Access“ unter ⚙️ JQL anlegen.").FontSize(12).TextColor(t.TextMuted)
				} else if len(entries) == 0 {
					msg := "Keine Aktivität für diesen Filter."
					if lastSync == "" {
						msg = "Lade Activity Streams …"
					}
					ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted)
				}
				lastDay := ""
				for idx, e := range entries {
					ent := e
					if d := dayLabel(ent.When); d != lastDay {
						lastDay = d
						ui.Text(c, d).FontSize(11).Bold().TextColor(t.TextMuted).Padding(4, 2, 0, 2)
					}
					card := ui.Column(c.Key(fmt.Sprintf("act-e-%d", idx))).
						FillWidth().Padding(7, 10).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(3)
					if ent.IssueKey != "" {
						card.Cursor(ui.CursorPointer).OnClick(func() { a.OpenEmbeddedTicketWindow(ent.Issue()) })
					}
					card.Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
							ui.Text(c, ent.When.Format("15:04")).FontSize(10.5).TextColor(t.TextMuted)
							ui.Text(c, ent.Author).FontSize(11.5).Bold().SingleLine()
							ui.Text(c, ent.Action).FontSize(11.5).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
							pillBadge(c, ent.InstanceName, t.Border.Alpha(0.6), t.TextMuted)
							if ent.URL != "" {
								ui.Link(c.Key(fmt.Sprintf("act-ext-%d", idx)), "↗", ent.URL).FontSize(12).Tooltip("Im Browser öffnen")
							}
						})
						if ent.IssueTitle != "" {
							ui.Text(c, ent.IssueTitle).FontSize(12).Bold().TextColor(t.Accent).SingleLine()
						}
						if ent.Detail != "" {
							ui.Text(c, ent.Detail).FontSize(11).TextColor(t.TextMuted).MaxLines(2)
						}
					})
				}
			})
		})
	})
}

// renderActivitySettings edits, per instance, which projects the stream shows.
func (a *App) renderActivitySettings(c *ui.Context, p *PanelSpec, errs map[string]string) {
	t := c.Theme()
	j := a.Jira
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "Projekte je Instanz, durch Komma getrennt (z. B. „ABC, XYZ“). Leer = alle Projekte der Site. Instanzen ein- und ausschalten und Zugänge pflegen: Modul „Jira Quick-Access“ → ⚙️ JQL.").
				FontSize(11.5).TextColor(t.TextMuted)
			for idx := range j.Instances {
				i := idx
				inst := &j.Instances[i]
				if !inst.Enabled {
					continue
				}
				ui.Column(c.Key(fmt.Sprintf("act-cfg-%d", i))).FillWidth().Padding(8, 10).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(6).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, inst.Name).FontSize(12).Bold()
						ui.Text(c, inst.BaseURL).FontSize(11).TextColor(t.TextMuted).Grow(1).SingleLine()
						if e := errs[inst.ID]; e != "" {
							pillBadge(c, "FEHLER: "+truncateStr(e, 30), t.Danger.Alpha(0.2), t.Danger)
						}
					})
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
						ui.TextInput(c.Key(fmt.Sprintf("act-cfg-proj-%d", i)), &inst.ActivityProjects).Placeholder("alle Projekte").Grow(1)
						if ui.Button(c.Key(fmt.Sprintf("act-cfg-save-%d", i)), "Speichern").Clicked() {
							_ = j.Save()
							a.refreshPanelAsync(p, "Activity-Filter für „"+inst.Name+"“ gespeichert")
						}
					})
				})
			}
		})
	})
}

// instancePill is a filter button that is highlighted while active.
func instancePill(c *ui.Context, key, label string, active bool, onClick func()) {
	var clicked bool
	if active {
		clicked = ui.PrimaryButton(c.Key(key), label).Clicked()
	} else {
		clicked = ui.Button(c.Key(key), label).Clicked()
	}
	if clicked {
		onClick()
	}
}
