package omamac

import (
	"context"
	"fmt"
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
	steps    []string
	output   strings.Builder
	errText  string
	canceled bool
	done     bool
	cancel   context.CancelFunc

	pending atomic.Bool // a repaint is scheduled
}

type aiRunView struct {
	title, output, errText string
	steps                  []string
	elapsed                time.Duration
	done, canceled         bool
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
		steps: append([]string(nil), r.steps...), elapsed: end.Sub(r.started),
		done: r.done, canceled: r.canceled,
	}
}

// startAIRun opens the dialog for a new run and returns the context the AI
// calls report to. finish must be called when the run ends.
func (a *App) startAIRun(title string) (context.Context, *aiRun) {
	ctx, cancel := context.WithTimeout(context.Background(), aiRunTimeout)
	r := &aiRun{title: title, started: time.Now(), cancel: cancel}
	ctx = WithAIProgress(ctx, &AIProgress{
		Step: func(s string) {
			r.mu.Lock()
			r.steps = append(r.steps, s)
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

// finish ends the run; on the UI thread. A failed run keeps its dialog open with the output.
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

func (r *aiRun) requestCancel() {
	r.mu.Lock()
	r.canceled = true
	r.mu.Unlock()
	r.cancel()
}

// renderAIRunDialog shows what the AI is doing: steps, its streamed output and the outcome.
func (a *App) renderAIRunDialog(c *ui.Context) {
	r := a.aiRun
	if r == nil {
		return
	}
	v := r.snapshot()
	t := c.Theme()
	ui.Modal(c.Key("ai-run"), &a.showAIRun, func() {
		ui.Column(c).Width(680).Gap(10).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				switch {
				case !v.done:
					ui.Spinner(c).Size(16, 16)
				case v.errText != "":
					ui.Text(c, "⚠️").FontSize(15)
				default:
					ui.Text(c, "✓").FontSize(15).Bold().TextColor(t.Success)
				}
				ui.Text(c, v.title).FontSize(16).Bold().Grow(1).MinWidth(0).SingleLine()
				ui.Text(c, fmt.Sprintf("%d s", int(v.elapsed.Seconds()))).FontSize(12).TextColor(t.TextMuted)
			})

			ui.Column(c).FillWidth().Gap(3).Children(func() {
				for i, s := range v.steps {
					ui.Text(c.Key(fmt.Sprintf("ai-step-%d", i)), "• "+s).FontSize(11.5).TextColor(t.TextMuted).FillWidth()
				}
			})

			box := ui.Scroll(c.Key("ai-run-out")).FillWidth().Height(300).Radius(8).Border(1, t.Border).Background(t.Background)
			box.Children(func() {
				ui.Column(c).FillWidth().Padding(10, 12).Children(func() {
					switch {
					case v.output != "":
						ui.Text(c, v.output).Font("monospace").FontSize(11).FillWidth().Selectable()
					case !v.done:
						ui.Text(c, "Warte auf die erste Ausgabe der KI …").FontSize(11.5).TextColor(t.TextMuted)
					default:
						ui.Text(c, "Keine Ausgabe.").FontSize(11.5).TextColor(t.TextMuted)
					}
					end := ui.Column(c.Key("ai-run-end")).Height(1).FillWidth()
					if len(v.output) != a.aiRunSeen {
						a.aiRunSeen = len(v.output)
						end.ScrollIntoView() // follow the output as it grows
					}
				})
			})

			if v.errText != "" {
				ui.Text(c, v.errText).FontSize(12).TextColor(t.Danger).FillWidth()
			}

			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				if v.output != "" && ui.Button(c.Key("ai-run-copy"), "Ausgabe kopieren").Clicked() {
					c.WriteClipboard(v.output)
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
				} else if ui.PrimaryButton(c.Key("ai-run-close"), "Schließen").Clicked() {
					a.showAIRun = false
				}
			})
		})
	})
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
