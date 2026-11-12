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
| 68 | DNS health calm | With Resolve directly on, and then with forwarders, leave it for an hour: the red "No DNS server is answering" does not appear while lookups work; a slow server shows amber, not red | | |
| 69 | Overview connection name | Rename the internet connection (for example T-Mobile): the Overview Internet box shows "T-Mobile (port)" | | |
| 70 | Map defaults | Open Overview: the map is on Both and 15 min | | |
| 71 | Install steps and size | Protection, Threats, Install threat watching: the box shows a real download size (not 0 MB); during the install a ticked step list moves from the package list to downloading to installing | | |
| 72 | Switch responds at once | Off / Watch and tell: the highlight moves the moment you click and a "Working on it" line shows until it finishes | | |
| 73 | Threats Overview | Protection, Threats opens on Overview: Status, Traffic watched, Load, Rules and Alerts cards, charts, What was seen, Devices involved and What it has been doing; with watching off, the cards say so | | |
| 74 | Blocks tab | The Protection tab reads Blocks, and the screens say category (not topic) | | |
| 75 | Threat engine stays up (0.9.4.16) | After updating to 0.9.4.16, with Watch and tell on: within a few minutes Threats shows Watching (not Failing); the Overview shows traffic and load; "keeps failing" does not appear | | |
| 76 | Threats page (0.9.4.17) | Threats is in the left menu with Overview, Alerts, Settings, Rules, Advanced; Overview opens with a status line; the Off / Watch switch and install box are under Settings; the charts show axes and a peak; an old #/protection/threats link lands on it | | |
| 77 | Named blocks (0.9.4.17) | Blocks, Start from Social media: name it, choose two networks and a device, Save: one row shows the name, what it holds, where, when. Edit it; pause it; turn it off. Add an exception for one device. Remove a network that the block uses: the question names the block, and the block no longer lists it | | |
| 78 | Keep delay low from measurements (0.9.4.18) | Internet, Speed: turn on Keep delay low for the home connection and accept automatic tests. The card shows Promised, Measured typical and In use, and says Learning. After a day or two of automatic tests it says Following your measurements and In use sits below the promised speed (about 369 for a line that tests near 369). Check the difference reports better, same or worse in words. Switch to Use fixed numbers and the numbers you typed are used. The history marks the tests Automatic | | |
| 79 | Management port (0.9.4.19) | Firewall, Protection: choose a spare port, Fixed address 10.99.0.5/24, Save and test. An address that overlaps a LAN is refused in words. Plug a computer into the port, set it to 10.99.0.6, open the console on 10.99.0.5 within a minute: the change is kept. Do it again and wait: it is undone. Review and apply the firewall. Turn on Close the console on the LAN (only possible from 10.99.0.5): after half a minute the console no longer answers on the LAN; unplug the cable for a minute and it answers there again. Nothing from the port reaches the internet | | |
| 80 | Themed controls (0.9.4.20) | Open Alerts, Blocks, Profiles, Logs and a schedule: every time box accepts typing (1830, 18:30) and also opens an hour and minute picker; every date opens a calendar and a date range picks two ends; every drop-down opens a styled list that works with arrow keys, Enter, Escape and typing a letter. Nothing shows the browser's own grey control | | |
| 81 | Usage per connection (0.9.4.20) | Internet, Usage with two connections: Data use shows each connection's total and its busiest devices for This billing period, Last 30 days and Choose dates; a device on both links appears under each with only that link's bytes. Data limit: set 500 GB and a billing day on one connection; the warning levels show in words | | |
| 82 | Speed limit (0.9.4.20) | Internet, Speed control, Limit speed: Download 5 Mb/s for one device, Save, Review and apply. A download test from that device shows about 5 Mb/s while another device is unaffected. Try Each device on a network of two devices, then All together. Turn Speed control is on off: speeds return to normal | | |
| 83 | Priority (0.9.4.20) | Keep delay low on. Give priority High to video calls for one device, Low to a download on another. Fill the line with the Low download: the call stays smooth. If a note says upload only, or that the queue cannot tell priorities apart, the kernel lacks a feature: record which | | |
| 84 | Limit by app or website (0.9.4.20) | Limit speed 2 Mb/s for the category Video (or a website you type), then open that site on a device: it is held to about 2 Mb/s a moment after the device first asked for the name. Policies lists the entry as Speed control and Create policy offers Limit speed and Give priority | | |
| 85 | Status colours (0.9.4.21) | Look at the menu dots, status pills and warnings on Overview, Protection, Routes and Policies: only red, yellow, green and blue appear, and a red dot is easy to tell from a yellow one. | | |
| 86 | Port names (0.9.4.21) | Give a port the name Management, then open Firewall rules, Policies, Review, Logs, Routes and Networks: the port reads Management (enp2s0) in each, including the sentences on the built-in rules. | | |
| 87 | Check progress (0.9.4.21) | Threats, Update rules now; Settings, Check now; Protection, Check again: each shows a ticked list while it works and a result when done. Threats, The program says Suricata and its version. | | |
| 88 | Routes follow service names (0.9.4.21) | Make the service Plex and a route that uses it. Rename the service to Media: the route now says Media. Try to delete Media: it is refused and names the route. | | |
| 89 | Route with a missing list (0.9.4.21) | If a route's service list is removed (restore an older backup), the route shows Needs attention and no traffic is moved by it. | | |
| 90 | New service form (0.9.4.21) | New service, name Plex: choose TCP, type 32400, Add; choose UDP, type 1900, Add; choose TCP and UDP, type 443, Add. Four entries are listed. 70000 and 2000-1000 are explained in words. Paste a list and add a known name work. Save and apply, then use it in a rule. | | |
| 91 | Rules as lines (0.9.4.22) | Firewall, Rules on a wide window opens on Rule lines: built-in rows S1 and S2, rules made by other pages, then your numbered rules, each with From, To, Service chips, Action, Matches and When. Click a row: it opens in place; click again to close. Narrow the window: it switches to Plain cards unless you chose a view, and your choice is still there after a reload. | | |
| 92 | Built-in and page rules read-only (0.9.4.22) | Open S1: a sentence and a note that it cannot be changed, no buttons. Pause a device, then open Paused: Home to Internet: the paused device is listed, with an Open Profiles button. | | |
| 93 | Per-service counts (0.9.4.22) | Make a rule for http, https, smtp and dns. Open it: a table lists each service with its own Matches and Last match. Browse a website from a device the rule covers: https goes up, the others do not. A rule with more than 8 services shows only one total. | | |
| 94 | Live switch (0.9.4.22) | Live on: send a ping or load a page that a rule matches; within two seconds Matches goes up, the row flashes and shows +N. Turn Live off: no flashing, and the numbers still move about every 20 seconds. | | |
| 95 | Export CSV (0.9.4.22) | Search for a word, then Export what is shown (CSV): the file has only the rows on screen, one row for each service of a rule, with Matches and Last match. It opens in a spreadsheet with the columns lined up. | | |
| 96 | Commit through the management port (0.9.4.23) | Open the console on the management port's address and make any firewall change, Save for later, then Verify and commit on the Review page: it goes live and is kept, with no message about being disconnected. With a limit on who may manage that leaves out your address, it is still refused in words. | | |
| 97 | Menu dots while a change is on trial (0.9.4.24) | Save a firewall change and let it go live: while the countdown shows, the Firewall, Internet and Networks dots are blue, not red. After it is kept the dots go out; changes saved for later show yellow. | | |
| 98 | Keep and Undo during a trial (0.9.4.24) | During the countdown both Keep this change and Undo now can be pressed. Press Keep: the change is kept at once. | | |
| 99 | Why a change was undone (0.9.4.24) | Commit a change from the management port. If it is undone, a message says why; Diagnostics, System log, search firewall, shows the applied, confirmed or refused and undone lines; Firewall, History shows Undone, not Waiting. | | |
| 100 | Apply progress list (0.9.4.25) | Make a firewall change and let it go live. The card at the bottom shows seven steps, each with a spinner then a green tick, and the countdown with Keep this change and Undo now. When it is kept the list says Applied successfully and stays until you press Close. The same list shows on the Review page. | | |
| 101 | Failed step (0.9.4.25) | Cause a change to fail (for example apply while the country lists are still downloading, or set a network change that clashes). The failed step is red with the reason and the list stays until you press Close. | | |
| 102 | Network change steps (0.9.4.25) | Change a network setting and apply. The list shows the network steps (writing the new network settings, switching the ports over) and then the same safety steps. | | |
| 103 | Paused line (0.9.4.25) | With nobody paused, Firewall, Rules shows the Paused line greyed out as Standing by, nobody paused, with a note. Pause a device on Profiles: it shows as active with the device's name. | | |
| 104 | DNS row detail (0.9.4.26) | Logs: open a DNS lookup. It shows Response (for example NOERROR, answered), Answer time and Answered by. Look up a name that does not exist: Response shows NXDOMAIN. | | |
| 105 | Connection row detail (0.9.4.26) | Open an allowed connection from a device a minute after it ended: Outcome, Sent, Received and Lasted are filled in and the Rule is named with a match count. Open a blocked one: Outcome says Blocked and the rule is named. | | |
| 106 | Live rule count (0.9.4.26) | With Live on, keep a connection row open while the same rule matches more traffic: the match count goes up on its own. | | |
| 107 | Log CSV (0.9.4.26) | Download as spreadsheet (CSV) with the kind left on all: DNS and connections are in one file with the detail columns, blank where they do not apply, and the file has only the rows that match your search. | | |
| 108 | Updates page (0.9.4.27) | The menu has Updates between Diagnostics and Settings. Its four tabs open: Updates (version, Check now, Update now), Schedule (Off, Notify, Automatic and the hour), What's new, Advanced (release page). Settings, General no longer has an Updates card. | | |
| 109 | Overview update notice (0.9.4.27) | With an update waiting and updating set to Off, a blue notice is at the top of the Overview. Remind me later hides it until tomorrow morning and Dismiss hides it for that version; a newer version shows it again. The menu shows a blue dot on Updates and the footer says Update available. | | |
| 110 | Restart and failed notices (0.9.4.27) | After a system update that needs a restart, a yellow notice and a yellow dot appear and stay until you restart. After a failed (undone) update a red notice and dot appear with the reason, and Dismiss is not offered. | | |
| 111 | Install from the new page (0.9.4.27) | Update now on the Updates page shows the ticked steps, the console restarts, the page reconnects and says what was installed. | | |
| 112 | Lists and labels (0.9.4.27) | Protection has a Lists and labels tab with the blocking list versions and Traffic labels; Settings, Advanced no longer has them. Search finds both on Protection. | | |
| 113 | Smaller fixes (0.9.4.27) | Logs, Activity: one time picker, axis reads in minutes, small speeds in kb/s. Bridge mode shows Saved, not applied when switched on but not applied. Settings, Account shows browsers as Chrome on Linux and so on. | | |
| 114 | What's new list (0.9.4.28) | Updates, What's new: the newest release is open and every older one is one line. Open a line, use Expand all and Collapse all, open a group such as Earlier in 0.9.4.x. The menu footer window and the Overview card's All release notes show the same list. | | |
| 115 | What's new search (0.9.4.28) | Type firewall in the search box: only releases that mention it are shown, opened; clearing the box brings back the collapsed list. A word nothing mentions says No release mentions that. | | |
| 116 | Release notes back to the start (0.9.4.28) | Expand all, scroll to the end: entries go back through 0.8.x, 0.7.x, 0.6.x and 0.5.x to Earliest builds. Check the early entries against what you remember. | | |
| 117 | Block lists update line (0.9.4.28) | Protection, Lists and labels: a line says the lists update automatically, with the time and the next run. Change when opens the schedule; Run now refreshes the lists in use. Lists nothing uses say Not in use yet. | | |
