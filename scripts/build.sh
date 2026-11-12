#!/usr/bin/env bash
# Builds OmaMac locally for macOS, Linux and Windows, without publishing anything
# (scripts/release.sh publishes the macOS release).
#
#   ./scripts/build.sh                  # all: macOS universal, Linux + Windows amd64/arm64
#   ./scripts/build.sh mac linux        # only these
#   ./scripts/build.sh windows/amd64    # any GOOS/GOARCH mygo knows
#   ./scripts/build.sh -t -o dist all   # run the tests first, write to dist/
#
# Shortcuts: mac (darwin/universal), linux (amd64 + arm64), windows (amd64 + arm64), all.
# Output goes to <out>/<goos>-<goarch>/ (default out: "out" in mygo.json, build.noindex):
#   macOS    OmaMac.app and omamac-<version>.dmg
#   Linux    the binary, a .deb, omamac-<version>-linux-<arch>.tar.gz with install.sh, a PKGBUILD
#   Windows  OmaMac.exe, and omamac-setup-<version>-<arch>.exe when NSIS (makensis) is installed
# MyGo needs no cgo, so every platform builds from any machine; the .app's signature and
# the .dmg need macOS.
set -euo pipefail
cd "$(dirname "$0")/.."

usage() { sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

OUT=""
TEST=0
TARGETS=()
while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage ;;
    -t|--test) TEST=1 ;;
    -o|--out) [ $# -ge 2 ] || usage 2; OUT="$2"; shift ;;
    all) TARGETS+=(darwin/universal linux/amd64 linux/arm64 windows/amd64 windows/arm64) ;;
    mac|macos|darwin) TARGETS+=(darwin/universal) ;;
    linux) TARGETS+=(linux/amd64 linux/arm64) ;;
    windows|win) TARGETS+=(windows/amd64 windows/arm64) ;;
    */*) TARGETS+=("$1") ;;
    *) echo "Unknown platform or flag: $1" >&2; usage 2 ;;
  esac
  shift
done
[ ${#TARGETS[@]} -gt 0 ] || TARGETS=(darwin/universal linux/amd64 linux/arm64 windows/amd64 windows/arm64)

# Each target once, in the order given.
UNIQUE=()
for t in "${TARGETS[@]}"; do
  case " ${UNIQUE[*]:-} " in *" $t "*) ;; *) UNIQUE+=("$t") ;; esac
done
PLATFORMS="$(IFS=,; echo "${UNIQUE[*]}")"

[ -n "${OUT}" ] || OUT="$(python3 -c 'import json;print(json.load(open("mygo.json")).get("out") or "dist")')"
VERSION="$(python3 -c 'import json;print(json.load(open("mygo.json"))["version"])')"

FLAGS=(-o "${OUT}" -platform "${PLATFORMS}")
case ",${PLATFORMS}," in
  *,windows/*)
    if ! command -v makensis >/dev/null 2>&1; then
      echo "▲ No NSIS: building OmaMac.exe without the Windows installer (brew install makensis)"
      FLAGS+=(-skip-installer)
    fi ;;
esac
if [ "$(uname -s)" != Darwin ] && [[ ",${PLATFORMS}," == *,darwin/* ]]; then
  echo "▲ Not on macOS: building the .app without a disk image"
  FLAGS+=(-skip-dmg)
fi

if [ "${TEST}" = 1 ]; then
  echo "▶ Tests"
  go test ./...
fi

echo "▶ Build OmaMac ${VERSION} for ${PLATFORMS} → ${OUT}/"
for t in "${UNIQUE[@]}"; do rm -rf "${OUT:?}/${t%/*}-${t#*/}"; done
go tool mygo build "${FLAGS[@]}"

echo
echo "▶ Built:"
for t in "${UNIQUE[@]}"; do
  dir="${OUT}/${t%/*}-${t#*/}"
  [ -d "${dir}" ] || { echo "  ${t}: nothing built" >&2; continue; }
  find "${dir}" -maxdepth 1 \( -name '*.dmg' -o -name '*.app' -o -name '*.deb' -o -name '*.tar.gz' \
    -o -name '*.exe' -o -name PKGBUILD -o \( -type f -perm -u+x -name omamac \) \) | sort | sed 's/^/  /'
done
