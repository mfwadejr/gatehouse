# Installing from a text screen or a serial console

The boot menu has three choices:

1. **Install Gatehouse**: installs on this machine, using its own screen and keyboard.
2. **Install Gatehouse (basic graphics)**: the same, for screens that show nothing with the first one.
3. **Install with serial console (115200 8N1)**: for machines with no screen, over a serial cable.

(The old "install from another computer" choice and its code are gone: the machine asks two questions on its own screen, and everything else is done in a browser after the restart.)

Every choice asks only for the disk and for the network port you will manage Gatehouse from. The administrator account, networks, name and time zone are set in a browser after the machine restarts, so there is nothing to write down and no code to type.

Use the third choice for a machine with no screen, or one you reach through a serial cable or a management card.

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
2. Which network port you will manage Gatehouse from. Plug a cable into it; the list updates by itself and shows the cable as soon as it is connected. A port with a cable is required (you can insist on one without, after a warning).
3. A confirmation: type the disk name; anything else starts over.
4. Progress, then "Remove the USB stick now, then press Enter to restart."

Ctrl-C at any question starts over. Nothing on the disk changes until you confirm.

After the restart the machine's screen shows an address. Open it in a browser on a computer connected to the port you chose, to create your administrator account and finish setup. The installed system does not use a serial console.

## First start after the installation

The port you chose asks the network for an address like any wired port. If no router answers within about 30 seconds, Gatehouse gives that port the address **192.168.100.1** and hands out addresses on it, so a laptop plugged straight into the machine can open https://192.168.100.1. The screen always shows the address to use.

Until the administrator account exists, the setup page only answers on the management port. Once the account exists the setup page is closed for good and the address shows the normal sign-in page. The temporary address goes away when setup is finished.
