# Changelog

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
