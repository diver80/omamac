package omamac

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// ---------------------------------------------------------------------------
// Export

// openExport opens the "Teilen" dialog for panels (system modules are left out).
func (a *App) openExport(panels []*PanelSpec) {
	a.exportIDs = nil
	for _, p := range panels {
		if p != nil && !p.IsBuiltin {
			a.exportIDs = append(a.exportIDs, p.ID)
		}
	}
	if len(a.exportIDs) == 0 {
		a.toast(T("Keine eigenen Panels zum Teilen – System-Module werden nicht exportiert"))
		return
	}
	a.showExport = true
}

func (a *App) exportPanels() []*PanelSpec {
	var out []*PanelSpec
	for _, id := range a.exportIDs {
		if p := a.findPanel(id); p != nil {
			out = append(out, p)
		}
	}
	return out
}

// exportBundle builds what the dialog's buttons hand out.
func (a *App) exportBundle() (*ShareBundle, error) {
	var catalog []CredentialMeta
	if a.Vault != nil {
		a.Vault.mu.RLock()
		catalog = append(catalog, a.Vault.Entries...)
		a.Vault.mu.RUnlock()
	}
	catalog = append(catalog, a.Jira.CredentialCatalog()...)
	return BuildShareBundle(a.exportPanels(), panelEnv(a.Vault, a.Jira), catalog, a.exportHistory, mygo.App.Version())
}

// renderExportDialog shows what leaves the Mac: the panels, the variables the
// receiver needs, the addresses involved – and refuses panels holding a secret.
func (a *App) renderExportDialog(c *ui.Context) {
	if !a.showExport {
		return
	}
	panels := a.exportPanels()
	t := c.Theme()
	env := panelEnv(a.Vault, a.Jira)
	ui.Modal(c.Key("export"), &a.showExport, func() {
		ui.Column(c).Width(600).Gap(12).Children(func() {
			title := fmt.Sprintf(T("%d Panels teilen"), len(panels))
			if len(panels) == 1 {
				title = fmt.Sprintf(T("„%s“ teilen"), panels[0].Title)
			}
			ui.Text(c, title).FontSize(18).Bold()
			ui.Text(c, T("Als .omamac-Datei oder über die Zwischenablage, z. B. per Chat. Live-Daten und Zugangswerte werden nie mitgegeben – nur die Namen der Variablen.")).
				FontSize(12).TextColor(t.TextMuted)

			blocked := false
			vars := map[string]bool{}
			var hosts []string
			seenHost := map[string]bool{}
			ui.Scroll(c.Key("export-list")).FillWidth().MaxHeight(360).Children(func() {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					for i, p := range panels {
						chk := CheckForSharing(p, env, a.exportHistory)
						blocked = blocked || chk.Blocked()
						for _, v := range chk.VarUsed {
							vars[v] = true
						}
						for _, h := range chk.Hosts {
							if !seenHost[h] {
								seenHost[h] = true
								hosts = append(hosts, h)
							}
						}
						ui.Row(c.Key(fmt.Sprintf("export-p-%d", i))).FillWidth().Padding(6, 10).Radius(8).Border(1, t.Border).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, p.Icon).FontSize(15)
							ui.Text(c, p.Title).FontSize(12.5).Bold().Grow(1).MinWidth(0).SingleLine()
							switch {
							case len(chk.Leaks) > 0:
								ui.Text(c, T("⚠ enthält den Wert von $")+strings.Join(chk.Leaks, ", $")).FontSize(11.5).TextColor(t.Danger)
							case len(chk.Tokens) > 0:
								ui.Text(c, T("⚠ enthält einen Token (")+chk.Tokens[0]+")").FontSize(11.5).TextColor(t.Danger)
							default:
								pillBadge(c, string(p.Type), t.Accent.Alpha(0.15), t.Accent)
							}
						})
					}
				})
			})

			if blocked {
				ui.Row(c).FillWidth().Padding(8, 12).Radius(8).Background(t.Danger.Alpha(0.10)).Children(func() {
					ui.Text(c, T("Markierte Panels enthalten einen Zugangswert im Klartext und werden so nicht geteilt. Ersetze ihn per „Mit KI anpassen“ durch die Variable, z. B. „Nutze $NAME statt des eingetragenen Tokens“.")).
						FontSize(12).TextColor(t.Danger).FillWidth()
				})
			}
			if len(vars) > 0 {
				names := make([]string, 0, len(vars))
				for v := range vars {
					names = append(names, "$"+v)
				}
				sort.Strings(names)
				ui.Text(c, T("Der Empfänger braucht diese Variablen im Tresor (ohne Werte): ")+strings.Join(names, ", ")).FontSize(12).FillWidth()
			}
			if len(hosts) > 0 {
				ui.Text(c, T("Enthält diese Adressen – prüfe, ob du sie weitergeben möchtest: ")+strings.Join(hosts, ", ")).FontSize(12).TextColor(t.Warning).FillWidth()
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Switch(c.Key("export-history"), &a.exportHistory)
				ui.Text(c, T("KI-Verlauf mitgeben (die Sätze, mit denen das Panel entstand)")).FontSize(12)
			})

			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c.Key("export-cancel"), T("Abbrechen")).Clicked() {
					a.showExport = false
				}
				if ui.Button(c.Key("export-clip"), T("In Zwischenablage")).Disabled(blocked).Clicked() {
					if b, err := a.exportBundle(); err != nil {
						a.toast("⚠️ " + err.Error())
					} else {
						c.WriteClipboard(string(b.Marshal()))
						a.showExport = false
						a.toast(fmt.Sprintf(T("%d Panel(s) in der Zwischenablage – zum Einfügen: Panels → ⋯ → Aus Zwischenablage importieren"), len(b.Panels)))
					}
				}
				if ui.PrimaryButton(c.Key("export-file"), T("Als Datei sichern …")).Disabled(blocked).Clicked() {
					a.saveExportFile()
				}
			})
		})
	})
}

// saveExportFile asks where to save; the dialog blocks, so it runs off the UI thread.
func (a *App) saveExportFile() {
	b, err := a.exportBundle()
	if err != nil {
		a.toast("⚠️ " + err.Error())
		return
	}
	data := b.Marshal()
	name := shareFileName(b.Panels)
	a.showExport = false
	go func() {
		home, _ := os.UserHomeDir()
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent:            a.studioWin,
			Title:             T("Panels teilen"),
			DefaultPath:       filepath.Join(home, "Desktop", name),
			Filters:           []mygo.FileFilter{{Name: T("OmaMac-Panels"), Extensions: []string{ShareFileExt}}},
			CreateDirectories: true,
		})
		if err != nil || path == "" {
			return
		}
		if filepath.Ext(path) == "" {
			path += "." + ShareFileExt
		}
		err = os.WriteFile(path, data, 0o644)
		a.updateUI(func() {
			if err != nil {
				a.toast(T("⚠️ Nicht gespeichert: ") + err.Error())
				return
			}
			a.toast(fmt.Sprintf(T("%d Panel(s) gesichert: %s"), len(b.Panels), filepath.Base(path)))
		})
	}()
}

// ---------------------------------------------------------------------------
// Import

// openImportData shows the import preview for a file's or the clipboard's content.
func (a *App) openImportData(data []byte, source string) {
	b, err := ParseShareBundle(data)
	if err != nil {
		a.toast(T("⚠️ Import aus ") + source + ": " + err.Error())
		return
	}
	a.importBundle, a.importSource = b, source
	a.importShowCmd = map[int]bool{}
	a.showImport = true
	a.activeTab = TabOverview
	if a.studioWin != nil {
		a.studioWin.Show()
		a.studioWin.Focus()
	}
}

// OpenImportFile is the entry for a .omamac file opened from Finder or dropped on the window.
func (a *App) OpenImportFile(path string) {
	if !a.desktop {
		a.pendingImports = append(a.pendingImports, path) // the window is not there yet
		return
	}
	a.updateUI(func() { // Finder and drop events: repaint with the dialog open
		data, err := os.ReadFile(path)
		if err != nil {
			a.toast("⚠️ " + err.Error())
			return
		}
		a.openImportData(data, filepath.Base(path))
	})
}

// pickImportFile asks for a .omamac file; the dialog blocks, so it runs off the UI thread.
func (a *App) pickImportFile() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  a.studioWin,
			Title:   T("Panels importieren"),
			Filters: []mygo.FileFilter{{Name: T("OmaMac-Panels"), Extensions: []string{ShareFileExt, "json"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.OpenImportFile(paths[0])
	}()
}

// knownVariables are the names panels get here: vault entries and Jira instances.
func (a *App) knownVariables() map[string]bool {
	known := map[string]bool{}
	for _, kv := range panelEnv(a.Vault, a.Jira) {
		if k, _, ok := strings.Cut(kv, "="); ok {
			known[k] = true
		}
	}
	return known
}

// renderImportDialog previews what would be imported: each panel with the
// command it runs, and which of the variables it needs are set up here.
func (a *App) renderImportDialog(c *ui.Context) {
	b := a.importBundle
	if !a.showImport || b == nil {
		return
	}
	t := c.Theme()
	known := a.knownVariables()
	ui.Modal(c.Key("import"), &a.showImport, func() {
		ui.Column(c).Width(640).Gap(12).Children(func() {
			title := fmt.Sprintf(T("%d Panels importieren"), len(b.Panels))
			if len(b.Panels) == 1 {
				title = fmt.Sprintf(T("„%s“ importieren"), b.Panels[0].Title)
			}
			ui.Text(c, title).FontSize(18).Bold()
			src := T("Aus ") + a.importSource
			if b.AppVersion != "" {
				src += T(" · erstellt mit OmaMac ") + b.AppVersion
			}
			ui.Text(c, src).FontSize(11.5).TextColor(t.TextMuted)

			ui.Row(c).FillWidth().Padding(8, 12).Radius(8).Background(t.Warning.Alpha(0.12)).Children(func() {
				ui.Text(c, T("Panels führen Befehle auf deinem Mac aus. Sie kommen ausgeschaltet an – sieh dir die Befehle an und schalte ein Panel erst dann in der Menüleiste ein.")).
					FontSize(12).FillWidth()
			})

			ui.Scroll(c.Key("import-list")).FillWidth().MaxHeight(380).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					for i, p := range b.Panels {
						idx := i
						ui.Column(c.Key(fmt.Sprintf("import-p-%d", i))).FillWidth().Padding(10, 12).Radius(8).Border(1, t.Border).Gap(6).Children(func() {
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, p.Icon).FontSize(16)
								ui.Column(c).Grow(1).MinWidth(0).Children(func() {
									ui.Text(c, p.Title).FontSize(13).Bold().SingleLine()
									if p.Subtitle != "" {
										ui.Text(c, p.Subtitle).FontSize(11.5).TextColor(t.TextMuted).SingleLine()
									}
								})
								pillBadge(c, string(p.Type), t.Accent.Alpha(0.15), t.Accent)
							})
							if cmd := strings.TrimSpace(p.Command); cmd != "" {
								open := a.importShowCmd[idx]
								ui.Collapsible(c, fmt.Sprintf(T("Befehl ansehen (%d Zeilen, alle %d s)"), strings.Count(cmd, "\n")+1, p.RefreshSeconds), &open, func() {
									codeBlock(c, cmd)
								})
								a.importShowCmd[idx] = open
							}
							if n := len(p.PromptHistory); n > 0 {
								ui.Text(c, fmt.Sprintf(T("KI-Verlauf: %d Schritte – „%s“"), n, truncateStr(p.PromptHistory[0].Prompt, 80))).FontSize(11.5).TextColor(t.TextMuted)
							}
						})
					}
				})
			})

			if len(b.Variables) > 0 {
				ui.Column(c).FillWidth().Gap(3).Children(func() {
					ui.Text(c, T("Benötigte Variablen")).FontSize(12).Bold()
					for i, v := range b.Variables {
						ui.Row(c.Key(fmt.Sprintf("import-v-%d", i))).FillWidth().Gap(8).Children(func() {
							if known[v.Name] {
								ui.Text(c, "✓").FontSize(12).Bold().TextColor(t.Success).Width(14)
							} else {
								ui.Text(c, "⚠").FontSize(12).TextColor(t.Warning).Width(14)
							}
							ui.Text(c, "$"+v.Name).Font("monospace").FontSize(11.5)
							hint := v.Description
							if !known[v.Name] {
								hint = T("fehlt – unter Einstellungen → Tresor anlegen")
								if v.Description != "" {
									hint += " (" + v.Description + ")"
								}
							}
							ui.Text(c, hint).FontSize(11.5).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
						})
					}
				})
			}

			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Spacer(c)
				if ui.Button(c.Key("import-cancel"), T("Abbrechen")).Clicked() {
					a.showImport = false
				}
				if ui.PrimaryButton(c.Key("import-ok"), T("Importieren")).Clicked() {
					a.importPanels(b)
				}
			})
		})
	})
}

// importPanels adds the bundle's panels, switched off and with IDs of their own.
func (a *App) importPanels(b *ShareBundle) {
	var last string
	for _, src := range b.Panels {
		p := *src
		p.ID = a.uniquePanelID(p.ID, p.Title)
		p.Enabled, p.PinnedFloating = false, false
		p.PromptHistory = append(append([]PromptStep(nil), src.PromptHistory...), PromptStep{
			Step: len(src.PromptHistory) + 1, Prompt: T("Importiert aus ") + a.importSource,
			Provider: "import", Timestamp: time.Now().Format("15:04:05"),
		})
		_ = SavePanel(a.Config.PanelsDir, &p)
		a.panels = append(a.panels, &p)
		last = p.ID
	}
	a.syncAllPanelRuntimes()
	if last != "" {
		a.selectPanel(last)
	}
	a.showImport = false
	a.importBundle = nil
	a.toast(fmt.Sprintf(T("%d Panel(s) importiert – ausgeschaltet; zum Anzeigen „Menüleiste“ einschalten"), len(b.Panels)))
}

// uniquePanelID keeps id when it is free, else derives a free one.
func (a *App) uniquePanelID(id, title string) string {
	base := strings.TrimSpace(id)
	if base == "" {
		base = Slugify(title)
	}
	if a.findPanel(base) == nil {
		return base
	}
	for n := 2; ; n++ {
		if cand := fmt.Sprintf("%s-%d", base, n); a.findPanel(cand) == nil {
			return cand
		}
	}
}

// importFromClipboard previews the panels on the clipboard (text copied from a chat, say).
func (a *App) importFromClipboard() {
	a.openImportData([]byte(mygo.Clipboard.ReadText()), T("der Zwischenablage"))
}
