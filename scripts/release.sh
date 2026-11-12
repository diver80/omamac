#!/usr/bin/env bash
# Builds OmaMac (macOS Universal, Linux amd64/arm64), publishes the signed release on
# GitHub (diver80/omamac, read by the in-app auto-updater and install.sh) and updates the
# Homebrew cask in diver80/homebrew-tap.
#
#   ./scripts/release.sh            # version from mygo.json
#
# Needs: gh (signed in as diver80) and the update signing key, either in
# MYGO_UPDATER_PRIVATE_KEY or at ~/Library/Application Support/mygo/update-keys/mygo-update.key.
# Before running: bump "version" in mygo.json and add a "## <version>" section to CHANGELOG.md.
set -euo pipefail
cd "$(dirname "$0")/.."

REPO="diver80/omamac"
TAP="diver80/homebrew-tap"
VERSION="$(python3 -c 'import json;print(json.load(open("mygo.json"))["version"])')"
TAG="v${VERSION}"
KEY_FILE="${HOME}/Library/Application Support/mygo/update-keys/mygo-update.key"

if [ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]; then
  [ -f "${KEY_FILE}" ] || { echo "Update signing key not found: ${KEY_FILE}" >&2; exit 1; }
  MYGO_UPDATER_PRIVATE_KEY="$(cat "${KEY_FILE}")"
  export MYGO_UPDATER_PRIVATE_KEY
fi
grep -q "^## ${VERSION}" CHANGELOG.md || { echo "CHANGELOG.md has no '## ${VERSION}' section" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "Working tree not clean — commit first." >&2; exit 1; }

echo "▶ Tests"
go test ./...

echo "▶ Tag ${TAG} & push"
git rev-parse "${TAG}" >/dev/null 2>&1 || git tag -a "${TAG}" -m "Release ${TAG}"
git push github HEAD "${TAG}"

echo "▶ Build darwin/universal, linux/amd64, linux/arm64 + upload to draft release"
rm -rf build build.noindex
go tool mygo build -platform darwin/universal,linux/amd64,linux/arm64 -upload

echo "▶ Publish release ${TAG} (makes it 'latest' for the auto-updater)"
gh release edit "${TAG}" --repo "${REPO}" --draft=false --latest

DMG="build.noindex/darwin-universal/omamac-${VERSION}.dmg"
SHA="$(shasum -a 256 "${DMG}" | awk '{print $1}')"

echo "▶ Update Homebrew cask in ${TAP}"
TAP_DIR="$(mktemp -d)"
trap 'rm -rf "${TAP_DIR}"' EXIT
gh repo clone "${TAP}" "${TAP_DIR}" -- --depth=1 -q
cat > "${TAP_DIR}/Casks/omamac.rb" <<CASK
cask "omamac" do
  version "${VERSION}"
  sha256 "${SHA}"

  url "https://github.com/${REPO}/releases/download/v#{version}/omamac-#{version}.dmg"
  name "OmaMac"
  desc "Menu bar status panels, Jira Quick-Access and Atlassian status for macOS"
  homepage "https://github.com/${REPO}"

  # OmaMac updates itself from GitHub releases (signed mygo updates)
  auto_updates true
  depends_on macos: :ventura

  app "OmaMac.app"

  zap trash: [
    "~/.config/omamac",
    "~/Library/Preferences/de.diver80.omamac.plist",
  ]

  # OmaMac is ad-hoc signed, not notarized: drop the download quarantine so
  # Gatekeeper lets it start (Homebrew 7 removed --no-quarantine).
  postflight_steps do
    run "/usr/bin/xattr",
        args:           ["-dr", "com.apple.quarantine", "{{appdir}}/OmaMac.app"],
        writable_paths: ["OmaMac.app"],
        writable_base:  :appdir,
        must_succeed:   false
  end

  caveats <<~EOS
    OmaMac is not notarized. If macOS still blocks the first launch, run:
      xattr -dr com.apple.quarantine /Applications/OmaMac.app
  EOS
end
CASK
(
  cd "${TAP_DIR}"
  # Homebrew 7 removed --no-quarantine
  sed -i '' 's/brew install --cask --no-quarantine /brew install --cask /' README.md
  sed -i '' '/--no-quarantine/d' README.md
  if ! grep -q "diver80/tap/omamac" README.md; then
    cat >> README.md <<'MD'

To install **OmaMac** (menu bar status panels, Jira Quick-Access & Atlassian status):

```bash
brew install --cask diver80/tap/omamac
```

OmaMac updates itself; `brew upgrade --cask omamac` works as well.
MD
  fi
  git add Casks/omamac.rb README.md
  if git diff --staged --quiet; then
    echo "  cask already up to date"
  else
    git commit -q -m "feat: release OmaMac ${VERSION}"
    git push -q origin HEAD
    echo "  pushed Casks/omamac.rb"
  fi
)

echo "✓ OmaMac ${VERSION} released"
echo "  https://github.com/${REPO}/releases/tag/${TAG}"
echo "  brew install --cask diver80/tap/omamac"
