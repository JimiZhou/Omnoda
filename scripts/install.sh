#!/bin/sh
# Omnoda user-level installer.
# curl -fsSL https://HOST/install.sh | sh -s -- --server https://HOST --code CODE
set -eu

SERVER=""
CODE=""
NAME=""
WORKSPACE=""
LABEL=""
BIN_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.omnoda"
VERSION="0.1.0"

# Trust anchor. Replace only through a signed key-rotation release.
# This key signs release binaries. A SHA256 file served next to the binary is not sufficient.
RELEASE_PUB_B64="${OMNODA_RELEASE_PUB:-/lAwmdAmddtlLwrmLv3P3+c82yUaMuYeJO7FW4LQW/k=}"

while [ $# -gt 0 ]; do
  case "$1" in
    --server) SERVER=$2; shift 2 ;;
    --code) CODE=$2; shift 2 ;;
    --name) NAME=$2; shift 2 ;;
    --workspace) WORKSPACE=$2; shift 2 ;;
    --label) LABEL=$2; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$SERVER" ] || [ -z "$CODE" ]; then
  echo "usage: install.sh --server URL --code CODE" >&2
  exit 2
fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported os: $os (use install.ps1 on Windows)" >&2; exit 1 ;;
esac

asset="omnoda_${VERSION}_${os}_${arch}"
url="${SERVER%/}/releases/${asset}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

download() {
  i=0
  while [ "$i" -lt 3 ]; do
    if curl -fsSL "$url" -o "$tmp/omnoda" && curl -fsSL "$url.sig" -o "$tmp/omnoda.sig"; then
      return 0
    fi
    i=$((i+1))
    sleep $((i*2))
  done
  echo "download failed: $url" >&2
  return 1
}

download

if [ "$RELEASE_PUB_B64" = "/lAwmdAmddtlLwrmLv3P3+c82yUaMuYeJO7FW4LQW/k=" ]; then
  echo "installer has no release trust anchor; refusing to run an unsigned binary" >&2
  exit 1
fi

# Verify with openssl if it can do raw Ed25519. Otherwise use the previously
# installed omnoda-verify. We never execute the downloaded omnoda before this check.
if ! printf '%s' "$RELEASE_PUB_B64" | base64 -d > "$tmp/pub.raw" 2>/dev/null; then
  printf '%s' "$RELEASE_PUB_B64" | openssl base64 -d -A > "$tmp/pub.raw"
fi
# OpenSSL 3 pkeyutl verifies raw Ed25519 when given a PEM. Build a temporary PEM.
if command -v python3 >/dev/null 2>&1; then
  python3 - "$tmp/pub.raw" "$tmp/pub.pem" << 'PY'
import base64, pathlib, sys
raw = pathlib.Path(sys.argv[1]).read_bytes()
# PKCS8 prefix for Ed25519 public key
prefix = bytes.fromhex("302a300506032b6570032100")
der = prefix + raw
pem = "-----BEGIN PUBLIC KEY-----\n" + base64.encodebytes(der).decode() + "-----END PUBLIC KEY-----\n"
pathlib.Path(sys.argv[2]).write_text(pem)
PY
  sig=$(tr -d '\n' < "$tmp/omnoda.sig")
  printf '%s' "$sig" | openssl base64 -d -A > "$tmp/omnoda.sig.bin"
  if ! openssl pkeyutl -verify -pubin -inkey "$tmp/pub.pem" -rawin -in "$tmp/omnoda" -sigfile "$tmp/omnoda.sig.bin" >/dev/null 2>&1; then
    echo "signature verification failed" >&2
    exit 1
  fi
else
  echo "python3 is required to verify the release signature" >&2
  exit 1
fi

install_atomic() {
  mkdir -p "$BIN_DIR"
  stage="$BIN_DIR/omnoda.new"
  prev="$BIN_DIR/omnoda.prev"
  cp "$tmp/omnoda" "$stage"
  chmod 755 "$stage"
  if [ -f "$BIN_DIR/omnoda" ]; then
    cp "$BIN_DIR/omnoda" "$prev"
  fi
  mv "$stage" "$BIN_DIR/omnoda"
  if ! "$BIN_DIR/omnoda" version >/dev/null 2>&1; then
    echo "new binary failed version check; rolling back" >&2
    if [ -f "$prev" ]; then mv "$prev" "$BIN_DIR/omnoda"; fi
    exit 1
  fi
}

install_atomic

join_args="--server $SERVER --code $CODE"
if [ -n "$NAME" ]; then join_args="$join_args --name $NAME"; fi
if [ -n "$WORKSPACE" ]; then join_args="$join_args --workspace $WORKSPACE"; fi
if [ -n "$LABEL" ]; then join_args="$join_args --label $LABEL"; fi
# shellcheck disable=SC2086
"$BIN_DIR/omnoda" join $join_args

# User session service. This is not a system-wide boot service.
# A system service needs an explicit admin install (see docs/installation.md).
if [ "$os" = "linux" ] && command -v systemctl >/dev/null 2>&1 && [ -n "${XDG_RUNTIME_DIR:-}" ]; then
  mkdir -p "${HOME}/.config/systemd/user"
  cat > "${HOME}/.config/systemd/user/omnoda.service" << EOF
[Unit]
Description=Omnoda node
After=network-online.target

[Service]
ExecStart=${BIN_DIR}/omnoda start
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload || true
  systemctl --user enable --now omnoda.service || "$BIN_DIR/omnoda" start >/dev/null 2>&1 &
elif [ "$os" = "darwin" ]; then
  mkdir -p "${HOME}/Library/LaunchAgents"
  cat > "${HOME}/Library/LaunchAgents/dev.omnoda.node.plist" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>dev.omnoda.node</string>
  <key>ProgramArguments</key><array><string>${BIN_DIR}/omnoda</string><string>start</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
EOF
  launchctl bootstrap "gui/$(id -u)" "${HOME}/Library/LaunchAgents/dev.omnoda.node.plist" || "$BIN_DIR/omnoda" start >/dev/null 2>&1 &
else
  "$BIN_DIR/omnoda" start >/dev/null 2>&1 &
fi

echo "Omnoda node installed for this user. Config: $CONFIG_DIR"
