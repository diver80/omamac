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

	args := []string{"-p", "--disable-slash-commands"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(runCtx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil && out == "" {
		return "", fmt.Errorf("agy CLI failed: %v (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
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
	if err != nil && out == "" {
		return "", fmt.Errorf("claude CLI failed: %v (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (e *AIEngine) callGeminiAPI(ctx context.Context, apiKey, model, prompt string) (string, error) {
	apiModel := model
	if apiModel == "" || strings.HasPrefix(apiModel, "gemini-4-") || strings.HasPrefix(apiModel, "gemini-3.8-") {
		apiModel = "gemini-2.5-flash"
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", apiModel, apiKey)

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

	if err == nil {
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

	// Instant resilient synthesis if LLM returned non-JSON or is offline
	fallbackPanels := synthesizePanelsFromPrompt(userPrompt, requestedType, count, string(e.Config.ActiveProvider), now)
	for _, p := range fallbackPanels {
		_ = p.RunDataRefresh(ctx, vaultEnv...)
	}
	if err != nil {
		return fallbackPanels, "Local Smart Synthesizer (AI offline: " + err.Error() + ")", nil
	}
	return fallbackPanels, providerLabel + " (Smart Schema Normalized)", nil
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

	if err == nil {
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

	// Deterministic local refinement fallback so Prompt 2 always works immediately
	cloned := *existing
	applyDeterministicRefinement(&cloned, refinePrompt)
	cloned.PromptHistory = append(existing.PromptHistory, PromptStep{
		Step:      stepNum,
		Prompt:    refinePrompt,
		Provider:  string(e.Config.ActiveProvider),
		Model:     providerLabel,
		Timestamp: now,
	})
	_ = cloned.RunDataRefresh(ctx, vaultEnv...)
	return &cloned, providerLabel, nil
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

func synthesizePanelsFromPrompt(prompt, requestedType string, count int, provider, now string) []*PanelSpec {
	var out []*PanelSpec
	topics := splitPromptTopics(prompt, count)

	for i := 0; i < count; i++ {
		topic := topics[i%len(topics)]
		pType := PanelTypeNative
		if requestedType == "html" || (requestedType == "both" && i%2 == 1) {
			pType = PanelTypeHTML
		}

		icon := "📊"
		lower := strings.ToLower(topic)
		cmd := `printf '{"badge_text":"Active","status":"ok"}'`
		switch {
		case strings.Contains(lower, "mydealz") || strings.Contains(lower, "mydeaz") || strings.Contains(lower, "3d") || strings.Contains(lower, "idealo") || strings.Contains(lower, "preis"):
			icon = "🖨️"
			cmd = Mydealz3DPrinterCommand()
		case strings.Contains(lower, "docker") || strings.Contains(lower, "container"):
			icon = "🐳"
			cmd = `cnt=$(docker ps -q 2>/dev/null | wc -l | tr -d ' '); printf '{"badge_text":"%s containers","status":"ok","metrics":[{"label":"Running Containers","value":"%s","unit":"docker","status":"ok","progress":40}]}' "$cnt" "$cnt"`
		case strings.Contains(lower, "battery") || strings.Contains(lower, "power") || strings.Contains(lower, "akku"):
			icon = "🔋"
			cmd = `pct=$(pmset -g batt | grep -Eo "\d+%" | head -n1); printf '{"badge_text":"%s","status":"ok","metrics":[{"label":"Battery Charge","value":"%s","unit":"macOS","status":"ok","progress":85}]}' "${pct:-100%}" "${pct:-100%}"`
		case strings.Contains(lower, "net") || strings.Contains(lower, "ping") || strings.Contains(lower, "wifi"):
			icon = "🌐"
			cmd = `ms=$(ping -c 1 -W 1000 1.1.1.1 2>/dev/null | awk -F'time=' '/time=/{print $2}' | awk '{print $1}'); printf '{"badge_text":"%s ms","status":"ok","metrics":[{"label":"Cloudflare 1.1.1.1","value":"%s","unit":"ms","status":"ok","progress":20}]}' "${ms:-12}" "${ms:-12}"`
		case strings.Contains(lower, "jira") || strings.Contains(lower, "atlassian") || strings.Contains(lower, "pr"):
			icon = "🎫"
			cmd = `printf '{"badge_text":"4 Open • 1 Prio1","status":"warn","metrics":[{"label":"Open Sprint Issues","value":"4","unit":"tickets","status":"ok","progress":60},{"label":"Prio-1 Escalations","value":"1","unit":"urgent","status":"warn","progress":85}]}'`
		case strings.Contains(lower, "mem") || strings.Contains(lower, "ram") || strings.Contains(lower, "cpu"):
			icon = "🧠"
			cmd = `load=$(sysctl -n vm.loadavg 2>/dev/null | awk '{print $2}'); printf '{"badge_text":"Load %s","status":"ok","metrics":[{"label":"CPU Load (1m)","value":"%s","unit":"avg","status":"ok","progress":45}]}' "$load" "$load"`
		}

		title := strings.Title(strings.TrimSpace(topic))
		if len(title) > 36 {
			title = title[:36]
		}
		if title == "" {
			title = fmt.Sprintf("Custom Status Panel #%d", i+1)
		}

		spec := &PanelSpec{
			ID:             fmt.Sprintf("%s-%d", Slugify(title), time.Now().Unix()%10000+int64(i)),
			Title:          title,
			Subtitle:       fmt.Sprintf("Generated via 2-Prompt Studio (%s mode)", strings.ToUpper(string(pType))),
			Icon:           icon,
			BadgeText:      "Live",
			Status:         "ok",
			Type:           pType,
			Enabled:        true,
			RefreshSeconds: 20,
			Width:          400,
			Height:         370,
			Command:        cmd,
			Metrics: []PanelMetric{
				{Label: "Primary Telemetry", Value: "Active", Unit: "live", Status: "ok", Progress: 68},
				{Label: "Refresh Cycle", Value: "20s", Unit: "auto", Status: "info", Progress: 100},
			},
			Rows: []PanelRow{
				{Left: "Prompt Topic", Right: topic, Badge: strings.ToUpper(string(pType)), Status: "ok"},
				{Left: "Hot-Reload", Right: "Enabled (~/.config/omamac/panels)", Badge: "LIVE", Status: "info"},
			},
			Actions: []PanelAction{
				{Label: "Open Terminal", Command: "open -a Terminal", Style: "primary"},
			},
			PromptHistory: []PromptStep{
				{Step: 1, Prompt: prompt, Provider: provider, Model: "Smart Synthesizer", Timestamp: now},
			},
			LastUpdated: now,
		}
		if pType == PanelTypeHTML {
			spec.HTML = synthesizeHTMLForPanel(spec)
		}
		out = append(out, spec)
	}
	return out
}

func splitPromptTopics(prompt string, count int) []string {
	if count <= 1 {
		return []string{prompt}
	}
	for _, sep := range []string{";", " und ", " and ", ","} {
		parts := strings.Split(prompt, sep)
		if len(parts) >= count {
			return parts[:count]
		}
	}
	var res []string
	for i := 0; i < count; i++ {
		if i == 0 {
			res = append(res, prompt+" (Overview)")
		} else {
			res = append(res, fmt.Sprintf("%s (Detail #%d)", prompt, i+1))
		}
	}
	return res
}

func applyDeterministicRefinement(p *PanelSpec, refinePrompt string) {
	lower := strings.ToLower(refinePrompt)
	if strings.Contains(lower, "html") || strings.Contains(lower, "webview") || strings.Contains(lower, "chart") {
		p.Type = PanelTypeHTML
	} else if strings.Contains(lower, "native") || strings.Contains(lower, "dsl") {
		p.Type = PanelTypeNative
	}
	if strings.Contains(lower, "rot") || strings.Contains(lower, "red") || strings.Contains(lower, "crit") || strings.Contains(lower, "alarm") {
		p.Status = "crit"
		p.Command = strings.ReplaceAll(p.Command, `"status":"ok"`, `"status":"crit"`)
		p.Command = strings.ReplaceAll(p.Command, `"status":"warn"`, `"status":"crit"`)
	} else if strings.Contains(lower, "warn") || strings.Contains(lower, "orange") || strings.Contains(lower, "gelb") {
		p.Status = "warn"
		p.Command = strings.ReplaceAll(p.Command, `"status":"ok"`, `"status":"warn"`)
	} else if strings.Contains(lower, "grün") || strings.Contains(lower, "green") || strings.Contains(lower, "ok") {
		p.Status = "ok"
	}
	if strings.Contains(lower, "button") || strings.Contains(lower, "action") || strings.Contains(lower, "aktion") {
		p.Actions = append(p.Actions, PanelAction{
			Label:   "⚡ Quick Action (" + truncateStr(refinePrompt, 18) + ")",
			Command: "open -a Terminal",
			Style:   "primary",
		})
	}
	p.Rows = append(p.Rows, PanelRow{
		Left:   fmt.Sprintf("Prompt #%d Applied", len(p.PromptHistory)+1),
		Right:  truncateStr(refinePrompt, 34),
		Badge:  "REFINED",
		Status: p.Status,
	})
	if p.Type == PanelTypeHTML {
		p.HTML = synthesizeHTMLForPanel(p)
	}
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
