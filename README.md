# OmaMac

Status-Panels für die macOS-Menüleiste und den Desktop, per Satz von einer KI gebaut, dazu ein Tastatur-Launcher für Programme und Fenster – inspiriert von Omarchy, nativ in Go mit [`mygo`](https://github.com/egoist/mygo).

## Funktionen

- **Panels für Menüleiste und Desktop:** Jedes Panel hat zwei Schalter – „Menüleiste“ zeigt es oben rechts, „Widget“ legt es schwebend auf den Desktop. Rechts in der Übersicht stehen Live-Vorschau, Intervall und Aktionen des gewählten Panels.
- **Per Satz erstellen und anpassen:** „+ Neues Panel“ beschreibt ein Panel in einem Satz; die KI (Gemini über Antigravity-Login oder API-Key, oder Claude) baut Panel und Abfrage-Skript. Änderungen gehen ebenso per Satz beim Panel.
- **Ohne KI im Betrieb:** Danach aktualisieren sich Panels lokal per Shell-/Python-Skript – ohne KI-Aufrufe und Tokens.
- **Schnellstart (`⌥ Space`):** Ein Suchfeld für Programme, Panels, Fensteranordnung und neue Panels; mit ↑↓ und ↵ bedienbar.
- **Vorlagen:** Fertige Panels per Schalter hinzufügen oder als Ausgangspunkt für ein eigenes KI-Panel nutzen.
- **Tresor (macOS-Schlüsselbund):** Suchbegriffe und Tokens erhalten Panels als `$VARIABLE`; die KI sieht nur den Namen.
- **System-Module:** Jira Quick-Access (JQL-Counter, Board, Ticket-Viewer; übernimmt die Instanzen aus `~/.jira-quick-access/config.json`) und Atlassian Status.
- **Automatische Updates** aus den GitHub-Releases; `omamac -check <panel.json>` prüft ein Panel und rendert eine PNG-Vorschau.

![Panels](docs/omamac-screenshots/01-panels.png)

![Schnellstart](docs/omamac-screenshots/05-schnellstart.png)

## Installation

```bash
brew install --cask diver80/tap/omamac
```

OmaMac aktualisiert sich danach selbst (signierte Updates aus den [GitHub-Releases](https://github.com/diver80/omamac/releases)).

## Entwicklung

```bash
go test ./...
OMAMAC_SCREENSHOTS=1 go test ./internal/omamac -run TestGenerateScreenshots   # Screenshots in docs/ erneuern
go run ./cmd/omamac                                   # Entwicklungsstart (ohne Auto-Update)
go tool mygo build -platform darwin/universal         # OmaMac.app + DMG nach build.noindex/darwin-universal/ (.noindex: nicht in Spotlight/Launchpad)
```

## Release

1. `version` in `mygo.json` erhöhen und einen Abschnitt `## <version>` in `CHANGELOG.md` anlegen (wird zu den Release-Notes im Update-Dialog).
2. Committen, dann `./scripts/release.sh`: testet, taggt, baut, lädt das Release nach `diver80/omamac` hoch, veröffentlicht es und aktualisiert den Cask in `diver80/homebrew-tap`.

Der geheime Update-Signaturschlüssel liegt unter `~/Library/Application Support/mygo/update-keys/mygo-update.key` (alternativ `MYGO_UPDATER_PRIVATE_KEY`). **Nicht ins Repo legen und sicher sichern** — ohne ihn können installierte Apps keine Updates mehr annehmen.
