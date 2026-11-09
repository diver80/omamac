# Changelog

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
