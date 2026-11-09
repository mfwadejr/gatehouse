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

1. **The internet port (WAN).** Choose the port your modem or internet router plugs into. You always choose it yourself; nothing is picked for you. It may be unplugged for now. Beside each connected port the installer says whether a DHCP server answers on it (it listens for up to eight seconds, in the background, sending its question again every two seconds because some routers answer slowly, and never asks for an address; it also reads the address the live system already got on that port, which names the server that gave it). An internet connection normally has one.
2. **Your own network port (LAN).** Plug a cable into it; the list updates by itself and shows the cable as soon as it is connected. A port with a cable is required (you can insist on one without, after a warning). Only one LAN port is chosen here. Other ports are set up in the browser after the restart: a second internet connection, separate LAN or VLAN networks, or extra ports in the main LAN.
3. **NopeWarden's address on that network** (the suggestion is fine), which never overlaps the network the internet port is on.
4. **Whether NopeWarden hands out addresses (DHCP) on that network.** The default is yes. If another DHCP server answered on the LAN port, the default is no and the screen says "DHCP is off because another DHCP server (address) answered on the port". You can answer yes anyway, and change it later in setup. If the port already has an address but nothing answered the check, the installer does not say "no server": it says the port already has an address from a DHCP server that did not answer, turns DHCP off by default and lets you turn it on. If the check could not run at all, the screen says so and the default stays yes.
5. **The disk.** The USB stick you started from is never offered. You must type its number.
6. **A confirmation:** type the disk name; anything else starts over.
7. Progress, then "Remove the USB stick now, then press Enter to restart."

Ctrl-C at any question starts over. Nothing on the disk changes until you confirm.

### The same network on both ports

If the same DHCP server answers on both the internet port and the LAN port, the two ports are on one network. This happens in a virtual machine when both network adapters are attached to the same bridge as your home network. NopeWarden only protects a network that traffic must pass through it to leave, so the installer warns you. It does not stop you, because a test setup may want this. For real use, or a useful test, put the LAN adapter on its own virtual network.

After the restart the machine's screen shows an address. Open it in a browser on a computer connected to the port you chose, to create your administrator account and finish setup. The setup wizard opens with your choices filled in (the internet port, the address, and DHCP turned off if you turned it off), and you confirm or change them. The installed system does not use a serial console.

## What the screens look like

Notes and hints on the screens are drawn in the normal text colour, not the dim grey some terminals use, so they read on any terminal. On a monitor the installer shows the NopeWarden badge. On a serial console it shows only the name, because the badge is drawn with block characters that serial terminals do not draw cleanly. Screens are centred whatever the screen size. Over a serial cable the installer asks the terminal how big it is and uses that, or 80 by 24 if the terminal does not answer. If a screen is too short for everything, the badge is dropped first, so the name and the choices always show.

## First start after the installation

The port you chose gets the address you chose in the installer, and until setup is finished NopeWarden hands out addresses on it so a laptop plugged straight in can open the setup page. If you turned DHCP off in the installer, it does not hand out any: set a fixed address on your computer in the same network, or use an address from the other DHCP server on that network, and open the address shown on the machine's screen. (If the installer was run without choosing an address, the port asks the network for one, and if no router answers within about 30 seconds NopeWarden gives it **192.168.100.1** and hands out addresses so a laptop plugged straight in can open https://192.168.100.1.) The screen always shows the address to use.

Until the administrator account exists, the setup page only answers on the management port. Once the account exists the setup page is closed for good and the address shows the normal sign-in page. The temporary address goes away when setup is finished.

## "No disk to install on was found"

The installer lists every disk of 4 GB or more except the USB stick you started from. If it finds none, the screen now shows what the machine can see: the disks it found (and any too small), and each storage controller with the driver in use. Reading it:

- **A controller says "none loaded":** the system has no driver for it. Send the lines you see.
- **No controller listed, or no disks at all, though the BIOS shows them:** set the BIOS disk controller to AHCI (not RAID or a vendor mode), save, and start again.
- **A disk is listed as "under 4 GB":** it is too small to install on.
