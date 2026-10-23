#!/bin/sh
# Build the Gatehouse installer image.
#
#   iso/build.sh /path/to/gatehouse [version]
#
# Run as root on Debian with live-build installed (the GitHub workflow runs it in a
# Debian 13 container). The result is out/gatehouse-<version>-amd64.iso and its checksum.
set -eu

BIN=${1:?usage: iso/build.sh /path/to/gatehouse-binary [version]}
VERSION=${2:-dev}
SRC=$(cd "$(dirname "$0")" && pwd)
OUT=${OUT:-$(pwd)/out}
WORK=$(mktemp -d /tmp/gatehouse-live.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

cp -a "$SRC/." "$WORK/"
install -D -m 0755 "$BIN" "$WORK/config/includes.chroot/usr/local/bin/gatehouse"
echo "$VERSION" > "$WORK/config/includes.chroot/etc/gatehouse-release"

cd "$WORK"
lb clean --purge >/dev/null 2>&1 || true
lb config
lb build

mkdir -p "$OUT"
ISO=$(ls "$WORK"/*.iso | head -n1)
NAME="gatehouse-$VERSION-amd64.iso"
mv "$ISO" "$OUT/$NAME"
(cd "$OUT" && sha256sum "$NAME" > "$NAME.sha256")
echo "built $OUT/$NAME"
