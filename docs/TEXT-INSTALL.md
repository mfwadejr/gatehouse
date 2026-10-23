# Installing from a text screen or a serial console

The boot menu has four choices:

1. **Install Gatehouse**: installs on this machine, using its own screen and keyboard.
2. **Install Gatehouse (basic graphics)**: the same, for screens that show nothing with the first one.
3. **Install from another computer**: the machine shows an address and a code, and you choose the disk in a web browser.
4. **Install with serial console (115200 8N1)**: for machines with no screen, over a serial cable.

Every choice asks only for the disk. The administrator account, networks, name and time zone are set in a browser after the machine restarts, so there is nothing to write down.

Use the fourth choice for a machine with no screen, or one you reach through a serial cable or a management card.

## Serial settings

| Setting | Value |
|---|---|
| Port | first serial port (COM1, `ttyS0`) |
| Speed | 115200 baud |
| Data bits / parity / stop bits | 8 / none / 1 (written 8N1) |
| Flow control | none |
| Terminal type | VT100 or ANSI (any terminal program) |

Examples from a computer with a USB serial adapter:

```
screen /dev/ttyUSB0 115200            # Linux or macOS (macOS: /dev/tty.usbserial-*)
minicom -D /dev/ttyUSB0 -b 115200
```

On Windows, PuTTY: Connection type *Serial*, line `COM3` (check Device Manager), speed `115200`.

The text installer answers on the machine's own screen and keyboard **and** on the serial port at the same time. Use whichever you have.

## Choosing the entry on a machine with no screen

The boot menu is also sent to the serial port, but it only waits four seconds. On a machine with no screen, press the down arrow twice and then Enter as soon as the menu appears (or blindly, a few seconds after the machine starts). On a BIOS machine the menu shows on the serial port only if the firmware's serial redirection is switched on.

## The questions

1. Which disk to install on. The USB stick you started from is never offered.
2. A confirmation: type the disk name; anything else starts over.
3. Progress, then a prompt to remove the USB stick and restart.

Ctrl-C at any question starts over. Nothing on the disk changes until you confirm.

After the restart the machine's screen shows an address. Open it in a browser on the same network to create your administrator account and finish setup. The installed system does not use a serial console.
