package omamac

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// aiRunTimeout bounds one create or refine run, image preparation and first data refresh included.
const aiRunTimeout = 3 * time.Minute

// aiRunMaxOutput is how much of the AI's output the dialog keeps (the end of it).
const aiRunMaxOutput = 60_000

// aiRun is one AI create or refine run as the "KI arbeitet" dialog shows it.
// The run's goroutine writes it, the UI reads it: mu guards the fields.
type aiRun struct {
	mu       sync.Mutex
	title    string
	started  time.Time
	ended    time.Time
	steps    []aiStep
	output   strings.Builder
	errText  string
	canceled bool
	done     bool
	result   *aiRunResult
	cancel   context.CancelFunc

	pending atomic.Bool // a repaint is scheduled
}

type aiStep struct {
	at   time.Duration // since the run started
	text string
}

// aiRunResult is what a successful run produced, prepared for display.
type aiRunResult struct {
	verb   string // "erstellt" or "angepasst"
	panels []aiResultPanel
}

type aiResultPanel struct {
	icon, title, subtitle, kind string
	refresh                     int
	command                     string
	actions                     [][2]string // label, command or URL
	metrics, rows, htmlBytes    int
	badge, status               string
	firstOutput                 string // what the command printed on its first run
	checkErr                    string
}

func summarizePanel(p *PanelSpec, checkErr error) aiResultPanel {
	r := aiResultPanel{
		icon: p.Icon, title: p.Title, subtitle: p.Subtitle, kind: "Native",
		refresh: p.RefreshSeconds, command: strings.TrimSpace(p.Command),
		metrics: len(p.Metrics), rows: len(p.Rows), htmlBytes: len(p.HTML),
		badge: p.BadgeText, status: p.Status,
		firstOutput: truncateStr(strings.TrimSpace(p.LastCommandOut), 600),
	}
	if p.Type == PanelTypeHTML {
		r.kind = "HTML"
	}
	for _, a := range p.Actions {
		target := a.Command
		if target == "" {
			target = a.URL
		}
		r.actions = append(r.actions, [2]string{a.Label, target})
	}
	if checkErr != nil {
		r.checkErr = checkErr.Error()
	}
	return r
}

type aiRunView struct {
	title, output, errText string
	steps                  []aiStep
	elapsed                time.Duration
	done, canceled         bool
	result                 *aiRunResult
}

func (r *aiRun) snapshot() aiRunView {
	r.mu.Lock()
	defer r.mu.Unlock()
	end := r.ended
	if !r.done {
		end = time.Now()
	}
	return aiRunView{
		title: r.title, output: r.output.String(), errText: r.errText,
		steps: append([]aiStep(nil), r.steps...), elapsed: end.Sub(r.started),
		done: r.done, canceled: r.canceled, result: r.result,
	}
}

// progress estimates how far the run is, 0..1, from its steps, the streamed
// output and the time: the AI's answer length is unknown up front.
func (v aiRunView) progress() float64 {
	if v.done {
		return 1
	}
	p := 0.04
	sent, answered, checking := false, false, false
	for _, s := range v.steps {
		switch {
		case strings.HasPrefix(s.text, "Anfrage an"):
			sent = true
		case strings.HasPrefix(s.text, "Antwort erhalten"):
			answered = true
		case strings.HasPrefix(s.text, "Führe"), strings.HasPrefix(s.text, "Prüfe"):
			checking = true
		}
	}
	switch {
	case checking:
		p = 0.92
	case answered:
		p = 0.85
	case sent && v.output != "":
		p = 0.15 + 0.68*(1-math.Exp(-float64(len(v.output))/2500))
	case sent:
		p = 0.1 + 0.6*(1-math.Exp(-v.elapsed.Seconds()/40))
	}
	return p
}

// startAIRun opens the dialog for a new run and returns the context the AI
// calls report to. finish must be called when the run ends.
func (a *App) startAIRun(title string) (context.Context, *aiRun) {
	ctx, cancel := context.WithTimeout(context.Background(), aiRunTimeout)
	r := &aiRun{title: title, started: time.Now(), cancel: cancel}
	ctx = WithAIProgress(ctx, &AIProgress{
		Step: func(s string) {
			r.mu.Lock()
			r.steps = append(r.steps, aiStep{at: time.Since(r.started), text: s})
			r.mu.Unlock()
			a.repaintAIRun(r)
		},
		Output: func(s string) {
			r.mu.Lock()
			r.output.WriteString(s)
			if r.output.Len() > aiRunMaxOutput*2 {
				tail := r.output.String()[r.output.Len()-aiRunMaxOutput:]
				r.output.Reset()
				r.output.WriteString("…" + tail)
			}
			r.mu.Unlock()
			a.repaintAIRun(r)
		},
	})
	a.aiRun = r
	a.aiRunSeen = 0
	a.aiRunShowRaw = false
	a.aiRunExpanded = false
	a.showAIRun = true
	// the elapsed time ticks while the AI is silent
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			r.mu.Lock()
			done := r.done
			r.mu.Unlock()
			if done {
				return
			}
			a.repaintAIRun(r)
		}
	}()
	return ctx, r
}

// repaintAIRun schedules one repaint for a burst of progress (streamed text comes in many small pieces).
func (a *App) repaintAIRun(r *aiRun) {
	if !r.pending.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(80 * time.Millisecond)
		r.pending.Store(false)
		a.updateUI(func() {})
	}()
}

// finish ends the run; on the UI thread. The dialog stays open until OK.
func (r *aiRun) finish(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
	r.ended = time.Now()
	r.cancel()
	if err != nil {
		if r.canceled {
			r.errText = "Abgebrochen."
		} else {
			r.errText = errText(err)
		}
	}
}

// setResult records what a successful run produced; checks[i] is panels[i]'s check error.
func (r *aiRun) setResult(verb string, panels []*PanelSpec, checks []error) {
	res := &aiRunResult{verb: verb}
	for i, p := range panels {
		var err error
		if i < len(checks) {
			err = checks[i]
		}
		res.panels = append(res.panels, summarizePanel(p, err))
	}
	r.mu.Lock()
	r.result = res
	r.mu.Unlock()
}

func (r *aiRun) requestCancel() {
	r.mu.Lock()
	r.canceled = true
	r.mu.Unlock()
	r.cancel()
}

// prettyAIOutput indents the JSON in an AI answer (inside ``` fences or plain) for reading.
func prettyAIOutput(raw string) string {
	s := strings.TrimSpace(raw)
	i := strings.IndexAny(s, "[{")
	j := strings.LastIndexAny(s, "]}")
	if i < 0 || j <= i {
		return raw
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s[i:j+1]), "", "  "); err != nil {
		return raw
	}
	return buf.String()
}

// renderAIRunDialog shows what the AI is doing. "Protokoll" shows its steps
// and streamed answer, "Nur Fortschritt" a progress bar; both end with the
// formatted result and stay open until OK.
func (a *App) renderAIRunDialog(c *ui.Context) {
	r := a.aiRun
	if r == nil {
		return
	}
	v := r.snapshot()
	t := c.Theme()
	compact := a.Config.AIRunView == AIRunViewProgress && !a.aiRunExpanded
	ui.Modal(c.Key("ai-run"), &a.showAIRun, func() {
		ui.Column(c).Width(720).Gap(12).Children(func() {
			a.renderAIRunHeader(c, v)

			body := ui.Scroll(c.Key("ai-run-body")).FillWidth().MaxHeight(540)
			body.Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					if compact {
						a.renderAIRunProgress(c, v)
					} else {
						a.renderAIRunSteps(c, v)
					}
					if v.errText != "" {
						ui.Row(c).FillWidth().Padding(10, 12).Radius(8).Background(t.Danger.Alpha(0.10)).Children(func() {
							ui.Text(c, v.errText).FontSize(12).TextColor(t.Danger).FillWidth().Selectable()
						})
					}
					if v.result != nil {
						for i, p := range v.result.panels {
							a.renderAIResultPanel(c, fmt.Sprintf("ai-res-%d", i), p)
						}
					}
					if !compact {
						a.renderAIRunOutput(c, v)
					}
					end := ui.Column(c.Key("ai-run-end")).Height(1).FillWidth()
					if !v.done && len(v.output) != a.aiRunSeen {
						a.aiRunSeen = len(v.output)
						end.ScrollIntoView() // follow the answer as it streams in
					}
				})
			})

			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if compact && textButton(c, "ai-run-expand", "Protokoll anzeigen") {
					a.aiRunExpanded = true
				}
				if !compact && v.output != "" && ui.Button(c.Key("ai-run-copy"), "Ausgabe kopieren").Tooltip("Als eingerücktes JSON, wenn die Antwort gültiges JSON ist").Clicked() {
					c.WriteClipboard(prettyAIOutput(v.output))
					a.toast("Ausgabe kopiert")
				}
				ui.Spacer(c)
				if !v.done {
					if ui.Button(c.Key("ai-run-hide"), "Im Hintergrund").Tooltip("Die KI arbeitet weiter; oben rechts wieder öffnen").Clicked() {
						a.showAIRun = false
					}
					if ui.Button(c.Key("ai-run-cancel"), "Abbrechen").Clicked() {
						r.requestCancel()
					}
				} else if ok := ui.PrimaryButton(c.Key("ai-run-ok"), "OK"); ok.Clicked() || ok.Shortcut(0, ui.KeyEnter) {
					a.showAIRun = false
				}
			})
		})
	})
}

func (a *App) renderAIRunHeader(c *ui.Context, v aiRunView) {
	t := c.Theme()
	title := v.title
	if v.done && v.errText == "" && v.result != nil {
		n := len(v.result.panels)
		if n == 1 {
			title = fmt.Sprintf("„%s“ %s", v.result.panels[0].title, v.result.verb)
		} else {
			title = fmt.Sprintf("%d Panels %s", n, v.result.verb)
		}
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		switch {
		case !v.done:
			ui.Spinner(c).Size(18, 18)
		case v.errText != "":
			ui.Text(c, "⚠️").FontSize(17)
		default:
			ui.Text(c, "✓").FontSize(18).Bold().TextColor(t.Success)
		}
		ui.Text(c, title).FontSize(16).Bold().Grow(1).MinWidth(0).SingleLine()
		ui.Text(c, formatSeconds(v.elapsed)).FontSize(12).TextColor(t.TextMuted)
	})
}

// renderAIRunProgress is the compact view: a bar and the current step.
func (a *App) renderAIRunProgress(c *ui.Context, v aiRunView) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Progress(c, v.progress()).FillWidth()
		current := "Starte …"
		if n := len(v.steps); n > 0 {
			current = v.steps[n-1].text
		}
		if v.done {
			current = "Fertig."
			if v.errText != "" {
				current = "Fehlgeschlagen."
			}
		}
		ui.Text(c, current).FontSize(12).TextColor(t.TextMuted).FillWidth().SingleLine()
	})
}

// renderAIRunSteps lists the steps with their time; the last one spins while the run lasts.
func (a *App) renderAIRunSteps(c *ui.Context, v aiRunView) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		for i, s := range v.steps {
			last := i == len(v.steps)-1
			ui.Row(c.Key(fmt.Sprintf("ai-step-%d", i))).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Width(16).Justify(ui.Center).Children(func() {
					if last && !v.done {
						ui.Spinner(c).Size(12, 12)
					} else {
						ui.Text(c, "✓").FontSize(11.5).Bold().TextColor(t.Success)
					}
				})
				ui.Text(c, s.text).FontSize(12).Grow(1).MinWidth(0)
				ui.Text(c, "+"+formatSeconds(s.at)).FontSize(11).TextColor(t.TextMuted).Font("monospace")
			})
		}
	})
}

// renderAIRunOutput shows the AI's answer: live while it streams, then
// indented and folded away below the formatted result.
func (a *App) renderAIRunOutput(c *ui.Context, v aiRunView) {
	t := c.Theme()
	code := func(text string) {
		ui.Column(c).FillWidth().Padding(10, 12).Radius(8).Border(1, t.Border).Background(t.Background).Children(func() {
			ui.Text(c, text).Font("monospace").FontSize(11).FillWidth().Selectable()
		})
	}
	switch {
	case !v.done && v.output == "":
		ui.Text(c, "Warte auf die erste Ausgabe der KI …").FontSize(12).TextColor(t.TextMuted)
	case !v.done:
		ui.Text(c, "Antwort der KI (live)").FontSize(11.5).Bold().TextColor(t.TextMuted)
		code(formatJSONStream(v.output, true))
	case v.output == "":
	case v.result != nil:
		ui.Collapsible(c, "Antwort der KI", &a.aiRunShowRaw, func() { code(formatJSONStream(v.output, true)) })
	default:
		ui.Text(c, "Antwort der KI").FontSize(11.5).Bold().TextColor(t.TextMuted)
		code(formatJSONStream(v.output, true))
	}
}

// renderAIResultPanel shows one created or changed panel: what it shows, its command and its first run.
func (a *App) renderAIResultPanel(c *ui.Context, key string, p aiResultPanel) {
	t := c.Theme()
	label := func(s string) { ui.Text(c, s).FontSize(11).Bold().TextColor(t.TextMuted) }
	code := func(text string) {
		ui.Column(c).FillWidth().Padding(8, 10).Radius(6).Background(t.Background).Children(func() {
			ui.Text(c, text).Font("monospace").FontSize(11).FillWidth().Selectable()
		})
	}
	ui.Column(c.Key(key)).FillWidth().Padding(12, 14).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, p.icon).FontSize(18)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, p.title).FontSize(14).Bold().SingleLine()
				if p.subtitle != "" {
					ui.Text(c, p.subtitle).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
				}
			})
			pillBadge(c, p.kind, t.Accent.Alpha(0.15), t.Accent)
		})

		var facts []string
		if p.refresh > 0 {
			facts = append(facts, fmt.Sprintf("aktualisiert alle %d s", p.refresh))
		}
		if p.kind == "HTML" {
			facts = append(facts, fmt.Sprintf("HTML %d KB", (p.htmlBytes+1023)/1024))
		} else {
			facts = append(facts, fmt.Sprintf("%d Kennzahlen · %d Zeilen", p.metrics, p.rows))
		}
		if n := len(p.actions); n > 0 {
			facts = append(facts, fmt.Sprintf("%d Aktionen", n))
		}
		ui.Text(c, strings.Join(facts, " · ")).FontSize(11.5).TextColor(t.TextMuted)

		if p.command != "" {
			label("BEFEHL")
			code(p.command)
			label("ERSTER ABRUF")
			bg, fg := statusColors(t, p.status)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				switch {
				case p.checkErr != "":
					ui.Text(c, "⚠ "+p.checkErr).FontSize(12).TextColor(t.Danger).Grow(1).MinWidth(0)
				case p.badge != "":
					pillBadge(c, p.badge, bg, fg)
				}
			})
			if p.firstOutput != "" {
				code(formatJSONStream(p.firstOutput, true))
			}
		}
		if len(p.actions) > 0 {
			label("AKTIONEN")
			for i, act := range p.actions {
				ui.Row(c.Key(fmt.Sprintf("%s-act-%d", key, i))).FillWidth().Gap(8).Children(func() {
					ui.Text(c, act[0]).FontSize(12).Width(170).SingleLine()
					ui.Text(c, act[1]).Font("monospace").FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
				})
			}
		}
	})
}

// formatSeconds prints a duration as "7 s" or "1:05 min".
func formatSeconds(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%d s", s)
	}
	return fmt.Sprintf("%d:%02d min", s/60, s%60)
}

// ---------------------------------------------------------------------------
// Image attachments for the create dialog and the refine box.

// renderImageAttachments shows the attached images with a remove button each,
// and a button to add more. Dropping and pasting are wired by the caller.
func (a *App) renderImageAttachments(c *ui.Context, key string, imgs *[]string) {
	t := c.Theme()
	ui.Row(c.Key(key)).FillWidth().Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		for i, path := range *imgs {
			ui.Column(c.Key(fmt.Sprintf("%s-img-%d", key, i))).Gap(2).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Size(72, 54).Radius(6).Border(1, t.Border).Background(t.Background).
					AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					if src := a.thumbnail(path); src != nil {
						ui.Image(c, src).Size(70, 52)
					} else {
						ui.Text(c, "🖼").FontSize(20)
					}
				})
				ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
					ui.Text(c, truncateStr(filepath.Base(path), 12)).FontSize(10).TextColor(t.TextMuted).SingleLine()
					idx := i
					if ui.Button(c.Key(fmt.Sprintf("%s-rm-%d", key, i)), "✕").Tooltip("Bild entfernen").Clicked() {
						*imgs = append((*imgs)[:idx:idx], (*imgs)[idx+1:]...)
					}
				})
			})
		}
		if len(*imgs) < MaxAIImages {
			if ui.Button(c.Key(key+"-add"), "📎 Bild …").Tooltip("Screenshot, Mockup oder Beispiel für die KI – auch per Drag & Drop oder ⌘V").Clicked() {
				a.pickImages(imgs)
			}
			if len(*imgs) == 0 {
				ui.Text(c, "Bilder auch hineinziehen oder mit ⌘V einfügen").FontSize(11).TextColor(t.TextMuted)
			}
		}
	})
}

// thumbnail returns a decoded preview of path, cached; nil for formats the UI can't draw (HEIC).
func (a *App) thumbnail(path string) ui.ImageSource {
	if a.thumbs == nil {
		a.thumbs = map[string]ui.ImageSource{}
	}
	if src, ok := a.thumbs[path]; ok {
		return src
	}
	var src ui.ImageSource
	if img, err := LoadAIImage(path); err == nil {
		src, _ = ui.DecodeImage(img.Data)
	}
	a.thumbs[path] = src
	return src
}

// addImages appends image paths to imgs, skipping duplicates, other files and those over the limit.
func (a *App) addImages(imgs *[]string, paths []string) {
	skipped := 0
	for _, p := range paths {
		dup := false
		for _, have := range *imgs {
			dup = dup || have == p
		}
		switch {
		case dup:
		case !IsImageFile(p) || len(*imgs) >= MaxAIImages:
			skipped++
		default:
			*imgs = append(*imgs, p)
		}
	}
	if skipped > 0 {
		a.toast(fmt.Sprintf("%d Datei(en) übersprungen – nur Bilder, höchstens %d", skipped, MaxAIImages))
	}
}

// pickImages asks for image files; the dialog blocks, so it runs off the UI thread.
func (a *App) pickImages(imgs *[]string) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   a.studioWin,
			Title:    "Bilder für die KI",
			Multiple: true,
			Filters:  []mygo.FileFilter{{Name: "Bilder", Extensions: []string{"png", "jpg", "jpeg", "gif", "heic", "heif", "webp", "tiff", "bmp"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.updateUI(func() { a.addImages(imgs, paths) })
	}()
}

// pasteImage takes an image from the clipboard (⌘V in a prompt field holding no text).
func (a *App) pasteImage(imgs *[]string) bool {
	data := mygo.Clipboard.ReadImage()
	if len(data) == 0 {
		return false
	}
	path, err := SavePastedImage(data)
	if err != nil {
		a.toast("⚠️ Bild nicht eingefügt: " + err.Error())
		return true
	}
	a.addImages(imgs, []string{path})
	return true
}
