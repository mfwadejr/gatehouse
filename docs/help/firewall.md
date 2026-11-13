# Firewall

## What it is for
Decides what each of your networks may reach, and lets you add exceptions. The type of each network does most of the work in plain terms.

![The firewall page, shown with example data](img/firewall.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
Tabs along the top:
- **Networks**: **Who can reach whom** comes first, a grid of every network against every other; select a box to change it or to see its rules. Under it, **Your networks**, each with a type. For each: **Can reach the internet**, **Devices here can talk to each other**, **Can manage NopeWarden**, and under Advanced **NopeWarden is the gateway**, **Make devices here use the enforced DNS server** and **Allow remote login (SSH)**. **Test my access** shows whether a device can reach something. **In plain words** says what the firewall does right now.
- **Rules**: your own rules. **Rule lines** shows one row for each rule: its number (the order they are checked), name, From, To, Service, Action, **Matches** and When. Long lists show the first two and +N for the rest. Click a row to open it: the rule in a sentence, its counts, and **Up**, **Down**, **Pause…**, **Turn off**, **Edit** and **Remove**. A rule with a few services shows how often each one matched. The rules that are built in open for reading only, and so do the ones other pages make, such as Paused devices, which lists who is blocked and has a button to Profiles. **Plain cards** is the older view with a sentence for each rule; narrow windows open on it, and the view you choose is remembered. **Live** (on by default) refreshes the counters every second or two and flashes a row the moment it matches, so you can see whether a rule is really being hit; with it off the counters refresh about every 20 seconds. **Export what is shown (CSV)** saves the rows on screen, after your search and filters, with one row for each service. Each rule has **Pause…** (for a set time, or until you resume it), **Turn off**, **Edit** and **Remove**, the same controls routes, blocks and port forwards have. Deleting one can be undone for a few seconds. **Create policy** on the Policies page opens this form for you too.
- **Always on**, at the bottom of the Rules tab and closed until you press **Show**: the seven things NopeWarden does before any rule of yours is looked at, such as dropping broken connections, letting replies back in, limiting ping to this box, letting devices ask for an address and for names, letting the IPv6 and error messages through, refusing encrypted DNS sent to this box so apps fall back quickly, and dropping everything else. Each has a plain reason and a count of the packets it has handled. They cannot be changed.
- On the Rules tab, a rule is marked Duplicate, Not needed or Never applies (and has a warning line) when an earlier rule already covers it: it is the same as that rule, or that rule already does the job, or that rule decides the other way so this one never applies. Nothing is changed for you; remove the extra rule or move it above the other.
- **Device groups**: named groups of devices to use in rules, such as Media players. A group is a shortcut for rules; to filter devices or turn their internet off at set hours, use Profiles under Devices & access.
- **Services**: names for ports, to pick in rules. See Services below.
- The **Paused** line under "Set on other pages" is the rule that blocks paused devices from the internet. It is greyed out and says **Standing by, nobody paused** until you pause a device or a profile on the Profiles page, and then it shows who is blocked.
- **Open ports**: **Open a port** lets something outside reach an app on one of your devices, like Plex or a game server. Each one can be paused, put on a schedule, turned off, edited or removed the same way as a rule. A paused rule or port is closed until the time runs out or you press **Resume now**; the change waits for **Review and apply** like any other.
- **Countries**: block or allow whole countries.
- **Discovery**: let devices on different networks find each other, such as casting and printers.
- **Protection**: **Block encrypted DNS to other servers**, **Also block well-known encrypted-DNS websites**, **Ask for a code before changing the firewall** and **Log blocked traffic**.
- **History**: **Change history**, every time the firewall was changed and whether you kept it.

## Common tasks
1. Open a port for a game console: **Open ports**, **Open a port**, choose the app, choose the device, **Save**, **Review and apply**.
2. Stop a network reaching the internet: **Networks**, turn off **Can reach the internet** for it.
3. Allow one device to reach another network: **Rules**, add a rule, choose the device and the service.

4. Open a port in three taps: **Open ports**, **Open a port**, tap the app, tap the device. NopeWarden tells you which port it used. If that is not the right port, choose **Change port** and enter it yourself. A port already in use is never picked for you: NopeWarden asks you for a different one.
5. See everything the firewall does: **Rules**, **Rule lines**. The built-in rules, the rules other pages make and your open ports are listed there too (in **Plain cards**, turn on **Show system rules**).
6. Check that a rule is working: leave **Live** on, then try the thing the rule is for. Its **Matches** count goes up and the row flashes.
6. Watch for infected devices and break-in attempts, and block apps and categories: watching for threats is on its own **Threats** page, and blocking apps and categories is on the **Protection** page, both in the left menu. It watches and tells you in plain words; it does not block threats. If it stops, your traffic is not touched.

## Example
Open Plex for your media server: **Open a port**, choose Plex, choose the server, optionally limit it to your own country, **Save**, **Review and apply**.

## If something goes wrong
NopeWarden refuses a change that would leave you no way to manage it, and always keeps safe starting rules above everything you can edit. If a change breaks something, the 60-second way back puts it right.

## Letting devices open ports (UPnP)
**Let devices open ports by themselves (UPnP)** lets a game console or media app ask for a port instead of you opening it by hand. It opens that port for that device only, and it is off until you switch it on. Turn it off if you want every open port to be your own choice.

## A port just for managing the device
The **Management port** card moved to Settings, General, because it is a setting of the device and not of the firewall. The firewall rules it needs are still made for you and show under **Protected** at the top of this page.

## Services (a tab of Firewall)
**What it is for**
A service is a name for one or more ports, such as "plex" for tcp/32400. Rules pick services by name instead of by number.

![The Services tab, shown with example data](img/services.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

**What is on the page**
- **Search services**: type a name or a port.
- **Your services**: the ones you made, with **New service**, **Edit** and **Delete**. Deleting one can be undone for a few seconds, unless a rule uses it.
- **Built in**: names NopeWarden already knows. They cannot be changed.

**Common tasks**
1. Make a service: **New service**, give it a **Name**. Under **Ports** choose **Protocol** (TCP, UDP, **TCP and UDP**, or ICMP for ping), type a port or a range such as 27000-27050, and press **Add**. Each port appears in the list below, and ✕ removes one. A wrong entry is explained in words under the box. For a known name such as https use **add a known name**; for a long list use **paste a list**, one entry per line like tcp/32400. Then **Save**.
2. Use it: in any rule or open port, type its name in the "What for" box. Several can go in one box, separated by commas or spaces.
3. Find a built-in one: type its name in **Search services**. The built-in **console** service is this console itself. It follows the console port, so it changes if you move the console to another port.

4. Rename a service you made: **Edit**, change the **Name**, **Save**. Rules and open ports that use it follow the new name. A service used by an open port cannot be deleted until the port is removed.

**Example**
Make "console-game" with udp/3074 and tcp/3074, then use it in a rule for your console.

**If something goes wrong**
A service that a rule, an open port or a route uses cannot be deleted until that changes; NopeWarden names it.
