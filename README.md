# omarc (OmaMac) — Omarchy & OmaLauncher for macOS (`mygo`)

Keyboard-first macOS Wal-Panel Launcher, Window Tiler & 2-Prompt Status Panel / Desktop Widget Studio built natively in Go with `github.com/egoist/mygo`.

## Key Features
- **0-Token Live Polling**: AI (Gemini via Antigravity OAuth Login / API Key or Claude) is used **only** when creating (Prompt #1) or refining (Prompt #2) panels. Background polling executes local shell/Python scripts with **0 AI tokens**.
- **OmaLauncher Quick-Card & Store**: Create `[ widget ]`, `[ app ]`, or `[ panel ]` in `3x3` or `6x2` grid sizes, switch themes (`TokyoNight`, `Vantablack`, `White`, `Catppuccin Mocha`), or install from the 5-category Store (including Pixel-Art Owl & live `mydealz` 3D-printer radar).
- **Central Credentials Vault (`macOS Keychain`)**: Zero-leak `$ENV_VAR` injection into panels via `mygo.Secrets`.
- **`omarc -check <panel.json>`**: Validates dependencies, runs live data refresh, and renders a Retina PNG preview.

## Build & Run
```bash
go test -v ./...
go build -o ./bin/omarc ./cmd/omarc
./bin/omarc
```
