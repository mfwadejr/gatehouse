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
| 7 | Firewall rule applies | Add a rule, Review and apply, confirm; traffic changes as expected; rollback after not confirming | | |
| 8 | Pause a rule | Pause for 5 minutes; traffic changes; it comes back by itself | | |
| 9 | Port forward | Open a port to a device; reach it from outside or a second network | | |
| 10 | Route by port through a VPN | Route one service (for example NTP) through a VPN; check the exit address | | |
| 11 | Route through connections in order | Route through a main and a backup connection; unplug the main; traffic moves to the backup and returns | | |
| 12 | Create policy | The button opens the right form for each type | | |
| 13 | Schedule | A rule or route with a time window works inside it and not outside it | | |
| 14 | Admin activity | Logs, Admin activity lists each change above, including pause and resume | | |
| 15 | Backup and restore | Download a backup, restore it; routes, pauses and schedules are the same | | |
| 16 | Survives a reboot | Reboot; everything above is still in place and working | | |
| 17 | Temperatures and disk | Diagnostics shows sensible temperatures and no disk errors | | |
| 18 | Console on a phone | Open the console at phone width; Create policy, Routes and Firewall forms are usable | | |
