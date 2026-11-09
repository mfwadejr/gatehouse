# Hardware acceptance table

Run this on each machine before trusting a release on it. Tick a row only after you have seen it work.
Record the machine, the version (Settings, About) and the date at the top of each copy.

| # | Check | How | Machine 1 | Machine 2 |
|---|-------|-----|-----------|-----------|
| 1 | Installs and boots | Install from the USB stick; the console opens at its address | | |
| 2 | Every network port has a cable light and a speed | Networks page shows each port as Connected with its speed | | |
| 3 | Internet works | Internet page says Connected; a device on the LAN opens a website | | |
| 4 | DHCP hands out an address | A new device gets one in the right range | | |
| 5 | Keep address | Devices, Keep address on a row; unplug and reconnect, same address; Release address | | |
| 6 | DNS answers and blocks | A blocked name is refused; an allowed name resolves | | |
| 7 | Firewall rule applies | Add a rule with Save and apply; the change goes live and is kept by itself; traffic changes as expected; rollback after not confirming | | |
| 8 | Pause a rule | Pause for 5 minutes; traffic changes; it comes back by itself | | |
| 9 | Port forward | Open a port to a device; reach it from outside or a second network | | |
| 10 | Route by port through a VPN | Route one service (for example NTP) through a VPN; check the exit address | | |
| 11 | Route through connections in order | Route through a main and a backup connection; unplug the main; traffic moves to the backup and returns | | |
| 12 | Create policy | Policies page, Create policy opens the right form for each type | | |
| 13 | Schedule | A rule or route with a time window works inside it and not outside it | | |
| 14 | Admin activity | Logs, Admin activity lists each change above, including pause and resume | | |
| 15 | Backup and restore | Download a backup, restore it; routes, pauses and schedules are the same | | |
| 16 | Survives a reboot | Reboot; everything above is still in place and working | | |
| 17 | Temperatures and disk | Diagnostics shows sensible temperatures and no disk errors | | |
| 18 | Console on a phone | Open the console at phone width; Create policy, Routes and Firewall forms are usable | | |
| 19 | Policies page | Rules, routes, blocks and open ports all appear in one list; filter by kind; Pause, Turn off, Edit and Remove work from a row | | |
| 20 | Save and apply keeps itself | Save a harmless rule; the countdown bar appears and the change is kept after a few seconds | | |
| 21 | Lockout undoes itself | On the spare machine only: save a change that cuts the console off (for example a rule blocking the management network); within 60 seconds the old settings return and the console answers again | | |
| 22 | Save for later | Save two changes with Save for later; the badge shows 2; Review lists both in plain words; nothing is live yet | | |
| 23 | Verify and commit | From Review, Verify and commit; both changes go live, are kept, and the badge clears | | |
| 24 | Discard one line | Save three changes for later; Discard on the second; the second and third are gone and the first remains, with a clear message; Discard on the last line drops only that line | | |
| 25 | Discard all | Discard all changes puts everything back as it was running | | |
| 26 | Warning stops auto-apply | Save a risky change; it is not applied on its own; the Review page shows the warning | | |
| 27 | Two areas at once | Save a network change and a firewall change for later; Verify and commit applies network first, then firewall | | |
| 28 | Authenticator memory | With two-factor on: the code is asked once when applying and not again for 15 minutes | | |
| 29 | Time zones | Setup wizard (after a factory reset): the list starts with the United States zones in plain words; saving keeps the chosen zone | | |
| 30 | Image name | The installer file name contains the version, and Settings, Updates shows the same version | | |
| 31 | Packet size measured | Connect a provider VPN (try PIA, whose file sets no size): its card shows Packet size with "measured", and a secure web request through it, for example `curl --interface tunN https://checkip.amazonaws.com`, works | | |
| 32 | Packet size after a reconnect | Turn the VPN off and on (or restart it): within about 30 seconds the card shows a size again, and the Internet sees you as address fills in without waiting ten minutes | | |
| 33 | Test packet size button | Press Test packet size: it shows each size with the addresses that answered and applies the largest size two addresses agree on | | |
| 34 | Test addresses | Change the order and add an entry in Test addresses, Save; the Review page shows "Changed the VPN test addresses."; Restore defaults puts the five addresses and four services back | | |
| 35 | One schedule | Settings: set updates to every 6 hours, lists to daily 04:10; the sentence beside each says when the next run is; Off disables | | |
| 36 | Resolve directly | DNS, Servers: turn on Resolve directly; names resolve; `dig @127.0.0.1 -p 5335 example.com` answers; port 53 is still NopeWarden's | | |
| 37 | DNSSEC failure | Look up dnssec-failed.org with DNSSEC on: it fails and the query log explains why in plain words | | |
| 38 | Fallback | With Resolve directly on, block outbound port 53/UDP+TCP to the internet; within a minute lookups work via encrypted forwarders and an alert appears; unblock and it returns | | |
| 39 | DNS staged | Add a DNS list with Save for later; Review shows a DNS item; nothing live; Discard puts DNS back | | |
| 40 | DNS follows route | Device on a VPN route: its lookups leave through the tunnel (check the tunnel counter) | | |
| 41 | Threat name lookup | A threat alert shows a name for the address (or "no name"), and Look up again retries | | |
| 42 | View names / custom names | Protection: View names on a topic lists them; a per-profile custom name blocks | | |
| 43 | Show more | A page with over 15 rows shows 15 and Show more | | |
| 44 | Security fix buttons | Each fix button opens the exact edit window | | |
| 45 | Map and VPN traffic | 5 minute range, hover popout, Your VPNs card, a VPN traffic graph, grey Idle on a quiet tunnel | | |
| 46 | Edit a route | Edit a route and Save: it saves (no invalid request body) | | |
| 47 | VPN import | Import the PIA and several other providers' .ovpn files | | |
| 48 | Rename | Console, installer, services and folders all say NopeWarden | | |
| 49 | Installer: internet port always chosen | Install in a VM or on a machine with two cabled ports: pressing Enter alone at "Which port is the internet (WAN)?" chooses nothing; a note under each connected port says whether a DHCP server answers there (it may first say "checking") | | |
| 50 | Installer: another DHCP server on the LAN port | Plug the LAN port into a network that has a DHCP server (a home router): the screen says "DHCP is off because another DHCP server (address) answered"; Enter keeps it off; the setup wizard then opens with address handing-out off and a note saying so; during setup NopeWarden hands out no addresses on that port | | |
| 51 | Installer: no other server | LAN port on its own network: "No other DHCP server answered", default yes; the setup page loads from a laptop plugged in, which gets an address from NopeWarden | | |
| 52 | Installer: both ports on one network | VM with both adapters on the same bridge as the home network: the installer warns the two ports look like one network and the suggested LAN address avoids the internet port's network; the install can still be completed | | |
| 53 | Installer: serial console header | Over a 115200 serial console every installer screen shows only the NopeWarden name (no block badge); on a monitor the smooth badge shows | | |
| 54 | Installer: centred | On a monitor and on a large serial terminal window the screens are centred left to right and top to bottom, and nothing is cut off on a 24-line terminal | | |
| 55 | One colour source | After changing a badge colour in tokens.json and running the web build, the favicon, logo file, console palette and boot pictures follow (boot pictures after `python3 brand/make_brand.py`) | | |
| 56 | Resolve directly on a fresh install | On a freshly installed machine open DNS, choose Resolve directly: no "root.key" error; the card says Using Unbound; Test it passes | | |
| 57 | In-app update fixes it | On a machine installed from 0.9.4.10 (before the fix), use Update now (no new ISO), then choose Resolve directly: it works with no commands typed | | |
| 58 | Boot countdown | Legacy BIOS boot of the installer image: no "Automatic boot in N seconds" text appears; Esc still shows the menu | | |
| 59 | Installer text readable | The grey notes (subtitle, hints, step line, DHCP notes, port notes) read clearly on a monitor and a serial terminal | | |
| 60 | DHCP check on real hardware | Both ports plugged into a home network with a router: both notes name the router (or say the port already has an address); DHCP defaults to off for the LAN port with the reason on screen | | |
| 61 | Logo size | Sign-in, setup and browser installer: 56 px badge centred above the name, heading centred below; the left menu header is larger; on a phone only the larger badge shows in the top bar | | |
| 62 | Account box | Left menu bottom: avatar, name and role, theme button, Sign out button (tooltip "Sign out", one click signs out), one non-wrapping "Version … · What's new" line, in dark and light, and on a phone | | |
| 63 | Overview box | Status line reads "1 thing needs your attention" (or "N things need"), links to the page, "and N more" opens the box; the arrow hides it and it stays hidden after reload and in another browser signed in as you | | |
| 64 | Dismiss, remind later | Dismiss a suggestion (stays hidden until it changes, comes back with Show dismissed); Remind me later on an amber row hides it for a day or a week; red rows have neither; the left-menu dots stay | | |
| 65 | Map Both | Overview map: Outgoing, Incoming and Both; Both shows green and amber lines, one list with out and in counts, a click opens Logs for the country | | |
| 66 | Undo a deleted DNS list | Delete a DNS list, press Undo: the waiting removal disappears from Review; no second waiting change appears | | |
| 67 | Resolve directly shows progress | DNS: choose Resolve directly; at once the card shows Working on it with the current step and a spinner, the choice is locked, then it turns green with a message | | |
