package omamac

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

// renderTabSettings groups the settings in sections (AI, Jira instances, vault, general).
// Every change is saved as it is made; the shortcut takes effect with Enter.
func (a *App) renderTabSettings(c *ui.Context) {
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().MaxWidth(860).Padding(20).Gap(16).Children(func() {
			pageHeader(c, T("Einstellungen"), "", nil)
			a.renderAISettings(c)
			if a.Jira != nil {
				section(c, "cfg-jira", T("Jira-Instanzen"),
					T("Zugänge für alle Jira-Module und für $JIRA_<NAME>_* in eigenen Panels. Ausgeschaltet fehlt eine Instanz überall; was ein einzelnes Modul zeigt, steht in dessen Konfiguration."),
					func() { a.renderJiraInstanceEditor(c, "cfg-jira") })
			}
			a.renderVaultSettings(c)
			a.renderLauncherSettings(c)
			a.renderGeneralSettings(c)
		})
	})
}

func (a *App) renderAISettings(c *ui.Context) {
	t := c.Theme()
	section(c, "cfg-ai", T("KI-Anbieter"),
		T("Die KI wird nur beim Erstellen und Anpassen von Panels genutzt. Im laufenden Betrieb aktualisieren sich Panels lokal per Skript – ohne KI."),
		func() {
			changed := false
			ui.Row(c).Children(func() {
				changed = ui.Segmented(c.Key("cfg-active-prov"), &a.providerIdx, "Google Gemini", "Anthropic Claude").Changed()
			})

			ui.Form(c, func() {
				if a.providerIdx == 0 {
					ui.Field(c, T("Anmeldung"), func() {
						ui.Row(c).Children(func() {
							changed = ui.Segmented(c.Key("cfg-gem-auth"), &a.geminiAuthIdx, T("Antigravity-Login"), T("API-Key")).Changed() || changed
						})
					})
					if a.geminiAuthIdx == 0 {
						a.renderAntigravityStatusField(c)
					}
					ui.Field(c, T("Modell"), func() {
						changed = ui.Select(c.Key("cfg-gem-model"), &a.geminiModel, geminiModelList).Width(320).Changed() || changed
					})
					if a.geminiAuthIdx == 1 {
						ui.Field(c, T("API-Key"), func() {
							changed = ui.TextInput(c.Key("cfg-gem-key"), &a.geminiKey).Password().
								Placeholder("AIzaSy…").FillWidth().Changed() || changed
						})
					}
				} else {
					ui.Field(c, T("Anmeldung"), func() {
						ui.Row(c).Children(func() {
							changed = ui.Segmented(c.Key("cfg-cl-auth"), &a.claudeAuthIdx, T("API-Key"), T("Antigravity-Login"), "Claude CLI").Changed() || changed
						})
					})
					if a.claudeAuthIdx == 1 {
						a.renderAntigravityStatusField(c)
					}
					if a.claudeAuthIdx == 2 {
						ui.Field(c, T("Programm"), func() {
							ui.Text(c, T("Nutzt das lokal angemeldete „claude -p“.")).FontSize(12).TextColor(t.TextMuted)
						})
					}
					ui.Field(c, T("Modell"), func() {
						changed = ui.Select(c.Key("cfg-cl-model"), &a.claudeModel, claudeModelList).Width(320).Changed() || changed
					})
					if a.claudeAuthIdx == 0 || a.claudeAuthIdx == 2 {
						label, placeholder := T("API-Key"), T("sk-ant-api… oder Token aus „claude setup-token“")
						if a.claudeAuthIdx == 2 {
							label, placeholder = "Token", T("optional: sk-ant-oat… aus „claude setup-token“")
						}
						ui.Field(c, label, func() {
							changed = ui.TextInput(c.Key("cfg-cl-key"), &a.claudeKey).Password().
								Placeholder(placeholder).FillWidth().Changed() || changed
						})
						if isClaudeOAuthToken(a.claudeKey) {
							ui.Field(c, "", func() {
								ui.Text(c, T("Token aus „claude setup-token“ – wird über die claude CLI genutzt (kein API-Key).")).FontSize(12).TextColor(t.TextMuted)
							})
						}
					}
				}
			})
			ui.Form(c, func() {
				ui.Field(c, T("Während die KI arbeitet"), func() {
					ui.Column(c).Gap(4).Children(func() {
						ui.Row(c).Children(func() {
							changed = ui.Segmented(c.Key("cfg-ai-view"), &a.aiRunViewIdx, T("Protokoll"), T("Nur Fortschritt")).Changed() || changed
						})
						hint := T("Zeigt jeden Schritt und die Antwort der KI, während sie geschrieben wird.")
						if a.aiRunViewIdx == 1 {
							hint = T("Zeigt einen Fortschrittsbalken; das Protokoll ist einen Klick entfernt.")
						}
						ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted)
					})
				})
			})
			if changed {
				a.saveSettingsFromDraft()
			}

			ui.Row(c).FillWidth().Children(func() {
				if ui.Button(c.Key("cfg-test-ai"), T("Verbindung testen")).Disabled(a.busy).Clicked() {
					a.testAIConnection()
				}
			})
		})
}

// renderAntigravityStatusField shows whether the Antigravity login works, with the buttons to fix it.
func (a *App) renderAntigravityStatusField(c *ui.Context) {
	t := c.Theme()
	ui.Field(c, "Status", func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			status := "warn"
			if a.AgyStatus.Available {
				status = "ok"
			}
			statusDot(c, status)
			ui.Text(c, a.AgyStatus.StatusSummary).FontSize(12).TextColor(t.TextMuted).Grow(1).MinWidth(0)
			if !a.AgyStatus.Available {
				if ui.Button(c.Key("cfg-agy-login"), T("Anmelden …")).Tooltip(T("Startet „agy“ im Terminal")).Clicked() {
					_ = TriggerAntigravityLogin(a.Config)
					a.toast(T("Antigravity-Anmeldung im Terminal gestartet"))
				}
			}
			if ui.Button(c.Key("cfg-agy-check"), T("Erneut prüfen")).Clicked() {
				a.AgyStatus = InspectAntigravityAuth(a.Config)
			}
		})
	})
}

func (a *App) renderVaultSettings(c *ui.Context) {
	t := c.Theme()
	section(c, "cfg-vault", T("Tresor"),
		T("Suchbegriffe und Tokens liegen im macOS-Schlüsselbund. Panels erhalten sie als Umgebungsvariable – die KI sieht nur den Namen, nie den Wert."),
		func() {
			if a.Vault != nil {
				storageNote(c, a.Vault.StorageErr, T("Werte"), "vault.json")
			}
			if a.Vault != nil && len(a.Vault.Entries) > 0 {
				ui.Column(c).FillWidth().Radius(8).Border(1, t.Border).Background(t.Background).Children(func() {
					for idx, entry := range a.Vault.Entries {
						e := entry
						if idx > 0 {
							ui.Divider(c)
						}
						ui.Row(c.Key("vault-row-"+e.Name)).FillWidth().Padding(8, 12).AlignItems(ui.Center).Gap(12).Children(func() {
							ui.Text(c, "$"+e.Name).FontSize(12).Bold().TextColor(t.Accent).SingleLine()
							ui.Text(c, e.Description).FontSize(11.5).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
							if ui.Button(c.Key("vault-del-"+e.Name), T("Entfernen")).Clicked() {
								_ = a.Vault.Delete(e.Name)
								a.toast("$" + e.Name + T(" aus dem Tresor entfernt"))
							}
						})
					}
				})
			}

			if jiraVars := a.Jira.CredentialCatalog(); len(jiraVars) > 0 {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Text(c, T("Automatisch aus Jira Quick-Access (pflegen unter Einstellungen → Jira-Instanzen):")).FontSize(11.5).TextColor(t.TextMuted)
					ui.Column(c).FillWidth().Radius(8).Border(1, t.Border).Children(func() {
						for idx, m := range jiraVars {
							if idx > 0 {
								ui.Divider(c)
							}
							ui.Row(c.Key("vault-jira-"+m.Name)).FillWidth().Padding(6, 12).AlignItems(ui.Center).Gap(12).Children(func() {
								ui.Text(c, "$"+m.Name).FontSize(12).Bold().TextColor(t.Accent.Alpha(0.8)).SingleLine()
								ui.Text(c, m.Description).FontSize(11.5).TextColor(t.TextMuted).Grow(1).MinWidth(0).SingleLine()
							})
						}
					})
				})
			}

			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c.Key("vault-new-key"), &a.vaultNewKey).Placeholder("NAME").Width(180)
				ui.TextInput(c.Key("vault-new-val"), &a.vaultNewVal).Password().Placeholder(T("Wert")).Width(180)
				ui.TextInput(c.Key("vault-new-desc"), &a.vaultNewDesc).Placeholder(T("Beschreibung für die KI (optional)")).Grow(1)
				if ui.Button(c.Key("vault-add-btn"), T("Hinzufügen")).
					Disabled(strings.TrimSpace(a.vaultNewKey) == "" || strings.TrimSpace(a.vaultNewVal) == "").Clicked() {
					if a.Vault != nil {
						_ = a.Vault.Set(a.vaultNewKey, a.vaultNewVal, a.vaultNewDesc)
						a.toast("$" + NormalizeEnvVarName(a.vaultNewKey) + T(" im Schlüsselbund gespeichert"))
						a.vaultNewKey, a.vaultNewVal, a.vaultNewDesc = "", "", ""
					}
				}
			})
		})
}

// renderLauncherSettings: the launcher's shortcut, where its sentences go, the terminal
// the coding agents run in, and the bar on the desktop.
func (a *App) renderLauncherSettings(c *ui.Context) {
	t := c.Theme()
	section(c, "cfg-launcher", T("Schnellstart & Agenten"),
		T("Im Schnellstart öffnet ein Name die App, ein Satz geht an die KI. Die OmaMac-KI baut Panels im Dialog – mit Screenshots und Mockups. Ein Coding-Agent arbeitet im Hintergrund im Panel-Ordner, ohne Terminal: Er liest dort AGENTS.md, darf nur Dateien dieses Ordners ändern und omamac check aufrufen. „Im Terminal fortsetzen“ öffnet seine Sitzung interaktiv im gewählten Terminal."),
		func() {
			ui.Form(c, func() {
				ui.Field(c, T("Kürzel"), func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						if ui.TextInput(c.Key("cfg-shortcut"), &a.shortcutText).Placeholder("Alt+Space").Width(180).Submitted() {
							a.applyLauncherShortcut()
						}
						ui.Text(c, T("Mit Enter übernehmen")).FontSize(11.5).TextColor(t.TextMuted)
					})
				})
				ui.Field(c, T("Sätze an"), func() {
					ui.Column(c).Gap(4).Children(func() {
						choices := a.agentChoices()
						names := make([]string, len(choices))
						for i, ch := range choices {
							names[i] = ch.Name
						}
						cur := a.currentAgent().Name
						if ui.Select(c.Key("cfg-agent"), &cur, names).Width(240).Changed() {
							for _, ch := range choices {
								if ch.Name == cur {
									a.setAgent(ch.ID)
								}
							}
						}
						hint := T("Gefunden: ") + a.installedAgentNames()
						if !a.agents.isDetected() {
							hint = T("Suche installierte Agenten …")
						}
						ui.Text(c, hint).FontSize(11.5).TextColor(t.TextMuted)
					})
				})
				ui.Field(c, "Terminal", func() {
					terms := installedTerminals()
					idx := 0
					for i, name := range terms {
						if name == a.terminal() {
							idx = i
						}
					}
					ui.Row(c).Children(func() {
						if ui.Segmented(c.Key("cfg-terminal"), &idx, terms...).Changed() {
							a.Config.Terminal = terms[idx]
							_ = SaveConfig(a.ConfigPath, a.Config)
						}
					})
				})
				ui.Field(c, T("Leiste auf dem Desktop"), func() {
					ui.Column(c).Gap(4).Children(func() {
						on := a.Config.LauncherWidget
						if ui.Switch(c.Key("cfg-bar"), &on).Changed() {
							a.setLauncherWidget(on)
						}
						ui.Text(c, T("Eine schwebende Leiste wie in Omalauncher: ein Klick öffnet den Schnellstart darüber, Bilder lassen sich darauf ziehen.")).
							FontSize(11.5).TextColor(t.TextMuted)
					})
				})
				if goos == "linux" {
					ui.Field(c, T("Obere Leiste"), func() {
						ui.Column(c).Gap(4).Children(func() {
							on := a.Config.Waybar != "off"
							if ui.Switch(c.Key("cfg-waybar"), &on).Changed() {
								a.setBar(on)
							}
							ui.Text(c, T("Die Einträge der Menüleiste als Module in Omarchys Leiste (oder Waybar), mit ihrem Text: ein Klick öffnet das Panel. Die Konfiguration wird vorher gesichert.")).
								FontSize(11.5).TextColor(t.TextMuted)
						})
					})
				}
				ui.Field(c, T("Panel-Ordner"), func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, a.Config.PanelsDir).FontSize(12).Grow(1).MinWidth(0).SingleLine()
						if ui.Button(c.Key("cfg-panels-dir"), T("Im Finder zeigen")).Clicked() {
							mygo.Shell.ShowItemInFolder(filepath.Join(a.Config.PanelsDir, "AGENTS.md"))
						}
					})
				})
			})
		})
}

func (a *App) renderGeneralSettings(c *ui.Context) {
	t := c.Theme()
	section(c, "cfg-general", T("Allgemein"), "", func() {
		ui.Form(c, func() {
			ui.Field(c, T("Sprache"), func() {
				ui.Column(c).Gap(4).Children(func() {
					idx := 0
					for i, tag := range uiLanguages {
						if a.Config.Language == tag {
							idx = i + 1
						}
					}
					ui.Row(c).Children(func() {
						if ui.Segmented(c.Key("cfg-language"), &idx, "System", "Deutsch", "English").Changed() {
							a.Config.Language = ""
							if idx > 0 {
								a.Config.Language = uiLanguages[idx-1]
							}
							_ = SaveConfig(a.ConfigPath, a.Config)
							a.applyLanguage()
						}
					})
					ui.Text(c, T("Folgt der Sprache von macOS, wenn „System“ gewählt ist.")).FontSize(11.5).TextColor(t.TextMuted)
				})
			})
			ui.Field(c, T("Protokoll"), func() {
				ui.Column(c).Gap(4).FillWidth().Children(func() {
					ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, LogPath()).FontSize(12).Grow(1).MinWidth(0).SingleLine()
						if ui.Button(c.Key("cfg-log-open"), T("Öffnen")).Clicked() {
							a.openLogFile()
						}
					})
					ui.Text(c, T("Läufe der Panel-Skripte, KI-Aufrufe, Jira-Fehler und gestartete Agenten. Werte aus dem Tresor und Jira-Tokens stehen dort nur als $NAME.")).
						FontSize(11.5).TextColor(t.TextMuted)
				})
			})
			ui.Field(c, "Version", func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, appVersion()).FontSize(12)
					if ui.Button(c.Key("cfg-check-update"), T("Nach Updates suchen")).Clicked() {
						updater.CheckForUpdates()
					}
				})
			})
		})
	})
}

func (a *App) testAIConnection() {
	a.saveSettingsFromDraft()
	a.busy = true
	a.busyLabel = T("Teste die KI-Verbindung …")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg, err := a.AI.TestConnection(ctx)
		a.updateUI(func() {
			a.busy = false
			if err != nil {
				a.statusErr = true
				a.statusMsg = T("KI-Verbindung fehlgeschlagen: ") + err.Error()
				a.toast("⚠️ " + a.statusMsg)
				return
			}
			a.clearStatus()
			a.toast("✓ " + msg)
		})
	}()
}

// saveSettingsFromDraft stores the AI settings of the form. The shortcut has its own step.
func (a *App) saveSettingsFromDraft() {
	c := a.Config
	if a.providerIdx == 1 {
		c.ActiveProvider = ProviderClaude
	} else {
		c.ActiveProvider = ProviderGemini
	}
	if a.geminiAuthIdx == 1 {
		c.GeminiAuthMode = GeminiAuthAPIKey
	} else {
		c.GeminiAuthMode = GeminiAuthAntigravity
	}
	c.GeminiModel = a.geminiModel
	c.GeminiAPIKey = strings.TrimSpace(a.geminiKey)

	switch a.claudeAuthIdx {
	case 1:
		c.ClaudeAuthMode = ClaudeAuthAntigravity
	case 2:
		c.ClaudeAuthMode = ClaudeAuthCLI
	default:
		c.ClaudeAuthMode = ClaudeAuthAPIKey
	}
	c.ClaudeModel = a.claudeModel
	c.AIRunView = AIRunViewLog
	if a.aiRunViewIdx == 1 {
		c.AIRunView = AIRunViewProgress
	}
	c.ClaudeAPIKey = strings.TrimSpace(a.claudeKey)
	c.Normalize()

	_ = SaveConfig(a.ConfigPath, c)
	a.AgyStatus = InspectAntigravityAuth(c)
}

// applyLauncherShortcut registers the shortcut typed in the settings for the launcher.
func (a *App) applyLauncherShortcut() {
	c := a.Config
	next := strings.TrimSpace(a.shortcutText)
	if next == "" || next == c.LauncherShortcut {
		a.shortcutText = c.LauncherShortcut
		return
	}
	mygo.GlobalShortcut.UnregisterAll()
	if err := mygo.GlobalShortcut.Register(next, func() { a.ToggleWalPanel() }); err != nil {
		a.toast(T("⚠️ Kürzel „") + next + T("“ ließ sich nicht registrieren: ") + err.Error())
		_ = mygo.GlobalShortcut.Register(c.LauncherShortcut, func() { a.ToggleWalPanel() })
		a.shortcutText = c.LauncherShortcut
		return
	}
	c.LauncherShortcut = next
	_ = SaveConfig(a.ConfigPath, c)
	a.toast(T("Schnellstart-Kürzel: ") + shortcutLabel(c.LauncherShortcut))
}
