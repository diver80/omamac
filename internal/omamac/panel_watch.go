package omamac

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// Coding agents write panels straight into the panels folder. OmaMac watches it: a new
// file arrives as a panel switched off, a changed one replaces its panel. A command nobody
// has allowed yet – new, or changed outside OmaMac – does not run until the user allows it
// in the main window, where it is shown. What OmaMac writes itself (and what the user made
// in OmaMac: with its AI, from a template or an import) needs no approval.

// ownWrites remembers what SavePanel wrote, so the watch can tell OmaMac's own changes.
var ownWrites = struct {
	mu   sync.Mutex
	sums map[string][32]byte
}{sums: map[string][32]byte{}}

func noteOwnWrite(path string, data []byte) {
	ownWrites.mu.Lock()
	ownWrites.sums[path] = sha256.Sum256(data)
	ownWrites.mu.Unlock()
}

func isOwnWrite(path string, data []byte) bool {
	ownWrites.mu.Lock()
	defer ownWrites.mu.Unlock()
	sum, ok := ownWrites.sums[path]
	return ok && sum == sha256.Sum256(data)
}

// commandFingerprint identifies what a panel runs: its command and its actions' commands.
// It is "" for a panel that runs nothing (and for the built-in modules, which run no scripts).
func commandFingerprint(p *PanelSpec) string {
	if p == nil || p.BuiltinKind != "" {
		return ""
	}
	var parts []string
	if c := strings.TrimSpace(p.Command); c != "" {
		parts = append(parts, c)
	}
	for _, a := range p.Actions {
		if c := strings.TrimSpace(a.Command); c != "" {
			parts = append(parts, "action:"+c)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// approvals are the commands the user allowed, by panel ID. They live next to the
// configuration, outside the panels folder the agents work in.
type approvals struct {
	mu      sync.Mutex
	path    string
	ids     map[string]string // panel ID → commandFingerprint
	shipped map[string]string // the templates' own: delivered with OmaMac, always allowed
}

func approvalsPath(configPath string) string {
	if configPath == "" {
		configPath = DefaultConfigPath()
	}
	return filepath.Join(filepath.Dir(configPath), "approved-commands.json")
}

// loadApprovals reads the allowed commands. Without the file (OmaMac before 1.10) every
// panel there is counts as allowed: they all ran before.
func loadApprovals(path string, panels []*PanelSpec) *approvals {
	ap := &approvals{path: path, ids: map[string]string{}, shipped: map[string]string{}}
	for _, p := range StarterPanels() {
		if fp := commandFingerprint(p); fp != "" {
			ap.shipped[p.ID] = fp
		}
	}
	for _, cat := range StoreCatalog() {
		if cat.Spec != nil {
			if fp := commandFingerprint(cat.Spec); fp != "" {
				ap.shipped[cat.Spec.ID] = fp
			}
		}
	}
	raw, err := os.ReadFile(path)
	if err == nil && json.Unmarshal(raw, &ap.ids) == nil {
		return ap
	}
	ap.ids = map[string]string{}
	for _, p := range panels {
		if fp := commandFingerprint(p); fp != "" {
			ap.ids[p.ID] = fp
		}
	}
	ap.saveLocked()
	return ap
}

func (ap *approvals) saveLocked() {
	raw, _ := json.MarshalIndent(ap.ids, "", "  ")
	if err := os.WriteFile(ap.path, raw, 0o600); err != nil {
		logf("watch", "approvals not saved: %v", err)
	}
}

// allowed reports whether p's commands may run.
func (ap *approvals) allowed(p *PanelSpec) bool {
	fp := commandFingerprint(p)
	if fp == "" || ap == nil {
		return true
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()
	return ap.ids[p.ID] == fp || ap.shipped[p.ID] == fp
}

// allow lets p's current commands run.
func (ap *approvals) allow(p *PanelSpec) {
	if ap == nil {
		return
	}
	fp := commandFingerprint(p)
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if ap.ids[p.ID] == fp || (fp == "" && ap.ids[p.ID] == "") {
		return
	}
	if fp == "" {
		delete(ap.ids, p.ID)
	} else {
		ap.ids[p.ID] = fp
	}
	ap.saveLocked()
}

func (ap *approvals) forget(id string) {
	if ap == nil {
		return
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if _, ok := ap.ids[id]; ok {
		delete(ap.ids, id)
		ap.saveLocked()
	}
}

// isHeld reports whether p waits for the user to allow its commands.
func (a *App) isHeld(p *PanelSpec) bool { return !a.approved.allowed(p) }

// allowPanel is the user's OK in the review box: the commands run from now on.
func (a *App) allowPanel(p *PanelSpec) {
	a.approved.allow(p)
	logf("watch", "%s: commands allowed by the user", p.ID)
	a.refreshPanelAsync(p, fmt.Sprintf(T("„%s“ freigegeben"), p.Title))
}

// watchPanels follows the panels folder while the app runs.
func (a *App) watchPanels() {
	dir := a.Config.PanelsDir
	w, err := mygo.WatchFiles(context.Background(), dir, mygo.FileWatchOptions{Debounce: 250 * time.Millisecond})
	if err != nil {
		logf("watch", "cannot watch %s: %v", dir, err)
		return
	}
	go func() {
		for {
			events, err := w.Next(context.Background())
			if err != nil {
				logf("watch", "watch of %s ended: %v", dir, err)
				return
			}
			names := map[string]bool{}
			for _, e := range events {
				for _, path := range []string{e.Path, e.OldPath} {
					if strings.HasSuffix(path, ".json") && !strings.Contains(path, "/") && !e.IsDir {
						names[path] = true
					}
				}
			}
			if len(names) == 0 {
				continue
			}
			sorted := make([]string, 0, len(names))
			for n := range names {
				sorted = append(sorted, n)
			}
			sort.Strings(sorted)
			a.updateUI(func() {
				for _, n := range sorted {
					a.reloadPanelFile(n)
				}
			})
		}
	}()
}

// reloadPanelFile takes over a panel file changed outside OmaMac (name is its file name in
// the panels folder). Runs on the main thread.
func (a *App) reloadPanelFile(name string) {
	path := filepath.Join(a.Config.PanelsDir, name)
	id := strings.TrimSuffix(name, ".json")
	existing := a.findPanel(id)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if existing != nil && existing.Deletable() {
			a.removePanel(id)
			a.approved.forget(id)
			logf("watch", "%s: file removed, panel removed", id)
			a.toast(fmt.Sprintf(T("„%s“ entfernt – die Datei wurde gelöscht"), existing.Title))
		}
		return
	}
	if err != nil || isOwnWrite(path, data) {
		return
	}
	var p PanelSpec
	if err := json.Unmarshal(data, &p); err != nil {
		logf("watch", "%s: not a valid panel: %v", name, err)
		a.toast(fmt.Sprintf(T("%s ist kein gültiges Panel: %v"), name, err))
		return
	}
	p.ID = id // the file name is the id
	p.Normalize()

	if existing == nil {
		wanted := p.Enabled || p.PinnedFloating
		p.Enabled, p.PinnedFloating = false, false // the switches are the user's
		if wanted {
			_ = SavePanel(a.Config.PanelsDir, &p)
		}
		a.panels = append(a.panels, &p)
		logf("watch", "%s: new panel from the folder (held: %v)", id, a.isHeld(&p))
		a.selectPanel(id)
		a.syncAllPanelRuntimes()
		a.announceExternal(&p, fmt.Sprintf(T("Neues Panel „%s“ – ausgeschaltet; in OmaMac prüfen und einschalten"), p.Title))
		if !a.isHeld(&p) {
			a.refreshPanelAsync(&p, "")
		}
		return
	}

	p.Enabled, p.PinnedFloating = existing.Enabled, existing.PinnedFloating
	for i, q := range a.panels {
		if q.ID == id {
			a.panels[i] = &p
		}
	}
	if rt, ok := a.runtimes[id]; ok && rt.flyout != nil && !rt.flyout.IsDestroyed() {
		rt.flyout.Destroy() // it shows the new version when opened again
		rt.flyout = nil
	}
	a.syncAllPanelRuntimes()
	held := a.isHeld(&p)
	logf("watch", "%s: changed in the folder (held: %v)", id, held)
	if held {
		a.selectPanel(id)
		a.announceExternal(&p, fmt.Sprintf(T("Befehl von „%s“ geändert – läuft erst nach deiner Freigabe in OmaMac"), p.Title))
		return
	}
	a.refreshPanelAsync(&p, fmt.Sprintf(T("„%s“ neu geladen"), p.Title))
}

// announceExternal tells about a panel an agent wrote: in the window, and as a system
// notification when its commands wait, since the user is in the agent's terminal then.
func (a *App) announceExternal(p *PanelSpec, msg string) {
	a.toast(msg)
	if !a.desktop || !a.isHeld(p) {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:    reviewNotificationPrefix + p.ID,
		Title: "OmaMac",
		Body:  msg,
		Group: "omamac-review",
	})
	go func() { _ = n.Show() }()
}

const reviewNotificationPrefix = "omamac-review-"

// onNotificationClick opens the main window at the panel a review notification is about.
func (a *App) onNotificationClick(id string) {
	panelID, ok := strings.CutPrefix(id, reviewNotificationPrefix)
	if !ok {
		return
	}
	a.updateUI(func() {
		if a.findPanel(panelID) != nil {
			a.selectPanel(panelID)
		}
		a.setTab(TabOverview)
		a.showStudio()
	})
}

// removePanel takes a panel out of the app (its file is already gone or removed by the caller).
func (a *App) removePanel(id string) {
	var next []*PanelSpec
	for _, item := range a.panels {
		if item.ID != id {
			next = append(next, item)
		}
	}
	a.panels = next
	if a.selectedPanelID == id && len(a.panels) > 0 {
		a.selectPanel(a.panels[0].ID)
	}
	a.syncAllPanelRuntimes()
}
