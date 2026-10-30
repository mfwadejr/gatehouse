// Plain-language release notes, newest first. Add an entry for every release.
export const RELEASES = [
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
