// Plain-language release notes, newest first. Add an entry for every release.
export const RELEASES = [
  {
    version: '0.9.4.28',
    title: 'A shorter What\'s new, and block lists that say they update themselves',
    items: [
      'What\'s new shows the newest release open and every older one as a single line you can open. Older releases are grouped (0.9.4.x, 0.9.3.x and so on), there are Expand all and Collapse all buttons, and a search box that shows only the releases mentioning your words.',
      'The notes now go all the way back: the releases that had no entry are filled in, back to the first builds. Where an old build changed little, its entry says so.',
      'Protection, Lists and labels says the block lists update themselves, when they next will, and has a Change when link and a Run now button. A list nothing uses yet says Not in use yet and is downloaded the first time a block needs it.',
    ],
  },
  {
    version: '0.9.4.27',
    title: 'Updates get their own page',
    items: [
      'Updates are now a page of their own in the System group, with four tabs: Updates, Schedule, What\'s new and Advanced. The schedule for update checks is in one place, and the release page is under Advanced.',
      'A notice at the top of the Overview says when an update is ready, whichever way updating is set. Update now opens the Updates page, Remind me later hides it until tomorrow morning, and Dismiss hides it for that version only.',
      'The Updates entry in the menu shows a blue dot when an update is ready, yellow when a restart is needed and red when the last update did not work; the Overview shows a yellow or red notice for those two. The line at the bottom of the menu says Update available.',
      'Blocking lists and Traffic labels are now on Protection, under Lists and labels. My network (the printable summary) is under Diagnostics.',
      'Logs, Activity has one time picker, the speed charts say 30 min or 2 h instead of raw seconds, and slow speeds are drawn in kb/s.',
      'Bridge mode says Saved, not applied when it is set up but not yet live. A blocking list that could not be downloaded says so instead of showing an error. Signed-in browsers are named (Chrome on Linux) instead of raw text. The system log says why it is empty and offers Show everything. The data limit alert points to Internet, Usage.',
    ],
  },
  {
    version: '0.9.4.26',
    title: 'More detail when you open a log row',
    items: [
      'Logs: open a DNS lookup to see the response code (with what it means), how long the answer took and who answered, an upstream DNS server or NopeWarden itself.',
      'Open a connection to see how it ended (Completed, Reset, Timed out, Still open or Blocked), the bytes sent and received, how long it lasted, and the rule that decided it with how many connections that rule has matched so far. The count follows the firewall live while Live is on.',
      'Download as spreadsheet (CSV) now exports exactly what the log shows, lookups and connections together, with a column for each of these details, empty where they do not apply. The rule match count is the total when you download.',
      'The outcome, bytes and time are read from the router every few seconds, only for IPv4, and only for connections logged from now on; older rows say not known yet.',
    ],
  },
  {
    version: '0.9.4.25',
    title: 'Watch a change go live',
    items: [
      'When a change goes live you now see the steps, ticked off as they finish: checking it is safe, saving a way back, loading the new rules, filling in the device and country lists, starting the 60-second safety timer, checking you can still reach NopeWarden, and keeping the change or undoing it. Each step says in a line what is happening. Network changes show their own steps.',
      'The list is on the card at the bottom of the window, where the countdown is, and on the Review page. Keep this change, Undo now and the countdown stay on the card.',
      'When it worked the list says Applied successfully and stays until you close it. When a step fails it turns red, says why and stays until you close it, and the previous settings are still in use.',
      'Firewall, Rules: the Paused line is greyed out and says Standing by, nobody paused when no device is paused, with a note that pausing is done on the Profiles page. While someone is paused it shows as active with their names.',
    ],
  },
  {
    version: '0.9.4.24',
    title: 'See why a change was undone',
    items: [
      'A change that is on trial is no longer a red dot in the menu. Red now means something is broken; a change waiting for its check shows blue, and changes saved but not live yet stay yellow.',
      'Keep this change and Undo now can be pressed while a commit is still waiting. Before, both were greyed out during the trial, so a change could not be kept by hand.',
      'When NopeWarden cannot keep a change by itself, a message now says why, instead of the countdown running out in silence.',
      'The System log now records each firewall change applied, each confirmation received or refused, and each change that was not confirmed in time and was undone, with the address and port it came from. Search the log for firewall.',
      'Fixed: Firewall, History showed an undone change as Waiting for ever. It now says Undone, and an alert is raised.',
    ],
  },
  {
    version: '0.9.4.23',
    title: 'Applying changes through the management port',
    items: [
      'Fixed: when the console was opened through the management port, Review refused Verify and commit and said the connection would no longer be allowed to manage this device. The check now knows the management port keeps managing, unless the limit on who may manage leaves out your address.',
    ],
  },
  {
    version: '0.9.4.22',
    title: 'Firewall rules as lines, with live counters',
    items: [
      'Firewall, Rules now opens as rule lines: one row for each rule with its number, name, From, To, Service, Action, Matches and When, the way most firewalls list them. Long lists show the first two entries and +N. Plain cards is still there, narrow windows open on it, and the view you choose is remembered.',
      'Click a line to open it: the rule in a sentence, its counts, and the buttons. The built-in rules open for reading only, and so do the rules other pages make. Paused devices shows who is blocked right now, with a button to Profiles.',
      'A rule with a few services (up to 8) counts each one on its own, so you can see which of them is being used.',
      'The Live switch is on by default: counters update every second or two and a row flashes when it matches, so you can tell whether a rule is really being hit. With Live off the counters still refresh about every 20 seconds.',
      'Export what is shown (CSV) saves the rows on screen, after your search and filters, one row for each service, with the counts and when each last matched. The built-in console rules now count their matches too.',
    ],
  },
  {
    version: '0.9.4.21',
    title: 'One colour code, the names you gave your ports, and a better New service form',
    items: [
      'Status colours are now only red, yellow, green and blue everywhere, including the dots in the menu. The coral that was hard to tell from red is gone, and the build refuses a screen that brings it back.',
      'A port you renamed shows that name everywhere, written Name (enp2s0): in rules and the sentences about built-in rules, Policies, Review, the logs, Routes and Networks.',
      'Threats says Suricata, with its version, on The program card.',
      'Update rules now, Check now and Check again show the same ticked progress as System updates, so you can see that something is happening.',
      'Fixed: renaming a service now also updates the routes that use it. A service a route uses cannot be deleted, and the message names the route. A route whose service list has gone sends nothing, and shows Needs attention instead of sending everything.',
      'New service: choose TCP, UDP, TCP and UDP or ICMP, type a port or range, and press Add. Each entry is a line you can remove, mistakes are explained under the box, and known names and pasted lists still work.',
    ],
  },
  {
    version: '0.9.4.20',
    title: 'Speed control, a redesigned Usage page and controls that match',
    items: [
      'Internet has a new Speed control tab. Limit speed holds a network, profile or device to a download and upload speed in Mb/s, shared or for each device. Give priority puts traffic first (High), last (Low) or back to Normal when the connection is full. Either can cover everything or only chosen apps, categories and websites, always or at chosen hours. One switch, Speed control is on, pauses all entries at once. The same entries appear on the Policies page, and Create policy can start them.',
      'Priority works through the smart queue, so Keep delay low must be on for the connection. The queue now tells traffic classes apart when a priority entry exists, and says so in the console if this device can only do it for uploads, or not at all. A fault never slows anything.',
      'Usage is redesigned. Data use shows what each internet connection used, or all of them, for this billing period, this or last month, 30 or 7 days, or dates you choose, with the busiest devices per connection. Data limit is set per connection, with its own billing day and warning levels. A device is counted against the connection its traffic really used.',
      'Every date, time and drop-down in the console now looks and behaves the same: typed or picked times, a calendar for dates and ranges, and drop-downs you can use with the keyboard and type to find an item.',
      'Fixed: after Apply, the Review page could keep showing the change as on trial, or still waiting, after the countdown reached zero, and the automatic keep could start late. The page now follows NopeWarden every second while a change is on trial and asks again when the time is up.',
      'Fixed: the Overview page asked for a live feed that did not exist and showed an error in the browser log.',
    ],
  },
  {
    version: '0.9.4.19',
    title: 'A management port of its own',
    items: [
      'Firewall, Protection has a new Management port card. Set one spare port aside so the console and remote login answer there and nothing is routed through it. It takes an address from DHCP or a fixed one, and a fixed address that overlaps a network this device already serves is refused.',
      'A change waits for you to open the console on the new address. If nobody does within a minute it is undone on its own, and Undo now does it sooner.',
      'Close the console on the LAN while this port is healthy is off until you turn it on, and can only be turned on from the management address. The LAN console closes after the port has been healthy for half a minute and opens again if it stops answering for a minute.',
      'The port is listed under the protected rules, is kept when you restore a backup or reset the network, and is not used in bridge mode.',
    ],
  },
  {
    version: '0.9.4.18',
    title: 'Keep delay low follows what your line really delivers',
    items: [
      'Keep delay low used to hold a connection to 95 % of the speed the plan promises, which on a variable line (such as home internet) is above what it delivers, so it never did anything. It now shows three speeds per connection: Promised, Measured typical (a low figure, the 25th percentile, of the last 7 days of full tests on a quiet line) and the speed in use. Follow my measurements is the default once it is on; Use fixed numbers keeps the old way with speeds you type.',
      'It moves toward the measured speed a little each day (at most 10 % up and 20 % down), and says Learning, holding to 85 % of your best test, until there are about 8 good tests over at least 2 days.',
      'Measure the line automatically (off until you turn it on, and offered when you switch Keep delay low on) tests about every 4 hours, only after the line has been quiet for a minute. The queue is taken off for the length of each automatic test so it measures the line, not the queue. Failed tests, short tests, tests on a busy line, tests that went through the queue and tests during an outage are never counted.',
      'Check the difference measures the delay under load without and with the queue, and says in words whether it helped.',
    ],
  },
  {
    version: '0.9.4.17',
    title: 'Threats gets its own page, and blocks hold many things',
    items: [
      'Threats is now its own item in the menu, with the tabs Overview, Alerts, Settings, Rules and Advanced. The Overview opens with a one-line status and the numbers; the Off / Watch switch, the explanation and the install and remove buttons moved to Settings. The charts now have axes, a peak figure and a legend, like the DNS page. Old links to Protection, Threats still work.',
      'A block can now have a name and hold many apps, categories and websites at once, and apply to everyone or to any mix of networks, profiles and devices, with one How, one When and one on/off switch. Without a name it is named for you, such as "Block Facebook and 3 more". Your existing blocks became blocks of one automatically.',
      'Exceptions are their own list. The Blocks page has starting points (Social media, Streaming, Gaming, AI chat) and shows how many lookups each block refused in the last day.',
      'Removing a network, profile or device now takes it out of the blocks that used it, and the question tells you which blocks are affected. Blocks are part of backups and restores.',
    ],
  },
  {
    version: '0.9.4.16',
    title: 'Threat watching recovers after a failed start',
    items: [
      'Fixed threat watching staying stuck on \"keeps failing\" even after the settings fix in 0.9.4.15. When the engine failed to start it left a process-ID file behind, and it then refused every later start with \"pid file exists but appears stale\". NopeWarden no longer asks the engine for that file, clears the old one, and restarts the engine.',
      'The failure message now shows the engine\'s own words even when they do not contain a word like \"failed\".',
    ],
  },
  {
    version: '0.9.4.15',
    title: 'Threat watching starts properly',
    items: [
      'Fixed threat watching stopping again and again on a real install. NopeWarden\'s settings for the engine replaced its built-in list of address and port groups, so none of the real rules could load and it exited. It now keeps them, and the engine starts with the full rule set. An update fixes it by itself: NopeWarden rewrites the settings and restarts the engine.',
      'When the engine does fail, the message now shows its own reason (for example which rule could not load) instead of a general line from the system.',
    ],
  },
  {
    version: '0.9.4.14',
    title: 'Calmer DNS health, a Threats overview, and clearer installs',
    items: [
      'The red \"No DNS server is answering\" message now appears only when every DNS server is really down and none has answered for 30 seconds. A slow server is no longer treated as a dead one, a server needs several failed lookups over at least 10 seconds before it is marked down, and Resolve directly is never called slow just because a new name takes a moment to find. Servers are tested every 30 seconds.',
      'Installing or removing threat watching shows a ticked list of steps (checking the package list, downloading, installing, keeping it off until you turn it on, checking it is in place), and the install box now shows the real download size instead of 0 MB.',
      'The Off / Watch and tell switch moves the moment you click it and says what it is doing while the program starts or stops.',
      'The Overview box for the internet shows the name you gave the connection, for example T-Mobile (enp0s20f0).',
      'The Overview map opens on Both and the last 15 minutes.',
      'Threats opens on a new Overview, laid out like the DNS one: status, traffic watched, load on the machine, rules and alerts, charts, what was seen, which devices were involved, and what the program has been doing.',
      'The Protection tab "Apps & topics" is now called Blocks, and the screens say category instead of topic.',
    ],
  },
  {
    version: '0.9.4.12',
    title: 'Resolve directly tells you what it is doing',
    items: [
      'Choosing Resolve directly (or switching back) now shows what NopeWarden is doing while you wait: setting up Unbound, waiting for its first answer, then switching your devices over. The switch is locked until it finishes, and if it cannot be done the message says why and that your lookups are unchanged.',
      'This update also carries the corrected map (Both) colours and the help wording fix that 0.9.4.11 was rebuilt with.',
    ],
  },
  {
    version: '0.9.4.11',
    title: 'Resolve directly works on a new install, and a tidier console',
    items: [
      'Resolve directly now works on a fresh install. Unbound\'s DNSSEC key file was missing, so it refused to start; NopeWarden now puts it in place, and says so in plain words if it cannot.',
      'The Overview box was reworked. The status line says how many things need your attention and links to where. The box has two lists (needs your attention, and suggestions), a hide arrow, Dismiss for suggestions and Remind me later for amber items. Your choices are saved under your account, so they follow you to every browser. The dots in the left menu always stay.',
      'The Overview map has Both, with green lines for what your devices connect to and amber lines for attempts from the internet.',
      'A larger badge and name on sign-in, setup and the browser installer, and a larger header in the left menu. The box at the bottom of the menu is tidier, with a Sign out button.',
      'The text installer: grey notes use the normal text colour so they read on any terminal, and the DHCP check listens longer, asks again, and reads the address the live system already has. If a port has an address but no server answered, it says so and leaves DHCP off instead of claiming there is none.',
      'The installer image no longer flashes an Automatic boot countdown on older (BIOS) machines.',
      'Undo on a deleted DNS list cancels the waiting removal instead of adding a second waiting change.',
    ],
  },
  {
    version: '0.9.4.10',
    title: 'An installer that checks for other DHCP servers',
    items: [
      'The installer always asks which port is the internet (WAN) and which is your own network (LAN). Beside each connected port it says whether a DHCP server answers there.',
      'For your own network it asks for NopeWarden\'s address and whether NopeWarden should hand out addresses (DHCP). If another DHCP server already answers on that network, the default is off and the screen says why. Setup then starts with address handing-out off, with a note, and NopeWarden hands out no addresses while you finish setup.',
      'If both ports look like one network (a virtual machine with both adapters on the same bridge as your home network), the installer warns you. It still lets you continue, and it never suggests a LAN address inside the internet port\'s network.',
      'Installer screens are centred top to bottom as well as across, whatever the screen size. Over a serial console they show just the NopeWarden name, and the installer asks the terminal how big it is.',
      'The badge, favicon, console colours and boot pictures now come from one set of design tokens, so changing a colour changes them together.',
    ],
  },
  {
    version: '0.9.4',
    title: 'One update schedule, a resolver of its own, and staged DNS changes',
    items: [
      'The product is named NopeWarden. Hard on threats. Easy on humans. The console, installer image, program, services and folders all use the name. The shield icon stays until the logo is ready.',
      'One schedule for every recurring job: update checks, blocklist refreshes, country lists and threat rules each have a plain-English schedule (every N hours, daily at a time, weekly on a day, or off) in one place, and failed jobs retry after 5 minutes, 15, 30 and then every hour.',
      'DNS: a Resolve directly option turns on NopeWarden\'s own Unbound resolver, which asks the internet\'s root servers itself instead of a forwarder. It can check DNSSEC and explains failures in plain words. If direct lookups stop working, NopeWarden falls back to your encrypted forwarders and raises an alert. Unbound is off until you turn it on and only listens inside the box.',
      'DNS changes now wait on the Review page like firewall and network changes, so you can apply or discard them together. Review has a DNS area.',
      'DNS follows the route: a device sent through a VPN or a connection looks names up through that same path, and the Routes page shows the addresses learned for each website route.',
      'Threat alerts look up the name of the address behind them and retry when the first try finds nothing, with a Look up again button.',
      'Protection: View names shows what a topic blocks, per profile custom blocked names, and names you can allow from inside a category.',
      'Long pages show the first 15 rows and a Show more button.',
      'Security check: each fix button opens the exact window to change, not just the page.',
      'Overview: 5 and 15 minute map ranges, a hover popout beside the country, and a capped Your VPNs card. VPNs and networks have their own traffic graph and totals, and a quiet tunnel shows a grey Idle pill.',
      'VPN import accepts more real-world profile shapes (byte-order marks, Windows line endings, more harmless directives) and says clearly why a profile is refused.',
      'Forget device now waits on the Review page like other changes (the device shows Forgetting… until applied, and Discard keeps it). Countries and the add-network form have Save for later too, and adding a network now shows up on Review in plain words. Changes to blocks (apps, topics and names) wait with the DNS changes. Deleting a block or a downloaded blocklist no longer asks "Are you sure?": it is removed at once with an Undo button.',
      'Fixed: saving an edited route said invalid request body; the Updates are ready banner stayed after installing; the packages updated line and Checked time were missing; the Pause menu on Routes was out of proportion; logs refresh twice as fast.',
    ],
  },
  {
    version: '0.9.3.2',
    title: 'VPN tunnels set their own packet size',
    items: [
      'A VPN to a provider could show Working while secure web pages stalled, because the tunnel was set to a larger packet size than the connection under it carries. NopeWarden now measures the size that gets through and sets it itself, for every VPN that goes out to a provider or another site, and measures again after every reconnect, when big packets stop getting through, and every 30 minutes.',
      'Each provider VPN card shows the Packet size and where it came from (measured, from the profile, or default), and a Test packet size button measures it now and shows what each address answered.',
      'A new Test addresses card lists the addresses used for the test (1.1.1.1, 8.8.8.8, 1.0.0.1, 8.8.4.4, 9.9.9.9) and the services asked what address the internet sees (Google, Amazon, Cloudflare). You can add, change, remove and reorder them, and restore the built-in lists.',
      'Internet sees you as now fills in within about 20 seconds of a reconnect (a failed lookup used to wait ten minutes), and says why when it cannot be found.',
    ],
  },
  {
    version: '0.9.3.1',
    title: 'Private Internet Access profiles can be imported',
    items: [
      'VPN: a profile with the disable-occ line (Private Internet Access files have it) was refused. It is now accepted. The line only turns off an OpenVPN warning about mismatched settings. Anything that could run a program or read another file is still refused.',
    ],
  },
  {
    version: '0.9.3',
    title: 'Discard one change, Save for later in more places, and US time zones',
    items: [
      'Review changes: every waiting change now has its own Discard. It puts things back to just before that change. Because later changes can depend on earlier ones, it also drops the changes saved after it, and says so before it does. Discard all changes is still there.',
      'Save for later is now on more forms: the open-port editor, services, device groups, DHCP networks and reservations, connection groups, VPN settings, letting networks see each other, and who may manage NopeWarden. Each one also has Save and apply.',
      'Setup wizard: the time zone list starts with the United States (Eastern, Central, Mountain, Arizona, Pacific, Alaska, Hawaii) in plain words, followed by every other zone.',
      'The installer image is now named with its version (for example nopewarden-0.9.3-amd64.iso). The 0.9.2 image was called nopewarden-dev-amd64.iso by mistake.',
    ],
  },
  {
    version: '0.9.2',
    title: 'Save and it is live, a Policies page, and tidier alerts',
    items: [
      'Saving is now one click for a signed-in administrator. Save and apply puts a change live after a moment, keeps it once NopeWarden sees you can still reach it, and undoes it on its own if you cannot. Save for later keeps it on a new Review page instead.',
      'Review changes lists everything waiting in plain words, shows any warning or problem NopeWarden found, and one Verify and commit button puts it all live. Discard all changes puts everything back as it was running. A badge at the top of every page shows how many changes wait.',
      'If NopeWarden finds something worth a second look, or other changes are already waiting, nothing goes live on its own. It waits on the Review page with the reason.',
      'The authenticator code, when you asked for one before changing the firewall, is asked once and remembered for 15 minutes.',
      'New Policies page, next to Routes: every firewall rule, route, block and open port in one list, in plain words, with Pause, Turn off, Edit and Remove. Create policy moved there from the left menu.',
      'A sun and moon button by your name at the bottom of the menu switches between the dark and light look. The saved look now applies on every page and every visit (it used to apply only after opening Settings).',
      'Phones: the top bar is short (logo, Search, Menu) and the menu opens as a list instead of running off the edge. Rows in lists stack their buttons instead of squeezing the text.',
      'Threat alerts: each kind of alert on each device is one card with two lines of text, the website or name for each address, one Details link, and the buttons in a column on the right.',
      'Overview, What happened: one line per kind of alert per device, with how many different addresses were involved.',
      'Links are now the same size as the text around them everywhere, and the menu dots are all one size.',
    ],
  },
  {
    version: '0.9.1',
    title: 'One way to make every policy',
    items: [
      'Create policy: one button at the top of every page asks what you want to make (a route, a firewall rule, a block or a port forward) and opens the right form. They share the same pickers for who and where, the same list of services and ports, and the same wording.',
      'Routes can now match any mix of websites, addresses, ranges, countries and ports (for example NNTPS or NTP) for a network, a device, a group or an address, and send it through a VPN, a connection group, or one or several of your internet connections in the order you choose, to fail over or to share. A connection kept only as a backup can be used alone. Up to 16 routes.',
      'The same controls everywhere: every route, firewall rule, block and port forward can be turned on or off, paused (for a set time or until you resume), put on a schedule, edited and removed, and says in words what it does.',
      'Admin activity now lists every policy change, including pause, resume, schedule edits and reordering.',
      'Devices: Keep address and Release address on each row give a device a fixed DHCP address in one click.',
      'Overview: the networks table says how to read its charts and shows each network\u2019s peak. More cards have a helper line.',
      'Threat alerts are far less cluttered: each kind of alert on each device is now one card with a count and time range, one set of buttons, and its addresses folded under Show the addresses. Repeated identical rows are merged.',
      'On a phone, the Create policy choices and the route type selector now wrap instead of cutting words off.',
      'Backups, restore and reset include the new route, pause and schedule settings; old routes come back unchanged.',
      'Behind the scenes: tests now fill routes, connection groups, countries and firewall sets to their limits and one past, and every read-only page of the console is called on a fresh box.',
    ],
  },
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
      'Storage (Settings, Alerts & logs): a table of everything NopeWarden keeps, with entries, estimated size, the oldest entry and the rule that removes it. Every table that grows now has a rule: threat alerts and internet outages go after 365 days (you can change it), firewall change copies keep the newest 200 (you can change that too), and the total size limit now also trims the smaller histories if it is still exceeded. The limit is now applied every hour, not only when you pressed Clean up.',
      'An unused table that was never filled in was removed.',
      'Logs has a new Admin activity tab (administrators only): sign-ins, failed sign-ins and the changes people made, with who, what, when and from where. Search takes several words and a time range, and the count says how many match.',
      'Diagnostics now starts with Is NopeWarden itself working? One sentence, problems first, each with a Look button, then facts: how long NopeWarden and the machine have been up, when the firewall was last applied, disk and log space, the clock, and warnings in the last hour.',
      'Diagnostics has a System log tab: warnings and errors by default, Everything on request, search across up to 5000 lines with the words highlighted, a time range, and Follow. Known messages get a plain explanation.',
      'Console connections records every live connection your browser opens to NopeWarden: when, from where, how long, and why it closed, so the idle connection-error bursts can be told apart as the box or the network. Unusual endings also go to the system log.',
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
    title: 'Wireless removed: NopeWarden is wired only',
    items: [
      'Wi-Fi support is gone. The Wi-Fi page and its link in the sidebar, Wi-Fi join cards, the Wi-Fi internet option in the setup wizard, and the Wi-Fi columns and filters on Devices are removed. If you need Wi-Fi, plug in a separate access point or Wi-Fi router; NopeWarden still protects everything that connects through it.',
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
    version: '0.8.49',
    title: 'Build fix for 0.8.48 (build fix)',
    items: [
      "The terminal now starts reliably when the machine's working folder is not available to it.",
      'The port-name lookup uses the same list of ports as the rest of the console.',
    ],
  },
  {
    version: '0.8.48',
    title: 'Diagnostics page, terminal, Suricata reasons, SSH fix',
    items: [
      'A new Diagnostics page in the left menu: every part of NopeWarden with its state, why it is failing in plain words, its log, Restart, and Save a support bundle (passwords and keys hidden). The network tests are on it too.',
      'An optional Terminal on the Diagnostics page: a command line on the firewall from the console. It is off until you switch it on, asks for your password or code every time, closes by itself after 10 idle minutes, and each open and close is recorded in the Logs.',
      'Threat watching now says when the program keeps failing and restarting, shows the last thing it said, and has a Program log under Advanced. Its default memory limit is 2 GB.',
      'Remote login (SSH) could not be turned on because the machine had no SSH identity. NopeWarden now makes one when you turn it on, and once at first start.',
      'A new Protection page in the left menu. Overview gives a one-line verdict and the Security check; Apps & topics (moved from DNS) blocks apps, topics and websites; Threats is threat watching. Block lists for ads, trackers and adult sites stay under DNS.',
      'Threat watching is no longer installed behind your back. Press Install threat watching: NopeWarden shows the packages, the download size, the disk space needed and any worry about this machine, and installs only when you agree. Choose Light, Balanced or Strict, set a processor and memory limit, update the rules (and roll back to the previous ones), search and switch off single checks, and save or load your tuning. It only watches and tells you; blocking threats is not available yet. If it stops, nothing changes for your devices, and if it uses all of its processor share for ten minutes NopeWarden switches it off and says why.',
      'Add a network now offers five kinds: Trusted, Private, Guest, Isolated and Custom. Guest devices cannot see each other unless you turn that on. Choose the whole port or a VLAN on any port, and a VLAN with no address and no DHCP for a link to a switch or access point.',
      'If threat watching is on but the program is not running, the page now says why (for example it ran out of memory). Its default memory limit is now 2 GB, because the full rule set needs more than 1 GB.',
      'The Install window now shows the real download and disk size. The groups of checks sit in columns.',
      'Opening a port that the firewall itself uses (such as the console) now warns you instead of refusing.',
      'Choose how often NopeWarden looks for updates: 6 hours, 12 hours (the usual), a day, a week, or your own number of hours.',
      'More topics to block (scams, piracy, crypto mining, drugs, games, video, music, messaging, shopping, AI). Settings, Advanced shows which version of the lists is in use. Blocking also has a thorough choice, which is saved but does not act yet.',
      'If a block or threat watching ever goes wrong, your internet is not affected. This is now stated beside the setting, in the Logs when it happens, and in the Protection and Reference chapters.',
    ],
  },
  {
    version: '0.8.47',
    title: 'Threat watching says why it is not running',
    items: [
      'When threat watching is on but its program is not running, the page now says why: out of memory, still starting, or the last error.',
      'The memory limit now starts at 2 GB, because 1 GB was too small for the full rule set.',
    ],
  },
  {
    version: '0.8.46',
    title: 'Install window shows a real size',
    items: [
      'The install window showed 0 MB. The size is now worked out from the package list, and no size is shown when it cannot be worked out.',
      'Rule groups are shown in columns.',
    ],
  },
  {
    version: '0.8.45',
    title: 'Threat watching as an optional install, and more',
    items: [
      'Threat watching is an optional install. An Install button shows the packages, their sizes and any warnings for your machine. It runs watch-only, with processor and memory limits, and it never blocks anything.',
      'Presets Light, Balanced and Strict; rule updates that can be rolled back; rule search and groups; export and import; a watchdog that switches it off after ten minutes at its limit and says why.',
      'A new Protection page holds the security check and blocking by app and topic.',
      'Settings, Advanced shows which block list versions are in use, and the pages say that blocking fails open: if anything goes wrong, nothing is blocked because of it.',
      'Port forwards on a port the firewall itself uses now warn instead of refusing.',
      'Add a network offers five kinds (Trusted, Private, Guest, Isolated, Custom), and a VLAN can be made on any port.',
      'Update checks: choose how often to look (every 6 hours, every 12, daily, weekly or your own number of hours).',
      'Number boxes can be emptied to type a new value, and Help with this page is blue.',
    ],
  },
  {
    version: '0.8.44',
    title: 'First build of the 0.8.45 package (build fix)',
    items: [
      'Same content as 0.8.45. Its build failed on a test, so 0.8.45 replaced it.',
    ],
  },
  {
    version: '0.8.43',
    title: 'Open ports made simple, block apps and topics, threat watching',
    items: [
      'Open a port in three taps: pick the app, pick the device. NopeWarden says which port it used and lets you change it. It never picks a port for you; if one is taken it asks which one to use. Each device row also has an Open a port button. Open ports now show in the Rules list (under Show system rules) and in Services.',
      'You can give an app a different outside port and a different port on the device, which suits several copies of the same app. Renaming a service renames everything that uses it.',
      'Block an app, a topic (adult, gambling, social media, ads, malware) or a website for everyone, a network, a profile or one device, always or at set hours, with an exception for a device and a pause button. It blocks by name. Thorough checking of the traffic itself is shown but is not switched on yet. If anything goes wrong nothing is blocked.',
      'Each blocklist has View names, with a search and an Allow button.',
      'Threat protection (Firewall, Protection): reads what a detection engine notices and shows it in plain words in the Logs, with Allow for this device, Allow for all, Never check this device, and an Advanced tab. It watches and tells; it does not block. The engine itself is not installed by this version, so until it is the page says so.',
      'The Logs hide NopeWarden\'s own health checks. A checkbox shows them again.',
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
      'Fixes the automatic build for 0.8.39, which stopped because a help chapter shared a name with an older document on case-insensitive disks. The help now lives in its own folder. Nothing changes in how NopeWarden works.'
    ],
  },
  {
    version: '0.8.39',
    title: 'Built-in help, and undo instead of "Are you sure?"',
    items: [
      'Every page now has a "Help with this page" link. The help is built into NopeWarden, so it works with no internet: one chapter per page with a picture, a Getting started guide, a guide for when you are locked out, and a reference. It is also at /help/ on your box.',
      'Deleting a firewall group or a route no longer asks "Are you sure?". It is removed at once and a message offers Undo for a few seconds, which puts it back exactly as it was.',
      'The first-run setup now offers Bridge as a third choice next to the router choices, and walks you into the bridge steps.',
      'A new help chapter, "How easy is it?", lists the everyday jobs and how many steps each takes. Opening a port by hand (6 steps) is the longest and is next to shorten.',
    ],
  },
  {
    version: '0.8.38',
    title: 'Build fix',
    items: [
      'Fixes an automatic test that failed only in the first hour of a new month (the monthly provider report test looked at a time that had not happened yet). Nothing changes in how NopeWarden works.',
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
      'Behind the scenes, three automatic checks now run on every build and stop it if something is wrong: a colour or size written outside the shared styles, a style name that is not defined, and a screen that calls a part of NopeWarden that does not exist.',
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
      'Change opens a three-step walkthrough. Step 1: choose Router or Bridge. Step 2: plug in the cables and NopeWarden notices each port as it goes in (or pick the port from a list if the cable is already in). For a bridge you plug in the cable from your other router first, then your devices, and you can add a spare port for managing the box. Step 3: a plain cabling summary, then Apply.',
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
      'Fixed a likely cause of a provider VPN (such as ExpressVPN) that connected and then carried nothing: NopeWarden now tells the system network service to leave the address and route OpenVPN sets on its tunnel device alone. After updating, the Internet page shows changes waiting to be applied; press Review and apply once so the tunnel file is rewritten.',
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
      'Provider VPN files (ExpressVPN and others) now keep the certificate check and the packet-format line exactly as the provider wrote them. NopeWarden used to change one and drop the other, which could stop the connection from ever starting.',
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
      'Fixed: the build for 0.8.21 and 0.8.22 failed on a test that was too strict about timing. Nothing about how NopeWarden runs changed, and 0.8.21 and 0.8.22 include nothing else that is different.',
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
      'Move a device to another network now works from any port or Wi-Fi: NopeWarden moves that one device across to the VLAN network, so the device does not have to be plugged into the VLAN\'s port.',
      'Cable between the two units (optional): choose a spare port on each and the pair talks over it (settings copy, updates, connection copy), falling back to the shared network if it is unplugged. An unplugged cable never makes a unit take over (Settings, Advanced, pair).',
      'Keep the spare\'s internet connection ready (optional): the spare keeps its internet port connected but silent, copies the active unit\'s hardware address, and takes over faster. Not for PPPoE or Wi-Fi connections.',
      'Fixed: a build could fail on a slow machine because a test did not wait long enough for the console to start.',
    ],
  },
  {
    version: '0.8.19',
    title: 'Device placement, approvals, undo and more',
    items: [
      'Move a device to another network (Devices, Details): like UniFi\'s Virtual Network Override. Pick a VLAN network and NopeWarden puts that one device on it, wired or wireless. The device must be plugged into the same port the VLAN is on, and takes the change when it next asks for an address.',
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
      'High availability: pair two NopeWarden units so the second takes over by itself when the first fails. Settings are copied across, the spare is read-only and stays quiet, and connections carry over. Settings, Advanced. Needs a second unit with at least as many ports.',
      'Bridge mode: put NopeWarden between another router and your network and filter without routing. The console stays reachable on its own address, setup has a 60-second undo, and Reset to default always brings routing back. Settings, Advanced.',
      'Traffic labels: an optional small program names the application behind each connection by looking at the first packets (YouTube, Zoom, BitTorrent), and flags cautions such as self-signed certificates. Off by default. It is on fresh install images only.',
      'The top of Overview is tidier: suggestions, the security check and common setups now sit together in one collapsed card.',
    ],
  },
  {
    version: '0.8.17',
    title: 'Cards, reports and remote access',
    items: [
      'Wi-Fi join cards: a QR code that joins your Wi-Fi when scanned, and a card to print with the name and password. Works for Wi-Fi from NopeWarden and for any other access point (Wi-Fi page).',
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
      "Why can't it reach that? Pick a device and a website. NopeWarden checks pauses, DNS filtering and the firewall in order, says which one is in the way, and offers a fix such as Allow this site. It is on the Internet page and inside each device.",
      'Security check on the Overview: the things that most often leave a network exposed, each with a button that takes you to where it is fixed, and one plain sentence at the top.',
      'Undo: before every firewall or network change goes live, NopeWarden keeps a restore point. Settings, Changes lists them with a Revert button. A revert is reviewed and applied the usual way, with the usual 60 seconds to take it back.',
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
      'NopeWarden now tells you when your connection is shared by your provider (CGNAT) or sits behind another router, what stops working because of it, and the exact ports to open on the other router.',
      'One card for UPnP, under Firewall, Open ports. Choose who may use it and through which connection. It cannot be switched on until you have chosen.',
      'Open a port by picking the app and the device. Plex, Jellyfin, Minecraft and more are built in; add your own. The forward follows the device if its address changes, and can be limited to certain countries or addresses.',
      'Discovery: let printers, Chromecast, AirPlay and Home Assistant find each other across networks you pair (Firewall, Discovery).',
      'Alerts by email or ntfy: a new device, the internet going down and back, a watched device going offline, or NopeWarden repairing something itself. Quiet hours hold them until morning, and a test button proves it works (Settings).',
    ],
  },
  {
    version: '0.8.14',
    title: 'Countries, Routes and spare ports',
    items: [
      'Countries: block or allow whole countries in one place, for traffic going out, coming in, or both. NopeWarden warns you before a choice could lock you out of your own country.',
      'Routes: send only the websites, addresses or countries you choose through a VPN, or through one internet connection, and leave everything else alone. A "stop if down" switch keeps traffic from leaking if the VPN drops. Routes carry IPv4 only.',
      'Spare ports: on a network that uses a bridge, tick the ports that should act as a switch for it. NopeWarden tells you why a port cannot be ticked, and nothing changes until you apply.',
      'Adding a VPN no longer forces you to choose who uses it: pick "Nobody yet" and decide later.',
      'After you apply a change, NopeWarden checks that you can still reach it and keeps the change by itself. If you cannot, it still undoes itself within 60 seconds. You can switch this off to confirm by hand.',
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
  {
    version: '0.8.11',
    title: 'A Wi-Fi watchdog that restarted a working card',
    items: [
      'Fixed the watchdog restarting a working Wi-Fi card about once a minute.',
      'New switch on the Wi-Fi page: Restart the card automatically if it stops working (on by default).',
    ],
  },
  {
    version: '0.8.10',
    title: 'Wi-Fi "Check this card" table tidied',
    items: [
      'Columns are aligned and the headings read \'What the card says it can do\' and \'Listening test\'.',
    ],
  },
  {
    version: '0.8.9',
    title: 'Speed test schedule saves again',
    items: [
      'Saving the speed test schedule said \'invalid request body\'. Fixed.',
    ],
  },
  {
    version: '0.8.8',
    title: 'Build fix for 0.8.7 (build fix)',
    items: [
      'The new SSH firewall test needed administrator rights to run. Nothing changed on screen.',
    ],
  },
  {
    version: '0.8.7',
    title: 'Remote login (SSH) and a steadier Wi-Fi card',
    items: [
      'Remote login (SSH) in Settings: off by default, one switch. Sign in with an administrator name and the console password. It is reachable only from networks allowed to manage the device, never from the internet side, with a limit on wrong passwords, no root login, and every sign-in and failure in the log.',
      'Wi-Fi stall: the status is honest (Broadcasting only when the access point answers), the card is restarted automatically when it stops, a Keep the card awake switch turns power saving off, and a Wi-Fi report button gathers the details for troubleshooting.',
    ],
  },
  {
    version: '0.8.6',
    title: 'Switch a Wi-Fi card off and on',
    items: [
      'Each Wi-Fi card has a Switch off and Switch on button. Its settings and password are kept.',
    ],
  },
  {
    version: '0.8.5',
    title: 'Backups, forced DNS and many smaller things',
    items: [
      'Backups: on a schedule, keep the newest few, every backup checked when it is made. Check shows what is inside against what you have now, restore makes a safety backup first, download with a password, and upload a backup from elsewhere.',
      "Force DNS by network: devices must use NopeWarden's DNS, with exceptions for chosen devices or groups.",
      'Logs, Recording and storage shows the last clean-up and has Clean up now.',
      'Speed test: a list of test servers, a speed readout every second, and data used per country on the map.',
      "Change a network's port or VLAN after it is made, an extra ARP check before an address is handed out, pause for OpenVPN devices, and changes to a device group now reach the VPN.",
      'A new DNS Overview layout, and dotted gaps in the small charts where nothing was recorded.',
    ],
  },
  {
    version: '0.8.4',
    title: 'Services and a better address picker',
    items: [
      'A Services page: named services (a protocol and ports) you can use in firewall rules.',
      'Firewall rules got a better picker for who, where and what.',
    ],
  },
  {
    version: '0.8.3',
    title: 'DNS server checks',
    items: [
      'NopeWarden now checks that each DNS server answers, and shows the result.',
    ],
  },
  {
    version: '0.8.2',
    title: 'Chart fixes',
    items: [
      'Spacing and drawing fixes in the Overview charts.',
    ],
  },
  {
    version: '0.8.1',
    title: 'Overview and country fixes',
    items: [
      'Fixes to the Overview map and charts, and to country-based rules and the country lists.',
    ],
  },
  {
    version: '0.8.0',
    title: 'The Overview dashboard, speed test and device map',
    items: [
      'The Overview was rebuilt: what needs you first, an internet hero with a line per connection and history from 10 minutes to 30 days, tiles for devices, DNS, DHCP and security, a world map of where connections go, busiest devices, machine health and recent events.',
      'History is kept in three tiers: every 30 seconds for 2 hours, every 5 minutes for 25 hours and hourly for 31 days.',
      'Internet: a speed test with history. Devices: a List and a Map view.',
      'Recovery works over a serial console on machines without a screen.',
    ],
  },
  {
    version: '0.7.5',
    title: 'A small fix to how firewall changes are applied',
    items: [
      'A small fix to how firewall changes are applied.',
    ],
  },
  {
    version: '0.7.4',
    title: 'Boot menu look',
    items: [
      'Changes to the look of the boot menu.',
    ],
  },
  {
    version: '0.7.3',
    title: 'Boot menu look',
    items: [
      'Changes to the look of the boot menu.',
    ],
  },
  {
    version: '0.7.2',
    title: 'First-start setup fix',
    items: [
      "A fix in the first-start setup and the installer's management network step.",
    ],
  },
  {
    version: '0.7.1',
    title: 'Boot menu look',
    items: [
      'Changes to the look of the boot menu.',
    ],
  },
  {
    version: '0.7.0',
    title: 'Installer and boot screens reworked',
    items: [
      'The installer works out a plan for the ports and the management network, and the boot screen theme changed. The setup wizard follows the same plan.',
    ],
  },
  {
    version: '0.6.3',
    title: 'Installer screen tweaks',
    items: [
      'Small changes to the installer screens.',
    ],
  },
  {
    version: '0.6.2',
    title: 'Installer screen tweaks',
    items: [
      'Small changes to the installer screens.',
    ],
  },
  {
    version: '0.6.1',
    title: 'Wi-Fi in the setup wizard',
    items: [
      'The wizard can use a Wi-Fi network as the internet, and can set the LAN up as a bridge so Wi-Fi can join it later. (Wi-Fi was removed from NopeWarden in 0.8.53.)',
    ],
  },
  {
    version: '0.6.0',
    title: 'Setup wizard and checklist',
    items: [
      'The first-run wizard has the internet port as its own step: pick the port the modem plugs into and how the provider connects you.',
      'Your network step: the port, the address, and a DHCP switch.',
      'A Finish setting up checklist on the Overview, in any order, that goes away when done.',
      'Add a network asks what it is for (My own devices, Guests, Cameras and smart-home) and sets the switches for you.',
    ],
  },
  {
    version: '0.5.9',
    title: 'Add a network fix',
    items: [
      'A fix in Add a network.',
    ],
  },
  {
    version: '0.5.8',
    title: 'Recovery over a serial console',
    items: [
      'The setup and recovery screens work over a serial console on headless machines.',
    ],
  },
  {
    version: '0.5.7',
    title: 'Installer screen fixes',
    items: [
      'Fixes to the installer screens.',
    ],
  },
  {
    version: '0.5.6',
    title: 'Installer and boot screen fixes',
    items: [
      'Fixes to the installer and boot screens.',
    ],
  },
  {
    version: '0.5.5',
    title: 'Installer screen fixes',
    items: [
      'Fixes to the installer screens.',
    ],
  },
  {
    version: '0.5.4',
    title: 'Build and image changes only (build fix)',
    items: [
      'Changes to how the install image is built. Nothing you would see in the console.',
    ],
  },
  {
    version: '0.5.3',
    title: 'Build and image changes only (build fix)',
    items: [
      'Changes to how the install image is built. Nothing you would see in the console.',
    ],
  },
  {
    version: '0.5.2',
    title: 'A Networks page',
    items: [
      'Networks has its own page, and the DHCP, Firewall and Internet pages were tidied to match.',
    ],
  },
  {
    version: '0.5.1',
    title: 'Devices page tweaks',
    items: [
      'Small changes to the Devices page.',
    ],
  },
  {
    version: '0.1 to 0.5.0',
    title: 'The first builds',
    items: [
      'A console for networks, DHCP, DNS, firewall rules, VPN and logs, with a text installer, a recovery screen (Alt+F2), a watchdog that restarts the console if it stops, and lock-out protection with a way back.',
      'One update button for NopeWarden and the system together, signed and checked, with a restart-needed notice.',
      'Change the console port, restart or shut down from the console, and reset to default.',
      'Add a network as one form, with an address check and a check for another DHCP server.',
      'VPN: import a provider file, servers for your own devices, site-to-site over WireGuard.',
      'A Devices page with filters, vendor names, icons and per-device speed and data used.',
      'Logs with click-to-filter, a time range, CSV download, a summary and a world map.',
      'App names for about 70 well-known apps, and the first groundwork for a pair of boxes (high availability).',
    ],
  },
];
