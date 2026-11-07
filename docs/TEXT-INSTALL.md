# Installing from a text screen or a serial console

The boot menu has three choices:

1. **Install NopeWarden**: installs on this machine, using its own screen and keyboard.
2. **Install NopeWarden (basic graphics)**: the same, for screens that show nothing with the first one.
3. **Install with serial console (115200 8N1, VT100)**: for machines with no screen, over a serial cable. The speed in the name is the one speed for the whole menu and the installer; set your terminal program to it.

(The old "install from another computer" choice and its code are gone: the machine asks two questions on its own screen, and everything else is done in a browser after the restart.)

Every choice asks only for the disk and for the network port you will manage NopeWarden from. The administrator account, networks, name and time zone are set in a browser after the machine restarts, so there is nothing to write down and no code to type.

Use the third choice for a machine with no screen, or one you reach through a serial cable or a management card.

## Serial settings

These are written in one place, `iso/serial.conf`, and read by the build. The device, your terminal program and the build must all agree. GRUB (the boot menu) cannot detect a speed, so over a cable set up differently you will see nothing or garbage, or the menu will not take your keys.

| Setting | Value (default build) | Where it is set |
|---|---|---|
| Port | first serial port (COM1, `ttyS0`) | `SERIAL_UNIT=0` |
| Speed | 115200 baud | `SERIAL_SPEED=115200` |
| Data bits / parity / stop bits | 8 / none / 1 (8N1) | `SERIAL_WORD`, `SERIAL_PARITY`, `SERIAL_STOP` |
| Flow control | none | set the same on the device and in your terminal program |
| Terminal type | VT100 (arrow keys are `Esc [ A`) | `SERIAL_TERM`; set the same on the device |

**One speed.** Like other firewall distributions, NopeWarden uses 115200 for every image and every menu choice, and you never choose a speed. If your device's own console redirection (in its BIOS) is set to another speed, change it there to 115200. To build another speed, change `iso/serial.conf` or set the same names for one build, for example `SERIAL_SPEED=9600 iso/build.sh ./nopewarden 0.9.4` (the file name then ends `-serial9600`).

The boot menu, the installer and the start-up messages all use that one speed, so there is nothing to choose: set PuTTY, `screen` or minicom to 115200 and it works on every choice.

If the menu shows but ignores your keys, or shows garbage, check in this order: speed, 8N1, no flow control, VT100 on the device and in your terminal program. In the menu `Ctrl-N` and `Ctrl-P` move down and up, and `Ctrl-X` boots while editing an entry; they work even when arrow keys do not.

Examples from a computer with a USB serial adapter (use your speed):

```
screen /dev/tty.usbserial-110 115200    # macOS; on Linux /dev/ttyUSB0
minicom -D /dev/ttyUSB0 -b 115200
sudo cu -l /dev/tty.usbserial-110 -s 115200     # raw, sends keys as typed; quit with ~.
```

On Windows, PuTTY: Connection type *Serial*, line `COM3` (check Device Manager), speed 115200.

The text installer answers on the machine's own screen and keyboard **and** on the serial port at the same time. Use whichever you have.

## Choosing the entry on a machine with no screen

You do not need to press anything. The boot menu is hidden and the installer starts by itself after 2 seconds. If a screen stays blank, press `Esc` as the machine starts to show the menu and pick "basic graphics" (`b`). Over a serial cable, arrow keys can fail: an arrow is three bytes (`Esc [ B`) and a USB serial adapter sometimes delivers them apart, so GRUB reads a lone Escape and shows `[B` as typed text. Single keys do not have this problem, so each entry has one:

- `i`: Install NopeWarden (screen and keyboard)
- `b`: Install NopeWarden (basic graphics)
- `s`: Install with serial console

Press the letter; if the entry is only highlighted, press Enter. `Ctrl-N` and `Ctrl-P` (down, up) also work. On a BIOS machine the menu shows on the serial port only if the firmware's serial redirection is switched on.

## The questions

1. Which disk to install on. The USB stick you started from is never offered.
2. Which network port you will manage NopeWarden from. Plug a cable into it; the list updates by itself and shows the cable as soon as it is connected. A port with a cable is required (you can insist on one without, after a warning).
3. A confirmation: type the disk name; anything else starts over.
4. Progress, then "Remove the USB stick now, then press Enter to restart."

Ctrl-C at any question starts over. Nothing on the disk changes until you confirm.

After the restart the machine's screen shows an address. Open it in a browser on a computer connected to the port you chose, to create your administrator account and finish setup. The installed system does not use a serial console.

## First start after the installation

The port you chose asks the network for an address like any wired port. If no router answers within about 30 seconds, NopeWarden gives that port the address **192.168.100.1** and hands out addresses on it, so a laptop plugged straight into the machine can open https://192.168.100.1. The screen always shows the address to use.

Until the administrator account exists, the setup page only answers on the management port. Once the account exists the setup page is closed for good and the address shows the normal sign-in page. The temporary address goes away when setup is finished.

## "No disk to install on was found"

The installer lists every disk of 4 GB or more except the USB stick you started from. If it finds none, the screen now shows what the machine can see: the disks it found (and any too small), and each storage controller with the driver in use. Reading it:

- **A controller says "none loaded":** the system has no driver for it. Send the lines you see.
- **No controller listed, or no disks at all, though the BIOS shows them:** set the BIOS disk controller to AHCI (not RAID or a vendor mode), save, and start again.
- **A disk is listed as "under 4 GB":** it is too small to install on.
