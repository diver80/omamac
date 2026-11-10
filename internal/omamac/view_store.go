package omamac

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *App) renderTabStore(c *ui.Context) {
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(20).Gap(14).Children(func() {
			pageHeader(c, T("Vorlagen"), T("Fertige Panels per Schalter hinzufügen – oder als Ausgangspunkt für ein eigenes KI-Panel."), nil)
			a.renderStoreCatalogList(c, "tab")
		})
	})
}

func (a *App) findMatchingPanelForStore(it StoreCategoryItem) *PanelSpec {
	if it.Spec == nil {
		return nil
	}
	for _, p := range a.panels {
		if p.ID == it.Spec.ID || strings.HasPrefix(p.ID, it.Spec.ID+"-") || strings.EqualFold(p.Title, it.Spec.Title) {
			return p
		}
	}
	return nil
}

// renderStoreCatalogList lists the templates in one card: a switch adds and
// enables one, "Anpassen …" starts a new AI panel from its prompt.
func (a *App) renderStoreCatalogList(c *ui.Context, keyPrefix string) {
	t := c.Theme()
	catalog := StoreCatalog()

	ui.Column(c).FillWidth().Radius(10).Border(1, t.Border).Background(t.Surface).Children(func() {
		for idx, item := range catalog {
			it := item
			if idx > 0 {
				ui.Divider(c)
			}
			existing := a.findMatchingPanelForStore(it)
			isEnabled := existing != nil && existing.Enabled

			ui.Row(c.Key(fmt.Sprintf("%s-store-item-%d", keyPrefix, idx))).
				FillWidth().Padding(12, 16).AlignItems(ui.Center).Gap(14).Children(func() {
				ui.Text(c, it.Icon).FontSize(22).Width(30)
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, it.Title).FontSize(13.5).Bold().SingleLine()
					ui.Text(c, it.Category+" · "+it.Subtitle).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
				})

				if ui.Button(c.Key(fmt.Sprintf("%s-store-prompt-%d", keyPrefix, idx)), T("Anpassen …")).
					Tooltip(T("Als Ausgangspunkt für ein eigenes KI-Panel verwenden")).Clicked() {
					a.openCreate(it.Prompt)
				}

				swVal := isEnabled
				if ui.Switch(c.Key(fmt.Sprintf("%s-store-sw-%d", keyPrefix, idx)), &swVal).Label(it.Title).Changed() {
					a.setStoreItemEnabled(it, existing, swVal)
				}
			})
		}
	})
}

// setStoreItemEnabled enables or disables the panel of a template, installing it the first time.
func (a *App) setStoreItemEnabled(it StoreCategoryItem, existing *PanelSpec, enabled bool) {
	if existing != nil {
		existing.Enabled = enabled
		msg := T("„") + existing.Title + T("“ aus der Menüleiste genommen")
		if enabled {
			msg = T("„") + existing.Title + T("“ in der Menüleiste")
		}
		a.persistPanelToggle(existing, msg)
		return
	}
	if !enabled || it.Spec == nil {
		return
	}
	clone := *it.Spec
	clone.Enabled = true
	a.approved.allow(&clone)
	_ = SavePanel(a.Config.PanelsDir, &clone)
	a.panels = append(a.panels, &clone)
	a.selectPanel(clone.ID)
	a.syncAllPanelRuntimes()
	a.toast(T("„") + clone.Title + T("“ hinzugefügt"))
}
