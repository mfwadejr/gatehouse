# Keeping access: watchdog, console port, lock-out protection

## Watchdog
- `gatehouse.service` is `Type=notify` with `WatchdogSec=60`. The program says "alive" every 10 seconds only while the console answers on its port and nothing refuses connections on the DNS port. If that stops, systemd restarts it. A slow or offline DNS upstream is not a failure.
- The console now `Wants=` the network helper instead of `Requires=` it, so a helper restart no longer takes the console down. Neither service has a start limit; both restart after 2 seconds.
- Each automatic restart is written to the Gatehouse log ("The console restarted by itself"). After 3 restarts without the console answering, the machine's screen says "The console keeps stopping" and points to Alt+F2, option 4.
- The firewall lives in the kernel and keeps protecting the network while the program restarts. No hardware watchdog is used.

## Changeable console port
Settings, Console address. The new port is opened next to the old one and the firewall allows both. The change is final only when the console is reached on the new port and "Keep this port" is pressed there (120 seconds); otherwise it is undone. Ports used by DNS, DHCP, SSH, NTP, UPnP, a VPN or a port forward are refused, as is any port already in use. The saved port is copied to `console.port` in the data folder for the machine's screens and the starting firewall rules. Recovery option 5 puts it back to 443.

## Lock-out protection
- A built-in rule lets every management network reach the console; it sits above all editable rules.
- The port the device was first set up from (recorded when the first account is created) always reaches the console, even if networks are edited wrongly. It is never opened on an internet-facing port.
- Firewall page, "Protected" block: shows these rules in plain words; "Test my access" checks the saved firewall, your own connection, the address limit and that the console answers.
- Optional limit on who may manage (networks and/or address ranges). Empty means every management network. Saving is refused if it would cut off the address you are using.
- Applying a firewall change is still refused if it would disconnect you, and still rolls back unless confirmed.
- Recovery (Alt+F2): option 3 restores the safe starting rules; option 5 also removes the address limit and resets the console port to 443.
