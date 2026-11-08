package omamac

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// AIEngine handles Prompt 1 (Create 1..N panels) and Prompt 2 (Refine existing panel)
// across Gemini (Antigravity Login or API Key) and Claude (API Key, Antigravity or CLI).
type AIEngine struct {
	Config     *Config
	Vault      *Vault
	HTTPClient *http.Client
}

// NewAIEngine creates an AIEngine bound to cfg.
func NewAIEngine(cfg *Config) *AIEngine {
	return &AIEngine{
		Config: cfg,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// ActiveProviderSummary returns a human-readable badge string for the currently selected provider/auth mode.
func (e *AIEngine) ActiveProviderSummary() string {
	cfg := e.Config
	if cfg.ActiveProvider == ProviderClaude {
		switch cfg.ClaudeAuthMode {
		case ClaudeAuthAntigravity:
			return fmt.Sprintf("Claude (%s via Antigravity OAuth)", cfg.ClaudeModel)
		case ClaudeAuthCLI:
			return fmt.Sprintf("Claude (%s via claude CLI)", cfg.ClaudeModel)
		default:
			return fmt.Sprintf("Claude (%s via Anthropic API Key)", cfg.ClaudeModel)
		}
	}
	if cfg.GeminiAuthMode == GeminiAuthAntigravity {
		return fmt.Sprintf("Gemini (%s via Antigravity Login)", cfg.GeminiModel)
	}
	return fmt.Sprintf("Gemini (%s via Gemini API Key)", cfg.GeminiModel)
}

// TestConnection verifies the active AI provider and returns a short status message + latency.
func (e *AIEngine) TestConnection(ctx context.Context) (string, error) {
	start := time.Now()
	resp, providerUsed, err := e.Complete(ctx, "Reply with the exact string: OMAMAC_AI_OK")
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return "", err
	}
	snippet := strings.TrimSpace(resp)
	if len(snippet) > 80 {
		snippet = snippet[:80] + "..."
	}
	return fmt.Sprintf("Connected via %s (%s): %s", providerUsed, elapsed, snippet), nil
}

// Complete sends a prompt to the active LLM provider (Gemini or Claude) and returns the raw text response.
func (e *AIEngine) Complete(ctx context.Context, prompt string) (string, string, error) {
	cfg := e.Config
	if cfg.ActiveProvider == ProviderClaude {
		switch cfg.ClaudeAuthMode {
		case ClaudeAuthAntigravity:
			model := cfg.ClaudeModel
			if model == "" {
				model = "claude-sonnet-5-5-medium"
			}
			out, err := e.runAntigravityCLI(ctx, model, prompt)
			return out, "Claude/Antigravity (" + model + ")", err
		case ClaudeAuthCLI:
			out, err := e.runClaudeCLI(ctx, prompt)
			return out, "Claude CLI", err
		default:
			if strings.TrimSpace(cfg.ClaudeAPIKey) == "" {
				// Auto-fallback to Antigravity if available
				if st := InspectAntigravityAuth(cfg); st.Available {
					model := cfg.ClaudeModel
					if !strings.Contains(model, "5-5") {
						model = "claude-sonnet-5-5-medium"
					}
					out, err := e.runAntigravityCLI(ctx, model, prompt)
					return out, "Claude/Antigravity Fallback (" + model + ")", err
				}
				return "", "", errors.New("Anthropic API Key is empty (configure in Settings or switch Claude Auth Mode to Antigravity)")
			}
			out, err := e.callAnthropicAPI(ctx, cfg.ClaudeAPIKey, cfg.ClaudeModel, prompt)
			return out, "Claude API (" + cfg.ClaudeModel + ")", err
		}
	}

	// Default: Gemini
	if cfg.GeminiAuthMode == GeminiAuthAntigravity {
		st := InspectAntigravityAuth(cfg)
		if st.Available {
			out, err := e.runAntigravityCLI(ctx, cfg.GeminiModel, prompt)
			if err == nil {
				return out, "Gemini/Antigravity (" + cfg.GeminiModel + ")", nil
			}
			// If API key is also configured, try it as backup
			if strings.TrimSpace(cfg.GeminiAPIKey) != "" {
				out2, err2 := e.callGeminiAPI(ctx, cfg.GeminiAPIKey, cfg.GeminiModel, prompt)
				if err2 == nil {
					return out2, "Gemini API Key Fallback", nil
				}
			}
			return "", "", err
		}
		if strings.TrimSpace(cfg.GeminiAPIKey) != "" {
			out, err := e.callGeminiAPI(ctx, cfg.GeminiAPIKey, cfg.GeminiModel, prompt)
			return out, "Gemini API Key", err
		}
		return "", "", errors.New("Antigravity login not active and no Gemini API Key set – click 'Antigravity Login' or enter an API Key in Settings")
	}

	if strings.TrimSpace(cfg.GeminiAPIKey) == "" {
		if st := InspectAntigravityAuth(cfg); st.Available {
			out, err := e.runAntigravityCLI(ctx, cfg.GeminiModel, prompt)
			return out, "Gemini/Antigravity (" + cfg.GeminiModel + ")", err
		}
		return "", "", errors.New("Gemini API Key is empty (enter key in Settings or switch to Antigravity Login)")
	}
	out, err := e.callGeminiAPI(ctx, cfg.GeminiAPIKey, cfg.GeminiModel, prompt)
	return out, "Gemini API (" + cfg.GeminiModel + ")", err
}

func (e *AIEngine) runAntigravityCLI(ctx context.Context, model, prompt string) (string, error) {
	st := InspectAntigravityAuth(e.Config)
	bin := st.CLIPath
	if bin == "" {
		bin = "agy"
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, antigravityArgs(model, prompt)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		// CLIs print quota/login errors ("Usage Limit Reached") to stdout and exit non-zero.
		return "", fmt.Errorf("agy CLI: %v: %s", err, cliErrorDetail(out, stderr.String()))
	}
	return out, nil
}

// antigravityArgs builds the agy command line. -p (--print) takes the prompt as
// its value, so it goes last: "-p --disable-slash-commands <prompt>" sent the
// flag as the prompt (agy 1.5 rejects that).
func antigravityArgs(model, prompt string) []string {
	args := []string{"--disable-slash-commands"}
	if model != "" {
		args = append(args, "--model", model)
	}
	return append(args, "-p", prompt)
}

// unreachableErr says which AI failed, naming the configured one when the call did not get that far.
func (e *AIEngine) unreachableErr(providerLabel string, err error) error {
	if providerLabel == "" {
		providerLabel = e.ActiveProviderSummary()
	}
	return fmt.Errorf("KI nicht erreichbar (%s): %w", providerLabel, err)
}

func (e *AIEngine) runClaudeCLI(ctx context.Context, prompt string) (string, error) {
	bin := e.Config.ClaudeCLIPath
	if bin == "" {
		bin = "claude"
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, "-p", prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		return "", fmt.Errorf("claude CLI: %v: %s", err, cliErrorDetail(out, stderr.String()))
	}
	return out, nil
}

// cliErrorDetail returns the most useful part of a failed CLI's output for the error message.
func cliErrorDetail(stdout, stderr string) string {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = strings.TrimSpace(stdout)
	}
	if msg == "" {
		return "keine Ausgabe"
	}
	return truncateStr(strings.Join(strings.Fields(msg), " "), 200)
}

func (e *AIEngine) callGeminiAPI(ctx context.Context, apiKey, model, prompt string) (string, error) {
	apiModel := model
	if apiModel == "" || strings.HasPrefix(apiModel, "gemini-4-") || strings.HasPrefix(apiModel, "gemini-3.8-") {
		apiModel = "gemini-2.5-flash"
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", apiModel)

	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey) // never in the URL: transport errors print the URL

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Gemini API HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("Gemini API returned empty candidates")
	}
	return parsed.Candidates[0].Content.Parts[0].Text, nil
}

func (e *AIEngine) callAnthropicAPI(ctx context.Context, apiKey, model, prompt string) (string, error) {
	apiModel := model
	if apiModel == "" || strings.Contains(apiModel, "5-5") {
		apiModel = "claude-sonnet-4-20250514"
	}
	payload := map[string]any{
		"model":      apiModel,
		"max_tokens": 4096,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Anthropic API HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Content) == 0 {
		return "", errors.New("Anthropic API returned empty content")
	}
	return parsed.Content[0].Text, nil
}

// GeneratePanels executes Prompt 1: creates 1 or multiple status panels (supporting "native", "html", or "both").
func (e *AIEngine) GeneratePanels(ctx context.Context, userPrompt string, requestedType string, count int) ([]*PanelSpec, string, error) {
	if count <= 0 {
		count = 1
	}
	if count > 6 {
		count = 6
	}
	if requestedType == "both" && count < 2 {
		count = 2
	}

	vaultCatalog := "None configured."
	var vaultEnv []string
	if e.Vault != nil {
		vaultCatalog = e.Vault.AIPromptCatalog()
		vaultEnv = e.Vault.EnvSlice()
	}

	sysPrompt := fmt.Sprintf(`You are the OmaMac Status Panel Generator for macOS (Omarchy-inspired desktop environment).
Generate a JSON array of %d macOS status panel object(s) for the following user request:
USER PROMPT: %q
PANEL TYPE PREFERENCE: %q ("native" = GPU mygo/ui declarative metrics/rows/actions, "html" = self-contained dark glass HTML/CSS/JS micro-panel, "both" = generate both "native" and "html" panels).

AVAILABLE CENTRAL VAULT CREDENTIALS (injected automatically as environment variables into "command" at runtime; reference them as $VAR_NAME, never hardcode secrets):
%s

Return ONLY a valid JSON array of PanelSpec objects matching this exact schema (no markdown explanation outside JSON):
[
  {
    "id": "unique-kebab-id",
    "title": "Panel Title",
    "subtitle": "Short description",
    "icon": "⚡",
    "badge_text": "Compact menubar status",
    "status": "ok",
    "type": "native",
    "enabled": true,
    "pinned_floating": false,
    "refresh_seconds": 20,
    "width": 400,
    "height": 370,
    "command": "optional safe macOS shell/python3 command outputting JSON {\"badge_text\":\"...\",\"status\":\"ok\",\"metrics\":[...],\"rows\":[{\"left\":\"...\",\"right\":\"...\",\"badge\":\"...\",\"status\":\"ok\",\"url\":\"https://...\"}]}",
    "metrics": [
      {"label": "Metric 1", "value": "42%%", "unit": "load", "status": "ok", "progress": 42}
    ],
    "rows": [
      {"left": "Item name", "right": "Value", "badge": "OK", "status": "ok", "url": "https://example.com"}
    ],
    "actions": [
      {"label": "Action Button", "command": "open -a Terminal", "style": "primary"}
    ],
    "html": "<!DOCTYPE html><html>... (required when type is \"html\", dark TokyoNight glass styling, defines window.onOmaMacUpdate = function(data){...}) ...</html>"
  }
]`, count, userPrompt, requestedType, vaultCatalog)

	rawResp, providerLabel, err := e.Complete(ctx, sysPrompt)
	now := time.Now().Format("15:04:05")
	if err != nil {
		return nil, providerLabel, e.unreachableErr(providerLabel, err)
	}

	{
		if panels, parseErr := extractPanelsFromText(rawResp); parseErr == nil && len(panels) > 0 {
			for i, p := range panels {
				if requestedType == "native" {
					p.Type = PanelTypeNative
				} else if requestedType == "html" {
					p.Type = PanelTypeHTML
					if strings.TrimSpace(p.HTML) == "" {
						p.HTML = synthesizeHTMLForPanel(p)
					}
				} else if requestedType == "both" {
					if i%2 == 0 {
						p.Type = PanelTypeNative
					} else {
						p.Type = PanelTypeHTML
						if strings.TrimSpace(p.HTML) == "" {
							p.HTML = synthesizeHTMLForPanel(p)
						}
					}
				}
				p.Enabled = true
				p.Normalize()
				p.ID = fmt.Sprintf("%s-%d", Slugify(p.Title), time.Now().Unix()%10000+int64(i))
				p.PromptHistory = []PromptStep{
					{
						Step:      1,
						Prompt:    userPrompt,
						Provider:  string(e.Config.ActiveProvider),
						Model:     providerLabel,
						Timestamp: now,
					},
				}
				_ = p.RunDataRefresh(ctx, vaultEnv...)
			}
			return panels, providerLabel, nil
		}
	}

	return nil, providerLabel, fmt.Errorf("KI-Antwort enthielt kein gültiges Panel-JSON (%s): %s", providerLabel, truncateStr(strings.Join(strings.Fields(rawResp), " "), 160))
}

// RefinePanel executes Prompt 2 (or 3+): takes an existing PanelSpec and applies a natural-language refinement prompt.
func (e *AIEngine) RefinePanel(ctx context.Context, existing *PanelSpec, refinePrompt string) (*PanelSpec, string, error) {
	vaultCatalog := "None configured."
	var vaultEnv []string
	if e.Vault != nil {
		vaultCatalog = e.Vault.AIPromptCatalog()
		vaultEnv = e.Vault.EnvSlice()
	}

	currentJSON, _ := json.MarshalIndent(existing, "", "  ")
	sysPrompt := fmt.Sprintf(`You are the OmaMac 2-Prompt Status Panel Refiner for macOS.
Here is the current PanelSpec JSON:
%s

AVAILABLE CENTRAL VAULT CREDENTIALS (injected automatically as environment variables into "command" at runtime; reference them as $VAR_NAME):
%s

Apply the user's refinement instruction to this panel:
REFINEMENT PROMPT (Prompt #2): %q

Keep the same "id" (%q). If the user asks to switch between "native" and "html", update "type" and populate "metrics"/"rows" or "html" accordingly.
Return ONLY the updated single JSON object (no markdown commentary outside JSON).`, string(currentJSON), vaultCatalog, refinePrompt, existing.ID)

	rawResp, providerLabel, err := e.Complete(ctx, sysPrompt)
	now := time.Now().Format("15:04:05")
	stepNum := len(existing.PromptHistory) + 1
	if err != nil {
		return nil, providerLabel, e.unreachableErr(providerLabel, err)
	}

	{
		if panels, parseErr := extractPanelsFromText(rawResp); parseErr == nil && len(panels) > 0 {
			updated := panels[0]
			updated.ID = existing.ID
			updated.Enabled = existing.Enabled
			updated.PinnedFloating = existing.PinnedFloating
			if updated.Type == PanelTypeHTML && strings.TrimSpace(updated.HTML) == "" {
				updated.HTML = synthesizeHTMLForPanel(updated)
			}
			updated.PromptHistory = append(existing.PromptHistory, PromptStep{
				Step:      stepNum,
				Prompt:    refinePrompt,
				Provider:  string(e.Config.ActiveProvider),
				Model:     providerLabel,
				Timestamp: now,
			})
			updated.Normalize()
			_ = updated.RunDataRefresh(ctx, vaultEnv...)
			return updated, providerLabel, nil
		}
	}

	return nil, providerLabel, fmt.Errorf("KI-Antwort enthielt kein gültiges Panel-JSON (%s): %s", providerLabel, truncateStr(strings.Join(strings.Fields(rawResp), " "), 160))
}

func extractPanelsFromText(raw string) ([]*PanelSpec, error) {
	trimmed := strings.TrimSpace(raw)
	if idx := strings.Index(trimmed, "```json"); idx >= 0 {
		trimmed = trimmed[idx+7:]
		if end := strings.Index(trimmed, "```"); end >= 0 {
			trimmed = trimmed[:end]
		}
	} else if idx := strings.Index(trimmed, "```"); idx >= 0 {
		trimmed = trimmed[idx+3:]
		if end := strings.Index(trimmed, "```"); end >= 0 {
			trimmed = trimmed[:end]
		}
	}
	trimmed = strings.TrimSpace(trimmed)

	// Try array first
	if start := strings.Index(trimmed, "["); start >= 0 {
		if end := strings.LastIndex(trimmed, "]"); end > start {
			candidate := trimmed[start : end+1]
			var arr []*PanelSpec
			if err := json.Unmarshal([]byte(candidate), &arr); err == nil && len(arr) > 0 {
				return arr, nil
			}
		}
	}
	// Try single object
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			candidate := trimmed[start : end+1]
			var single PanelSpec
			if err := json.Unmarshal([]byte(candidate), &single); err == nil && single.Title != "" {
				return []*PanelSpec{&single}, nil
			}
		}
	}
	return nil, errors.New("could not parse PanelSpec JSON from response")
}

func synthesizeHTMLForPanel(p *PanelSpec) string {
	accent := "#7aa2f7"
	switch p.Status {
	case "crit":
		accent = "#f7768e"
	case "warn":
		accent = "#e0af68"
	case "ok":
		accent = "#9ece6a"
	}
	var rowsHTML strings.Builder
	for _, r := range p.Rows {
		rowsHTML.WriteString(fmt.Sprintf(`<div class="row"><span>%s</span><strong>%s</strong></div>`, r.Left, r.Right))
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8"/>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "JetBrains Mono", monospace; }
  body { background: rgba(20, 22, 36, 0.86); color: #c0caf5; padding: 14px; }
  .hdr { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; border-bottom: 1px solid rgba(255,255,255,0.08); padding-bottom: 8px; }
  .title { font-size: 14px; font-weight: 700; color: #fff; }
  .badge { font-size: 10px; font-weight: 700; padding: 3px 8px; border-radius: 99px; background: %s33; color: %s; border: 1px solid %s66; }
  .card { background: rgba(36, 40, 59, 0.8); border: 1px solid rgba(122,162,247,0.25); border-radius: 10px; padding: 10px; margin-bottom: 10px; }
  .big { font-size: 20px; font-weight: 700; color: %s; margin-top: 4px; }
  .sub { font-size: 11px; color: #7982a9; }
  .row { display: flex; justify-content: space-between; font-size: 12px; padding: 6px 0; border-bottom: 1px solid rgba(255,255,255,0.06); }
  .row:last-child { border-bottom: none; }
</style>
</head>
<body>
  <div class="hdr">
    <div class="title">%s %s</div>
    <span class="badge" id="statusBadge">%s</span>
  </div>
  <div class="card">
    <div class="sub">%s</div>
    <div class="big" id="liveVal">%s</div>
  </div>
  <div class="card">%s</div>
  <script>
    window.onOmaMacUpdate = function(data) {
      if (!data) return;
      if (data.badge_text) document.getElementById('liveVal').textContent = data.badge_text;
      if (data.status) document.getElementById('statusBadge').textContent = data.status.toUpperCase();
    };
  </script>
</body>
</html>`, accent, accent, accent, accent, p.Icon, p.Title, strings.ToUpper(p.Status), p.Subtitle, p.BadgeText, rowsHTML.String())
}

func truncateStr(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
