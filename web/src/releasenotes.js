// Plain-language release notes, newest first. Add an entry for every release.
export const RELEASES = [
  {
    version: '0.9.0',
    title: 'Find your place, and no silent failures',
    items: [
      'Your place on the map: a Find a place box takes a zip or postal code, a city or a street address and offers up to five matches. Pick one and the pin moves there. Only the words you type are sent, to OpenStreetMap, and only when you press Find. The place you save never leaves this machine.',
      'Failed saves are no longer silent: if the disk will not take a setting or a log line, Diagnostics now says how many saves failed in the last hour, when, and why.',
      'Behind the scenes: new checks call every page of the console as a stranger, as a read-only user and on an empty box, and an empty request to every route, to make sure each one refuses cleanly instead of crashing.',
    ],
  },
  {
    version: '0.8.57',
    title: 'Searchable links, clearer failures and tidier styles',
    items: [
      'Logs: the words you search for are highlighted in each row, a note says how far back the search reaches, and the search is part of the address (#/logs/log?q=word), so you can share or bookmark it. The same works on Admin activity.',
      'A firewall change that fails to apply now stays visible: the Firewall item in the menu gets a dot, Diagnostics says there is a problem, and the reason is shown until a change applies. It is also written to the activity log.',
      'The Security check buttons for SSH, UPnP and console access now open the edit window of the exact network that needs fixing, instead of the list.',
      'Remote login: an old Last attempt failed note from before a restart no longer lingers, and a note that is shown says when it happened.',
      'Behind the scenes: 70 repeated inline styles became shared named classes, and a slow-machine timing in one high-availability test was relaxed.',
    ],
  },
  {
    version: '0.8.56',
    title: 'Storage, and three clear kinds of log',
    items: [
      'Storage (Settings, Alerts & logs): a table of everything Gatehouse keeps, with entries, estimated size, the oldest entry and the rule that removes it. Every table that grows now has a rule: threat alerts and internet outages go after 365 days (you can change it), firewall change copies keep the newest 200 (you can change that too), and the total size limit now also trims the smaller histories if it is still exceeded. The limit is now applied every hour, not only when you pressed Clean up.',
      'An unused table that was never filled in was removed.',
      'Logs has a new Admin activity tab (administrators only): sign-ins, failed sign-ins and the changes people made, with who, what, when and from where. Search takes several words and a time range, and the count says how many match.',
      'Diagnostics now starts with Is Gatehouse itself working? One sentence, problems first, each with a Look button, then facts: how long Gatehouse and the machine have been up, when the firewall was last applied, disk and log space, the clock, and warnings in the last hour.',
      'Diagnostics has a System log tab: warnings and errors by default, Everything on request, search across up to 5000 lines with the words highlighted, a time range, and Follow. Known messages get a plain explanation.',
      'Console connections records every live connection your browser opens to Gatehouse: when, from where, how long, and why it closed, so the idle connection-error bursts can be told apart as the box or the network. Unusual endings also go to the system log.',
      'Logs and Diagnostics now open with one line saying what each is for, and point to the other.',
    ],
  },
  {
    version: '0.8.55',
    title: 'Faster console and a better Overview',
    items: [
      'The console loads much less up front: its main file is about a third of the size, and each page loads when you first open it.',
      'All live updates now share one connection instead of one per card. That frees the browser for ordinary requests and is the likely fix for the bursts of connection errors seen while the console sits idle.',
      'The speed feed that updates every second now sends only what is new after the first load, and cards that ask for the same thing at the same moment share one request.',
      'Overview speed chart windows: 5 minutes, 15 minutes, 1 hour, 24 hours, 7 days and 30 days.',
      'Your networks on the Overview is now a table showing each network\'s download and upload speed, updated every second, with its last-hour chart.',
      'The world map shows what was blocked in red, more strongly the larger the blocked share of a country; the hover note shows blocked and allowed counts for both directions.',
      'Your place on the map: you can now type latitude and longitude. The placement is within about a degree.',
    ],
  },
  {
    version: '0.8.54',
    title: 'Tidier layout and clearer wording',
    items: [
      'The sidebar is now in groups: Network (Internet, Networks, DNS, DHCP, Routes), Security (Firewall, Protection, Services, VPN), People and devices, and System. Every page keeps its address, so saved links still work.',
      'Backups: the setting that said Keep the newest now says Keep this many backups, and a line under it says it counts copies, not days.',
      'Cards that had no explanation now have one (Reserved addresses, DNS settings, Internet connections, Services search, Where you are signed in, Appearance).',
      'Threat alerts are grouped by kind, with how many times and on how many devices, and long lists show the first few with a Show all button.',
      'Add a device now lets you pick one that has already connected instead of typing its address.',
      'Help now explains sending logs elsewhere, traffic records, SNMP, high availability, bridge mode and UPnP.',
      'Behind the scenes: unused styles, colors and four unused routes were removed, and the build now fails if unused ones come back.',
    ],
  },
  {
    version: '0.8.53',
    title: 'Wireless removed: Gatehouse is wired only',
    items: [
      'Wi-Fi support is gone. The Wi-Fi page and its link in the sidebar, Wi-Fi join cards, the Wi-Fi internet option in the setup wizard, and the Wi-Fi columns and filters on Devices are removed. If you need Wi-Fi, plug in a separate access point or Wi-Fi router; Gatehouse still protects everything that connects through it.',
      'The installer no longer carries Wi-Fi software or wireless firmware (hostapd, wpa_supplicant, iw, regulatory data, and the Intel, Atheros, Broadcom and MediaTek Wi-Fi firmware), and wireless cards are blocked from loading, so a Wi-Fi card in the machine is ignored. Wired network card firmware is unchanged.',
      'Updating from an earlier version drops the saved Wi-Fi settings and join cards. Old backups still restore; their Wi-Fi part is skipped. The Help now says plainly that wireless is not included.',
      'The earlier Wi-Fi code is kept in the project archive folder for reference.',
    ],
  },
  {
    version: '0.8.52',
    title: 'Every link checked: Security check, search and Help',
    items: [
      'Every search result was opened in a real browser and checked: it lands on the right page and tab and finds its card. Four were wrong and are fixed (the DNS server settings and Who can reach whom). Search now lists top-level pages under Pages.',
      'The Help with this page link on Protection and Diagnostics opened nothing useful, because those two chapters were never built into the help. They are now, and a build check stops this happening again. All 110 help search results and every link inside the help pages were checked.',
      'The Diagnostics page could fail to draw when no services were listed. It now shows an empty list instead.',
      'The Security check no longer warns "SSH is on for Home" while remote login is switched off. Allowing a network to use SSH opens nothing until remote login itself is on, so the warning now appears only when both are true.',
      'The fix buttons in the Security check now open the exact tab that has the setting. SSH, console and UPnP warnings open Firewall, Networks (press Edit beside the network); the encrypted DNS warning opens Firewall, Protection; open ports open Firewall, Open ports; two-factor, backups, updates and alerts open the right Settings tab. The connection tester\'s buttons do the same. A test now checks every one of these.',
    ],
  },
  {
    version: '0.8.51',
    title: 'A livelier Overview, threat alerts you can read, search that works everywhere',
    items: [
      'The Overview is redone: a status line with a steady coloured dot, one box for what needs you, a large internet card, a world map with lines from your place to the busiest countries, and a What happened list that folds repeats together. The Suggestions card is gone; what needs you now shows as a small dot beside the page in the left menu.',
      'Settings, General has Your place on the map: click the map or pick a country so the lines start from your home. Common setups and the first-run checklist now live in Settings, General.',
      'Threat alerts are rewritten in plain words, name the device you gave a name to, say which way it started, and fold repeats into one row with a count. Harmless engine chatter is tucked into a folded section.',
      'Threat settings: a Custom choice appears when you change groups of checks, Restore defaults goes back to a setting, groups have plain descriptions and apply only when you press Apply, and Advanced has Reset all threat settings.',
      'The terminal asks for your password when you switch it on, and again only if 15 minutes have passed since the last check.',
      'A device asking this machine for encrypted DNS on port 853 is refused at once and shown in the Logs as Blocked, expected, so its software falls back to normal DNS quickly.',
      'Search now opens the exact tab and flashes the card it found, groups results into Pages, Settings, Devices, Networks and Help, finds help chapters, and opens a device straight on the Devices page. Every tab can be opened from its address. A build check stops a search result from pointing at something that no longer exists.',
    ],
  },
  {
    version: '0.8.50',
    title: 'Build fixes for 0.8.48',
    items: [
      'Fixes the automatic tests that stopped 0.8.48 and 0.8.49 from building. The terminal now also starts on machines where the home folder cannot be entered, and naming a port no longer depends on the real network cards of the computer running the tests. Everything in 0.8.48 is included.',
    ],
  },
  {
    version: '0.8.48',
    title: 'Diagnostics page, terminal, Suricata reasons, SSH fix',
    items: [
      'A new Diagnostics page in the left menu: every part of Gatehouse with its state, why it is failing in plain words, its log, Restart, and Save a support bundle (passwords and keys hidden). The network tests are on it too.',
      'An optional Terminal on the Diagnostics page: a command line on the firewall from the console. It is off until you switch it on, asks for your password or code every time, closes by itself after 10 idle minutes, and each open and close is recorded in the Logs.',
      'Threat watching now says when the program keeps failing and restarting, shows the last thing it said, and has a Program log under Advanced. Its default memory limit is 2 GB.',
      'Remote login (SSH) could not be turned on because the machine had no SSH identity. Gatehouse now makes one when you turn it on, and once at first start.',
      'A new Protection page in the left menu. Overview gives a one-line verdict and the Security check; Apps & topics (moved from DNS) blocks apps, topics and websites; Threats is threat watching. Block lists for ads, trackers and adult sites stay under DNS.',
      'Threat watching is no longer installed behind your back. Press Install threat watching: Gatehouse shows the packages, the download size, the disk space needed and any worry about this machine, and installs only when you agree. Choose Light, Balanced or Strict, set a processor and memory limit, update the rules (and roll back to the previous ones), search and switch off single checks, and save or load your tuning. It only watches and tells you; blocking threats is not available yet. If it stops, nothing changes for your devices, and if it uses all of its processor share for ten minutes Gatehouse switches it off and says why.',
      'Add a network now offers five kinds: Trusted, Private, Guest, Isolated and Custom. Guest devices cannot see each other unless you turn that on. Choose the whole port or a VLAN on any port, and a VLAN with no address and no DHCP for a link to a switch or access point.',
      'If threat watching is on but the program is not running, the page now says why (for example it ran out of memory). Its default memory limit is now 2 GB, because the full rule set needs more than 1 GB.',
      'The Install window now shows the real download and disk size. The groups of checks sit in columns.',
      'Opening a port that the firewall itself uses (such as the console) now warns you instead of refusing.',
      'Choose how often Gatehouse looks for updates: 6 hours, 12 hours (the usual), a day, a week, or your own number of hours.',
      'More topics to block (scams, piracy, crypto mining, drugs, games, video, music, messaging, shopping, AI). Settings, Advanced shows which version of the lists is in use. Blocking also has a thorough choice, which is saved but does not act yet.',
      'If a block or threat watching ever goes wrong, your internet is not affected. This is now stated beside the setting, in the Logs when it happens, and in the Protection and Reference chapters.',
    ],
  },
  {
    version: '0.8.43',
    title: 'Open ports made simple, block apps and topics, threat watching',
    items: [
      'Open a port in three taps: pick the app, pick the device. Gatehouse says which port it used and lets you change it. It never picks a port for you; if one is taken it asks which one to use. Each device row also has an Open a port button. Open ports now show in the Rules list (under Show system rules) and in Services.',
      'You can give an app a different outside port and a different port on the device, which suits several copies of the same app. Renaming a service renames everything that uses it.',
      'Block an app, a topic (adult, gambling, social media, ads, malware) or a website for everyone, a network, a profile or one device, always or at set hours, with an exception for a device and a pause button. It blocks by name. Thorough checking of the traffic itself is shown but is not switched on yet. If anything goes wrong nothing is blocked.',
      'Each blocklist has View names, with a search and an Allow button.',
      'Threat protection (Firewall, Protection): reads what a detection engine notices and shows it in plain words in the Logs, with Allow for this device, Allow for all, Never check this device, and an Advanced tab. It watches and tells; it does not block. The engine itself is not installed by this version, so until it is the page says so.',
      'The Logs hide Gatehouse\'s own health checks. A checkbox shows them again.',
    ],
  },
  {
    version: '0.8.42',
    title: 'DNS lookups show their connection',
    items: [
      'In the Logs, a DNS lookup now shows which internet connection it left through (for example T-Mobile), the same way other connections do. A lookup sent through a VPN still shows the VPN. Lookups answered from memory leave nothing, so they stay blank, and lookups logged before this version stay blank too.',
    ],
  },
  {
    version: '0.8.41',
    title: 'Internet page in tabs',
    items: [
      'The Internet page is split into tabs: Connections (your connections and how they share the work), Speed, Troubleshoot (network tools and "Why can\'t it reach that?") and Usage (data use and the monthly provider report). Warnings stay above the tabs, and the tab you chose is remembered.',
      'Includes the 0.8.40 build fix for the help chapters.',
    ],
  },
  {
    version: '0.8.40',
    title: 'Build fix',
    items: [
      'Fixes the automatic build for 0.8.39, which stopped because a help chapter shared a name with an older document on case-insensitive disks. The help now lives in its own folder. Nothing changes in how Gatehouse works.'
    ],
  },
  {
    version: '0.8.39',
    title: 'Built-in help, and undo instead of "Are you sure?"',
    items: [
      'Every page now has a "Help with this page" link. The help is built into Gatehouse, so it works with no internet: one chapter per page with a picture, a Getting started guide, a guide for when you are locked out, and a reference. It is also at /help/ on your box.',
      'Deleting a firewall group or a route no longer asks "Are you sure?". It is removed at once and a message offers Undo for a few seconds, which puts it back exactly as it was.',
      'The first-run setup now offers Bridge as a third choice next to the router choices, and walks you into the bridge steps.',
      'A new help chapter, "How easy is it?", lists the everyday jobs and how many steps each takes. Opening a port by hand (6 steps) is the longest and is next to shorten.',
    ],
  },
  {
    version: '0.8.38',
    title: 'Build fix',
    items: [
      'Fixes an automatic test that failed only in the first hour of a new month (the monthly provider report test looked at a time that had not happened yet). Nothing changes in how Gatehouse works.',
    ],
  },
  {
    version: '0.8.37',
    title: 'Settings in tabs',
    items: [
      'Settings is split into five tabs like the Firewall page: General (Device mode, Updates, Power, Console address, Appearance), Account and sign-in (Two-factor, Password, where you are signed in, Remote login), Alerts and logs (Alerts, syslog, traffic records, SNMP), Backup and recovery (Backups, Changes, printable summary, Reset) and Advanced (High availability, Bridge mode, Traffic labels, Guest sign-in page).',
      'The page reopens on the tab you used last. Search takes you straight to the right tab, and "Set up a pair" on the Device mode card opens High availability.',
      'People who are not administrators see only their own sign-in settings and Appearance, with no tabs.',
    ],
  },
  {
    version: '0.8.36',
    title: 'Build fix',
    items: [
      'Fixes the automatic style check added in 0.8.35, which ran before the shared styles were generated on a clean build and wrongly stopped it. Nothing changes in how the console looks or works.',
    ],
  },
  {
    version: '0.8.35',
    title: 'Tidy-up: spacing, colours and automatic checks',
    items: [
      'Cards inside an opened "Advanced" section (Settings and Firewall) no longer touch each other; they have the same gap as cards everywhere else.',
      'Several small parts of the console used style names that did not exist, so they quietly used a guessed colour instead of your light or dark theme (the pickers, icon buttons, the world map, the file drop area and the QR code backdrop). They now follow the theme. Shadows and the dimmed background behind dialogs became proper shared styles.',
      'Spacing written directly into screens now uses the shared spacing scale. Nothing moves; the sizes are the same.',
      'Behind the scenes, three automatic checks now run on every build and stop it if something is wrong: a colour or size written outside the shared styles, a style name that is not defined, and a screen that calls a part of Gatehouse that does not exist.',
    ],
  },
  {
    version: '0.8.34',
    title: 'Logs: shorter rows, details on click',
    items: [
      'Every row in the Logs is now at most two lines: the name of the app or website on top, and the port and service (or the kind of entry) underneath. Long names are cut off with "..." on that one line only; the full name is in the details.',
      'Near-identical connections or lookups that follow each other, from the same device to the same place with the same result and exit, become one row with a count (for example "accounts.meta.com x6"). The individual times are in the details.',
      'Click a row (or press Enter on it) to see the rest: why it was allowed or blocked, the rule, the host and address, the country, the networks, and the Block this or Allow this button. Blocking something is now two steps: click the row, then Block.',
      'The Networks and Why columns are off by default because "Out through" and the details say the same. They are still under Columns and rows. Times no longer wrap onto two lines.',
      'On a phone each entry is a small card with the same two lines.',
    ],
  },
  {
    version: '0.8.33',
    title: 'Device mode: Router or Bridge, with cabling help',
    items: [
      'Settings now starts with a Device mode card. It says in one sentence how this box works and which port is which (using the names you gave your ports), with a Change button.',
      'Change opens a three-step walkthrough. Step 1: choose Router or Bridge. Step 2: plug in the cables and Gatehouse notices each port as it goes in (or pick the port from a list if the cable is already in). For a bridge you plug in the cable from your other router first, then your devices, and you can add a spare port for managing the box. Step 3: a plain cabling summary, then Apply.',
      'Safety: nothing is saved until the last step and Cancel changes nothing. The port you are connected through is named ("leave this cable plugged in") and can never be chosen as the internet port or the cable to your other router. Applying a bridge, or going back to routing, uses the same 60-second keep-or-undo as before. Moving the internet connection to another port is saved for you to apply from the usual bar, and is refused if that port carries one of your networks.',
      'Bridge mode keeps addresses (DHCP) with your other router and says so. Settings you had before bridge mode are kept and come back when you return to Router.',
      'While a box is in a high-availability pair its mode cannot be changed, with a message explaining why. The pair setup is one click away from the card.',
      'The countdown after a bridge change now appears in the Device mode card; the Bridge mode section under Advanced still holds every detailed setting.',
    ],
  },
  {
    version: '0.8.32',
    title: 'Name lookups go through the VPN too; the Check keeps itself current',
    items: [
      'Devices you send through a VPN now have their website-name lookups sent through it as well, using the DNS server you already chose, with its filtering. Before, the names were looked up over your normal connection even though the sites themselves went through the VPN. These devices also get a DNS memory of their own, so an answer fetched through the VPN is never mixed with one from the normal connection.',
      'If the VPN goes down and its kill switch is on, those devices\' lookups are held back with the rest of their traffic and never go out the normal way. With the kill switch off, lookups carry on the normal way so DNS keeps working, and the VPN card says so.',
      'The Logs show the VPN under "Out through" for lookups that went through it.',
      'The Check beside a device on the VPN card now updates itself every few seconds, shows when it last ran, looks at the last 15 minutes instead of the last hour, counts name lookups as well as connections, and uses the device\'s current address. If the device moved to a new address, it says so and tells you to press Review and apply.',
    ],
  },
  {
    version: '0.8.31',
    title: 'Port names, lists in the firewall box, tidier screens',
    items: [
      'Network ports can have a name you choose. On Networks, press Rename on a port (for example "T-Mobile gateway"). The original port name stays underneath, and the new name shows on the Networks, Internet, Devices, DHCP, Device map and Test my internet pages. Clearing the name puts the original back. Only the label is stored, so nothing about the port itself changes.',
      'The "What for" box in a firewall rule takes several entries at once. Type or paste tcp/80, tcp/443, udp/1000-2000 (commas or spaces) and each becomes its own entry. Anything that is not a real service or port stays in the box with a note saying which part is wrong. Help text under the box shows the format.',
      'The "Updated just now" label on Overview is gone. It now appears only when updates stop, as "Reconnecting".',
      'Removed the "Services and ports" card from the firewall rules page (your own services are on the Services page). On the UPnP card the headings are now "Time" and "Request from a device".',
    ],
  },
  {
    version: '0.8.30',
    title: 'See which connection a device really uses',
    items: [
      'The Logs page has a new "Out through" column that names the connection each new connection left by, such as ExpressVPN NL or T-Mobile. Turn it on under Columns & rows if you do not see it yet.',
      'The Devices page shows "Sent through" for any device you have sent through a VPN, with a warning if that VPN is not working. The column appears on its own when at least one device uses a VPN.',
      'On a VPN card, each device under "Used by" has a Check link. It looks at that device\'s new connections from the last hour and tells you plainly whether all of them went through the VPN, or lists the ones that went out the normal way.',
    ],
  },
  {
    version: '0.8.29',
    title: 'Quieter VPN card while connecting',
    items: [
      'While OpenVPN is starting or reconnecting, the VPN card now just says "Connecting", and "Connected. Testing the connection" for the moment after it comes up. The old "test failed" message and the long list of OpenVPN lines no longer flash up during a normal connect.',
      'The OpenVPN messages only appear when the tunnel is really failing, and they sit behind a closed "Show details" line. The reason in plain words stays at the top.',
    ],
  },
  {
    version: '0.8.28',
    title: 'VPN tunnel keeps its address',
    items: [
      'Fixed a likely cause of a provider VPN (such as ExpressVPN) that connected and then carried nothing: Gatehouse now tells the system network service to leave the address and route OpenVPN sets on its tunnel device alone. After updating, the Internet page shows changes waiting to be applied; press Review and apply once so the tunnel file is rewritten.',
      'When OpenVPN says the tunnel is up but the device has no address inside it, the VPN card now says exactly that, instead of saying the connection never finished.',
    ],
  },
  {
    version: '0.8.27',
    title: 'Test my internet with several connections; shorter pages',
    items: [
      'Test my internet now checks each connection on its own when you have more than one: cable, modem, provider, a website-name lookup sent out through that connection, and its last speed test. Above the rows it says how the connections share the work (failover or load balancing) and which one is in use. A backup that waits on purpose is shown as waiting, not as a fault, and "What to try" appears only under the connection that has the problem.',
      'The VPN page no longer shows a second "Add a VPN" button in its empty card; the one at the top right is the way in, like the Routes page.',
      'Long lists start short. The log, the devices list, firewall rules and the built-in services show 15 (or 10) at first, with a button to show more. The log and device row choices now include 10 and 15.',
    ],
  },
  {
    version: '0.8.26',
    title: 'VPN tunnels say what is wrong; Save says what it did',
    items: [
      'Provider VPN files (ExpressVPN and others) now keep the certificate check and the packet-format line exactly as the provider wrote them. Gatehouse used to change one and drop the other, which could stop the connection from ever starting.',
      'A VPN tunnel that is not working now says why in plain words (login refused, no answer from the server, certificate rejected, no address given), and shows the last lines OpenVPN wrote. A line is added to the event log when a tunnel drops or fails.',
      'Save now tells you what happened: "Saved. Press Review and apply at the bottom of the window to start using it", "No changes. These are already saved", or "No changes. This is already in use".',
    ],
  },
  {
    version: '0.8.25',
    title: 'Settings page fixed',
    items: [
      'Fixed: the Settings page would not load. The Updates card drew itself differently once its data arrived, which React refuses, and that took the whole page down. The build now checks every screen for this mistake so it cannot come back.',
    ],
  },
  {
    version: '0.8.24',
    title: 'Live updates no longer cut off',
    items: [
      'Fixed: the live-update connections that keep screens current were being cut by the console about every 15 seconds, so the browser showed a stream of protocol errors and pages kept reconnecting. They now stay open.',
    ],
  },
  {
    version: '0.8.23',
    title: 'Build fix',
    items: [
      'Fixed: the build for 0.8.21 and 0.8.22 failed on a test that was too strict about timing. Nothing about how Gatehouse runs changed, and 0.8.21 and 0.8.22 include nothing else that is different.',
    ],
  },
  {
    version: '0.8.22',
    title: 'Clearer Who can reach whom',
    items: [
      'Who can reach whom is easier to read: Open is green, Blocked is red and Replies only is amber, each with a symbol and its word, in both the dark and light looks. The explanation under the table is now a short colour key.',
    ],
  },
  {
    version: '0.8.21',
    title: 'Fail back when you choose',
    items: [
      'When the main unit comes back after the other unit took over, nothing moves by itself. A message appears on every screen: fail back to the main unit, keep the other unit as the main unit, or decide later (it comes back after a day). The network keeps running on the current unit until you choose.',
      'Failing back first sends the latest settings to the main unit, so changes made while the spare ran are not lost, then steps aside. If the main unit does not take over, the unit that was running the network carries on by itself. Going back is greyed out, with the reason, while the main unit is still copying settings, is on another version, or is updating.',
      'Keeping the other unit as the main unit swaps the roles on both units without moving anything. The change is refused, and nothing changes, if the other unit cannot be reached.',
      'Fixed: settings changed on the spare while it ran the network now fit the main unit\'s port names when they are copied back.',
      'If one part of a screen cannot be drawn, it now shows what went wrong in that spot and the rest of the page keeps working, instead of leaving a blank page. Also fixed a graph with a single point on it.',
    ],
  },
  {
    version: '0.8.20',
    title: 'Safer pair updates, any-port device placement, warm spare',
    items: [
      'Updating a pair: from the active unit, Update now updates the spare first, checks that it works, hands the role to it, then updates the other unit, with one progress list. If the spare cannot update, the active unit is never touched. The roles end up swapped; hand back whenever you like.',
      'Move a device to another network now works from any port or Wi-Fi: Gatehouse moves that one device across to the VLAN network, so the device does not have to be plugged into the VLAN\'s port.',
      'Cable between the two units (optional): choose a spare port on each and the pair talks over it (settings copy, updates, connection copy), falling back to the shared network if it is unplugged. An unplugged cable never makes a unit take over (Settings, Advanced, pair).',
      'Keep the spare\'s internet connection ready (optional): the spare keeps its internet port connected but silent, copies the active unit\'s hardware address, and takes over faster. Not for PPPoE or Wi-Fi connections.',
      'Fixed: a build could fail on a slow machine because a test did not wait long enough for the console to start.',
    ],
  },
  {
    version: '0.8.19',
    title: 'Device placement, approvals, undo and more',
    items: [
      'Move a device to another network (Devices, Details): like UniFi\'s Virtual Network Override. Pick a VLAN network and Gatehouse puts that one device on it, wired or wireless. The device must be plugged into the same port the VLAN is on, and takes the change when it next asks for an address.',
      'New-device approval: new devices wait on a holding network until you allow or block them. Off by default; devices you already have are never held (Devices).',
      'Fewer prompts, and Undo: everyday changes no longer ask for a code. Only serious ones do (reset, restore, going back to an earlier setup, turning on remote login, plain backup download), with your authenticator code or, if you have none, your password. Deleting a firewall rule or an unused service shows Undo.',
      'Add more ports to this network (Networks, edit): turn a one-port network into a shared-port network without losing anything.',
      'A map on My network: a printable picture of how your networks connect.',
      'Keep delay low when the line is busy (Internet, under Speed test): holds a connection just under its plan speed so calls and games stay smooth. Needs the speeds your plan promises. Off by default.',
      'Guest sign-in page (Settings, Advanced): guests tap Connect, with optional terms, before they get internet. It can never cover your management network or internet port.',
      'Send traffic records (NetFlow or IPFIX) to a traffic-analysis tool, and let a monitoring tool read this device over SNMP, read only (Settings, Advanced). Both off by default; SNMP is reachable only from your management networks.',
    ],
  },
  {
    version: '0.8.18',
    title: 'Pairs, bridge mode and traffic labels',
    items: [
      'High availability: pair two Gatehouse units so the second takes over by itself when the first fails. Settings are copied across, the spare is read-only and stays quiet, and connections carry over. Settings, Advanced. Needs a second unit with at least as many ports.',
      'Bridge mode: put Gatehouse between another router and your network and filter without routing. The console stays reachable on its own address, setup has a 60-second undo, and Reset to default always brings routing back. Settings, Advanced.',
      'Traffic labels: an optional small program names the application behind each connection by looking at the first packets (YouTube, Zoom, BitTorrent), and flags cautions such as self-signed certificates. Off by default. It is on fresh install images only.',
      'The top of Overview is tidier: suggestions, the security check and common setups now sit together in one collapsed card.',
    ],
  },
  {
    version: '0.8.17',
    title: 'Cards, reports and remote access',
    items: [
      'Wi-Fi join cards: a QR code that joins your Wi-Fi when scanned, and a card to print with the name and password. Works for Wi-Fi from Gatehouse and for any other access point (Wi-Fi page).',
      'My network: one printable page of your networks, addresses, devices, open ports and how to get back in. It never contains passwords or keys (Settings).',
      'Provider report card: a month of outages and speed against your plan, ready to print and show your provider. Data use shows this billing period and your heaviest devices, with an optional monthly limit and warnings (Internet page).',
      'Reach my home from anywhere: a web name that follows your home address (DuckDNS, No-IP and similar), then the VPN, then your phone (VPN page). It warns when your connection is shared by your provider and cannot work.',
      'Encrypted DNS is one switch: Off, Automatic, Pick a provider or Custom, with Undo (DNS page).',
      'Filtering per network: give a whole network a profile, such as Enhanced filtering and safe search for Guests (Profiles page).',
      'Send logs elsewhere: copy events, and optionally firewall decisions, to a syslog server (Settings, Advanced). Off by default.',
      'Fixed: adding a DNS server with Plain (TCP) always failed. That option is gone, and the usual port is added for you.',
    ],
  },
  {
    version: '0.8.16',
    title: 'Profiles, pause, and finding out why',
    items: [
      'Profiles: Kids, Guests, Smart home and Work, each with a filtering level (Off, Basic, Enhanced or your own categories), safe search for Google, Bing, DuckDuckGo and YouTube, and hours when the internet is off, such as a bedtime. Put a device in a profile on the Devices page.',
      'Pause: stop the internet for one device, or for everyone in a profile, for 30 minutes, an hour, two hours, or until you resume. It takes effect in seconds with nothing to apply and comes back by itself.',
      "Why can't it reach that? Pick a device and a website. Gatehouse checks pauses, DNS filtering and the firewall in order, says which one is in the way, and offers a fix such as Allow this site. It is on the Internet page and inside each device.",
      'Security check on the Overview: the things that most often leave a network exposed, each with a button that takes you to where it is fixed, and one plain sentence at the top.',
      'Undo: before every firewall or network change goes live, Gatehouse keeps a restore point. Settings, Changes lists them with a Revert button. A revert is reviewed and applied the usual way, with the usual 60 seconds to take it back.',
      'Common setups: guest Wi-Fi, cameras on their own network, Plex from outside, and UniFi access points, each as steps in order.',
      'Internet provider presets fill in the usual settings for some providers. None is tested yet, and each says so.',
      'Optional: also block the best-known public encrypted-DNS websites (Firewall, Protection). It cannot be complete, and it says so.',
    ],
  },
  {
    version: '0.8.15',
    title: 'Fewer surprises: tests, alerts and simpler ports',
    items: [
      'Test my internet: one button checks the cable, the modem, your provider and website names in turn, and says what to try. Ping, trace and name lookup are on the same page.',
      'Gatehouse now tells you when your connection is shared by your provider (CGNAT) or sits behind another router, what stops working because of it, and the exact ports to open on the other router.',
      'One card for UPnP, under Firewall, Open ports. Choose who may use it and through which connection. It cannot be switched on until you have chosen.',
      'Open a port by picking the app and the device. Plex, Jellyfin, Minecraft and more are built in; add your own. The forward follows the device if its address changes, and can be limited to certain countries or addresses.',
      'Discovery: let printers, Chromecast, AirPlay and Home Assistant find each other across networks you pair (Firewall, Discovery).',
      'Alerts by email or ntfy: a new device, the internet going down and back, a watched device going offline, or Gatehouse repairing something itself. Quiet hours hold them until morning, and a test button proves it works (Settings).',
    ],
  },
  {
    version: '0.8.14',
    title: 'Countries, Routes and spare ports',
    items: [
      'Countries: block or allow whole countries in one place, for traffic going out, coming in, or both. Gatehouse warns you before a choice could lock you out of your own country.',
      'Routes: send only the websites, addresses or countries you choose through a VPN, or through one internet connection, and leave everything else alone. A "stop if down" switch keeps traffic from leaking if the VPN drops. Routes carry IPv4 only.',
      'Spare ports: on a network that uses a bridge, tick the ports that should act as a switch for it. Gatehouse tells you why a port cannot be ticked, and nothing changes until you apply.',
      'Adding a VPN no longer forces you to choose who uses it: pick "Nobody yet" and decide later.',
      'After you apply a change, Gatehouse checks that you can still reach it and keeps the change by itself. If you cannot, it still undoes itself within 60 seconds. You can switch this off to confirm by hand.',
    ],
  },
  {
    version: '0.8.13',
    title: 'Tidy-ups and small features',
    items: [
      'Search: press / or Ctrl+K (⌘K on a Mac) to jump to any page, setting, device or network.',
      'Blocklists now download on a fresh install, retry sooner when the internet is not ready yet, and say in plain words what went wrong. Each list shows how many names it blocks and how many it allows.',
      'Removing a network now removes everything that belonged to it: its address pool, its rules and its firewall network. You see what will go before you confirm.',
      'The "This machine" card shows real amounts for memory, disk and processor, and what is using the disk.',
      'A clear warning when a network is nearly out of addresses.',
      'You can choose the name of this firewall on the Your names page.',
    ],
  },
  {
    version: '0.8.12',
    title: 'Friendlier imports and names',
    items: [
      'More VPN provider files (such as ExpressVPN) import without changes, and if one cannot, every reason is listed at once.',
      'Your own names are checked when you save them, so a pasted web address cannot break a name.',
      'Messages appear top right and go away when clicked.',
    ],
  },
];
