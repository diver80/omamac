# omamac (OmaMac) — Omarchy & OmaLauncher for macOS (`mygo`)

Keyboard-first macOS Wal-Panel Launcher, Window Tiler & 2-Prompt Status Panel / Desktop Widget Studio built natively in Go with `github.com/egoist/mygo`.

## Key Features
- **Übersicht mit 1-Klick-Schaltern (`An / Aus`)**: Schalte Status-Panels in einer klaren Übersicht direkt per Switch für die **macOS-Menüleiste** oder als schwebendes **Desktop-Widget** ein und aus — inklusive sofortiger Live-Vorschau.
- **0-Token Live Polling**: KI (Gemini via Antigravity OAuth Login / API Key oder Claude) wird **nur** beim einmaligen Erstellen (Schritt 1) oder Anpassen (Schritt 2) genutzt. Im laufenden Betrieb führen die Panels lokal ihr Shell-/Python-Skript mit **0 KI-Tokens** aus.
- **Vorlagen-Store & Schnellstart (`Alt+Space`)**: Fertige Panels und Widgets in 5 Kategorien (System, Mac & Akku, Web/mydealz 3D-Drucker, Fokus-Timer, Pixel-Art Owl) direkt per Switch aktivierbar.
- **Zentraler Tresor (`macOS Keychain`)**: Zero-Leak `$ENV_VAR` Injektion in Panels über `mygo.Secrets`.
- **`omamac -check <panel.json>`**: Prüft Abhängigkeiten, führt ein Live-Daten-Update aus und rendert eine Retina-PNG-Vorschau.

## Build & Run
```bash
cd /Users/fhess/data/avono/dev/omamac
go test -v ./...
go build -o ./bin/omamac ./cmd/omamac
./bin/omamac
```
