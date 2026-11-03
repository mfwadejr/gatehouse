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
