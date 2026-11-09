package omamac

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // DecodeConfig for attached GIFs
	_ "image/jpeg" // DecodeConfig for attached JPEGs
	_ "image/png"  // DecodeConfig for attached PNGs
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Progress: what the AI is doing right now, shown live in the "KI arbeitet" dialog.

// AIProgress receives progress of one AI run: Step for milestones
// ("Anfrage gesendet"), Output for text the AI streams back. Either may be nil.
type AIProgress struct {
	Step   func(string)
	Output func(string)
}

type aiProgressKey struct{}

// WithAIProgress attaches p to ctx; AIEngine calls made with it report to p.
func WithAIProgress(ctx context.Context, p *AIProgress) context.Context {
	return context.WithValue(ctx, aiProgressKey{}, p)
}

func progressStep(ctx context.Context, format string, args ...any) {
	if p, _ := ctx.Value(aiProgressKey{}).(*AIProgress); p != nil && p.Step != nil {
		p.Step(fmt.Sprintf(format, args...))
	}
}

func progressOutput(ctx context.Context, text string) {
	if p, _ := ctx.Value(aiProgressKey{}).(*AIProgress); p != nil && p.Output != nil && text != "" {
		p.Output(text)
	}
}

// ---------------------------------------------------------------------------
// Images attached to a prompt.

// MaxAIImages bounds the images sent with one prompt.
const MaxAIImages = 5

const (
	maxImageSide  = 1568            // larger images cost tokens without helping the model
	maxImageBytes = 3_500_000       // stays under the providers' per-image limits once base64-encoded
	imageDirName  = "omamac-images" // in os.TempDir(): pasted and converted images
)

// AIImage is an image attached to a prompt, ready to send.
type AIImage struct {
	Path      string // the file sent (a converted copy for HEIC, huge or oversized images)
	MediaType string // image/png, image/jpeg or image/gif
	Data      []byte
}

// IsImageFile reports whether path has an image extension OmaMac accepts.
func IsImageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".heic", ".heif", ".webp", ".tif", ".tiff", ".bmp":
		return true
	}
	return false
}

// imageTempDir is where pasted and converted images go.
func imageTempDir() (string, error) {
	dir := filepath.Join(os.TempDir(), imageDirName)
	return dir, os.MkdirAll(dir, 0o700)
}

// SavePastedImage writes PNG bytes from the clipboard to a temp file and returns its path.
func SavePastedImage(png []byte) (string, error) {
	dir, err := imageTempDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("eingefuegt-%d.png", time.Now().UnixNano()))
	return path, os.WriteFile(path, png, 0o600)
}

// LoadAIImage reads an image for sending. PNG, JPEG and GIF within the size
// limits go as they are; anything else (HEIC, WebP, TIFF, large screenshots)
// is converted to a JPEG of at most maxImageSide pixels with macOS's sips.
func LoadAIImage(path string) (AIImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AIImage{}, err
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		switch format {
		case "png", "jpeg", "gif":
			if cfg.Width <= maxImageSide && cfg.Height <= maxImageSide && len(data) <= maxImageBytes {
				return AIImage{Path: path, MediaType: "image/" + format, Data: data}, nil
			}
		}
	}
	dir, err := imageTempDir()
	if err != nil {
		return AIImage{}, err
	}
	out := filepath.Join(dir, fmt.Sprintf("%s-%d.jpg", strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), time.Now().UnixNano()))
	cmd := exec.Command("/usr/bin/sips", "-s", "format", "jpeg", "-s", "formatOptions", "82",
		"-Z", fmt.Sprint(maxImageSide), path, "--out", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return AIImage{}, fmt.Errorf("Bild %s lässt sich nicht umwandeln: %v %s", filepath.Base(path), err, strings.TrimSpace(string(msg)))
	}
	data, err = os.ReadFile(out)
	if err != nil {
		return AIImage{}, err
	}
	return AIImage{Path: out, MediaType: "image/jpeg", Data: data}, nil
}

// LoadAIImages loads paths in order, reporting each as a progress step.
func LoadAIImages(ctx context.Context, paths []string) ([]AIImage, error) {
	var imgs []AIImage
	for _, p := range paths {
		img, err := LoadAIImage(p)
		if err != nil {
			return nil, err
		}
		progressStep(ctx, "Bild %s vorbereitet (%d KB)", filepath.Base(p), len(img.Data)/1024)
		imgs = append(imgs, img)
	}
	return imgs, nil
}

func (img AIImage) base64() string { return base64.StdEncoding.EncodeToString(img.Data) }

// imagePromptNote tells the model what the attached images are for.
func imagePromptNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("\n\nThe user attached %d image(s) as reference — for example a screenshot of the data source, a mockup of the desired look, or an error to deal with. Use what they show for the panel's content, layout and styling.", n)
}

// ---------------------------------------------------------------------------
// CLI runs with live output.

// runCLI runs an AI CLI in a neutral directory (so no project CLAUDE.md or
// settings get picked up), feeds it stdin, hands each stdout line to onLine as
// it comes and returns the whole stdout.
func runCLI(ctx context.Context, label, bin string, args, extraEnv []string, stdin []byte, onLine func(string)) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Dir = os.TempDir()
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.WaitDelay = 2 * time.Second // don't hang on a grandchild that keeps stdout open
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	var out strings.Builder
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 64*1024), 32<<20)
	for sc.Scan() {
		line := sc.Text()
		out.WriteString(line)
		out.WriteByte('\n')
		if onLine != nil {
			onLine(line)
		}
	}
	if sc.Err() != nil {
		_, _ = io.Copy(io.Discard, pipe)
	}
	err = cmd.Wait()
	stdout := strings.TrimSpace(out.String())
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return "", fmt.Errorf("%s: abgebrochen: %w", label, context.Canceled)
		case runCtx.Err() != nil:
			return "", fmt.Errorf("%s: keine Antwort nach %d s (Zeitüberschreitung)", label, int(time.Since(start).Seconds()))
		}
		// CLIs print quota/login errors ("Usage Limit Reached") to stdout and exit non-zero.
		return "", fmt.Errorf("%s: %v: %s", label, err, cliErrorDetail(stdout, stderr.String()))
	}
	return stdout, nil
}

// claudeStreamInput is the one user message sent to "claude -p --input-format stream-json".
func claudeStreamInput(prompt string, images []AIImage) []byte {
	content := []map[string]any{{"type": "text", "text": prompt}}
	for _, img := range images {
		content = append(content, map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": img.MediaType, "data": img.base64()},
		})
	}
	msg, _ := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": content},
	})
	return append(msg, '\n')
}

// claudeStreamEvent is the part of a "claude --output-format stream-json" line OmaMac reads.
type claudeStreamEvent struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Event   *struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	Message *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// claudeStreamCollector turns stream-json lines into live output and the final answer.
type claudeStreamCollector struct {
	ctx       context.Context
	deltas    strings.Builder
	assistant strings.Builder
	raw       strings.Builder // lines that are not JSON (older CLIs, wrappers)
	result    *claudeStreamEvent
}

func (c *claudeStreamCollector) line(line string) {
	var ev claudeStreamEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type == "" {
		c.raw.WriteString(line)
		c.raw.WriteByte('\n')
		progressOutput(c.ctx, line+"\n")
		return
	}
	switch ev.Type {
	case "stream_event":
		if ev.Event != nil && ev.Event.Type == "content_block_delta" && ev.Event.Delta.Type == "text_delta" {
			c.deltas.WriteString(ev.Event.Delta.Text)
			progressOutput(c.ctx, ev.Event.Delta.Text)
		}
	case "assistant":
		if ev.Message != nil {
			for _, part := range ev.Message.Content {
				if part.Type == "text" {
					c.assistant.WriteString(part.Text)
				}
			}
		}
	case "result":
		c.result = &ev
	}
}

// answer is the final text: the result event's, else what streamed, else raw stdout.
func (c *claudeStreamCollector) answer() (string, error) {
	if c.result != nil {
		if c.result.IsError {
			return "", errors.New(cliErrorDetail(c.result.Result, ""))
		}
		return strings.TrimSpace(c.result.Result), nil
	}
	for _, s := range []string{c.assistant.String(), c.deltas.String(), c.raw.String()} {
		if s = strings.TrimSpace(s); s != "" {
			return s, nil
		}
	}
	return "", errors.New("keine Ausgabe")
}
