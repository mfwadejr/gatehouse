#!/bin/sh
# Builds gh-ndpi as a program with no runtime dependencies (the libraries are linked in), or
# builds nothing. A failure here is not an error: the image is made without it and Gatehouse
# keeps naming applications from website names. Usage: build.sh OUTPUT_DIR
out="${1:?output directory}"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$out"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
for api in "" "-DGH_NDPI_NEWAPI"; do
  if gcc -O2 -Wall -Wno-deprecated-declarations $api -o "$tmp" "$here/gh-ndpi.c" \
       -Wl,-Bstatic -lndpi -lpcap -Wl,-Bdynamic -lm -lpthread >/dev/null 2>&1 \
     || gcc -O2 -Wall -Wno-deprecated-declarations $api -o "$tmp" "$here/gh-ndpi.c" \
       $(pkg-config --cflags --libs libndpi libpcap 2>/dev/null) -lm -lpthread >/dev/null 2>&1; then
    # Only ship it if it starts and reads a capture without crashing.
    if ldd "$tmp" 2>/dev/null | grep -qE 'libndpi|libpcap'; then
      echo "gh-ndpi: built, but it needs libraries the image does not have; skipped"; exit 0
    fi
    install -m 0755 "$tmp" "$out/gh-ndpi" && echo "gh-ndpi: built ($api)" && exit 0
  fi
done
echo "gh-ndpi: could not be built here; the image is made without it"
exit 0
