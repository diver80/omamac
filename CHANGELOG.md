# Changelog

## 1.13.0

- **Agent limits:** A new built-in module shows the limits of your Claude Code plan, as `/usage` in Claude Code does: the 5-hour session, the week and a model's own week. Each has a bar, the percent used and the time until it resets; a mark on the bar shows where an even pace would be.
  - It reads Claude Code's login from the keychain with the `security` tool (no prompt) and sends it only to Anthropic. The token is not stored or logged.
  - When the login has expired, the last numbers stay and the module says to start Claude Code once.
  - It is a dashboard tile on Macs where Claude Code is set up. Switched on for the menu bar it shows "31 % · 59 %"; it can also float on the desktop as a widget.
  - `>_` opens Claude Code in the terminal.

## 1.12.0

- **Coding agents run in the background, without a terminal:** Claude Code, Codex, Gemini CLI, Antigravity or opencode now work in OmaMac itself, in the panels folder, using each agent's non-interactive mode. This works on every Mac, with no terminal app and no AppleScript permission.
  - **Progress:** the "AI is working" dialog shows the agent's steps live ("Writes github-prs.json", "Runs: omamac check …") and then its answer.
  - **Limits:** the agent may only edit files in the panels folder and run `omamac` commands; anything else is refused and shown.
  - **Images:** Claude Code receives pasted images directly.
- **Reply and continue:** After a run, a reply in the dialog continues the same session. "Continue in the terminal" opens it interactively in Terminal, which every Mac has; iTerm or Ghostty only when chosen in the settings.
- **The panels written** are listed in the dialog, with a "Check" button when their commands wait for your OK.
- `omamac check` gives a panel's command only the variables it names.

## 1.11.0

- **Dashboard:** The main window opens on a dashboard. All panels are tiles in a grid, with the launcher's bar below them: a name opens an app, a sentence goes to the agent, and images can be pasted or dropped.
  - "Full screen" puts the dashboard on a Space of its own, a swipe away with three fingers. Real macOS desktop widgets would need a separate Swift extension.
  - Drag a tile by its header to reorder; right-click it to choose its size (small, medium, tall, wide), to refresh it or to remove it.
  - HTML panels show their page in the tile.
  - Panels that are only on the dashboard refresh while it is on screen.
- **Third switch "Dashboard"** in the panel list, next to "Menu bar" and "Widget". It is on for every panel at first.
- **Collapsible sidebar:** ☰ or ⌘\ collapses the sidebar to symbols. On the dashboard it starts collapsed.
- The launcher lists "Dashboard"; AGENTS.md tells agents about `dashboard_size`.

## 1.10.0

- **Agents in the launcher:** As in Omalauncher, a name opens the app and a sentence goes to an agent. The button next to the search field (or ⇥) chooses where sentences go:
  - OmaMac's own AI (Gemini or Claude) still builds panels in the dialog, with screenshots and mockups;
  - Claude Code, Codex, Gemini CLI, Antigravity or opencode start in your terminal (Ghostty, iTerm or Terminal), in the panels folder.
  Images pasted (⌘V) or dropped into the launcher go along to either.
- **Agent workspace:** The panels folder has an `AGENTS.md` (plus `CLAUDE.md` and `GEMINI.md` pointing to it) that tells agents how a panel file looks, how commands run and which variables there are (names only, never values).
  - `omamac check <panel>.json` runs a panel and shows its output and a preview, with secret values replaced by `$NAME`.
  - `omamac vars` lists the variables, and `omamac log` shows the recent log.
- **Panels from the folder:** OmaMac watches the panels folder. A panel a coding agent writes shows up at once, switched off. A command that is new or was changed outside OmaMac runs only once you allow it: the main window shows it in a "Check the command" box, and macOS notifies you. Panels made in OmaMac, from templates or imports, and all panels from before this version, need no approval.
- **Log:** `~/Library/Logs/OmaMac/omamac.log` records failed panel scripts, AI calls, Jira errors and agent starts, with vault values and tokens shown as `$NAME`.
  - Settings → General → Log opens it.
  - Each panel lists its last runs (time, duration, exit code, error).
- **Failed scripts show as failed:** A script that exits with an error without printing its JSON, or runs longer than 18 s, turns the panel red, with the reason. Error output no longer mixes into the panel's JSON, so a warning on stderr can't break a panel.
- **Launcher bar on the desktop (optional):** Settings → Launcher & agents → "Bar on the desktop", or 📌 in the launcher, puts a floating bar on the desktop. A click opens the launcher above it, and images can be dropped on it.

## 1.9.0

- **English UI:** OmaMac now speaks English and German. It follows the macOS language (English for everyone who reads neither); Settings → General → Language switches between System, Deutsch and English at once. Delivered panels and templates show their names in the chosen language, and the AI writes new panels' texts in it.
- The README's screenshots are English now; the German ones are in `docs/omamac-screenshots/de`.
- Command line help (`omamac -h`) is English.

## 1.8.2

- Die installierte Version steht jetzt im Hauptfenster oben links neben „OmaMac“ (Tooltip: Updates unter Einstellungen → Allgemein).

## 1.8.1

- **Zugangsdaten im Schlüsselbund:** Die API-Tokens der Jira-Instanzen und die Werte des Tresors liegen jetzt im macOS-Schlüsselbund – zusammen in einem Eintrag („de.diver80.omamac“), damit macOS nach einem Update nur einmal um Zugriff bittet („Immer erlauben“). Bisher standen die Jira-Tokens und eine Kopie der Tresor-Werte im Klartext in `jira_instances.json` bzw. `vault.json` (nur für den Benutzer lesbar). Beim ersten Start werden sie übernommen und aus den Dateien entfernt. Nur wenn der Schlüsselbund nicht nutzbar ist, bleiben sie wie bisher in der Datei; die Einstellungen sagen dann, warum.
- **Hinweise beim Hinzufügen:** „Neue Jira-Instanz“ erklärt Cloud-Token und Data-Center-PAT (mit Link zum Erstellen des Tokens) und wo er gespeichert wird; „Neues Panel“ weist auf die konfigurierbaren Jira-System-Module hin und darauf, dass ein KI-Panel seine Einstellungen im Skript trägt.

## 1.8.0

- **Konfiguration je Panel für System-Module:** Jira Aktivität und Jira Woche speichern ihre Einstellungen im Panel selbst – welche Instanzen, je Instanz Projekte bzw. JQL, „Meine Tickets/Alle“ bzw. „Geändert/Erledigt/Fällig“. Bearbeiten im Hauptfenster unter der Vorschau („Konfiguration“) oder im Panel über ⚙️.
- **Duplizieren:** Ein Modul lässt sich als weiteres Panel mit eigenen Einstellungen anlegen (z. B. eine Woche je Kunde) – über „Duplizieren“ in der Konfiguration oder im ⋯-Menü. Kopien lassen sich löschen.
- **Einstellungen → Jira-Instanzen:** Zugänge aller Jira-Module zentral verwalten (anlegen, ändern, für alle abschalten); „Instanzen verwalten …“ in den Modulen führt dorthin. Jira Quick-Access zeigt dieselbe Liste weiter unter ⚙️ JQL.
- KI-Panels bleiben wie bisher: ihre Einstellungen stehen im Abfrage-Skript.
- Die Instanz-Schalter und Filter aus 1.7.x (an der Instanz gespeichert) entfallen.

## 1.7.1

- **Instanzen je Modul:** Unter ⚙️ von Jira Aktivität und Jira Woche schaltet ein Schalter je Instanz, ob sie dort mitmacht – unabhängig von den anderen Modulen. Darunter Projekte bzw. Wochen-JQL der Instanz. „Instanzen verwalten …“ öffnet Jira Quick-Access auf ⚙️ JQL, wo Instanzen angelegt, geändert oder für alle Module abgeschaltet werden.

## 1.7.0

- **Jira Aktivität (neues System-Modul):** Der Activity Stream aller Jira-Instanzen in einer Liste, wie das Gadget in Data Center – wer was an welchem Ticket getan hat, mit Kommentartext, nach Tagen gruppiert. „Meine Tickets“ (zugewiesen, gemeldet oder beobachtet, letzte 14 Tage) oder „Alle“; je Instanz auf Projekte einschränkbar (⚙️). Filter nach Instanz und Suchfeld; ein Klick öffnet das Ticket.
- **Jira Woche (neues System-Modul):** Kanban einer Woche über alle Instanzen, eine Spalte je Tag, mit ◀ / Heute / ▶ zum Blättern. Einordnung nach „Geändert“, „Erledigt“ oder „Fällig“ (in der aktuellen Woche mit Spalte „Überfällig“); Kartenfarbe nach Status. Welche Tickets, legt je Instanz eine JQL fest (⚙️, Standard: mir zugewiesen).
- Beide kommen ausgeschaltet an und nutzen die Instanzen aus Jira Quick-Access.

## 1.6.2

- `omamac -check` gibt dem geprüften Panel jetzt auch die Jira-Variablen (`$JIRA_<NAME>_*`) mit, wie die App selbst.

## 1.6.1

- **Uhrzeiten in Ortszeit:** Die KI rechnet Zeitstempel aus APIs (meist UTC) beim Erstellen und Anpassen von Panels in die Ortszeit des Macs um, statt sie nur abzuschneiden. Bestehende Panels zeigen UTC weiter, bis sie per „Mit KI anpassen“ korrigiert werden.

## 1.6.0

- **Panels teilen:** „Teilen …“ im ⋯-/Rechtsklick-Menü eines Panels oder „Eigene Panels teilen …“ im ⋯-Menü der Übersicht. Als `.omamac`-Datei oder über die Zwischenablage (z. B. für den Chat). Live-Daten und Zugangswerte werden nie mitgegeben, nur die Namen der benötigten Variablen; der KI-Verlauf auf Wunsch. Der Dialog nennt die Adressen, mit denen ein Panel spricht. Steht ein Wert aus dem Tresor oder ein Token im Klartext im Panel, wird der Export verweigert.
- **Panels importieren:** per ⋯ → „Panels importieren …“, Doppelklick auf eine `.omamac`-Datei, Ziehen aufs Fenster oder „Aus Zwischenablage importieren“ (auch Panel-JSON der KI). Die Vorschau zeigt jeden Befehl und welche Variablen hier fehlen. Importierte Panels kommen ausgeschaltet an – ihr Befehl läuft erst nach dem Einschalten.

## 1.5.3

- Aufgeräumt: Beispieltexte ohne echte Namen; die Übernahme von Schlüsselbund-Einträgen aus der Zeit vor 1.2.1 entfällt.

## 1.5.2

- **Antwort der KI aufbereitet:** Statt JSON zeigt das KI-Fenster jedes Panel als Karte – Felder als Tabelle, `metrics`, `rows` und `actions` als Tabellen mit Spalten, Status als farbige Markierung, Befehle und HTML als Codeblock. Das funktioniert schon live, während die KI schreibt. Der Umschalter „Aufbereitet | JSON“ wechselt auf die eingerückte Rohfassung; die Wahl bleibt gespeichert. Auch die Ausgabe des ersten Datenabrufs wird so dargestellt.

## 1.5.1

- **Panels umbenennen:** „Umbenennen …“ im ⋯- und Rechtsklick-Menü oder ✎ beim gewählten Panel ändert Name, Symbol und den Text in der Menüleiste – auch bei den System-Modulen. Leer bleibt der Menüleisten-Text automatisch (vom Skript); `{status}` fügt den Wert des Skripts in den eigenen Text ein, z. B. „Kunde A {status}“. Eine Vorschau zeigt das Ergebnis; ein KI-Anpassen behält den eigenen Text.

## 1.5.0

- **Jira-Zugangsdaten für alle Panels:** Jede Instanz aus Jira Quick-Access stellt `$JIRA_<NAME>_URL`, `$JIRA_<NAME>_EMAIL` und `$JIRA_<NAME>_API_TOKEN` bereit (z. B. `$JIRA_KUNDE_A_API_TOKEN`). Panels und Aktionen bekommen sie als Umgebungsvariablen, die KI kennt nur die Namen. Kein zweites Eintragen im Tresor mehr; ein Tresor-Eintrag gleichen Namens hat Vorrang. Die Liste steht unter Einstellungen → Tresor.

## 1.4.2

- **Antwort der KI formatiert:** Das JSON wird eingerückt angezeigt – schon live, während die KI noch schreibt. Befehle und HTML in der Antwort erscheinen mit echten Zeilenumbrüchen statt `\n`. „Ausgabe kopieren“ liefert gültiges, eingerücktes JSON.

## 1.4.1

- **KI-Fenster bleibt bis „OK“ offen:** Nach dem Erstellen oder Anpassen zeigt es das Ergebnis aufbereitet – Titel, Darstellung, Aktualisierungsintervall, den Befehl als lesbaren Code, den ersten Datenabruf und die Aktionen. Die rohe Antwort der KI liegt eingeklappt darunter, als eingerücktes JSON. Nur „Im Hintergrund“ blendet das Fenster vorher aus.
- **Neue Einstellung „Während die KI arbeitet“:** „Protokoll“ (Schritte mit Zeit und die Antwort live) oder „Nur Fortschritt“ (Fortschrittsbalken und aktueller Schritt; das Protokoll ist einen Klick entfernt).

## 1.4.0

- **Live-Ausgabe der KI:** Beim Erstellen und Anpassen öffnet sich ein Fenster mit den Arbeitsschritten und der Antwort der KI, während sie geschrieben wird (Claude CLI live, andere Anbieter am Ende). „Im Hintergrund“ blendet es aus, „Abbrechen“ stoppt die KI. Schlägt etwas fehl, bleibt das Fenster mit der vollständigen Ausgabe offen; „KI-Ausgabe“ in der Fehlerzeile öffnet es wieder.
- **Bilder für die KI:** Screenshots, Mockups oder Beispiele beim Erstellen und Anpassen anhängen – per „📎 Bild …“, Drag & Drop oder ⌘V. Bis zu 5 Bilder; große Bilder und HEIC werden automatisch verkleinert und umgewandelt. Unterstützt von Claude (CLI, setup-token, API-Key) und Gemini-API-Key; mit Antigravity werden die Bilddateien dem Agenten freigegeben.
- Zeitlimit für einen KI-Lauf auf 3 Minuten erhöht.

## 1.3.3

- **Token aus „claude setup-token“ wird unterstützt:** Ein `sk-ant-oat…`-Token ist kein API-Key – die Anthropic-API lehnt ihn ab. OmaMac erkennt ihn jetzt im Feld „API-Key“ (oder im neuen Feld „Token“ beim Modus „Claude CLI“) und nutzt ihn über die claude CLI als `CLAUDE_CODE_OAUTH_TOKEN`. Damit läuft die KI auch nach Ablauf des normalen claude-Logins weiter.
- Mit echtem Anthropic-API-Key wird jetzt das eingestellte Modell verwendet statt eines veralteten Ersatzmodells.

## 1.3.2

- **KI über die claude CLI funktioniert wieder:** Die CLI startete mit allen Werkzeugen, MCP-Servern und Plugins und wurde nach 45 s abgebrochen („signal: killed“). OmaMac ruft sie jetzt als reines Text-Modell auf (ohne Werkzeuge und MCP-Server, mit dem eingestellten Modell); ein Panel entsteht in etwa 20 s.
- Zeitlimit für KI-CLIs auf 120 s erhöht; eine Zeitüberschreitung wird als solche gemeldet statt als „signal: killed“ mit fremder Warnung.

## 1.3.1

- **KI über Antigravity-Login funktioniert wieder:** Mit agy 1.5 schlug „Neues Panel“ und „Mit KI anpassen“ fehl („-p took --disable-slash-commands as its prompt“). Der Prompt wird jetzt korrekt übergeben.
- Fehlermeldungen nennen den KI-Anbieter auch dann, wenn der Aufruf schon vorher scheitert.
- Fenstertitel ist nur noch „OmaMac“.
- **Panels löschen leichter auffindbar:** sichtbarer Button „Panel löschen …“ beim gewählten Panel, Rechtsklick auf eine Panel-Zeile (gleiches Menü wie „⋯“) und ⌘⌫. Es wird immer nachgefragt; System-Module lassen sich weiterhin nur ausschalten.

## 1.3.0

**Aufgeräumte Oberfläche**

- Panels-Übersicht: eine Zeile pro Panel mit Status und den Schaltern „Menüleiste“ und „Widget“. Vorschau, Intervall, Native/HTML und Löschen stehen rechts beim gewählten Panel; Löschen fragt nach.
- Doppelte Hinweise entfernt (Banner, „Kurz erklärt“, Token-Hinweise in Kopf- und Statuszeile). Die Kurzanleitung erscheint einmal beim ersten Start und über „?“.
- Unten erscheint nur noch eine Zeile, wenn etwas fehlgeschlagen ist; Erfolge melden kurze Hinweise.
- Einstellungen in drei Bereichen (KI-Anbieter, Tresor, Allgemein). Es wird nur der gewählte KI-Anbieter gezeigt, Änderungen werden sofort gespeichert; das Schnellstart-Kürzel gilt nach Enter.
- Farbschema- und Rastergrößen-Auswahl entfernt: Sie hatten keine Wirkung.
- Vorlagen, Statusmeldungen und Antigravity-Status auf Deutsch.
- Das Menüleisten-Popover zeigt nur noch das Panel (Anpassen per KI geschieht im Hauptfenster).

**Einfachere Navigation**

- Das Hauptfenster hat drei Bereiche: Panels, Vorlagen, Einstellungen. „Fenster & Apps“ gibt es nur noch im Schnellstart.
- „+ Neues Panel“ öffnet einen Dialog: Beschreibung, Beispiele, Menüleiste oder Widget; Darstellung und Anzahl unter „Weitere Optionen“.
- „Mit KI anpassen“ steht direkt beim gewählten Panel, mit dem bisherigen Verlauf (⌘↵ zum Anwenden).
- Vorlagen: „Anpassen …“ öffnet den Dialog mit dem Text der Vorlage.
- Links in Panels öffnen sich nur noch einmal (vorher doppelt).

**Schnellstart als Launcher (⌥ Space)**

- Ein Suchfeld für alles: Programme (mit ihren Icons), Panels, Fensteranordnung und OmaMac selbst. Treffer sortiert nach Güte; als letzter Treffer „Neues Panel mit KI“ mit dem eingegebenen Text.
- Bedienung per Tastatur: ↑↓ wählen, ↵ startet bzw. schaltet ein Panel in der Menüleiste, esc schließt.
- Der Schnellstart schließt sich, wenn ein anderes Fenster den Fokus bekommt.

## 1.2.2

- **KI-Fehler werden angezeigt:** Ist die KI nicht erreichbar (Kontingent aufgebraucht, Login, Timeout) oder antwortet sie nicht mit Panel-JSON, wird kein Platzhalter-Panel mehr erzeugt und kein Panel verändert. Stattdessen erscheint die Fehlermeldung.
- CLI-Fehler (z. B. „Usage Limit Reached“ von agy/claude) zählen nicht mehr als KI-Antwort.
- **Schließen-Button (✕)** in Menüleisten-Popover und Desktop-Widget. Ein geschlossenes Widget wird abgepinnt; HTML-Widgets haben die macOS-Fensterknöpfe.
- Escape bzw. Cmd+W blenden das Schnellstart-Panel und das Hauptfenster nur aus; vorher waren sie bis zum Neustart weg.
- Beenden der App lässt angepinnte Widgets angepinnt.
- Widgets und Popover werden nach einer KI-Anpassung oder dem Wechsel Native/HTML neu aufgebaut.
- Absturz durch gleichzeitigen Tresor-Zugriff behoben; Gemini-API-Key steht nicht mehr in URLs bzw. Fehlermeldungen.
- Ein während der KI-Anpassung gelöschtes Panel taucht nicht wieder auf.
- Build-Ausgabe erscheint nicht mehr als zweite App in Spotlight/Launchpad.

## 1.2.1

- App-Kennung auf `de.diver80.omamac` umgestellt (privates Projekt). Im Schlüsselbund gespeicherte Tresor-Werte werden beim ersten Start automatisch übernommen.

## 1.2.0

- **Jira Quick-Access** als vorgeliefertes System-Modul: JQL-Counter über mehrere Jira-Instanzen, Liste, 3-Spalten-Board, Ticket-Viewer, Key/Git-Branch kopieren. Instanzen werden beim ersten Start automatisch aus `~/.jira-quick-access/config.json` übernommen.
- **Atlassian Status** als System-Modul: Live-Status von 8 Atlassian-Cloud-Diensten inkl. aktiver Incidents.
- System-Module sind per Schalter an/aus, aber nicht per KI anpassbar oder löschbar.
- Panels aktualisieren sich jetzt in ihrem eigenen Intervall statt pauschal alle 15 Sekunden.
- Übersicht: Titel werden nicht mehr abgeschnitten, Kopfzeile bricht sauber um.
- Automatische Updates über GitHub Releases (Menü „Nach Updates suchen…“), Installation per Homebrew.
