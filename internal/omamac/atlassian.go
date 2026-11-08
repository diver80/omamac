package omamac

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	BuiltinAtlassianStatus = "atlassian-status"
	BuiltinJiraQuickAccess = "jira-quick-access"
)

// AtlassianServiceEndpoint defines one of the 8 core Atlassian Cloud status pages.
type AtlassianServiceEndpoint struct {
	Name string
	URL  string
}

var DefaultAtlassianEndpoints = []AtlassianServiceEndpoint{
	{"Jira Software", "https://jira-software.status.atlassian.com"},
	{"Jira Service Management", "https://jira-service-management.status.atlassian.com"},
	{"Confluence", "https://confluence.status.atlassian.com"},
	{"Bitbucket", "https://status.bitbucket.org"},
	{"Atlassian Migrations", "https://migrations.status.atlassian.com"},
	{"Atlassian Analytics", "https://analytics.status.atlassian.com"},
	{"Rovo", "https://rovo.status.atlassian.com"},
	{"Rovo Dev", "https://rovodev.status.atlassian.com"},
}

type statuspageSummary struct {
	Page struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"page"`
	Status struct {
		Indicator   string `json:"indicator"`
		Description string `json:"description"`
	} `json:"status"`
	Incidents []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		Impact    string `json:"impact"`
		Shortlink string `json:"shortlink"`
		UpdatedAt string `json:"updated_at"`
	} `json:"incidents"`
}

// JiraInstanceConfig holds credentials and JQL for one Jira Cloud or Data Center site.
type JiraInstanceConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
	JQLQuery string `json:"jql_query"`
	Enabled  bool   `json:"enabled"`
}

// JiraIssue represents a single ticket fetched via JQL.
type JiraIssue struct {
	InstanceID   string    `json:"instance_id"`
	InstanceName string    `json:"instance_name"`
	BaseURL      string    `json:"base_url"`
	Key          string    `json:"key"`
	Summary      string    `json:"summary"`
	StatusName   string    `json:"status_name"`
	CategoryKey  string    `json:"category_key"` // "new" (To Do), "indeterminate" (In Progress), "done" (Done)
	Priority     string    `json:"priority"`
	IssueType    string    `json:"issue_type"`
	Updated      time.Time `json:"updated"`
	URL          string    `json:"url"`
}

// ColumnCategory returns 0 = To Do, 1 = In Progress, 2 = Done.
func (iss JiraIssue) ColumnCategory() int {
	cat := strings.ToLower(iss.CategoryKey)
	name := strings.ToLower(iss.StatusName)
	switch {
	case cat == "indeterminate" || strings.Contains(name, "progress") || strings.Contains(name, "arbeit") || strings.Contains(name, "review") || strings.Contains(name, "dev"):
		return 1
	case cat == "done" || strings.Contains(name, "done") || strings.Contains(name, "closed") || strings.Contains(name, "resolved") || strings.Contains(name, "erledigt"):
		return 2
	default:
		return 0
	}
}

// GitBranchName generates a clean branch name like feature/KEY-short-summary.
func (iss JiraIssue) GitBranchName() string {
	slug := Slugify(iss.Summary)
	if len(slug) > 38 {
		slug = strings.TrimRight(slug[:38], "-")
	}
	if slug == "" {
		return "feature/" + iss.Key
	}
	return fmt.Sprintf("feature/%s-%s", iss.Key, slug)
}

// JiraModuleState manages multi-instance JQL polling, board columns, and embedded ticket windows.
type JiraModuleState struct {
	ConfigPath string
	Instances  []JiraInstanceConfig
	Issues     []JiraIssue
	InstCounts map[string]int
	InstErrors map[string]string
	LastSync   string

	// Interactive UI state inside the module
	ViewMode         int    // 0 = 📋 Liste, 1 = 🗂️ Board (3 Spalten), 2 = ⚙️ Instanzen & JQL
	FilterInstanceID string // "" = Alle
	SearchQuery      string

	// New/Edit instance draft in tab 2
	NewInstName  string
	NewInstURL   string
	NewInstEmail string
	NewInstToken string
	NewInstJQL   string

	boardWin  *mygo.Window
	ticketWin *mygo.Window
	httpCli   *http.Client
	mu        sync.RWMutex
}

const defaultJiraJQL = "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"

// NewJiraModuleState initializes the Jira Quick-Access module and auto-imports ~/.jira-quick-access/config.json if available.
func NewJiraModuleState(omamacDir string) *JiraModuleState {
	cfgPath := filepath.Join(omamacDir, "jira_instances.json")
	st := &JiraModuleState{
		ConfigPath: cfgPath,
		InstCounts: make(map[string]int),
		InstErrors: make(map[string]string),
		httpCli: &http.Client{
			Timeout: 12 * time.Second,
		},
	}
	st.LoadOrImport()
	return st
}

// LoadOrImport loads ~/.config/omamac/jira_instances.json or imports ~/.jira-quick-access/config.json.
func (j *JiraModuleState) LoadOrImport() {
	if raw, err := os.ReadFile(j.ConfigPath); err == nil {
		var list []JiraInstanceConfig
		if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
			j.Instances = list
			return
		}
	}
	// Auto-import from ~/.jira-quick-access/config.json if present!
	if imported := j.ImportFromJiraQuickAccess(); imported > 0 {
		return
	}
}

// ImportFromJiraQuickAccess reads ~/.jira-quick-access/config.json and imports all configured Jira instances & JQL queries.
func (j *JiraModuleState) ImportFromJiraQuickAccess() int {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return 0
	}
	jqaPath := filepath.Join(home, ".jira-quick-access", "config.json")
	raw, err := os.ReadFile(jqaPath)
	if err != nil {
		return 0
	}
	var legacy struct {
		Instances []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			BaseURL  string `json:"base_url"`
			Email    string `json:"email"`
			APIToken string `json:"api_token"`
			JQLQuery string `json:"jql_query"`
		} `json:"instances"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil || len(legacy.Instances) == 0 {
		return 0
	}
	var out []JiraInstanceConfig
	for idx, inst := range legacy.Instances {
		if strings.TrimSpace(inst.BaseURL) == "" {
			continue
		}
		id := inst.ID
		if id == "" {
			id = fmt.Sprintf("inst-%d", idx+1)
		}
		jql := strings.TrimSpace(inst.JQLQuery)
		if jql == "" {
			jql = defaultJiraJQL
		}
		out = append(out, JiraInstanceConfig{
			ID:       id,
			Name:     inst.Name,
			BaseURL:  strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/"),
			Email:    strings.TrimSpace(inst.Email),
			APIToken: strings.TrimSpace(inst.APIToken),
			JQLQuery: jql,
			Enabled:  true,
		})
	}
	if len(out) > 0 {
		j.Instances = out
		_ = j.Save()
	}
	return len(out)
}

func (j *JiraModuleState) Save() error {
	if err := os.MkdirAll(filepath.Dir(j.ConfigPath), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(j.Instances, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(j.ConfigPath, data, 0o600)
}

// RefreshJiraPanel queries all enabled Jira instances concurrently and populates both j.Issues and the PanelSpec summary.
func (j *JiraModuleState) RefreshJiraPanel(ctx context.Context, p *PanelSpec) {
	j.mu.RLock()
	instances := make([]JiraInstanceConfig, len(j.Instances))
	copy(instances, j.Instances)
	j.mu.RUnlock()

	type instRes struct {
		id     string
		name   string
		issues []JiraIssue
		err    error
	}

	var activeInsts []JiraInstanceConfig
	for _, inst := range instances {
		if inst.Enabled && strings.TrimSpace(inst.BaseURL) != "" && strings.TrimSpace(inst.APIToken) != "" {
			activeInsts = append(activeInsts, inst)
		}
	}

	if len(activeInsts) == 0 {
		// Provide realistic demo tickets so the Board & List UI are immediately usable even before credentials are set
		demo := defaultDemoJiraIssues()
		j.mu.Lock()
		j.Issues = demo
		j.InstCounts = map[string]int{"demo": len(demo)}
		j.InstErrors = map[string]string{}
		j.LastSync = time.Now().Format("15:04:05")
		j.mu.Unlock()
		j.syncPanelSpecFromIssues(p, demo, 0)
		return
	}

	ch := make(chan instRes, len(activeInsts))
	for _, inst := range activeInsts {
		go func(target JiraInstanceConfig) {
			iss, err := j.fetchInstanceJQL(ctx, target)
			ch <- instRes{id: target.ID, name: target.Name, issues: iss, err: err}
		}(inst)
	}

	var allIssues []JiraIssue
	counts := make(map[string]int)
	errs := make(map[string]string)
	errCount := 0

	for range activeInsts {
		res := <-ch
		if res.err != nil {
			errs[res.id] = res.err.Error()
			errCount++
			continue
		}
		counts[res.id] = len(res.issues)
		allIssues = append(allIssues, res.issues...)
	}

	j.mu.Lock()
	j.Issues = allIssues
	j.InstCounts = counts
	j.InstErrors = errs
	j.LastSync = time.Now().Format("15:04:05")
	j.mu.Unlock()

	j.syncPanelSpecFromIssues(p, allIssues, errCount)
}

func (j *JiraModuleState) syncPanelSpecFromIssues(p *PanelSpec, issues []JiraIssue, errCount int) {
	if p == nil {
		return
	}
	todoCnt, progCnt, doneCnt := 0, 0, 0
	for _, iss := range issues {
		switch iss.ColumnCategory() {
		case 1:
			progCnt++
		case 2:
			doneCnt++
		default:
			todoCnt++
		}
	}
	total := len(issues)
	p.LastUpdated = time.Now().Format("15:04:05")
	p.BadgeText = fmt.Sprintf("%d Tickets (%d Prog)", total, progCnt)
	if errCount > 0 && total == 0 {
		p.Status = "warn"
	} else {
		p.Status = "ok"
	}
	p.Metrics = []PanelMetric{
		{Label: "Gesamt (JQL)", Value: fmt.Sprintf("%d", total), Unit: "Tickets", Status: "info", Progress: float64(minInt(total*5, 100))},
		{Label: "To Do / In Arbeit", Value: fmt.Sprintf("%d / %d", todoCnt, progCnt), Unit: fmt.Sprintf("%d Done", doneCnt), Status: "ok", Progress: float64(minInt(progCnt*20, 100))},
	}
	var rows []PanelRow
	for idx, iss := range issues {
		if idx >= 5 {
			break
		}
		st := "info"
		switch iss.ColumnCategory() {
		case 1:
			st = "warn"
		case 2:
			st = "ok"
		}
		rows = append(rows, PanelRow{
			Left:   fmt.Sprintf("[%s] %s", iss.Key, truncateStr(iss.Summary, 38)),
			Right:  iss.InstanceName + " ↗",
			Badge:  strings.ToUpper(truncateStr(iss.StatusName, 14)),
			Status: st,
			URL:    iss.URL,
		})
	}
	p.Rows = rows
}

func (j *JiraModuleState) fetchInstanceJQL(ctx context.Context, inst JiraInstanceConfig) ([]JiraIssue, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(inst.BaseURL), "/")
	jql := strings.TrimSpace(inst.JQLQuery)
	if jql == "" {
		jql = defaultJiraJQL
	}
	auth := ""
	if inst.Email != "" {
		creds := fmt.Sprintf("%s:%s", strings.TrimSpace(inst.Email), strings.TrimSpace(inst.APIToken))
		auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
	} else {
		auth = "Bearer " + strings.TrimSpace(inst.APIToken)
	}

	payload, _ := json.Marshal(map[string]any{
		"jql":        jql,
		"maxResults": 50,
		"fields":     []string{"summary", "status", "priority", "issuetype", "updated"},
	})

	endpoints := []string{
		baseURL + "/rest/api/3/search/jql",
		baseURL + "/rest/api/2/search",
	}

	var lastErr error
	for _, ep := range endpoints {
		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ep, bytes.NewReader(payload))
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := j.httpCli.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		cancel()

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusMethodNotAllowed {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var parsed struct {
			Issues []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
					Status  struct {
						Name           string `json:"name"`
						StatusCategory struct {
							Key string `json:"key"`
						} `json:"statusCategory"`
					} `json:"status"`
					Priority struct {
						Name string `json:"name"`
					} `json:"priority"`
					IssueType struct {
						Name string `json:"name"`
					} `json:"issuetype"`
					Updated string `json:"updated"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}

		var out []JiraIssue
		for _, raw := range parsed.Issues {
			out = append(out, JiraIssue{
				InstanceID:   inst.ID,
				InstanceName: inst.Name,
				BaseURL:      baseURL,
				Key:          raw.Key,
				Summary:      raw.Fields.Summary,
				StatusName:   raw.Fields.Status.Name,
				CategoryKey:  raw.Fields.Status.StatusCategory.Key,
				Priority:     raw.Fields.Priority.Name,
				IssueType:    raw.Fields.IssueType.Name,
				URL:          fmt.Sprintf("%s/browse/%s", baseURL, url.PathEscape(raw.Key)),
			})
		}
		return out, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no supported search endpoint on %s", baseURL)
}

func (j *JiraModuleState) FilteredIssues() []JiraIssue {
	j.mu.RLock()
	defer j.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(j.SearchQuery))
	var out []JiraIssue
	for _, iss := range j.Issues {
		if j.FilterInstanceID != "" && iss.InstanceID != j.FilterInstanceID {
			continue
		}
		if q != "" {
			if !strings.Contains(strings.ToLower(iss.Key), q) &&
				!strings.Contains(strings.ToLower(iss.Summary), q) &&
				!strings.Contains(strings.ToLower(iss.StatusName), q) &&
				!strings.Contains(strings.ToLower(iss.InstanceName), q) {
				continue
			}
		}
		out = append(out, iss)
	}
	return out
}

func defaultDemoJiraIssues() []JiraIssue {
	return []JiraIssue{
		{
			InstanceID:   "demo",
			InstanceName: "Privat",
			BaseURL:      "https://example.atlassian.net",
			Key:          "OMAMAC-101",
			Summary:      "Jira Quick-Access & Atlassian Status als native System-Module integrieren",
			StatusName:   "In Progress",
			CategoryKey:  "indeterminate",
			Priority:     "High",
			IssueType:    "Story",
			URL:          "https://example.atlassian.net/browse/OMAMAC-101",
		},
		{
			InstanceID:   "demo",
			InstanceName: "Kunde A",
			BaseURL:      "https://kunde-a.atlassian.net",
			Key:          "CLOUD-402",
			Summary:      "JQL Counter & 3-Spalten Kanban-Board direkt im Popover bereitstellen",
			StatusName:   "To Do",
			CategoryKey:  "new",
			Priority:     "Medium",
			IssueType:    "Task",
			URL:          "https://kunde-a.atlassian.net/browse/CLOUD-402",
		},
		{
			InstanceID:   "demo",
			InstanceName: "Kunde B",
			BaseURL:      "https://kunde-b.atlassian.net",
			Key:          "UAT-88",
			Summary:      "Automatischer Import aus ~/.jira-quick-access/config.json abgeschlossen",
			StatusName:   "Done",
			CategoryKey:  "done",
			Priority:     "High",
			IssueType:    "Task",
			URL:          "https://kunde-b.atlassian.net/browse/UAT-88",
		},
	}
}

// RefreshAtlassianStatusPanel fetches live status across https://status.atlassian.com and all 8 product statuspages.
func RefreshAtlassianStatusPanel(ctx context.Context, p *PanelSpec) {
	if p == nil {
		return
	}
	cli := &http.Client{Timeout: 8 * time.Second}

	type svcStat struct {
		idx       int
		name      string
		url       string
		indicator string
		desc      string
		incidents int
		ok        bool
	}

	ch := make(chan svcStat, len(DefaultAtlassianEndpoints))
	for i, ep := range DefaultAtlassianEndpoints {
		go func(idx int, endpoint AtlassianServiceEndpoint) {
			reqCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint.URL+"/api/v2/summary.json", nil)
			if err != nil {
				ch <- svcStat{idx: idx, name: endpoint.Name, url: endpoint.URL}
				return
			}
			req.Header.Set("User-Agent", "OmaMac-AtlassianStatusMonitor/1.2")
			resp, err := cli.Do(req)
			if err != nil {
				ch <- svcStat{idx: idx, name: endpoint.Name, url: endpoint.URL}
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				ch <- svcStat{idx: idx, name: endpoint.Name, url: endpoint.URL}
				return
			}
			var sum statuspageSummary
			if err := json.NewDecoder(resp.Body).Decode(&sum); err != nil {
				ch <- svcStat{idx: idx, name: endpoint.Name, url: endpoint.URL}
				return
			}
			ind := strings.ToLower(strings.TrimSpace(sum.Status.Indicator))
			if ind == "" {
				ind = "none"
			}
			ch <- svcStat{
				idx:       idx,
				name:      endpoint.Name,
				url:       endpoint.URL,
				indicator: ind,
				desc:      sum.Status.Description,
				incidents: len(sum.Incidents),
				ok:        true,
			}
		}(i, ep)
	}

	results := make([]svcStat, 0, len(DefaultAtlassianEndpoints))
	for range DefaultAtlassianEndpoints {
		results = append(results, <-ch)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].idx < results[j].idx })

	operationalCnt := 0
	unreachableCnt := 0
	totalIncidents := 0
	worstStatus := "ok"
	var rows []PanelRow

	for _, r := range results {
		if !r.ok {
			unreachableCnt++
			rows = append(rows, PanelRow{
				Left:   r.name,
				Right:  "nicht erreichbar ↗",
				Badge:  "UNBEKANNT",
				Status: "info",
				URL:    r.url,
			})
			continue
		}
		totalIncidents += r.incidents
		rowStatus := "ok"
		badge := "OPERATIONAL"
		switch r.indicator {
		case "none":
			operationalCnt++
		case "minor", "maintenance":
			rowStatus = "warn"
			badge = strings.ToUpper(r.indicator)
			if worstStatus == "ok" {
				worstStatus = "warn"
			}
		case "major", "critical":
			rowStatus = "crit"
			badge = strings.ToUpper(r.indicator)
			worstStatus = "crit"
		default:
			operationalCnt++
		}
		right := "Statuspage ↗"
		if r.incidents > 0 {
			right = fmt.Sprintf("%d Incident(s) ↗", r.incidents)
		} else if rowStatus != "ok" && r.desc != "" {
			right = truncateStr(r.desc, 28) + " ↗"
		}
		rows = append(rows, PanelRow{
			Left:   r.name,
			Right:  right,
			Badge:  badge,
			Status: rowStatus,
			URL:    r.url,
		})
	}

	p.LastUpdated = time.Now().Format("15:04:05")
	p.Status = worstStatus
	if totalIncidents > 0 {
		p.BadgeText = fmt.Sprintf("⚠ %d Incident(s)", totalIncidents)
	} else if unreachableCnt == len(DefaultAtlassianEndpoints) {
		p.BadgeText = "Offline?"
		p.Status = "info"
	} else {
		p.BadgeText = fmt.Sprintf("%d/%d Operational", operationalCnt, len(DefaultAtlassianEndpoints))
	}
	p.Metrics = []PanelMetric{
		{
			Label:    "Atlassian Cloud Dienste",
			Value:    fmt.Sprintf("%d / %d", operationalCnt, len(DefaultAtlassianEndpoints)),
			Unit:     "OK",
			Status:   worstStatus,
			Progress: float64(operationalCnt) / float64(len(DefaultAtlassianEndpoints)) * 100,
		},
		{
			Label:    "Aktive Störungen",
			Value:    fmt.Sprintf("%d", totalIncidents),
			Unit:     "Incidents",
			Status:   map[bool]string{true: "warn", false: "ok"}[totalIncidents > 0],
			Progress: map[bool]float64{true: 100, false: 5}[totalIncidents > 0],
		},
	}
	p.Rows = rows
}

func copyToMacClipboard(text string) {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}

// OpenEmbeddedTicketWindow opens a native WebKit window for the selected Jira issue,
// stripping bloated Atlassian navigation headers just like jira-quick-access.
func (a *App) OpenEmbeddedTicketWindow(iss JiraIssue) {
	if a.Jira == nil {
		return
	}
	if a.Jira.ticketWin != nil && !a.Jira.ticketWin.IsDestroyed() {
		a.Jira.ticketWin.Destroy()
		a.Jira.ticketWin = nil
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     fmt.Sprintf("%s — %s (%s)", iss.Key, iss.Summary, iss.InstanceName),
		Width:     860,
		Height:    640,
		MinWidth:  640,
		MinHeight: 460,
	})
	a.Jira.ticketWin = win
	win.Page().LoadURL(iss.URL)
	win.Page().OnDOMReady(func() {
		// Hide bulky top Jira navigation bars for clean focus on the ticket
		_, _ = win.Page().Eval(`
			(function(){
				const style = document.createElement('style');
				style.textContent = 'header[role="banner"], #ak-main-navigation { display: none !important; }';
				document.head.appendChild(style);
			})();
		`)
	})
}

// OpenFullJiraBoardWindow opens a dedicated wide 3-column Kanban Board window (To Do / In Progress / Done).
func (a *App) OpenFullJiraBoardWindow(p *PanelSpec) {
	if a.Jira == nil {
		return
	}
	if a.Jira.boardWin != nil && !a.Jira.boardWin.IsDestroyed() {
		a.Jira.boardWin.Show()
		a.Jira.boardWin.Focus()
		a.Jira.boardWin.Invalidate()
		return
	}
	a.Jira.ViewMode = 1 // Board mode
	a.Jira.boardWin = mygo.NewWindow(mygo.WindowOptions{
		Title:     "OmaMac — Jira Quick-Access Board (To Do • In Progress • Done)",
		Width:     1080,
		Height:    680,
		MinWidth:  820,
		MinHeight: 500,
		Content: ui.View(func(c *ui.Context) {
			a.renderJiraQuickAccessModule(c, p, nil)
		}),
	})
}

// renderJiraQuickAccessModule renders the built-in Jira Quick-Access module (Multi-Instance JQL Counter, List, 3-Column Board & Settings).
func (a *App) renderJiraQuickAccessModule(c *ui.Context, p *PanelSpec, onClose func()) {
	t := c.Theme()
	j := a.Jira
	if j == nil {
		return
	}
	sBg, sFg := statusColors(t, p.Status)
	issues := j.FilteredIssues()
	j.mu.RLock()
	totalIssues := len(j.Issues)
	instCounts := make(map[string]int, len(j.InstCounts))
	for k, v := range j.InstCounts {
		instCounts[k] = v
	}
	instErrors := make(map[string]string, len(j.InstErrors))
	for k, v := range j.InstErrors {
		instErrors[k] = v
	}
	lastSync := j.LastSync
	j.mu.RUnlock()

	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		// Header Row
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).DragWindow().Children(func() {
			ui.Text(c, p.Icon+" "+p.Title).FontSize(14).Bold().Grow(1).MinWidth(120).SingleLine()
			pillBadge(c, p.BadgeText, sBg, sFg)
			if ui.Button(c.Key("jira-open-board-win"), "⤢").Tooltip("Board in großem Fenster öffnen").Clicked() {
				a.OpenFullJiraBoardWindow(p)
			}
			if ui.Button(c.Key("jira-refresh-btn"), "↻").Tooltip("Alle Jira-Instanzen (JQL) jetzt abfragen").Clicked() {
				go func() {
					j.RefreshJiraPanel(context.Background(), p)
					a.updateUI(func() {
						a.syncAllPanelRuntimes()
						a.toast("Jira JQL für alle Instanzen aktualisiert")
					})
				}()
			}
			closeButton(c, "jira-close-btn", onClose)
		})

		// Instance Beacons / Filter Pills + View Mode Segmented Control
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			allActive := j.FilterInstanceID == ""
			allLbl := fmt.Sprintf("Alle (%d)", totalIssues)
			if allActive {
				if ui.PrimaryButton(c.Key("jira-inst-all"), allLbl).Clicked() {
					j.FilterInstanceID = ""
				}
			} else {
				if ui.Button(c.Key("jira-inst-all"), allLbl).Clicked() {
					j.FilterInstanceID = ""
				}
			}

			for _, inst := range j.Instances {
				if !inst.Enabled {
					continue
				}
				instCopy := inst
				lbl := fmt.Sprintf("%s (%d)", instCopy.Name, instCounts[instCopy.ID])
				if instErrors[instCopy.ID] != "" {
					lbl = instCopy.Name + " (⚠)"
				}
				if j.FilterInstanceID == instCopy.ID {
					if ui.PrimaryButton(c.Key("jira-inst-"+instCopy.ID), lbl).Clicked() {
						j.FilterInstanceID = ""
					}
				} else {
					if ui.Button(c.Key("jira-inst-"+instCopy.ID), lbl).Clicked() {
						j.FilterInstanceID = instCopy.ID
					}
				}
			}

			ui.Spacer(c)
			if lastSync != "" {
				ui.Text(c, "Stand "+lastSync).FontSize(10.5).TextColor(t.TextMuted)
			}
			ui.Segmented(c.Key("jira-view-mode"), &j.ViewMode, "📋 Liste", "🗂️ Board", "⚙️ JQL")
		})

		switch j.ViewMode {
		case 0:
			// Mode 0: Ticket List + Search + Direct Ticket/Branch/Browser Actions
			ui.TextInput(c.Key("jira-search-in"), &j.SearchQuery).
				Placeholder("Tickets filtern nach Key, Titel, Status oder Instanz...").
				FillWidth()

			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					if len(issues) == 0 {
						ui.Text(c, "Keine Tickets für diesen Filter gefunden.").FontSize(12).TextColor(t.TextMuted)
					}
					for _, iss := range issues {
						issCopy := iss
						colCat := issCopy.ColumnCategory()
						catBadgeCol := t.Accent
						if colCat == 1 {
							catBadgeCol = t.Warning
						} else if colCat == 2 {
							catBadgeCol = t.Success
						}

						ui.Column(c.Key("jira-row-"+issCopy.InstanceID+"-"+issCopy.Key)).
							FillWidth().Padding(8, 10).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(4).
							Cursor(ui.CursorPointer).
							OnClick(func() {
								a.OpenEmbeddedTicketWindow(issCopy)
							}).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
								pillBadge(c, issCopy.Key, t.Accent.Alpha(0.20), t.Accent)
								pillBadge(c, issCopy.InstanceName, t.Border.Alpha(0.6), t.TextMuted)
								pillBadge(c, strings.ToUpper(issCopy.StatusName), catBadgeCol.Alpha(0.20), catBadgeCol)
								ui.Spacer(c)
								if ui.Button(c.Key("jira-key-"+issCopy.Key), "📋 Key").Tooltip("Ticket-Key kopieren").Clicked() {
									copyToMacClipboard(issCopy.Key)
									a.toast("Kopiert: " + issCopy.Key)
								}
								if ui.Button(c.Key("jira-br-"+issCopy.Key), "🌿 Branch").Tooltip("Git-Branch-Namen kopieren: " + issCopy.GitBranchName()).Clicked() {
									br := issCopy.GitBranchName()
									copyToMacClipboard(br)
									a.toast("Git-Branch kopiert: " + br)
								}
								if ui.Button(c.Key("jira-ext-"+issCopy.Key), "↗").Tooltip("Im Standard-Browser öffnen").Clicked() {
									_ = mygo.Shell.OpenExternal(issCopy.URL)
								}
							})
							ui.Text(c, issCopy.Summary).FontSize(12).Bold().SingleLine()
						})
					}
				})
			})

		case 1:
			// Mode 1: 3-Column Kanban Board (To Do / In Progress / Done)
			var cols [3][]JiraIssue
			for _, iss := range issues {
				cIdx := iss.ColumnCategory()
				cols[cIdx] = append(cols[cIdx], iss)
			}
			colTitles := [3]string{"To Do", "In Progress", "Done"}
			colColors := [3]ui.Color{t.Accent, t.Warning, t.Success}

			ui.Row(c).Grow(1).MinHeight(0).FillWidth().Gap(8).Children(func() {
				for colIdx := 0; colIdx < 3; colIdx++ {
					cIdx := colIdx
					colIssues := cols[cIdx]
					colCol := colColors[cIdx]

					ui.Column(c.Key(fmt.Sprintf("jira-board-col-%d", cIdx))).
						Grow(1).FillHeight().Padding(8).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(6).Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
							ui.Text(c, colTitles[cIdx]).FontSize(12).Bold().TextColor(colCol).Grow(1)
							pillBadge(c, fmt.Sprintf("%d", len(colIssues)), colCol.Alpha(0.20), colCol)
						})
						ui.Divider(c)
						ui.Scroll(c).Grow(1).FillWidth().Children(func() {
							ui.Column(c).FillWidth().Gap(6).Children(func() {
								for _, iss := range colIssues {
									issCopy := iss
									ui.Column(c.Key(fmt.Sprintf("jira-bd-card-%s-%s", issCopy.InstanceID, issCopy.Key))).
										FillWidth().Padding(7, 8).Radius(6).Border(1, colCol.Alpha(0.45)).Background(t.Background).Gap(3).
										Cursor(ui.CursorPointer).
										OnClick(func() {
											a.OpenEmbeddedTicketWindow(issCopy)
										}).Children(func() {
										ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
											ui.Text(c, issCopy.Key).FontSize(11).Bold().TextColor(colCol).Grow(1).SingleLine()
											ui.Text(c, issCopy.InstanceName).FontSize(10).TextColor(t.TextMuted).SingleLine()
											if ui.Button(c.Key("bd-open-"+issCopy.Key), "↗").Clicked() {
												_ = mygo.Shell.OpenExternal(issCopy.URL)
											}
										})
										ui.Text(c, issCopy.Summary).FontSize(11).MaxLines(2)
									})
								}
							})
						})
					})
				}
			})

		case 2:
			// Mode 2: Multi-Instance & JQL Configuration + 1-Click ~/.jira-quick-access Import
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, "Konfigurierte Jira-Instanzen & JQL").FontSize(12.5).Bold().Grow(1)
						if ui.Button(c.Key("jira-reimport-jqa"), "🔄 Aus ~/.jira-quick-access importieren").Clicked() {
							cnt := j.ImportFromJiraQuickAccess()
							go func() {
								j.RefreshJiraPanel(context.Background(), p)
								a.updateUI(func() {
									a.syncAllPanelRuntimes()
									a.toast(fmt.Sprintf("%d Jira-Instanzen aus ~/.jira-quick-access importiert!", cnt))
								})
							}()
						}
					})

					delIdx := -1
					for idx := range j.Instances {
						iCopy := idx
						inst := &j.Instances[iCopy]
						ui.Column(c.Key(fmt.Sprintf("jira-cfg-inst-%d", iCopy))).
							FillWidth().Padding(8, 10).Radius(8).Border(1, t.Border).Background(t.Surface).Gap(6).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
								if ui.Switch(c.Key(fmt.Sprintf("jira-inst-sw-%d", iCopy)), &inst.Enabled).Changed() {
									_ = j.Save()
									go func() {
										j.RefreshJiraPanel(context.Background(), p)
										a.updateUI(func() { a.syncAllPanelRuntimes() })
									}()
								}
								ui.Text(c, inst.Name+" ("+inst.BaseURL+")").FontSize(12).Bold().Grow(1).SingleLine()
								if errTxt := instErrors[inst.ID]; errTxt != "" {
									pillBadge(c, "FEHLER: "+truncateStr(errTxt, 24), t.Danger.Alpha(0.2), t.Danger)
								} else {
									pillBadge(c, fmt.Sprintf("%d Tickets", instCounts[inst.ID]), t.Success.Alpha(0.2), t.Success)
								}
							})
							ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
								ui.Text(c, "JQL:").FontSize(11).TextColor(t.TextMuted)
								ui.TextInput(c.Key(fmt.Sprintf("jira-inst-jql-%d", iCopy)), &inst.JQLQuery).Grow(1)
								if ui.Button(c.Key(fmt.Sprintf("jira-inst-del-%d", iCopy)), "🗑").Tooltip("Instanz entfernen").Clicked() {
									delIdx = iCopy
								}
								if ui.Button(c.Key(fmt.Sprintf("jira-inst-save-%d", iCopy)), "Speichern").Clicked() {
									_ = j.Save()
									go func() {
										j.RefreshJiraPanel(context.Background(), p)
										a.updateUI(func() {
											a.syncAllPanelRuntimes()
											a.toast("JQL für '" + inst.Name + "' gespeichert & aktualisiert")
										})
									}()
								}
							})
						})
					}

					if delIdx >= 0 && delIdx < len(j.Instances) {
						removedName := j.Instances[delIdx].Name
						j.mu.Lock()
						j.Instances = append(j.Instances[:delIdx:delIdx], j.Instances[delIdx+1:]...)
						j.mu.Unlock()
						_ = j.Save()
						a.toast("Jira-Instanz '" + removedName + "' entfernt")
						go func() {
							j.RefreshJiraPanel(context.Background(), p)
							a.updateUI(func() { a.syncAllPanelRuntimes() })
						}()
					}

					// Add a new Jira instance (Cloud: E-Mail + API-Token, Data Center: only PAT)
					ui.Column(c.Key("jira-new-inst")).
						FillWidth().Padding(8, 10).Radius(8).Border(1, t.Accent.Alpha(0.45)).Background(t.Surface).Gap(6).Children(func() {
						ui.Text(c, "➕ Neue Jira-Instanz").FontSize(12).Bold()
						ui.Row(c).FillWidth().Gap(6).Children(func() {
							ui.TextInput(c.Key("jira-new-name"), &j.NewInstName).Placeholder("Name (z. B. Firma)").Grow(1)
							ui.TextInput(c.Key("jira-new-url"), &j.NewInstURL).Placeholder("https://firma.atlassian.net").Grow(2)
						})
						ui.Row(c).FillWidth().Gap(6).Children(func() {
							ui.TextInput(c.Key("jira-new-email"), &j.NewInstEmail).Placeholder("E-Mail (leer = Data Center PAT)").Grow(1)
							ui.TextInput(c.Key("jira-new-token"), &j.NewInstToken).Placeholder("API-Token / PAT").Password().Grow(1)
						})
						ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
							ui.TextInput(c.Key("jira-new-jql"), &j.NewInstJQL).Placeholder(defaultJiraJQL).Grow(1)
							if ui.PrimaryButton(c.Key("jira-new-add"), "Hinzufügen").
								Disabled(strings.TrimSpace(j.NewInstURL) == "" || strings.TrimSpace(j.NewInstToken) == "").Clicked() {
								name := strings.TrimSpace(j.NewInstName)
								baseURL := strings.TrimRight(strings.TrimSpace(j.NewInstURL), "/")
								if name == "" {
									if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
										name = strings.Split(u.Host, ".")[0]
									} else {
										name = baseURL
									}
								}
								jql := strings.TrimSpace(j.NewInstJQL)
								if jql == "" {
									jql = defaultJiraJQL
								}
								j.mu.Lock()
								j.Instances = append(j.Instances, JiraInstanceConfig{
									ID:       fmt.Sprintf("inst-%d", time.Now().UnixNano()),
									Name:     name,
									BaseURL:  baseURL,
									Email:    strings.TrimSpace(j.NewInstEmail),
									APIToken: strings.TrimSpace(j.NewInstToken),
									JQLQuery: jql,
									Enabled:  true,
								})
								j.mu.Unlock()
								_ = j.Save()
								j.NewInstName, j.NewInstURL, j.NewInstEmail, j.NewInstToken, j.NewInstJQL = "", "", "", "", ""
								go func() {
									j.RefreshJiraPanel(context.Background(), p)
									a.updateUI(func() {
										a.syncAllPanelRuntimes()
										a.toast("Jira-Instanz '" + name + "' hinzugefügt")
									})
								}()
							}
						})
					})
				})
			})
		}
	})
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
