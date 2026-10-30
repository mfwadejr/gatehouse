// Plain-language release notes, newest first. Add an entry for every release.
export const RELEASES = [
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
