# Firewall rules: devices, groups, ports, schedules, domains and countries

A rule says who it covers, what traffic, when, and what happens. This page is how the pieces work.

## Who a rule covers

Each side of a rule (the devices it starts from, and where it goes) can hold any mix of:

| What you pick | Stored as | Notes |
|---|---|---|
| A device | `dev:aa:bb:cc:dd:ee:ff` | Keyed by hardware (MAC) address, shown by name. |
| A device group | `grp:Kids` | Group once, use in any rule. |
| An address, network or range | `192.168.1.50`, `192.168.20.0/24`, `10.0.0.5-10.0.0.9` | As before. |
| A hardware address typed by hand | `aa:bb:cc:dd:ee:ff` | Source only, and only for your own networks. A MAC is refused as a destination. |
| A website | `dom:example.com` | Destination only. Covers its subdomains. |
| A country | `geo:DE` | Destination only. |

A rule keeps a reference to the device, not a copy of its address. When DHCP gives the device a new
address the rule follows it, within a couple of seconds, with nothing to apply or confirm. Behind the
scenes each reference is a named nftables set; the ruleset only names the set and Gatehouse fills it
from DHCP leases and reservations. A device with no address yet matches nothing, and the rule list
shows a warning.

Groups can be set to "reserved addresses only": a device then counts only at the address reserved for
it, so someone borrowing a free address is not covered.

A device or group that a rule uses cannot be deleted. The error lists the rules. Renaming a group
updates the rules that use it. Device and group names resolve through the device list; they are separate
from the DNS filtering groups.

"Also require the device's hardware address" adds a check on the Ethernet address, so another device
cannot borrow the address. It only works for devices on your own networks.

Devices are IPv4 for now (from DHCP). Typed IPv6 addresses, websites and countries work for both.

## What traffic

* Services by name (`https`, `ssh`, ...), ports (`tcp/8080`, `udp/1000-2000`), or **lists of ports**
  saved once under Groups & lists and used as `list:Name`. Several can be mixed in one rule.
* **IP version**: both (default), IPv4 only, or IPv6 only.
* **Connection state** (under More options): any, or only new connections.

## When

A rule can be always on, every day between two times, on chosen days, or once between two moments.
The firewall does the time check itself, so schedules keep working if the console is down.

The kernel evaluates schedules in UTC. Gatehouse converts your local times (the time zone set on the
machine) when it builds the ruleset, and builds it again when summer time starts or ends. It does that
automatically only when nothing else about the firewall has changed since the live ruleset was made, so
it never puts anyone's unfinished edits live. If other changes are waiting, the usual apply bar appears.

## Websites

For `dom:` the DNS server notes the addresses it hands out for the domain (and any CNAME chain behind
it) and puts them into the rule's set, with a timeout based on the record's lifetime. This works for
devices that use Gatehouse for DNS, which is every device when DNS enforcement is on. A device that
already had the address cached before the rule existed is matched from its next lookup.

## Countries

Country matching uses the delegation files the five regional internet registries publish (RIPE NCC,
ARIN, APNIC, LACNIC, AFRINIC). The first rule that names a country triggers a download; the lists are
refreshed weekly. Applying a country rule before the first download finishes is refused with a message.

Region rules never match your own networks (private, shared and link-local ranges) or the far end of your
VPN connections (peers and servers whose address is known). Region matching is for destinations only.

Accuracy is that of the registries: the country a block was handed out to, not where a given device
physically is.

## Hit counts

Each rule counts how many new matches it has had and when it last matched. The kernel counters restart
whenever the firewall is loaded, so Gatehouse adds what it sees to a running total that survives reloads.
A rule that is renamed or moved to other networks starts counting from zero.

## The matrix

The matrix on the Networks tab shows each pair of networks as Open, Blocked, or **Replies only**
(this side cannot start connections, but can answer ones the other side started). A number in brackets
counts the rules written for that pair. Selecting a box lets you:

* **Allow all**, **Block all** or **Replies only** for the pair. These create rules owned by the matrix
  (named `Matrix xxxxxxxx`, shown as "From the matrix"). Choosing again replaces that rule; **Back to the
  default** removes it. Replies only also adds a rule letting the other network start connections, unless
  the network types already do.
* See the rules for that pair, or add one with the networks already filled in.

Rules you wrote by hand are never touched by the matrix and still apply first. The built-in protected
rules that keep you able to manage Gatehouse stay above everything. Nothing is live until you apply.
The Internet row can only be blocked or reset from the matrix; to let the internet reach a device, open a
port. The matrix does not offer Replies only to or from the Internet.

## The rule list

Search by name, device, port or note; filter by network or by allow/block. System rules (the built-in
protected ones) are hidden unless you choose "Show system rules". Each row shows who and where as named
chips, when it applies, and its match count and last match.
