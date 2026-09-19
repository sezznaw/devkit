#!/bin/sh
# devkit installer.
#
#   curl -fsSL https://raw.githubusercontent.com/sezznaw/devkit/main/install.sh | sh
#
# Environment variables:
#   GITHUB_TOKEN     required only if the repository is private
#   GITHUB_REPO      default sezznaw/devkit
#   DEVKIT_VERSION   default: latest release tag (e.g. v0.3.0)
#   INSTALL_DIR      default /usr/local/bin (falls back to ~/.local/bin if not writable)
#   REGISTRY_REPO    default sezznaw/devkit-registry (written to the initial config)
set -eu

GITHUB_REPO="${GITHUB_REPO:-sezznaw/devkit}"
REGISTRY_REPO="${REGISTRY_REPO:-sezznaw/devkit-registry}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
API="https://api.github.com/repos/$GITHUB_REPO"

auth() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then printf 'Authorization: Bearer %s' "$GITHUB_TOKEN"; else printf 'X-Devkit: 1'; fi
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

archive="devkit_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if [ -z "${GITHUB_TOKEN:-}" ]; then
  # Public repository: plain download URLs, no API calls (the anonymous API is
  # limited to 60 requests per hour per IP, easy to hit behind an office NAT).
  if [ -z "${DEVKIT_VERSION:-}" ]; then
    tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$GITHUB_REPO/releases/latest" | sed 's#.*/tag/##')
    case "$tag" in
      v*) ;;
      *) echo "could not determine the latest release of $GITHUB_REPO (private repository? set GITHUB_TOKEN)" >&2; exit 1 ;;
    esac
  else
    tag=$DEVKIT_VERSION
  fi
  base="https://github.com/$GITHUB_REPO/releases/download/$tag"
  echo "[1/3] downloading devkit $tag ($os/$arch)"
  curl -fL# -o "$tmp/$archive" "$base/$archive"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
else
  # Private repository: assets must be fetched through the API by asset id.
  if [ -z "${DEVKIT_VERSION:-}" ]; then
    release_url="$API/releases/latest"
  else
    release_url="$API/releases/tags/$DEVKIT_VERSION"
  fi
  release=$(curl -fsSL -H "$(auth)" -H "Accept: application/vnd.github+json" "$release_url") \
    || { echo "could not fetch release info from $release_url (check GITHUB_TOKEN)" >&2; exit 1; }
  tag=$(printf '%s\n' "$release" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
  # In the API response every asset object starts with its own
  # ".../releases/assets/<id>" url, followed by its name; remember the last
  # asset url seen and print it when the wanted name comes up.
  asset_url() {
    printf '%s\n' "$release" | awk -v want="$1" '
      /"url": *"[^"]*\/releases\/assets\/[0-9]+"/ { match($0, /https:[^"]*/); u = substr($0, RSTART, RLENGTH) }
      index($0, "\"name\": \"" want "\"") { print u; exit }'
  }
  archive_url=$(asset_url "$archive")
  sums_url=$(asset_url "checksums.txt")
  [ -n "$archive_url" ] || { echo "release $tag has no asset $archive" >&2; exit 1; }
  [ -n "$sums_url" ] || { echo "release $tag has no checksums.txt" >&2; exit 1; }
  echo "[1/3] downloading devkit $tag ($os/$arch)"
  curl -fL# -H "$(auth)" -H "Accept: application/octet-stream" -o "$tmp/$archive" "$archive_url"
  curl -fsSL -H "$(auth)" -H "Accept: application/octet-stream" -o "$tmp/checksums.txt" "$sums_url"
fi

echo "[2/3] verifying checksum"
(cd "$tmp" && grep " $archive\$" checksums.txt | shasum -a 256 -c - >/dev/null) \
  || { echo "checksum verification failed" >&2; exit 1; }
echo "[3/3] installing"

tar -xzf "$tmp/$archive" -C "$tmp"

if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi
install -m 0755 "$tmp/devkit" "$INSTALL_DIR/devkit"

echo "installed to $INSTALL_DIR/devkit"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to your PATH" ;;
esac

# Seed the config if none exists yet.
cfg="$HOME/.devkit/config.yaml"
if [ ! -f "$cfg" ]; then
  mkdir -p "$HOME/.devkit"
  {
    [ -n "${GITHUB_TOKEN:-}" ] && echo "github_token: $GITHUB_TOKEN"
    echo "registry_repo: $REGISTRY_REPO"
    echo "devkit_repo: $GITHUB_REPO"
  } > "$cfg"
  chmod 600 "$cfg"
  echo "wrote default config to $cfg"
fi

"$INSTALL_DIR/devkit" version
echo
echo "next: cd into your project directory and run:  devkit ngs <service>"
echo "      (the first run creates devkit.yaml for you to fill in; missing Go, kitex and thriftgo are installed automatically)"
