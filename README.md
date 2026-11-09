# OmaMac

Status panels for the macOS menu bar and desktop, built by an AI from a single sentence, plus a keyboard launcher for apps and windows – inspired by Omarchy, written natively in Go with [`mygo`](https://github.com/egoist/mygo).

The name is short for **Omarchy on the Mac**: [Omarchy](https://omarchy.org) is DHH's opinionated Arch Linux + Hyprland setup, whose keyboard-first launcher and status bar inspired OmaMac. OmaMac is an independent project, not affiliated with Omarchy, and brings that idea to macOS without replacing the macOS desktop.

The app's interface is in German.

## Features

- **Panels for the menu bar and the desktop:** Every panel has two switches – "Menüleiste" shows it in the top-right menu bar, "Widget" floats it on the desktop. The overview shows the selected panel's live preview, refresh interval and actions on the right.
- **Create and change panels with a sentence:** "+ Neues Panel" describes a panel in one sentence; the AI (Gemini via Antigravity login or API key, or Claude) builds the panel and its data script. Changes work the same way, right next to the panel. Attach screenshots or mockups (button, drag & drop or ⌘V) and watch the AI's answer stream in while it works.
- **No AI at runtime:** Afterwards, panels refresh locally with their shell/Python script – no AI calls, no tokens.
- **Launcher (`⌥ Space`):** One search field for apps, panels, window layouts and new panels; driven with ↑↓ and ↵.
- **Share panels:** Export one or all of your panels as a `.omamac` file or via the clipboard, without live data or secrets – only the names of the variables they need. Import shows each panel's command first and adds panels switched off. A panel holding a secret in plain text cannot be exported.
- **Templates:** Add ready-made panels with a switch, or use one as the starting point for your own AI panel.
- **Vault (macOS Keychain):** Panels receive search terms and tokens as `$VARIABLE`; the AI only ever sees the name. The Jira Quick-Access instances add `$JIRA_<NAME>_URL`, `_EMAIL` and `_API_TOKEN` automatically.
- **Built-in modules:** Jira Quick-Access (JQL counters, board, ticket viewer; imports the instances from `~/.jira-quick-access/config.json`) and Atlassian Status.
  Two more work on the same Jira instances: **Jira Activity**, the activity stream of all sites like the Data Center gadget (your tickets or everything, optionally per project), and **Jira Week**, a Kanban board with a column per day that pages week by week, by last change, resolution or due date. Built-in modules keep their settings in the panel (which instances, JQL or projects per instance) and can be duplicated, e.g. one week board per customer; the Jira instances themselves are managed under Settings.
- **Automatic updates** from the GitHub releases; `omamac -check <panel.json>` checks a panel and renders a PNG preview.

![Panels](docs/omamac-screenshots/01-panels.png)

![Launcher](docs/omamac-screenshots/05-schnellstart.png)

## Installation

```bash
brew install --cask diver80/tap/omamac
```

OmaMac then keeps itself up to date (signed updates from the [GitHub releases](https://github.com/diver80/omamac/releases)).

## Development

```bash
go test ./...
OMAMAC_SCREENSHOTS=1 go test ./internal/omamac -run TestGenerateScreenshots   # refresh the screenshots in docs/
go run ./cmd/omamac                                   # development run (no auto-update)
go tool mygo build -platform darwin/universal         # OmaMac.app + DMG in build.noindex/darwin-universal/ (.noindex: kept out of Spotlight/Launchpad)
```

## Release

1. Bump `version` in `mygo.json` and add a `## <version>` section to `CHANGELOG.md` (it becomes the release notes in the update dialog).
2. Commit, then run `./scripts/release.sh`: it tests, tags, builds, uploads the release to `diver80/omamac`, publishes it and updates the cask in `diver80/homebrew-tap`.

The secret update signing key lives at `~/Library/Application Support/mygo/update-keys/mygo-update.key` (or `MYGO_UPDATER_PRIVATE_KEY`). **Never put it in the repo, and keep a safe backup** – without it, installed apps can no longer accept updates.
