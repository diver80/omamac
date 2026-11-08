package omamac

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

var createExamples = []struct{ label, prompt string }{
	{"🖨️ mydealz 3D-Drucker", "Create a status panel listing current 3D-Drucker deals from mydealz.de/gruppe/3d-drucker and monitoring Idealo target price using $MYDEALZ_KEYWORD and $IDEALO_TARGET_PRICE"},
	{"🐳 Docker-Container", "Create a Docker & Kubernetes container health status panel with running container count and restart action"},
	{"⚡ Akku & CPU-Last", "Create 2 panels: Battery health & charging wattage; CPU thermal & memory pressure"},
}

func (a *App) renderTabPromptStudio(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().MaxWidth(860).Padding(20).Gap(16).Children(func() {
			pageHeader(c, "KI-Studio", "Panels per Satz erstellen und anpassen.", nil)

			section(c, "st-create", "Neues Panel", "Beschreibe in einem Satz, was angezeigt werden soll. Variablen aus dem Tresor ($NAME) werden eingebunden.", func() {
				ui.TextArea(c.Key("st-p1-area"), &a.prompt1Text).
					Placeholder("z. B. „Zeige die aktuellen 3D-Drucker-Deals von mydealz und warne, wenn der Idealo-Preis unter $IDEALO_TARGET_PRICE fällt“").
					Height(74).FillWidth()
				ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
					for idx, ex := range createExamples {
						if ui.Button(c.Key(fmt.Sprintf("preset-%d", idx)), ex.label).Clicked() {
							a.prompt1Text = ex.prompt
						}
					}
				})
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Segmented(c.Key("st-p1-type"), &a.prompt1TypeIdx, "Native", "HTML", "Beide")
					ui.Segmented(c.Key("st-p1-cnt"), &a.prompt1CountIdx, panelCountLabels...)
					ui.Spacer(c)
					if ui.PrimaryButton(c.Key("st-p1-submit"), "Erstellen").
						Disabled(a.busy || strings.TrimSpace(a.prompt1Text) == "").Clicked() {
						a.triggerPrompt1Create()
					}
				})
			})

			sel := a.findPanel(a.selectedPanelID)
			section(c, "st-refine", "Panel anpassen", "Wähle ein eigenes Panel und beschreibe die Änderung.", func() {
				ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
					for _, p := range a.panels {
						if p.IsBuiltin {
							continue
						}
						pCopy := p
						lbl := pCopy.Icon + " " + pCopy.Title
						key := c.Key("st-sel-" + pCopy.ID)
						clicked := false
						if a.selectedPanelID == pCopy.ID {
							clicked = ui.PrimaryButton(key, lbl).Clicked()
						} else {
							clicked = ui.Button(key, lbl).Clicked()
						}
						if clicked {
							a.selectPanel(pCopy.ID)
						}
					}
				})
				ui.TextArea(c.Key("st-p2-area"), &a.prompt2Text).
					Placeholder("z. B. „Hebe Bambu-Lab-Deals grün hervor“").
					Height(70).FillWidth()
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					if sel != nil && sel.IsBuiltin {
						ui.Text(c, "System-Module lassen sich nicht per KI ändern.").FontSize(11.5).TextColor(t.TextMuted)
					}
					ui.Spacer(c)
					if ui.PrimaryButton(c.Key("st-p2-submit"), "Anpassen").
						Disabled(a.busy || sel == nil || sel.IsBuiltin || strings.TrimSpace(a.prompt2Text) == "").Clicked() {
						a.triggerPrompt2Refine(sel.ID, a.prompt2Text)
					}
				})
			})
		})
	})
}

func (a *App) renderTabWindowManager(c *ui.Context) {
	ui.Column(c).Grow(1).FillWidth().Padding(20).Gap(14).Children(func() {
		pageHeader(c, "Fenster & Apps", "Ordne das aktive Fenster an oder starte ein Programm – auch per "+shortcutLabel(a.Config.LauncherShortcut)+".", nil)
		a.renderWalLauncherMode(c)
	})
}
