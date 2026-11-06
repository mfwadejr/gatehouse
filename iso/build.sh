#!/bin/sh
# Build the NopeWarden installer image.
#
#   iso/build.sh /path/to/nopewarden [version]
#
# Run as root on Debian with live-build installed (the GitHub workflow runs it in a
# Debian 13 container). The result is out/nopewarden-<version>-amd64.iso and its checksum.
set -eu

BIN=${1:?usage: iso/build.sh /path/to/nopewarden-binary [version]}
VERSION=${2:-dev}
SRC=$(cd "$(dirname "$0")" && pwd)
OUT=${OUT:-$(pwd)/out}
WORK=$(mktemp -d /tmp/nopewarden-live.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

cp -a "$SRC/." "$WORK/"
install -D -m 0755 "$BIN" "$WORK/config/includes.chroot/usr/local/bin/nopewarden"
echo "$VERSION" > "$WORK/config/includes.chroot/etc/nopewarden-release"

cd "$WORK"
lb clean --purge >/dev/null 2>&1 || true
lb config
lb build

# Size report: what the image costs, biggest installed packages first. Kept beside the image.
mkdir -p "$OUT"
{
  echo "NopeWarden $VERSION image size report"
  echo
  echo "Largest installed packages (KiB):"
  chroot "$WORK/chroot" dpkg-query -W -f='${Installed-Size}\t${Package}\n' 2>/dev/null | sort -rn | head -25
  echo
  echo "Image parts:"
  ls -l "$WORK"/binary/live/*.squashfs 2>/dev/null | awk '{printf "%d MiB  %s\n", $5/1048576, $9}'
  ls -l "$WORK"/*.iso | awk '{printf "%d MiB  %s\n", $5/1048576, $9}'
} > "$OUT/size-report.txt"

ISO=$(ls "$WORK"/*.iso | head -n1)
# Put the NopeWarden boot menus on the finished image and fail the build if anything else shows.
sh "$SRC/brand-iso.sh" "$ISO" "$SRC/config/includes.binary/nopewarden-boot"
mkdir -p "$OUT"
NAME="nopewarden-$VERSION-amd64.iso"
mv "$ISO" "$OUT/$NAME"
(cd "$OUT" && sha256sum "$NAME" > "$NAME.sha256")
echo "built $OUT/$NAME"
