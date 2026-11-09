package omamac

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

var createExamples = []struct{ label, prompt string }{
	{"mydealz 3D-Drucker", "Create a status panel listing current 3D-Drucker deals from mydealz.de/gruppe/3d-drucker and monitoring Idealo target price using $MYDEALZ_KEYWORD and $IDEALO_TARGET_PRICE"},
	{"Docker-Container", "Create a Docker & Kubernetes container health status panel with running container count and restart action"},
	{"Akku & CPU-Last", "Create 2 panels: Battery health & charging wattage; CPU thermal & memory pressure"},
}

// openCreate opens the "Neues Panel" dialog in the main window, with prompt as
// the description when given (a template's, say).
func (a *App) openCreate(prompt string) {
	if prompt != "" {
		a.prompt1Text = prompt
	}
	a.activeTab = TabOverview
	a.showCreate = true
	if a.studioWin != nil {
		a.studioWin.Show()
		a.studioWin.Focus()
	}
	if a.walWin != nil {
		a.walWin.Hide()
	}
}

// renderCreateDialog describes a new panel in one sentence; the AI builds it.
func (a *App) renderCreateDialog(c *ui.Context) {
	t := c.Theme()
	ui.Modal(c.Key("create"), &a.showCreate, func() {
		dlg := ui.Column(c).Width(560).Gap(14)
		if files := dlg.DroppedFiles(); files != nil {
			a.addImages(&a.createImages, files)
		}
		if dlg.FileDragOver() {
			dlg.Border(2, t.Accent).Radius(10)
		}
		dlg.Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				ui.Text(c, "Neues Panel").FontSize(18).Bold()
				ui.Text(c, "Beschreibe in einem Satz, was angezeigt werden soll. Variablen aus dem Tresor ($NAME) werden eingebunden. Ein Screenshot oder Mockup hilft der KI oft mehr als viele Worte.").
					FontSize(12).TextColor(t.TextMuted)
			})

			ui.TextArea(c.Key("create-prompt"), &a.prompt1Text).AutoFocus().
				Placeholder("z. B. „Zeige die aktuellen 3D-Drucker-Deals von mydealz und warne, wenn der Idealo-Preis unter $IDEALO_TARGET_PRICE fällt“").
				Height(96).FillWidth().
				OnPaste(func(text string) bool { return text == "" && a.pasteImage(&a.createImages) })

			a.renderImageAttachments(c, "create-images", &a.createImages)

			infoBox(c, func() {
				ui.Text(c, "Für Jira gibt es fertige System-Module: Quick-Access, Aktivität und Woche. Unter „Panels“ einschalten, im Hauptfenster unter der Vorschau konfigurieren und per „Duplizieren“ mehrfach nutzen, z. B. je Kunde. "+
					"Ein KI-Panel trägt seine Einstellungen dagegen fest im Abfrage-Skript – ändern lässt es sich mit einem Satz an die KI.").
					FontSize(11.5).TextColor(t.TextMuted)
			})

			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, "Beispiele:").FontSize(11.5).TextColor(t.TextMuted)
				for idx, ex := range createExamples {
					if textButton(c, fmt.Sprintf("create-ex-%d", idx), ex.label) {
						a.prompt1Text = ex.prompt
					}
				}
			})

			ui.Form(c, func() {
				ui.Field(c, "Anzeigen in", func() {
					ui.Row(c).Children(func() {
						ui.Segmented(c.Key("create-place"), &a.createPlaceIdx, "Menüleiste", "Menüleiste und als Widget")
					})
				})
			})

			ui.Collapsible(c, "Weitere Optionen", &a.showCreateMore, func() {
				ui.Form(c, func() {
					ui.Field(c, "Darstellung", func() {
						ui.Row(c).Children(func() {
							ui.Segmented(c.Key("create-type"), &a.prompt1TypeIdx, "Native", "HTML", "Beide")
						})
					})
					ui.Field(c, "Anzahl", func() {
						ui.Row(c).Children(func() {
							ui.Segmented(c.Key("create-count"), &a.prompt1CountIdx, panelCountLabels...)
						})
					})
				})
			})

			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "KI: "+a.AI.ActiveProviderSummary()).FontSize(11).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
				if ui.Button(c.Key("create-cancel"), "Abbrechen").Clicked() {
					a.showCreate = false
				}
				if ui.PrimaryButton(c.Key("create-submit"), "Erstellen").
					Disabled(a.busy || strings.TrimSpace(a.prompt1Text) == "").Clicked() {
					a.showCreate = false
					a.triggerPrompt1Create()
				}
			})
		})
	})
}
