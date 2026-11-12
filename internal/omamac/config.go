package omamac

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ProviderType identifies the active LLM backend for generating and refining status panels.
type ProviderType string

const (
	ProviderGemini ProviderType = "gemini"
	ProviderClaude ProviderType = "claude"
)

// GeminiAuthMode controls how Gemini is authenticated.
type GeminiAuthMode string

const (
	GeminiAuthAntigravity GeminiAuthMode = "antigravity" // Uses local Antigravity OAuth session / agy CLI
	GeminiAuthAPIKey      GeminiAuthMode = "apikey"      // Uses direct GEMINI_API_KEY REST API
)

// ClaudeAuthMode controls how Claude is authenticated.
type ClaudeAuthMode string

const (
	ClaudeAuthAPIKey      ClaudeAuthMode = "apikey"      // Uses direct ANTHROPIC_API_KEY REST API
	ClaudeAuthAntigravity ClaudeAuthMode = "antigravity" // Uses Antigravity agy CLI with claude-sonnet/opus models
	ClaudeAuthCLI         ClaudeAuthMode = "claude-cli"  // Uses local claude CLI binary
)

// Config holds persistent settings for OmaMac (Wal-Panel, AI Providers, Hotkeys, Theme).
type Config struct {
	ActiveProvider ProviderType `json:"active_provider"`

	// Gemini Configuration (Antigravity Login preferred, fallback to API Key)
	GeminiAuthMode       GeminiAuthMode `json:"gemini_auth_mode"`
	GeminiModel          string         `json:"gemini_model"`
	GeminiAPIKey         string         `json:"gemini_api_key"`
	AntigravityCLIPath   string         `json:"antigravity_cli_path"`
	AntigravityTokenPath string         `json:"antigravity_token_path"`

	// Claude Configuration (Anthropic API Key, Antigravity Claude, or Claude CLI)
	ClaudeAuthMode ClaudeAuthMode `json:"claude_auth_mode"`
	ClaudeModel    string         `json:"claude_model"`
	ClaudeAPIKey   string         `json:"claude_api_key"`
	ClaudeCLIPath  string         `json:"claude_cli_path"`

	// Wal-Panel & Omarchy Desktop Settings
	LauncherShortcut string `json:"launcher_shortcut"`
	// LauncherAgent is what the launcher hands sentences to: "" OmaMac's own AI, else an
	// agentDef ID ("claude", "codex", …). Terminal is the app the agents run in ("" the first
	// installed of Ghostty, iTerm, Terminal). LauncherWidget shows the launcher's bar on the desktop.
	LauncherAgent  string `json:"launcher_agent,omitempty"`
	Terminal       string `json:"terminal,omitempty"`
	LauncherWidget bool   `json:"launcher_widget,omitempty"`
	// Waybar (Linux): "" OmaMac adds its modules to Waybar as it starts, then "on";
	// "off" when the user took them out (waybar.go).
	Waybar string `json:"waybar,omitempty"`

	// DashboardOrder is the order of the dashboard's tiles (panel IDs; panels not in it follow).
	DashboardOrder []string `json:"dashboard_order,omitempty"`
	// The main window's sidebar: collapsed to symbols in the other views, open on the dashboard.
	SidebarCollapsed     bool `json:"sidebar_collapsed,omitempty"`
	DashboardSidebarOpen bool `json:"dashboard_sidebar_open,omitempty"`
	// Language is "de" or "en"; empty follows the language of macOS.
	Language            string `json:"language,omitempty"`
	PanelsDir           string `json:"panels_dir"`
	DefaultPanelType    string `json:"default_panel_type"` // "native", "html", or "both"
	OnboardingDismissed bool   `json:"onboarding_dismissed"`

	// AIRunView is what the dialog shows while the AI works: AIRunViewLog or AIRunViewProgress.
	AIRunView string `json:"ai_run_view,omitempty"`
	// AIOutputRaw shows the AI's answer as indented JSON instead of tables.
	AIOutputRaw bool `json:"ai_output_raw,omitempty"`
}

const (
	AIRunViewLog      = "log"      // steps and the AI's streamed answer
	AIRunViewProgress = "progress" // a progress bar; the log one click away
)

// DefaultConfigDir returns ~/.config/omamac.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".omamac"
	}
	return filepath.Join(home, ".config", "omamac")
}

// DefaultConfigPath returns ~/.config/omamac/config.json.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.json")
}

// DefaultConfig returns a sensible default configuration with Antigravity auto-detection.
func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	agyBin := filepath.Join(home, ".local", "bin", "agy")
	if _, err := os.Stat(agyBin); err != nil {
		agyBin = "agy"
	}
	agyToken := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")

	claudeBin := "/opt/homebrew/bin/claude"
	if _, err := os.Stat(claudeBin); err != nil {
		claudeBin = "claude"
	}

	geminiMode := GeminiAuthAntigravity
	if _, err := os.Stat(agyToken); err != nil {
		if os.Getenv("GEMINI_API_KEY") != "" {
			geminiMode = GeminiAuthAPIKey
		}
	}

	claudeMode := ClaudeAuthAPIKey
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		if _, err := os.Stat(agyToken); err == nil {
			claudeMode = ClaudeAuthAntigravity
		}
	}

	return &Config{
		ActiveProvider:       ProviderGemini,
		GeminiAuthMode:       geminiMode,
		GeminiModel:          "gemini-4-argon-medium",
		GeminiAPIKey:         os.Getenv("GEMINI_API_KEY"),
		AntigravityCLIPath:   agyBin,
		AntigravityTokenPath: agyToken,
		ClaudeAuthMode:       claudeMode,
		ClaudeModel:          "claude-sonnet-5-5-medium",
		ClaudeAPIKey:         os.Getenv("ANTHROPIC_API_KEY"),
		ClaudeCLIPath:        claudeBin,
		LauncherShortcut:     "Alt+Space",
		PanelsDir:            filepath.Join(DefaultConfigDir(), "panels"),
		DefaultPanelType:     "native",
	}
}

// LoadConfig loads the configuration from disk or initializes defaults.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = SaveConfig(path, cfg)
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}
	cfg.Normalize()
	return cfg, nil
}

// Normalize fills in any missing default fields.
func (c *Config) Normalize() {
	def := DefaultConfig()
	if c.ActiveProvider == "" {
		c.ActiveProvider = def.ActiveProvider
	}
	if c.GeminiAuthMode == "" {
		c.GeminiAuthMode = def.GeminiAuthMode
	}
	if c.GeminiModel == "" {
		c.GeminiModel = def.GeminiModel
	}
	if c.AntigravityCLIPath == "" {
		c.AntigravityCLIPath = def.AntigravityCLIPath
	}
	if c.AntigravityTokenPath == "" {
		c.AntigravityTokenPath = def.AntigravityTokenPath
	}
	if c.ClaudeAuthMode == "" {
		c.ClaudeAuthMode = def.ClaudeAuthMode
	}
	if c.AIRunView != AIRunViewProgress {
		c.AIRunView = AIRunViewLog
	}
	if c.ClaudeModel == "" {
		c.ClaudeModel = def.ClaudeModel
	}
	if c.ClaudeCLIPath == "" {
		c.ClaudeCLIPath = def.ClaudeCLIPath
	}
	if c.LauncherShortcut == "" {
		c.LauncherShortcut = def.LauncherShortcut
	}
	if c.PanelsDir == "" {
		c.PanelsDir = def.PanelsDir
	}
	if c.DefaultPanelType == "" {
		c.DefaultPanelType = "native"
	}
}

// SaveConfig writes the configuration atomically to disk.
func SaveConfig(path string, cfg *Config) error {
	if path == "" {
		path = DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
