#!/bin/sh
# Sign a release file with the minisign-compatible Ed25519 raw signature used by install.sh.
# Usage: scripts/release-sign.sh <file> <private-key-file>
# Private key file is 64 raw bytes (seed || public) or a 32-byte seed.
set -eu
file=$1
key=$2
out=$file.sig
# Implemented by the omnoda-sign helper so the script does not embed a second crypto stack.
exec "$(dirname "$0")/../bin/omnoda-sign" -key "$key" -file "$file" -out "$out"
