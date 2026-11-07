#!/bin/sh
# Lays the NopeWarden boot menus over a boot-loader tree.
#
#   brand-boot.sh DIR ARTDIR
#
# DIR holds isolinux/ and boot/grub/ (an unpacked ISO, or just those two folders); ARTDIR holds
# splash.png and theme/. Safe to run twice. Nothing it writes names any other product.
set -e
# Serial settings come from serial.conf (speed, port, data bits, parity, stop bits, terminal).
. "$(dirname "$0")/serial.conf"
# A non-standard serial speed marks an image for serial-only devices: the serial choice is the one
# already highlighted, so Enter is enough and no arrow key is needed.
GRUB_DEFAULT_ENTRY=0
[ "$SERIAL_SPEED" = "115200" ] || GRUB_DEFAULT_ENTRY=2
# The menu is hidden and the installer starts by itself after 2 seconds. Esc shows the menu (for the
# basic-graphics choice if a screen stays blank). The serial image starts its serial choice.
GRUB_TIMEOUT=2
ISO_TIMEOUT=$((GRUB_TIMEOUT * 10))
ISO_DEFAULT=nopewarden
[ "$GRUB_DEFAULT_ENTRY" = "0" ] || ISO_DEFAULT=serial
D=${1:?usage: brand-boot.sh DIR ARTDIR}
ART=${2:?usage: brand-boot.sh DIR ARTDIR}
EXTRA="rd.udev.log_level=0 consoleblank=0 nopewarden.serial=$SERIAL_SPEED"
# What an earlier run added; stripped so a second run does not stack it up.
# Boot-menu variables (like findiso=${iso_path}) are dropped too: the new menu does not set them.
unextra() { sed -E 's/ (rd\.udev\.log_level=0|consoleblank=0|nopewarden\.serial=[0-9]+|nopewarden\.text|nomodeset)//g; s/ [^ ]*$\{[^ ]*\}//g'; }

[ -d "$D/isolinux" ] && cp "$ART/splash.png" "$D/isolinux/splash.png"
if [ -d "$D/boot/grub" ]; then
	rm -rf "$D/boot/grub/theme"
	cp -r "$ART/theme" "$D/boot/grub/theme"
fi

# ---- BIOS (isolinux / syslinux) ----
if [ -f "$D"/isolinux/live.cfg ]; then
	K=$(awk '$1=="linux"||$1=="kernel"{print $2; exit}' "$D"/isolinux/live.cfg)
	I=$(awk '$1=="initrd"{print $2; exit}' "$D"/isolinux/live.cfg)
	A=$(awk '$1=="append"{$1=""; print; exit}' "$D"/isolinux/live.cfg | unextra)
	# The graphical menu module; the image normally has it already.
	for m in vesamenu.c32 libcom32.c32 libutil.c32 ldlinux.c32; do
		if [ ! -f "$D/isolinux/$m" ]; then
			for d in /usr/lib/syslinux/modules/bios /usr/share/syslinux; do
				[ -f "$d/$m" ] && cp "$d/$m" "$D"/isolinux/ && break
			done
		fi
	done
	if [ -n "$K" ] && [ -n "$I" ]; then
		SAFE=$(echo "$A" | sed 's/ splash//')
		cat > "$D"/isolinux/isolinux.cfg <<CFG
path
prompt 0
menu hidden
noescape 1
timeout $ISO_TIMEOUT
ui vesamenu.c32
default $ISO_DEFAULT
menu title
menu background splash.png
menu margin 0
menu hshift 17
menu vshift 17
menu width 46
menu rows 3
menu tabmsg Choose with the arrow keys, then press Enter to continue
menu tabmsgrow 22
menu color screen 0 #ffc3c2b7 #00000000 none
menu color border 0 #00000000 #00000000 none
menu color title 0 #fff0efec #00000000 none
menu color unsel 0 #ffc3c2b7 #00000000 none
menu color sel 0 #ffffffff #ff3987e5 none
menu color hotsel 0 #ffffffff #ff3987e5 none
menu color hotkey 0 #ffc3c2b7 #00000000 none
menu color timeout 0 #ff898781 #00000000 none
menu color timeout_msg 0 #ff898781 #00000000 none
menu color tabmsg 0 #ff898781 #00000000 none
menu color cmdmark 0 #00000000 #00000000 none
menu color cmdline 0 #00000000 #00000000 none

label nopewarden
  menu label Install NopeWarden  (press Enter to start)
  linux $K
  initrd $I
  append $A $EXTRA nopewarden.text

label basic
  menu label Install NopeWarden (basic graphics)
  linux $K
  initrd $I
  append $SAFE $EXTRA nomodeset nopewarden.text

label serial
  menu label Install with serial console ($SERIAL_SPEED 8N1)
  text help
  For machines with no screen. Set your terminal program (PuTTY, screen, minicom) to
  $SERIAL_SPEED baud, 8 data bits, no parity, 1 stop bit, no flow control, VT100.
  The installer shows on the first serial port; this choice also sends the start-up messages there.
  endtext
  linux $K
  initrd $I
  append $SAFE $EXTRA nomodeset nopewarden.text console=tty0 console=ttyS$SERIAL_UNIT,${SERIAL_SPEED}n8
CFG
	fi
fi

# ---- UEFI, and BIOS machines that start GRUB ----
if [ -d "$D"/boot/grub ]; then
	L=$(grep -h -m1 -E '^[[:space:]]*linux[[:space:]]' "$D"/boot/grub/*.cfg | head -n1 | sed -E 's/^[[:space:]]*linux[[:space:]]+//' | unextra)
	I=$(grep -h -m1 -E '^[[:space:]]*initrd[[:space:]]' "$D"/boot/grub/*.cfg | head -n1 | sed -E 's/^[[:space:]]*initrd[[:space:]]+//')
	# Keep whatever tells GRUB which disk the image is on.
	KEEP=$(grep -h -E '^[[:space:]]*(search|set root)' "$D"/boot/grub/*.cfg 2>/dev/null | head -n3 || true)
	for f in chroot/usr/share/grub/unicode.pf2 /usr/share/grub/unicode.pf2; do
		[ -f "$f" ] && cp "$f" "$D"/boot/grub/unicode.pf2 && break
	done
	if [ -n "$L" ] && [ -n "$I" ]; then
		SAFE=$(echo "$L" | sed 's/ splash//')
		cat > "$D"/boot/grub/grub.cfg <<CFG
$KEEP
set default=$GRUB_DEFAULT_ENTRY
set timeout=$GRUB_TIMEOUT
set timeout_style=hidden
# On a serial console the menu is plain text: make the chosen entry stand out.
set menu_color_normal=light-gray/black
set menu_color_highlight=black/light-gray
insmod all_video
insmod gfxterm
insmod png
if [ -f /boot/grub/unicode.pf2 ]; then
  loadfont /boot/grub/unicode.pf2
fi
set gfxmode=auto
set gfxpayload=keep
terminal_output gfxterm
if serial --unit=$SERIAL_UNIT --speed=$SERIAL_SPEED --word=$SERIAL_WORD --parity=$SERIAL_PARITY --stop=$SERIAL_STOP; then
  terminal_input console serial
  terminal_output gfxterm serial
fi
set theme=/boot/grub/theme/theme.txt

menuentry --hotkey=i "Install NopeWarden  (press i)" {
  linux $L $EXTRA nopewarden.text
  initrd $I
}
menuentry --hotkey=b "Install NopeWarden (basic graphics, press b)" {
  linux $SAFE $EXTRA nomodeset nopewarden.text
  initrd $I
}
menuentry --hotkey=s "Install with serial console ($SERIAL_SPEED 8N1, VT100, press s)" {
  linux $SAFE $EXTRA nomodeset nopewarden.text console=tty0 console=ttyS$SERIAL_UNIT,${SERIAL_SPEED}n8
  initrd $I
}
CFG
	fi
fi
