package omamac

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Sharing panels: a .omamac file (or the same JSON on the clipboard) carries
// panels without their live data and without any secret – only the names of
// the variables they need.

const (
	ShareFileExt    = "omamac"
	shareFormatName = "omamac-panels"
	shareFormatVer  = 1
)

// ShareBundle is the content of a .omamac file.
type ShareBundle struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	AppVersion string           `json:"app_version,omitempty"`
	ExportedAt string           `json:"exported_at,omitempty"`
	Panels     []*PanelSpec     `json:"panels"`
	Variables  []CredentialMeta `json:"variables,omitempty"` // names and descriptions, never values
}

// ShareCheck is what the export dialog tells about one panel before it leaves the Mac.
type ShareCheck struct {
	Panel   *PanelSpec
	Leaks   []string // variables whose value appears in the panel as plain text: export refused
	Tokens  []string // token-like strings that are not from the vault: export refused
	Hosts   []string // addresses the panel talks to – shown so the user can decide
	VarUsed []string // $VARIABLES the panel needs
}

// Blocked reports whether the panel must not be exported as it is.
func (c ShareCheck) Blocked() bool { return len(c.Leaks) > 0 || len(c.Tokens) > 0 }

var (
	// $NAME / ${NAME} in shell, os.environ.get("NAME") / os.environ["NAME"] / os.getenv("NAME") in Python
	varRefRes = []*regexp.Regexp{
		regexp.MustCompile(`\$\{?([A-Z][A-Z0-9_]{2,})\}?`),
		regexp.MustCompile(`environ(?:\.get\(|\[)\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
		regexp.MustCompile(`getenv\(\s*['"]([A-Z][A-Z0-9_]{2,})['"]`),
	}
	urlRe    = regexp.MustCompile(`https?://[^\s'"<>()\\]+`)
	tokenRes = []*regexp.Regexp{
		regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{16,}`),
		regexp.MustCompile(`ATATT[A-Za-z0-9_=-]{20,}`),
		regexp.MustCompile(`AIza[0-9A-Za-z_-]{30,}`),
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`),
		regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	}
	// shell variables every Mac has: not something the receiver must provide
	commonEnv = map[string]bool{"HOME": true, "PATH": true, "USER": true, "TMPDIR": true, "PWD": true, "SHELL": true, "LANG": true}
)

// shareText is everything of p that is exported and could carry a secret or an address.
func shareText(p *PanelSpec) string {
	var b strings.Builder
	for _, s := range []string{p.Title, p.Subtitle, p.Command, p.HTML, p.MenuBarText} {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	for _, a := range p.Actions {
		b.WriteString(a.Label + "\n" + a.Command + "\n" + a.URL + "\n")
	}
	if p.Command == "" { // static rows and metrics are exported too
		for _, r := range p.Rows {
			b.WriteString(r.Left + "\n" + r.Right + "\n" + r.URL + "\n")
		}
		for _, m := range p.Metrics {
			b.WriteString(m.Label + "\n" + m.Value + "\n")
		}
	}
	for _, s := range p.PromptHistory {
		b.WriteString(s.Prompt + "\n")
	}
	return b.String()
}

// CheckForSharing looks at p as it would be exported. env are the KEY=VALUE pairs
// panels get (vault and Jira): a value of them found in the panel is a leak.
func CheckForSharing(p *PanelSpec, env []string, withHistory bool) ShareCheck {
	exp := sanitizeForShare(p, withHistory)
	text := shareText(exp)
	c := ShareCheck{Panel: p}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || len(v) < 8 || strings.HasPrefix(v, "http") {
			continue // short values (prices, keywords) and URLs are no secrets
		}
		if strings.Contains(text, v) {
			c.Leaks = append(c.Leaks, k)
		}
	}
	for _, re := range tokenRes {
		for _, m := range re.FindAllString(text, -1) {
			c.Tokens = append(c.Tokens, m[:min(len(m), 12)]+"…")
		}
	}
	hosts := map[string]bool{}
	for _, raw := range urlRe.FindAllString(text, -1) {
		if u, err := url.Parse(raw); err == nil && u.Host != "" && !strings.Contains(u.Host, "$") {
			hosts[u.Host] = true
		}
	}
	for h := range hosts {
		c.Hosts = append(c.Hosts, h)
	}
	sort.Strings(c.Hosts)
	c.VarUsed = panelVariables(exp)
	return c
}

// panelVariables lists the $VARIABLES a panel's command, HTML and actions use.
func panelVariables(p *PanelSpec) []string {
	seen := map[string]bool{}
	var out []string
	texts := []string{p.Command, p.HTML}
	for _, a := range p.Actions {
		texts = append(texts, a.Command, a.URL)
	}
	for _, t := range texts {
		for _, re := range varRefRes {
			for _, m := range re.FindAllStringSubmatch(t, -1) {
				if name := m[1]; !seen[name] && !commonEnv[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// sanitizeForShare returns the part of p worth sharing: no live data of the
// last run, no local state, and the AI history only when asked for.
func sanitizeForShare(p *PanelSpec, withHistory bool) *PanelSpec {
	c := *p
	c.LastUpdated, c.LastCommandOut, c.LastCheckMsg = "", "", ""
	c.Enabled, c.PinnedFloating = false, false
	c.IsBuiltin, c.BuiltinKind = false, ""
	if strings.TrimSpace(c.Command) != "" { // the command produces these again
		c.Metrics, c.Rows, c.BadgeText, c.Status = nil, nil, "", ""
	}
	c.PromptHistory = nil
	if withHistory {
		for _, s := range p.PromptHistory {
			c.PromptHistory = append(c.PromptHistory, PromptStep{Step: s.Step, Prompt: s.Prompt, Provider: s.Provider})
		}
	}
	c.Actions = append([]PanelAction(nil), p.Actions...)
	return &c
}

// BuildShareBundle exports panels; catalog describes the variables they use.
// Builtin modules are skipped, a blocked panel is an error.
func BuildShareBundle(panels []*PanelSpec, env []string, catalog []CredentialMeta, withHistory bool, appVersion string) (*ShareBundle, error) {
	b := &ShareBundle{Format: shareFormatName, Version: shareFormatVer, AppVersion: appVersion, ExportedAt: time.Now().Format(time.RFC3339)}
	desc := map[string]string{}
	for _, m := range catalog {
		desc[m.Name] = m.Description
	}
	vars := map[string]bool{}
	for _, p := range panels {
		if p.IsBuiltin {
			continue
		}
		if chk := CheckForSharing(p, env, withHistory); chk.Blocked() {
			return nil, fmt.Errorf("„%s“ enthält einen Zugangswert im Klartext – ersetze ihn durch eine $VARIABLE (z. B. per „Mit KI anpassen“)", p.Title)
		}
		exp := sanitizeForShare(p, withHistory)
		b.Panels = append(b.Panels, exp)
		for _, v := range panelVariables(exp) {
			if !vars[v] {
				vars[v] = true
				b.Variables = append(b.Variables, CredentialMeta{Name: v, Description: desc[v]})
			}
		}
	}
	if len(b.Panels) == 0 {
		return nil, errors.New("keine eigenen Panels zum Exportieren (System-Module werden nicht exportiert)")
	}
	sort.Slice(b.Variables, func(i, j int) bool { return b.Variables[i].Name < b.Variables[j].Name })
	return b, nil
}

// Marshal writes the bundle as indented JSON, the same for a file and the clipboard.
func (b *ShareBundle) Marshal() []byte {
	data, _ := json.MarshalIndent(b, "", "  ")
	return append(data, '\n')
}

// ParseShareBundle reads a .omamac file or pasted text. It also takes a bare
// panel object or array – the JSON the AI writes – so that sharing that works too.
func ParseShareBundle(data []byte) (*ShareBundle, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, errors.New("leer – nichts zum Importieren")
	}
	var b ShareBundle
	if err := json.Unmarshal([]byte(text), &b); err == nil && b.Format == shareFormatName {
		if b.Version > shareFormatVer {
			return nil, fmt.Errorf("die Datei stammt von einer neueren OmaMac-Version (%s) – bitte OmaMac aktualisieren", b.AppVersion)
		}
	} else {
		panels, perr := extractPanelsFromText(text)
		if perr != nil || len(panels) == 0 {
			return nil, errors.New("kein OmaMac-Panel erkannt – erwartet wird eine .omamac-Datei oder Panel-JSON")
		}
		b = ShareBundle{Format: shareFormatName, Version: shareFormatVer, Panels: panels}
	}
	var ok []*PanelSpec
	for _, p := range b.Panels {
		if p == nil || strings.TrimSpace(p.Title) == "" {
			continue
		}
		p.IsBuiltin, p.BuiltinKind = false, ""
		p.Enabled, p.PinnedFloating = false, false
		p.Normalize()
		ok = append(ok, p)
	}
	if len(ok) == 0 {
		return nil, errors.New("die Datei enthält keine Panels")
	}
	b.Panels = ok
	if len(b.Variables) == 0 { // bare panel JSON: work them out
		seen := map[string]bool{}
		for _, p := range ok {
			for _, v := range panelVariables(p) {
				if !seen[v] {
					seen[v] = true
					b.Variables = append(b.Variables, CredentialMeta{Name: v})
				}
			}
		}
	}
	return &b, nil
}

// shareFileName suggests a file name for exporting panels.
func shareFileName(panels []*PanelSpec) string {
	if len(panels) == 1 {
		return Slugify(panels[0].Title) + "." + ShareFileExt
	}
	return fmt.Sprintf("omamac-panels-%s.%s", time.Now().Format("2006-01-02"), ShareFileExt)
}
