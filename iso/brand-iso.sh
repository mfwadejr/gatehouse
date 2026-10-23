#!/bin/sh
# Puts the Gatehouse boot menus on a finished image and checks the result.
#
#   brand-iso.sh IMAGE.iso ARTDIR
#
# This is the step that guarantees nobody sees another product's boot menu: it works on the
# final image, whatever the build tools did earlier, and it fails the build if anything else
# is still on the boot screens.
set -eu
ISO=${1:?usage: brand-iso.sh IMAGE.iso ARTDIR}
ART=${2:?usage: brand-iso.sh IMAGE.iso ARTDIR}
HERE=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

xorriso -osirrox on -indev "$ISO" -extract /isolinux "$T/isolinux" -extract /boot/grub "$T/boot/grub" >/dev/null 2>"$T/log" || { cat "$T/log"; exit 1; }
chmod -R u+w "$T"
[ -f "$T/isolinux/isolinux.bin" ] || { echo "brand-iso: no BIOS boot loader found in the image"; exit 1; }
[ -d "$T/boot/grub" ] || { echo "brand-iso: no UEFI boot loader found in the image"; exit 1; }

HAD_THEME=no
[ -d "$T/boot/grub/theme" ] && HAD_THEME=yes
# Only the menu files change; the boot loader itself and the UEFI image are left exactly as built.
rm -f "$T/boot/grub/efi.img" "$T/isolinux/isolinux.bin"
sh "$HERE/brand-boot.sh" "$T" "$ART"

rm -f "$ISO.new"
set -- -indev "$ISO" -outdev "$ISO.new" -boot_image any replay -overwrite on
[ "$HAD_THEME" = yes ] && set -- "$@" -rm_r /boot/grub/theme --
set -- "$@" -map "$T/isolinux/isolinux.cfg" /isolinux/isolinux.cfg -map "$T/isolinux/splash.png" /isolinux/splash.png
set -- "$@" -map "$T/boot/grub/grub.cfg" /boot/grub/grub.cfg -map "$T/boot/grub/theme" /boot/grub/theme
[ -f "$T/boot/grub/unicode.pf2" ] && set -- "$@" -map "$T/boot/grub/unicode.pf2" /boot/grub/unicode.pf2
xorriso "$@" -commit >/dev/null 2>"$T/log" || { cat "$T/log"; exit 1; }

# Both ways of starting the image must still be there.
xorriso -indev "$ISO" -report_el_torito plain 2>/dev/null | awk '/El Torito boot img/{print $6, $7, $8}' >"$T/before" || true
xorriso -indev "$ISO.new" -report_el_torito plain 2>/dev/null | awk '/El Torito boot img/{print $6, $7, $8}' >"$T/after" || true
cmp -s "$T/before" "$T/after" || { echo "brand-iso: the boot setup changed"; diff "$T/before" "$T/after" || true; exit 1; }

# Nothing on a boot screen may name another product.
V=$(mktemp -d)
xorriso -osirrox on -indev "$ISO.new" -extract /isolinux/isolinux.cfg "$V/isolinux.cfg" -extract /boot/grub/grub.cfg "$V/grub.cfg" -extract /isolinux/splash.png "$V/splash.png" >/dev/null 2>&1
for f in isolinux.cfg grub.cfg; do
	if [ ! -s "$V/$f" ] || grep -n -i -E 'debian|gnu|live system|live-build|utilities' "$V/$f"; then
		echo "brand-iso: $f is missing or still has stock text"; rm -rf "$V"; exit 1
	fi
done
cmp -s "$V/splash.png" "$ART/splash.png" || { echo "brand-iso: the boot picture was not replaced"; exit 1; }
rm -rf "$V"
mv "$ISO.new" "$ISO"
echo "brand-iso: boot menus branded and checked"
