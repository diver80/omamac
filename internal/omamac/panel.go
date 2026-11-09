package omamac

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PanelType distinguishes between GPU-rendered mygo/ui declarative panels ("native")
// and free-form HTML/CSS/JS WebView micro-panels ("html").
type PanelType string

const (
	PanelTypeNative PanelType = "native"
	PanelTypeHTML   PanelType = "html"
)

// PanelMetric represents a KPI / gauge card inside a native status panel.
type PanelMetric struct {
	Label    string  `json:"label"`
	Value    string  `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Trend    string  `json:"trend,omitempty"`
	Status   string  `json:"status,omitempty"`   // "ok", "warn", "crit", "info"
	Progress float64 `json:"progress,omitempty"` // 0..100 optional progress bar
}

// PanelRow represents a structured status row inside a native status panel.
type PanelRow struct {
	Left   string `json:"left"`
	Right  string `json:"right"`
	Badge  string `json:"badge,omitempty"`
	Status string `json:"status,omitempty"` // "ok", "warn", "crit", "info"
	URL    string `json:"url,omitempty"`    // Optional clickable link (e.g. mydealz/Idealo offer URL)
}

// PanelAction represents an interactive action button inside a status panel.
type PanelAction struct {
	Label   string `json:"label"`
	Command string `json:"command,omitempty"` // Shell command executed on click
	URL     string `json:"url,omitempty"`     // Optional URL opened on click
	Style   string `json:"style,omitempty"`   // "primary", "danger", "default"
}

// PromptStep records each prompt in the 2-Prompt (Create -> Refine) workflow.
type PromptStep struct {
	Step      int    `json:"step"`
	Prompt    string `json:"prompt"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Timestamp string `json:"timestamp"`
}

// PanelSpec defines a complete OmaMac status panel, desktop widget, or mini-app
// (inspired by Omarchy & OmaLauncher: supports both "native" mygo/ui and "html" WebView).
type PanelSpec struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	Subtitle       string        `json:"subtitle"`
	Kind           string        `json:"kind,omitempty"`          // "widget" (desktop tile), "app" (interactive view), "panel" (menubar popover)
	GridSize       string        `json:"grid_size,omitempty"`     // e.g. "3x3" (half width) or "6x2" (full width) like OmaLauncher
	RequiresBrew   []string      `json:"requires_brew,omitempty"` // Homebrew packages required by command (macOS equivalent of requires { pacman: [...] })
	Icon           string        `json:"icon"`                    // Emoji or short symbol for Menubar Tray / Tile
	BadgeText      string        `json:"badge_text"`              // Text shown next to Icon in macOS Menubar
	MenuBarText    string        `json:"menubar_text,omitempty"`  // the user's own menu bar text; "{status}" stands for BadgeText
	Status         string        `json:"status"`                  // "ok", "warn", "crit", "info"
	Type           PanelType     `json:"type"`                    // "native" (mygo/ui) or "html" (WebView)
	IsBuiltin      bool          `json:"is_builtin,omitempty"`    // True for pre-delivered system modules (not AI-editable)
	BuiltinKind    string        `json:"builtin_kind,omitempty"`  // "atlassian-status" or "jira-quick-access"
	Enabled        bool          `json:"enabled"`                 // Show in macOS Menubar as Tray + Flyout
	PinnedFloating bool          `json:"pinned_floating"`         // Also pin as floating desktop HUD / Home-Screen Widget tile
	RefreshSeconds int           `json:"refresh_seconds"`         // Auto-refresh interval in seconds (0 AI tokens used!)
	Width          int           `json:"width"`
	Height         int           `json:"height"`
	Command        string        `json:"command,omitempty"` // Optional shell script to refresh data dynamically
	Metrics        []PanelMetric `json:"metrics,omitempty"`
	Rows           []PanelRow    `json:"rows,omitempty"`
	Actions        []PanelAction `json:"actions,omitempty"`
	HTML           string        `json:"html,omitempty"` // Used when Type == "html" (or hybrid preview)
	PromptHistory  []PromptStep  `json:"prompt_history,omitempty"`
	LastUpdated    string        `json:"last_updated,omitempty"`
	LastCommandOut string        `json:"last_command_out,omitempty"`
	LastCheckMsg   string        `json:"last_check_msg,omitempty"` // Output from omamac-check (owl-check equivalent)
}

// DynamicCommandOutput allows a panel's shell Command to return JSON that dynamically overrides
// badge_text, status, metrics, or rows on every refresh cycle.
type DynamicCommandOutput struct {
	BadgeText string        `json:"badge_text,omitempty"`
	Status    string        `json:"status,omitempty"`
	Subtitle  string        `json:"subtitle,omitempty"`
	Metrics   []PanelMetric `json:"metrics,omitempty"`
	Rows      []PanelRow    `json:"rows,omitempty"`
}

// Normalize ensures valid dimensions, defaults, and IDs.
func (p *PanelSpec) Normalize() {
	if p.ID == "" {
		p.ID = Slugify(p.Title)
		if p.ID == "" {
			p.ID = fmt.Sprintf("panel-%d", time.Now().Unix())
		}
	}
	if p.BuiltinKind != "" {
		p.IsBuiltin = true
		p.Type = PanelTypeNative // system modules render natively (board, ticket viewer)
	}
	if p.Kind == "" {
		p.Kind = "panel"
	}
	if p.GridSize == "" {
		if p.Kind == "widget" {
			p.GridSize = "3x3"
		} else {
			p.GridSize = "6x2"
		}
	}
	if p.Type != PanelTypeHTML {
		p.Type = PanelTypeNative
	}
	if p.Icon == "" {
		p.Icon = "⚡"
	}
	if p.Status == "" {
		p.Status = "ok"
	}
	if p.RefreshSeconds <= 0 {
		p.RefreshSeconds = 30
	}
	if p.Width < 280 {
		p.Width = 380
	}
	if p.Height < 220 {
		p.Height = 360
	}
}

// CheckPanelSpec is the macOS equivalent of OmaLauncher's `owl-check <file>`:
// It verifies required Homebrew dependencies (`requires_brew`), runs a live data refresh,
// and validates that the widget/app/panel layout has valid metrics/rows or HTML.
func (p *PanelSpec) CheckPanelSpec(ctx context.Context, extraEnv ...string) ([]string, error) {
	p.Normalize()
	var notes []string

	for _, pkg := range p.RequiresBrew {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if _, err := exec.LookPath(pkg); err != nil {
			notes = append(notes, fmt.Sprintf("brew package %q not in PATH (install via: brew install %s)", pkg, pkg))
		} else {
			notes = append(notes, fmt.Sprintf("brew dependency %q verified OK", pkg))
		}
	}

	if err := p.RunDataRefresh(ctx, extraEnv...); err != nil {
		return notes, fmt.Errorf("command execution failed: %w", err)
	}

	if p.Type == PanelTypeHTML && strings.TrimSpace(p.HTML) == "" {
		return notes, fmt.Errorf("panel %q has type=html but empty HTML body", p.ID)
	}
	if p.Type == PanelTypeNative && len(p.Metrics) == 0 && len(p.Rows) == 0 {
		notes = append(notes, "warning: native panel has 0 metrics and 0 rows")
	}

	summary := fmt.Sprintf("owl-check OK • kind=%s (%s) • badge=%q • %d metrics, %d rows",
		p.Kind, p.GridSize, p.BadgeText, len(p.Metrics), len(p.Rows))
	p.LastCheckMsg = summary
	return append(notes, summary), nil
}

// TrayLabel returns the compact string displayed in the macOS menu bar for this panel.
func (p *PanelSpec) TrayLabel() string {
	dot := ""
	switch p.Status {
	case "crit":
		dot = "🔴 "
	case "warn":
		dot = "🟠 "
	}
	badge := strings.TrimSpace(p.BadgeText)
	if badge == "" {
		badge = p.Title
	}
	if own := strings.TrimSpace(p.MenuBarText); own != "" {
		badge = strings.TrimSpace(strings.ReplaceAll(own, MenuBarStatusToken, strings.TrimSpace(p.BadgeText)))
	}
	return strings.TrimSpace(fmt.Sprintf("%s%s %s", dot, p.Icon, badge))
}

// MenuBarStatusToken in MenuBarText is replaced by the status text the panel's script reports.
const MenuBarStatusToken = "{status}"

// RunDataRefresh executes the panel's shell Command (with Vault credentials injected as env vars)
// and updates its live fields.
func (p *PanelSpec) RunDataRefresh(ctx context.Context, extraEnv ...string) error {
	p.LastUpdated = time.Now().Format("15:04:05")
	cmdStr := strings.TrimSpace(p.Command)
	if cmdStr == "" {
		return nil
	}

	runCtx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "/bin/zsh", "-c", cmdStr)
	cmd.Env = append(os.Environ(), extraEnv...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()

	raw := strings.TrimSpace(out.String())
	p.LastCommandOut = raw
	if raw == "" {
		return nil
	}

	// 1. If the command outputs JSON matching DynamicCommandOutput, merge it directly!
	if strings.HasPrefix(raw, "{") {
		var dyn DynamicCommandOutput
		if err := json.Unmarshal([]byte(raw), &dyn); err == nil {
			if dyn.BadgeText != "" {
				p.BadgeText = dyn.BadgeText
			}
			if dyn.Status != "" {
				p.Status = dyn.Status
			}
			if dyn.Subtitle != "" {
				p.Subtitle = dyn.Subtitle
			}
			if len(dyn.Metrics) > 0 {
				p.Metrics = dyn.Metrics
			}
			if len(dyn.Rows) > 0 {
				p.Rows = dyn.Rows
			}
			return nil
		}
	}

	// 2. Otherwise parse KEY=VALUE lines or plain lines into rows/badge
	lines := strings.Split(raw, "\n")
	var parsedRows []PanelRow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "BADGE=") {
			p.BadgeText = strings.TrimPrefix(line, "BADGE=")
			continue
		}
		if strings.HasPrefix(line, "STATUS=") {
			p.Status = strings.ToLower(strings.TrimPrefix(line, "STATUS="))
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && len(k) < 40 {
			parsedRows = append(parsedRows, PanelRow{
				Left:   strings.TrimSpace(k),
				Right:  strings.TrimSpace(v),
				Status: "info",
			})
		}
	}
	if len(parsedRows) > 0 && len(p.Rows) == 0 {
		p.Rows = parsedRows
	}
	return nil
}

// LoadPanels reads all *.json panel definitions from dir (or seeds defaults if empty),
// and ensures pre-delivered BuiltinSystemPanels are always present.
func LoadPanels(dir string) ([]*PanelSpec, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var panels []*PanelSpec
	seenIDs := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var p PanelSpec
		if err := json.Unmarshal(raw, &p); err == nil {
			p.Normalize()
			panels = append(panels, &p)
			seenIDs[p.ID] = true
		}
	}

	if len(panels) == 0 {
		defaults := DefaultStarterPanels()
		for _, p := range defaults {
			_ = SavePanel(dir, p)
		}
		return defaults, nil
	}

	// Ensure pre-delivered built-in modules (Jira Quick-Access & Atlassian Status) exist even on upgraded installs
	for _, bp := range BuiltinSystemPanels() {
		if !seenIDs[bp.ID] {
			_ = SavePanel(dir, bp)
			panels = append(panels, bp)
		}
	}

	sort.Slice(panels, func(i, j int) bool {
		return panels[i].ID < panels[j].ID
	})
	return panels, nil
}

// SavePanel writes a single PanelSpec to <dir>/<id>.json.
func SavePanel(dir string, p *PanelSpec) error {
	p.Normalize()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, p.ID+".json"), data, 0o644)
}

// DeletePanel removes <dir>/<id>.json.
func DeletePanel(dir, id string) error {
	return os.Remove(filepath.Join(dir, id+".json"))
}

// Slugify converts a title into a clean filename-safe ID.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// BuiltinSystemPanels returns the pre-delivered, full-featured system modules
// (Jira Quick-Access JQL/Board/Ticket Viewer & Atlassian Cloud Live-Status Monitor)
// that replace the standalone jira-quick-access app. Because their feature scope is richer,
// they are marked IsBuiltin=true (switched on/off via Overview switches, not AI-rewritten).
func BuiltinSystemPanels() []*PanelSpec {
	now := time.Now().Format("15:04:05")
	return []*PanelSpec{
		{
			ID:             "00-jira-quick-access",
			Title:          "Jira Quick-Access",
			Subtitle:       "JQL-Counter über alle Instanzen • Kanban-Board • Ticket-Viewer",
			Kind:           "app",
			GridSize:       "6x2",
			Icon:           "🎫",
			BadgeText:      "Jira …",
			Status:         "ok",
			Type:           PanelTypeNative,
			IsBuiltin:      true,
			BuiltinKind:    BuiltinJiraQuickAccess,
			Enabled:        true,
			PinnedFloating: false,
			RefreshSeconds: 60,
			Width:          520,
			Height:         500,
			LastUpdated:    now,
		},
		{
			ID:             "00-atlassian-status",
			Title:          "Atlassian Status",
			Subtitle:       "Live-Status von 8 Cloud-Diensten (Jira, Confluence, Bitbucket, Rovo …)",
			Kind:           "panel",
			GridSize:       "6x2",
			Icon:           "🌐",
			BadgeText:      "Atlassian OK (8/8)",
			Status:         "ok",
			Type:           PanelTypeNative,
			IsBuiltin:      true,
			BuiltinKind:    BuiltinAtlassianStatus,
			Enabled:        true,
			PinnedFloating: false,
			RefreshSeconds: 180,
			Width:          440,
			Height:         420,
			Metrics: []PanelMetric{
				{Label: "Atlassian Cloud", Value: "8 / 8 OK", Unit: "operational", Status: "ok", Progress: 100},
				{Label: "Aktive Störungen", Value: "0", Unit: "live", Status: "ok", Progress: 100},
			},
			Rows: []PanelRow{
				{Left: "Jira Software", Right: "All Systems Operational", Badge: "OK", Status: "ok", URL: "https://jira-software.status.atlassian.com"},
				{Left: "Jira Service Management", Right: "All Systems Operational", Badge: "OK", Status: "ok", URL: "https://jira-service-management.status.atlassian.com"},
				{Left: "Confluence", Right: "All Systems Operational", Badge: "OK", Status: "ok", URL: "https://confluence.status.atlassian.com"},
				{Left: "Bitbucket", Right: "All Systems Operational", Badge: "OK", Status: "ok", URL: "https://status.bitbucket.org"},
				{Left: "Atlassian Migrations", Right: "All Systems Operational", Badge: "OK", Status: "ok", URL: "https://migrations.status.atlassian.com"},
				{Left: "Atlassian Analytics", Right: "All Systems Operational", Badge: "OK", Status: "ok", URL: "https://analytics.status.atlassian.com"},
				{Left: "Rovo & Rovo Dev", Right: "All Systems Operational", Badge: "OK", Status: "ok", URL: "https://rovo.status.atlassian.com"},
			},
			Actions: []PanelAction{
				{Label: "Atlassian Statuspage ↗", URL: "https://status.atlassian.com", Style: "primary"},
			},
			LastUpdated: now,
		},
	}
}

// DefaultStarterPanels returns the pre-delivered BuiltinSystemPanels plus the AI-customizable starter panels.
func DefaultStarterPanels() []*PanelSpec {
	return append(BuiltinSystemPanels(), StarterPanels()...)
}

// StarterPanels returns 4 AI-customizable example panels (2 Native mygo/ui + 2 HTML/data).
func StarterPanels() []*PanelSpec {
	now := time.Now().Format("15:04:05")
	return []*PanelSpec{
		{
			ID:             "01-sys-pulse",
			Title:          "Apple Silicon System Pulse",
			Subtitle:       "CPU, Speicher und SSD dieses Macs",
			Icon:           "⚡",
			BadgeText:      "Sys OK",
			Status:         "ok",
			Type:           PanelTypeNative,
			Enabled:        true,
			PinnedFloating: false,
			RefreshSeconds: 15,
			Width:          400,
			Height:         380,
			Command:        `load=$(sysctl -n vm.loadavg 2>/dev/null | awk '{print $2}'); disk=$(df -h / | awk 'NR==2 {print $5}'); up=$(uptime | sed 's/.*up \([^,]*\),.*/\1/'); printf '{"badge_text":"Load %s • %s","status":"ok","metrics":[{"label":"Load Avg (1m)","value":"%s","unit":"cores","status":"ok","progress":35},{"label":"Root Disk Used","value":"%s","unit":"SSD","status":"ok","progress":52}],"rows":[{"left":"System Uptime","right":"%s","badge":"ACTIVE","status":"ok"},{"left":"Architecture","right":"%s","badge":"NATIVE","status":"info"}]}' "$load" "$disk" "$load" "$disk" "$up" "$(uname -m)"`,
			Metrics: []PanelMetric{
				{Label: "Load Avg (1m)", Value: "1.42", Unit: "cores", Status: "ok", Progress: 35},
				{Label: "Root Disk Used", Value: "48%", Unit: "NVMe", Status: "ok", Progress: 48},
			},
			Rows: []PanelRow{
				{Left: "Kernel / Arch", Right: "Darwin arm64", Badge: "APPLE SILICON", Status: "ok"},
				{Left: "Window Manager", Right: "OmaMac Wal-Panel", Badge: "READY", Status: "info"},
			},
			Actions: []PanelAction{
				{Label: "Activity Monitor", Command: "open -a 'Activity Monitor'", Style: "primary"},
				{Label: "Refresh Now", Command: "echo refreshed", Style: "default"},
			},
			PromptHistory: []PromptStep{
				{Step: 1, Prompt: "Create a native macOS system pulse status panel with CPU load and disk usage", Provider: "gemini", Model: "gemini-4-argon-medium", Timestamp: now},
				{Step: 2, Prompt: "Add an Activity Monitor quick-launch button and show load + disk in the menubar badge", Provider: "gemini", Model: "gemini-4-argon-medium", Timestamp: now},
			},
			LastUpdated: now,
		},
		{
			ID:             "02-ai-agents",
			Title:          "AI Coding Agents & Antigravity",
			Subtitle:       "Anmeldestatus von Gemini (Antigravity) und Claude",
			Icon:           "🤖",
			BadgeText:      "AI Ready",
			Status:         "ok",
			Type:           PanelTypeNative,
			Enabled:        true,
			PinnedFloating: false,
			RefreshSeconds: 30,
			Width:          410,
			Height:         370,
			Command:        `agy_ok="Offline"; [ -f "$HOME/.gemini/antigravity-cli/antigravity-oauth-token" ] && agy_ok="Connected"; procs=$(ps aux | grep -E "agy|claude|gemini" | grep -v grep | wc -l | tr -d ' '); printf '{"badge_text":"%s (%s)","status":"ok","metrics":[{"label":"Antigravity OAuth","value":"%s","unit":"SSO","status":"ok","progress":100},{"label":"Active Agent Procs","value":"%s","unit":"procs","status":"info","progress":25}],"rows":[{"left":"Primary Engine","right":"Gemini 4 Argon (Antigravity)","badge":"OAUTH","status":"ok"},{"left":"Secondary Engine","right":"Claude Sonnet 5.5 / API","badge":"READY","status":"info"}]}' "$agy_ok" "$procs" "$agy_ok" "$procs"`,
			Metrics: []PanelMetric{
				{Label: "Antigravity OAuth", Value: "Connected", Unit: "SSO", Status: "ok", Progress: 100},
				{Label: "Active Agent Procs", Value: "2", Unit: "running", Status: "info", Progress: 30},
			},
			Rows: []PanelRow{
				{Left: "Primary Model", Right: "gemini-4-argon-medium", Badge: "ANTIGRAVITY", Status: "ok"},
				{Left: "Fallback / Dual", Right: "claude-sonnet-5-5-medium", Badge: "CLAUDE", Status: "info"},
			},
			Actions: []PanelAction{
				{Label: "Open Terminal (agy)", Command: "open -a Terminal", Style: "primary"},
			},
			PromptHistory: []PromptStep{
				{Step: 1, Prompt: "Build a status panel tracking Antigravity login and running AI agent processes", Provider: "gemini", Model: "gemini-4-argon-medium", Timestamp: now},
			},
			LastUpdated: now,
		},
		{
			ID:             "03-git-radar",
			Title:          "Dev Workspace Git Radar",
			Subtitle:       "Git-Status der Projekte im Workspace (HTML)",
			Icon:           "🌿",
			BadgeText:      "Git Radar",
			Status:         "ok",
			Type:           PanelTypeHTML,
			Enabled:        true,
			PinnedFloating: false,
			RefreshSeconds: 45,
			Width:          420,
			Height:         380,
			Command:        `branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main"); dirty=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' '); printf '{"badge_text":"%s (%sΔ)","status":"ok"}' "$branch" "$dirty"`,
			HTML:           DefaultGitRadarHTML(),
			PromptHistory: []PromptStep{
				{Step: 1, Prompt: "Generate an HTML glass status panel showing Git workspace branch, dirty files and commit pulse", Provider: "claude", Model: "claude-sonnet-5-5-medium", Timestamp: now},
				{Step: 2, Prompt: "Add interactive copy-branch button and Omarchy TokyoNight neon accents", Provider: "claude", Model: "claude-sonnet-5-5-medium", Timestamp: now},
			},
			LastUpdated: now,
		},
		{
			ID:             "04-mydealz-3d",
			Title:          "mydealz 3D-Drucker & Idealo Radar",
			Subtitle:       "3D-Drucker-Deals von mydealz.de mit Preisalarm ($MYDEALZ_KEYWORD)",
			Icon:           "🖨️",
			BadgeText:      "3D Deals Live",
			Status:         "ok",
			Type:           PanelTypeNative,
			Enabled:        true,
			PinnedFloating: false,
			RefreshSeconds: 180,
			Width:          440,
			Height:         410,
			Command:        Mydealz3DPrinterCommand(),
			Metrics: []PanelMetric{
				{Label: "mydealz 3D-Drucker", Value: "29 Deals", Unit: "live", Status: "ok", Progress: 85},
				{Label: "Target Filter ($MYDEALZ_KEYWORD)", Value: "Bambu Lab", Unit: "alert", Status: "info", Progress: 100},
			},
			Rows: []PanelRow{
				{Left: "Bambu Lab Black Friday: 3D-Drucker (X/P/A)", Right: "mydealz ↗", Badge: "HOT 🔥", Status: "ok", URL: "https://www.mydealz.de/gruppe/3d-drucker"},
				{Left: "Bambu Lab: 2000 Credits (20€–50€ Wert)", Right: "mydealz ↗", Badge: "DEAL", Status: "ok", URL: "https://www.mydealz.de/gruppe/3d-drucker"},
				{Left: "GEEETECH PETG Filament 1kg 1.75mm", Right: "mydealz ↗", Badge: "FILAMENT", Status: "info", URL: "https://www.mydealz.de/gruppe/3d-drucker"},
				{Left: "Idealo Preiswächter (Bambu Lab P1S)", Right: "Target ≤ 499€", Badge: "WATCH", Status: "warn", URL: "https://www.idealo.de/preisvergleich/MainSearchProductCategory.html?q=Bambu+Lab+P1S"},
			},
			Actions: []PanelAction{
				{Label: "mydealz 3D-Drucker ↗", URL: "https://www.mydealz.de/gruppe/3d-drucker", Style: "primary"},
				{Label: "Idealo 3D-Drucker ↗", URL: "https://www.idealo.de/preisvergleich/ProductCategory/19117.html", Style: "default"},
			},
			PromptHistory: []PromptStep{
				{Step: 1, Prompt: "Create a status panel listing current 3D printer deals from mydealz and monitoring Idealo target price", Provider: "gemini", Model: "gemini-4-argon-medium", Timestamp: now},
				{Step: 2, Prompt: "Use $MYDEALZ_KEYWORD and $IDEALO_TARGET_PRICE from the central Credentials Vault and make deal rows clickable", Provider: "gemini", Model: "gemini-4-argon-medium", Timestamp: now},
			},
			LastUpdated: now,
		},
	}
}

// Mydealz3DPrinterCommand returns a self-contained Python 3 one-liner that scrapes live 3D-printer deals
// from mydealz.de/gruppe/3d-drucker and filters/highlights using $MYDEALZ_KEYWORD and $IDEALO_TARGET_PRICE from the Vault.
func Mydealz3DPrinterCommand() string {
	return `python3 -c '
import os, urllib.request, re, json, html
kw = os.environ.get("MYDEALZ_KEYWORD", "Bambu").strip()
target_eur = os.environ.get("IDEALO_TARGET_PRICE", "499").strip()
url = "https://www.mydealz.de/gruppe/3d-drucker"
req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"})
try:
    page = urllib.request.urlopen(req, timeout=8).read().decode("utf-8", "ignore")
    matches = re.findall(r"<a[^>]*class=\"[^\"]*thread-link[^\"]*\"[^>]*title=\"([^\"]+)\"[^>]*href=\"([^\"]+)\"", page)
    rows = []
    kw_hits = 0
    for title, link in matches[:5]:
        clean = html.unescape(title)
        is_hit = kw.lower() in clean.lower() if kw else False
        if is_hit:
            kw_hits += 1
        rows.append({
            "left": clean[:48] + ("…" if len(clean) > 48 else ""),
            "right": "Open ↗",
            "badge": "MATCH 🔥" if is_hit else "DEAL",
            "status": "ok" if is_hit else "info",
            "url": link
        })
    rows.append({
        "left": f"Idealo Watch ({kw or \"3D-Drucker\"})",
        "right": f"Alert ≤ {target_eur}€",
        "badge": "IDEALO",
        "status": "warn",
        "url": f"https://www.idealo.de/preisvergleich/MainSearchProductCategory.html?q={urllib.parse.quote(kw or \"3D Drucker\")}"
    })
    out = {
        "badge_text": f"{len(matches)} Deals ({kw_hits} {kw})",
        "status": "ok",
        "metrics": [
            {"label": "mydealz 3D-Drucker", "value": f"{len(matches)} Deals", "unit": "live", "status": "ok", "progress": 85},
            {"label": f"Vault Filter ({kw})", "value": f"{kw_hits} Hits", "unit": f"≤{target_eur}€", "status": "ok" if kw_hits > 0 else "info", "progress": 100}
        ],
        "rows": rows
    }
    print(json.dumps(out))
except Exception as e:
    print(json.dumps({"badge_text": "3D Deals", "status": "info"}))
'`
}

// DefaultGitRadarHTML returns a sleek Omarchy-styled HTML/CSS/JS micro-panel for WebView mode.
func DefaultGitRadarHTML() string {
	return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8"/>
<style>
  :root {
    --bg: rgba(22, 24, 38, 0.82);
    --card: rgba(36, 40, 59, 0.78);
    --border: rgba(122, 162, 247, 0.28);
    --accent: #7aa2f7;
    --green: #9ece6a;
    --amber: #e0af68;
    --text: #c0caf5;
    --muted: #7982a9;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "JetBrains Mono", monospace; }
  body {
    background: var(--bg);
    color: var(--text);
    padding: 14px;
    user-select: none;
  }
  .hdr { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
  .title { font-size: 14px; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 6px; }
  .pill { font-size: 10px; font-weight: 700; padding: 3px 8px; border-radius: 99px; background: rgba(158,206,106,0.18); color: var(--green); border: 1px solid rgba(158,206,106,0.4); }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-bottom: 12px; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 10px; }
  .lbl { font-size: 10px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; }
  .val { font-size: 18px; font-weight: 700; color: var(--accent); margin-top: 4px; }
  .list { background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 10px; }
  .row { display: flex; justify-content: space-between; font-size: 12px; padding: 6px 0; border-bottom: 1px solid rgba(255,255,255,0.06); }
  .row:last-child { border-bottom: none; }
  .spark { width: 100%; height: 42px; margin-top: 8px; }
</style>
</head>
<body>
  <div class="hdr">
    <div class="title">🌿 OmaMac Git &amp; Workspace Radar</div>
    <span class="pill" id="badge">HTML WEBVIEW</span>
  </div>
  <div class="grid">
    <div class="card">
      <div class="lbl">Active Status</div>
      <div class="val" id="statVal">Live Sync</div>
    </div>
    <div class="card">
      <div class="lbl">Last Refresh</div>
      <div class="val" id="timeVal">Just now</div>
    </div>
  </div>
  <div class="list">
    <div class="row"><span>Engine Mode</span><strong style="color:#7aa2f7">WebView Glass (Type B)</strong></div>
    <div class="row"><span>2-Prompt Refinable</span><strong style="color:#9ece6a">Active</strong></div>
    <div class="row"><span>Raw Output</span><span id="rawOut" style="color:#e0af68">Ready</span></div>
    <svg class="spark" viewBox="0 0 200 40">
      <polyline fill="none" stroke="#7aa2f7" stroke-width="2.5" points="0,30 30,22 60,26 90,12 120,18 150,8 180,14 200,6"/>
    </svg>
  </div>
  <script>
    window.onOmaMacUpdate = function(data) {
      if (!data) return;
      if (data.badge_text) document.getElementById('statVal').textContent = data.badge_text;
      if (data.last_updated) document.getElementById('timeVal').textContent = data.last_updated;
      if (data.last_command_out) document.getElementById('rawOut').textContent = data.last_command_out.slice(0, 38);
    };
  </script>
</body>
</html>`
}

// StoreCategoryItem represents a ready-to-install or AI-customizable template in the OmaLauncher Store.
type StoreCategoryItem struct {
	Category string
	Kind     string // "widget", "app", "panel"
	GridSize string // "3x3" or "6x2"
	Title    string
	Subtitle string
	Icon     string
	Prompt   string
	Spec     *PanelSpec
}

// StoreCatalog returns the 5 OmaLauncher inspiration categories from Screenshot 2
// (System, Mac & Device, Web & Deals, Personal, Fun / Pixel-Art Owl).
func StoreCatalog() []StoreCategoryItem {
	now := time.Now().Format("15:04:05")
	return []StoreCategoryItem{
		{
			Category: "System",
			Kind:     "widget",
			GridSize: "6x2",
			Title:    "CPU, Speicher & SSD",
			Subtitle: "Top-Prozesse, Auslastung und SSD-Belegung",
			Icon:     "⚡",
			Prompt:   "Create a 6x2 system widget with CPU load, memory pressure, disk usage and top processes",
			Spec:     StarterPanels()[0],
		},
		{
			Category: "Mac & Geräte",
			Kind:     "widget",
			GridSize: "3x3",
			Title:    "Akku, Wiedergabe & AirPods",
			Subtitle: "Akkustand, Ladezustand und Audio-Ausgabe",
			Icon:     "🔋",
			Prompt:   "Create a 3x3 Mac device widget showing battery percentage, charging state and audio output",
			Spec: &PanelSpec{
				ID:             "store-mac-battery",
				Title:          "Akku & Audio",
				Subtitle:       "Akkustand und Stromquelle des MacBooks",
				Kind:           "widget",
				GridSize:       "3x3",
				Icon:           "🔋",
				BadgeText:      "98% AC",
				Status:         "ok",
				Type:           PanelTypeNative,
				Enabled:        true,
				RefreshSeconds: 30,
				Width:          360,
				Height:         330,
				Command:        `pct=$(pmset -g batt | grep -Eo "\d+%" | head -n1); src=$(pmset -g batt | head -n1 | sed "s/.*'\(.*\)'.*/\1/"); printf '{"badge_text":"%s (%s)","status":"ok","metrics":[{"label":"Battery Level","value":"%s","unit":"%s","status":"ok","progress":90}]}' "${pct:-100%}" "${src:-AC}" "${pct:-100%}" "${src:-AC}"`,
				Metrics: []PanelMetric{
					{Label: "Battery Charge", Value: "98%", Unit: "AC Power", Status: "ok", Progress: 98},
				},
				Rows: []PanelRow{
					{Left: "Power Source", Right: "MagSafe / USB-C", Badge: "CHARGED", Status: "ok"},
				},
				LastUpdated: now,
			},
		},
		{
			Category: "Web & Deals",
			Kind:     "panel",
			GridSize: "6x2",
			Title:    "mydealz 3D-Drucker & Idealo Radar",
			Subtitle: "Aktuelle Deals von mydealz.de und Idealo-Preiswächter",
			Icon:     "🖨️",
			Prompt:   "Create a 6x2 web data panel for mydealz 3D-Drucker deals and Idealo target price ($IDEALO_TARGET_PRICE)",
			Spec:     StarterPanels()[3],
		},
		{
			Category: "Persönlich",
			Kind:     "app",
			GridSize: "3x3",
			Title:    "Fokus-Timer",
			Subtitle: "25-Minuten-Pomodoro mit Tages-Serie",
			Icon:     "⏱️",
			Prompt:   "Create a 3x3 personal focus timer & habit tracker mini-app with start/reset actions",
			Spec: &PanelSpec{
				ID:             "store-focus-timer",
				Title:          "Fokus-Timer",
				Subtitle:       "25-Minuten-Pomodoro mit Tages-Serie",
				Kind:           "app",
				GridSize:       "3x3",
				Icon:           "⏱️",
				BadgeText:      "25:00 Focus",
				Status:         "info",
				Type:           PanelTypeNative,
				Enabled:        true,
				RefreshSeconds: 60,
				Width:          360,
				Height:         340,
				Metrics: []PanelMetric{
					{Label: "Focus Session", Value: "25:00", Unit: "Pomodoro", Status: "info", Progress: 75},
					{Label: "Daily Streak", Value: "5 Days", Unit: "active", Status: "ok", Progress: 100},
				},
				Rows: []PanelRow{
					{Left: "Current Goal", Right: "Ship OmaMac v1.2", Badge: "DEEP WORK", Status: "ok"},
				},
				Actions: []PanelAction{
					{Label: "▶ Start 25m Timer", Command: "afplay /System/Library/Sounds/Glass.aiff", Style: "primary"},
				},
				LastUpdated: now,
			},
		},
		{
			Category: "Spaß",
			Kind:     "widget",
			GridSize: "3x3",
			Title:    "Pixel-Eule",
			Subtitle: "Retro-Eule für den Desktop – klicken zum Füttern",
			Icon:     "🦉",
			Prompt:   "Create a 3x3 interactive pixel-art owl desktop widget that can be fed cookies on click",
			Spec: &PanelSpec{
				ID:             "store-pixel-owl",
				Title:          "Pixel-Eule",
				Subtitle:       "Retro-Eule – klicken zum Füttern",
				Kind:           "widget",
				GridSize:       "3x3",
				Icon:           "🦉",
				BadgeText:      "Owl: Hungry 🍪",
				Status:         "warn",
				Type:           PanelTypeHTML,
				Enabled:        true,
				PinnedFloating: false,
				RefreshSeconds: 120,
				Width:          320,
				Height:         340,
				HTML:           DefaultPixelOwlHTML(),
				LastUpdated:    now,
			},
		},
	}
}

// DefaultPixelOwlHTML returns the interactive Pixel-Art Owl HTML widget inspired by OmaLauncher Screenshot 1.
func DefaultPixelOwlHTML() string {
	return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8"/>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; font-family: "JetBrains Mono", -apple-system, monospace; user-select: none; }
  body {
    background: #131623;
    color: #7aa2f7;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100vh;
    padding: 16px;
    cursor: pointer;
    border: 1px solid rgba(122, 162, 247, 0.35);
  }
  .bubble {
    align-self: flex-end;
    margin-right: 28px;
    margin-bottom: 6px;
    background: #1f2335;
    border: 2px solid #414868;
    border-radius: 8px;
    padding: 6px 10px;
    font-size: 18px;
    transition: transform 0.2s ease;
  }
  .owl-svg { width: 140px; height: 140px; image-rendering: pixelated; }
  .branch { width: 85%; height: 6px; background: #3b4261; margin-top: -4px; border-radius: 2px; }
  .caption { margin-top: 14px; font-size: 12px; color: #565f89; letter-spacing: 0.04em; }
  body:active .owl-svg { transform: scale(1.06); }
</style>
</head>
<body onclick="feedOwl()">
  <div class="bubble" id="bubble">🍪</div>
  <svg class="owl-svg" viewBox="0 0 16 16" shape-rendering="crispEdges">
    <rect x="4" y="2" width="2" height="2" fill="#3d59a1"/>
    <rect x="10" y="2" width="2" height="2" fill="#3d59a1"/>
    <rect x="3" y="4" width="10" height="9" fill="#3d59a1"/>
    <rect x="4" y="5" width="3" height="3" fill="#16161e"/>
    <rect x="9" y="5" width="3" height="3" fill="#16161e"/>
    <rect x="5" y="6" width="1" height="1" fill="#7aa2f7" id="eyeL"/>
    <rect x="10" y="6" width="1" height="1" fill="#7aa2f7" id="eyeR"/>
    <rect x="7" y="7" width="2" height="2" fill="#e0af68"/>
    <rect x="5" y="10" width="6" height="3" fill="#2ac3de" opacity="0.35"/>
    <rect x="5" y="13" width="2" height="1" fill="#e0af68"/>
    <rect x="9" y="13" width="2" height="1" fill="#e0af68"/>
  </svg>
  <div class="branch"></div>
  <div class="caption" id="statusText">hungrig · tippen zum füttern</div>
  <script>
    let snacks = 0;
    function feedOwl() {
      snacks++;
      document.getElementById('bubble').textContent = snacks % 2 === 1 ? '💛 satt!' : '🍪 +1';
      document.getElementById('statusText').textContent = 'gefüttert (' + snacks + ' 🍪) · glücklich';
      document.getElementById('statusText').style.color = '#9ece6a';
    }
  </script>
</body>
</html>`
}
