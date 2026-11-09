package omamac

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshAtlassianStatusPanel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/minor/"):
			_, _ = w.Write([]byte(`{"status":{"indicator":"minor","description":"Degraded"},"incidents":[{"id":"1","name":"Slow search"}]}`))
		case strings.HasPrefix(r.URL.Path, "/down/"):
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"status":{"indicator":"none","description":"All Systems Operational"},"incidents":[]}`))
		}
	}))
	defer srv.Close()

	orig := DefaultAtlassianEndpoints
	defer func() { DefaultAtlassianEndpoints = orig }()
	DefaultAtlassianEndpoints = []AtlassianServiceEndpoint{
		{"Jira", srv.URL + "/ok"},
		{"Confluence", srv.URL + "/minor"},
		{"Bitbucket", srv.URL + "/down"},
	}

	p := &PanelSpec{ID: "x"}
	RefreshAtlassianStatusPanel(context.Background(), p)

	if p.Status != "warn" {
		t.Fatalf("status = %q, want warn", p.Status)
	}
	if p.BadgeText != "⚠ 1 Incident(s)" {
		t.Fatalf("badge = %q", p.BadgeText)
	}
	if len(p.Rows) != 3 {
		t.Fatalf("rows = %d", len(p.Rows))
	}
	if p.Rows[0].Badge != "OPERATIONAL" || p.Rows[1].Badge != "MINOR" || p.Rows[2].Badge != "UNBEKANNT" {
		t.Fatalf("badges = %q %q %q", p.Rows[0].Badge, p.Rows[1].Badge, p.Rows[2].Badge)
	}
	// unreachable services must not be counted as operational
	if p.Metrics[0].Value != "1 / 3" {
		t.Fatalf("operational metric = %q, want 1 / 3", p.Metrics[0].Value)
	}
}

func TestRefreshJiraPanelFetchesAndCounts(t *testing.T) {
	var gotAuth, gotJQL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotJQL, _ = body["jql"].(string)
		_, _ = w.Write([]byte(`{"issues":[
			{"key":"AB-1","fields":{"summary":"First","status":{"name":"To Do","statusCategory":{"key":"new"}}}},
			{"key":"AB-2","fields":{"summary":"Second","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}}}}
		]}`))
	}))
	defer srv.Close()

	j := NewJiraModuleState(t.TempDir())
	j.Instances = []JiraInstanceConfig{{ID: "i1", Name: "Test", BaseURL: srv.URL + "/", Email: "me@example.com", APIToken: "tok", JQLQuery: "project = AB", Enabled: true}}
	p := &PanelSpec{ID: "00-jira-quick-access"}
	j.RefreshJiraPanel(context.Background(), p)

	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotJQL != "project = AB" {
		t.Fatalf("jql = %q", gotJQL)
	}
	if j.InstCounts["i1"] != 2 || len(j.Issues) != 2 {
		t.Fatalf("counts = %v, issues = %d", j.InstCounts, len(j.Issues))
	}
	if j.Issues[0].URL != srv.URL+"/browse/AB-1" {
		t.Fatalf("url = %q", j.Issues[0].URL)
	}
	if p.BadgeText != "2 Tickets (1 Prog)" {
		t.Fatalf("badge = %q", p.BadgeText)
	}
}

func TestRefreshJiraPanelReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	j := NewJiraModuleState(t.TempDir())
	j.Instances = []JiraInstanceConfig{{ID: "i1", Name: "Bad", BaseURL: srv.URL, APIToken: "x", Enabled: true}}
	p := &PanelSpec{}
	j.RefreshJiraPanel(context.Background(), p)
	if j.InstErrors["i1"] != "HTTP 401" {
		t.Fatalf("errors = %v", j.InstErrors)
	}
	if p.Status != "warn" {
		t.Fatalf("status = %q", p.Status)
	}
}

func TestImportFromJiraQuickAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacyDir := filepath.Join(home, ".jira-quick-access")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"instances":[
		{"id":"inst-1","name":"acme","base_url":"https://acme.atlassian.net/","email":"a@b.c","api_token":"t1","jql_query":""},
		{"id":"","name":"empty","base_url":"  "}
	]}`
	if err := os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	j := NewJiraModuleState(dir) // auto-imports on first start
	if len(j.Instances) != 1 {
		t.Fatalf("instances = %d", len(j.Instances))
	}
	inst := j.Instances[0]
	if inst.BaseURL != "https://acme.atlassian.net" || inst.JQLQuery != defaultJiraJQL || !inst.Enabled {
		t.Fatalf("imported = %+v", inst)
	}
	st, err := os.Stat(filepath.Join(dir, "jira_instances.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600 (contains API tokens)", st.Mode().Perm())
	}
}

func TestJiraIssueHelpers(t *testing.T) {
	cases := map[string]int{"new": 0, "indeterminate": 1, "done": 2}
	for cat, want := range cases {
		if got := (JiraIssue{CategoryKey: cat}).ColumnCategory(); got != want {
			t.Errorf("ColumnCategory(%q) = %d, want %d", cat, got, want)
		}
	}
	br := JiraIssue{Key: "AB-1", Summary: "Fix the login bug"}.GitBranchName()
	if br != "feature/AB-1-fix-the-login-bug" {
		t.Fatalf("branch = %q", br)
	}
}

func TestLoadPanelsAddsBuiltinsToExistingInstall(t *testing.T) {
	dir := t.TempDir()
	for _, sp := range StarterPanels() {
		if err := SavePanel(dir, sp); err != nil {
			t.Fatal(err)
		}
	}
	panels, err := LoadPanels(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, p := range panels {
		if p.IsBuiltin {
			found[p.BuiltinKind] = true
		}
	}
	if !found[BuiltinJiraQuickAccess] || !found[BuiltinAtlassianStatus] {
		t.Fatalf("builtins missing after upgrade: %v", found)
	}
}

// Jira instances offer their credentials to panels; the AI only ever sees names.
func TestJiraCredentialsForPanels(t *testing.T) {
	j := NewJiraModuleState(t.TempDir())
	j.Instances = []JiraInstanceConfig{
		{Name: "Kunde A", BaseURL: "https://kunde-a.atlassian.net/", Email: "me@example.com", APIToken: "tok-a"},
		{Name: "kunde-a", BaseURL: "https://other.atlassian.net", Email: "me@example.com", APIToken: "tok-b"},
		{Name: "", BaseURL: "https://acme.atlassian.net"},
	}
	env := strings.Join(j.CredentialEnv(), "\n")
	for _, want := range []string{
		"JIRA_KUNDE_A_URL=https://kunde-a.atlassian.net",
		"JIRA_KUNDE_A_API_TOKEN=tok-a",
		"JIRA_KUNDE_A_2_API_TOKEN=tok-b",
		"JIRA_ACME_URL=https://acme.atlassian.net",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("env lacks %q:\n%s", want, env)
		}
	}

	vault, err := LoadVault(filepath.Join(t.TempDir(), "vault.json"))
	if err != nil {
		t.Fatal(err)
	}
	vault.values["JIRA_KUNDE_A_API_TOKEN"] = "from-vault"
	vault.Entries = append(vault.Entries, CredentialMeta{Name: "JIRA_KUNDE_A_API_TOKEN", Description: "own"})

	catalog := credentialCatalogText(vault, j)
	if strings.Contains(catalog, "tok-") || !strings.Contains(catalog, "$JIRA_KUNDE_A_2_API_TOKEN") || strings.Count(catalog, "$JIRA_KUNDE_A_API_TOKEN ") != 1 {
		t.Fatalf("catalog:\n%s", catalog)
	}

	p := &PanelSpec{ID: "t", Command: `echo "$JIRA_KUNDE_A_URL $JIRA_KUNDE_A_API_TOKEN"`}
	if err := p.RunDataRefresh(context.Background(), panelEnv(vault, j)...); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.LastCommandOut, "https://kunde-a.atlassian.net from-vault") {
		t.Fatalf("command saw %q; want the Jira URL and the vault's token", p.LastCommandOut)
	}
}
