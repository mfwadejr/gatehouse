// Plain-language release notes, newest first. Add an entry for every release.
export const RELEASES = [
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
