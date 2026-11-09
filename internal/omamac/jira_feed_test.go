package omamac

import (
	"strings"
	"testing"
	"time"
)

const sampleActivityFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:activity="http://activitystrea.ms/spec/1.0/">
<entry>
<id>urn:uuid:1</id>
<title type="html">&lt;a href="https://acme.atlassian.net/secure/ViewProfile.jspa?accountId=1" class="activity-item-user activity-item-author"&gt;Jane Doe&lt;/a&gt; changed the status to In Progress on &lt;a aria-label="ABC-12 is not done" href="https://acme.atlassian.net/browse/ABC-12?streamsSourceProduct=jira"&gt;ABC-12 - Fix the login &amp;amp; logout&lt;/a&gt;</title>
<content type="html">&lt;ul class="updates"&gt;&lt;li&gt;Changed the Remaining Estimate to '0 minutes'&lt;/li&gt;&lt;li&gt;Logged '1h'&lt;/li&gt;&lt;/ul&gt;</content>
<author><name>Jane Doe</name></author>
<updated>2026-11-09T15:36:20.527Z</updated>
<link href="https://acme.atlassian.net/browse/ABC-12?streamsSourceProduct=jira" rel="alternate"/>
</entry>
<entry>
<id>urn:uuid:2</id>
<title type="html">&lt;a href="x"&gt;John Roe&lt;/a&gt; commented on &lt;a href="https://acme.atlassian.net/browse/XY_Z-7?streamsSourceProduct=jira"&gt;XY_Z-7 - Report&lt;/a&gt;</title>
<summary type="html">&lt;p&gt;Looks good&lt;/p&gt;</summary>
<author><name>John Roe</name></author>
<updated>2026-11-08T08:00:00Z</updated>
</entry>
</feed>`

func TestParseActivityFeed(t *testing.T) {
	inst := JiraInstanceConfig{ID: "i1", Name: "Acme", BaseURL: "https://acme.atlassian.net/"}
	got, err := parseActivityFeed([]byte(sampleActivityFeed), inst)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	e := got[0]
	if e.Author != "Jane Doe" || e.IssueKey != "ABC-12" || e.IssueTitle != "ABC-12 - Fix the login & logout" ||
		e.Action != "changed the status to In Progress on" || e.URL != "https://acme.atlassian.net/browse/ABC-12" {
		t.Fatalf("entry 0 = %+v", e)
	}
	if !strings.Contains(e.Detail, "Remaining Estimate") || !strings.Contains(e.Detail, "· Logged '1h'") {
		t.Fatalf("detail = %q", e.Detail)
	}
	if !e.When.Equal(time.Date(2026, 11, 9, 15, 36, 20, 527e6, time.UTC)) {
		t.Fatalf("when = %v", e.When)
	}
	if got[1].IssueKey != "XY_Z-7" || got[1].Action != "commented on" || got[1].Detail != "Looks good" {
		t.Fatalf("entry 1 = %+v", got[1])
	}
}

func TestActivityURL(t *testing.T) {
	u := activityURL(JiraInstanceConfig{BaseURL: "https://acme.atlassian.net/", Email: "me@example.com", ActivityProjects: "abc, xyz"}, 40, []string{"ABC-1", "XYZ-2"})
	for _, want := range []string{"https://acme.atlassian.net/activity?", "maxResults=40", "providers=issues", "os_authType=basic", "streams=key+IS+ABC+XYZ", "streams=issue-key+IS+ABC-1+XYZ-2"} {
		if !strings.Contains(u, want) {
			t.Fatalf("%s lacks %s", u, want)
		}
	}
	// Data Center with a PAT: no forced Basic auth, no filter without projects.
	if u := activityURL(JiraInstanceConfig{BaseURL: "https://jira.example.com"}, 10, nil); strings.Contains(u, "os_authType") || strings.Contains(u, "streams") {
		t.Fatal(u)
	}
}

func TestWeekStartAndTitle(t *testing.T) {
	// Sunday 15.11.2026 belongs to the week of Monday 09.11.
	sun := time.Date(2026, 11, 15, 22, 0, 0, 0, time.Local)
	if got := weekStart(sun, 0); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("weekStart = %v", got)
	}
	if got := weekStart(sun, -1); !got.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("last week = %v", got)
	}
	if got := weekTitle(time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)); got != "KW 46 · 09.–15.11.2026" {
		t.Fatalf("title = %q", got)
	}
	if got := weekTitle(time.Date(2026, 12, 28, 0, 0, 0, 0, time.Local)); got != "KW 53 · 28.12.–03.01.2027" {
		t.Fatalf("title = %q", got)
	}
	// Across the switch to summer time the week still has 7 days.
	mon := time.Date(2026, 3, 23, 0, 0, 0, 0, time.Local)
	if d := daysBetween(mon, time.Date(2026, 3, 29, 23, 0, 0, 0, time.Local)); d != 6 {
		t.Fatalf("daysBetween = %d", d)
	}
}

func TestWeekJQL(t *testing.T) {
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)
	got := weekJQL("project = ABC ORDER BY rank", weekByUpdated, start, false)
	if got != `(project = ABC) AND updated >= "2026-11-09" AND updated < "2026-11-16" ORDER BY updated ASC` {
		t.Fatal(got)
	}
	got = weekJQL("", weekByDue, start, true)
	if got != `(assignee = currentUser()) AND (duedate >= "2026-11-09" AND duedate < "2026-11-16" OR (duedate < "2026-11-09" AND resolution is EMPTY)) ORDER BY duedate ASC` {
		t.Fatal(got)
	}
}

func TestBuiltinJiraBoardsStartOff(t *testing.T) {
	for _, p := range BuiltinSystemPanels() {
		if (p.BuiltinKind == BuiltinJiraActivity || p.BuiltinKind == BuiltinJiraWeek) && (p.Enabled || p.PinnedFloating) {
			t.Fatalf("%s should start switched off", p.ID)
		}
	}
}
