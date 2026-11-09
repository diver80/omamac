package omamac

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Shared building blocks of the OmaMac views: one look for headings, sections,
// status and switches, so every view reads the same way.

func pillBadge(c *ui.Context, label string, bg, fg ui.Color) ui.Element {
	return ui.Text(c, label).FontSize(10.5).Bold().Padding(2, 8).Radius(10).Background(bg).TextColor(fg).SingleLine()
}

// closeButton draws a ✕ button that calls onClose; nothing when onClose is nil (inline previews).
func closeButton(c *ui.Context, key string, onClose func()) {
	if onClose == nil {
		return
	}
	if ui.Button(c.Key(key), "✕").Tooltip("Schließen").Clicked() {
		onClose()
	}
}

func statusColors(t *ui.Theme, status string) (ui.Color, ui.Color) {
	switch strings.ToLower(status) {
	case "crit", "error", "danger":
		return t.Danger.Alpha(0.18), t.Danger
	case "warn", "warning":
		return t.Warning.Alpha(0.20), t.Warning
	case "info":
		return t.Accent.Alpha(0.18), t.Accent
	default:
		return t.Success.Alpha(0.18), t.Success
	}
}

// appVersion is the installed version from the app bundle, "dev" when run from source.
func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return "dev"
}

// infoBox is a quiet hint in a dialog or form: a tinted box around what fn builds.
func infoBox(c *ui.Context, fn func()) {
	t := c.Theme()
	ui.Column(c).FillWidth().Padding(8, 10).Radius(8).Background(t.Accent.Alpha(0.08)).Border(1, t.Accent.Alpha(0.25)).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "ℹ︎").FontSize(12).TextColor(t.Accent)
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(fn)
		})
	})
}

// storageNote says where secrets are kept: the Keychain, or – with the reason – a 0600 file.
func storageNote(c *ui.Context, storageErr, what, file string) {
	t := c.Theme()
	if storageErr == "" {
		ui.Text(c, "🔒 "+what+" liegen im macOS-Schlüsselbund (ein Eintrag „"+secretsService+"“).").FontSize(11).TextColor(t.TextMuted)
		return
	}
	ui.Text(c, "⚠ "+what+" liegen in ~/.config/omamac/"+file+" (nur für dich lesbar), weil: "+storageErr).FontSize(11).TextColor(t.Warning)
}

// statusDot draws the small colored circle that stands for a status everywhere.
func statusDot(c *ui.Context, status string) {
	_, fg := statusColors(c.Theme(), status)
	ui.Row(c).Size(8, 8).Radius(4).Background(fg)
}

// pageHeader draws a view's title, an optional muted line under it and, right
// of them, what actions builds.
func pageHeader(c *ui.Context, title, subtitle string, actions func()) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(19).Bold().SingleLine()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(12).TextColor(t.TextMuted)
			}
		})
		if actions != nil {
			actions()
		}
	})
}

// section draws a titled card: the settings' and the dialogs' grouping.
func section(c *ui.Context, key, title, hint string, fn func()) {
	t := c.Theme()
	ui.Column(c.Key(key)).FillWidth().Padding(16, 18).Radius(10).Border(1, t.Border).Background(t.Surface).Gap(12).Children(func() {
		ui.Column(c).FillWidth().Gap(3).Children(func() {
			ui.Text(c, title).FontSize(14).Bold()
			if hint != "" {
				ui.Text(c, hint).FontSize(11.5).TextColor(t.TextMuted)
			}
		})
		fn()
	})
}

// fixedCell centers what fn builds in a column of the given width, so the
// switches of a list line up under their column titles.
func fixedCell(c *ui.Context, width float32, fn func()) {
	ui.Row(c).Width(width).Justify(ui.Center).AlignItems(ui.Center).Children(fn)
}

// panelStatusText is the short live status of a panel, as the menu bar shows it.
func panelStatusText(p *PanelSpec) string {
	if s := strings.TrimSpace(p.BadgeText); s != "" {
		return s
	}
	return p.Title
}

// panelMetaLine sums up how a panel refreshes: "Alle 60 s · Native · Stand 19:50:04".
func panelMetaLine(p *PanelSpec) string {
	kind := "Native"
	if p.Type == PanelTypeHTML {
		kind = "HTML"
	}
	parts := []string{fmt.Sprintf("Alle %d s", p.RefreshSeconds), kind}
	if p.LastUpdated != "" {
		parts = append(parts, "Stand "+p.LastUpdated)
	}
	return strings.Join(parts, " · ")
}

// shortcutLabel shows a shortcut as macOS writes it: "Alt+Space" → "⌥ Space".
func shortcutLabel(s string) string {
	if s == "" {
		return ""
	}
	r := strings.NewReplacer("Cmd+", "⌘", "Command+", "⌘", "Ctrl+", "⌃", "Control+", "⌃",
		"Alt+", "⌥", "Option+", "⌥", "Shift+", "⇧")
	out := r.Replace(s)
	// a space between the symbols and the key reads better: "⌥Space" → "⌥ Space"
	for i, ch := range out {
		if !strings.ContainsRune("⌘⌃⌥⇧", ch) {
			if i > 0 {
				return out[:i] + " " + out[i:]
			}
			break
		}
	}
	return out
}

// textButton is a button that reads as a link in the accent color, for light
// actions inside text (examples, "Verlauf"). ui.Link would open a URL.
func textButton(c *ui.Context, key, label string) bool {
	t := c.Theme()
	btn := ui.ButtonBase(c.Key(key)).Cursor(ui.CursorPointer)
	hovered := btn.Hovered()
	btn.Children(func() {
		txt := ui.Text(c, label).FontSize(11.5).TextColor(t.Accent).SingleLine()
		if hovered {
			txt.TextColor(t.Accent.Alpha(0.75))
		}
	})
	return btn.Clicked()
}
