# Installing from a text screen or a serial console

The boot menu has three choices:

1. **Install Gatehouse**: the normal graphical installer, finished from a web browser on another computer.
2. **Install Gatehouse (basic graphics)**: the same, for screens that show nothing with the first one.
3. **Install Gatehouse (text mode / serial console: 115200 baud, 8N1)**: asks the same questions on the machine itself. No second computer, no network and no setup code are needed.

Use the third choice for a machine with a plain text screen, one with no screen at all, or one you reach through a serial cable or a management card.

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
2. Your network: the port for your own network, an optional internet port (automatic, username and password, or fixed address), and how Gatehouse is used on your network. Every question has a suggested answer in brackets, so pressing Enter is enough for the usual setup. For fixed addresses, the gateway is guessed from the address you type.
3. Device name and time zone (type part of a name to search).
4. Administrator name and password, and whether to require two-step sign-in.
5. A review. You confirm by typing the disk name; anything else starts over.
6. Progress, then a prompt to remove the USB stick and restart.

Ctrl-C at any question starts over. Nothing on the disk changes until the review is confirmed.

After the restart the installed system does not use a serial console. The local screen shows the address to open in a browser, as usual.
