# Gatehouse specification

Working name: Gatehouse. DNS, DHCP, gateway and zone firewall appliance with a web console, installed from a bootable USB stick.

Status: first build. Everything below is in scope for the first bootable image unless it is listed under "Deferred".

## 1. Goals

- One appliance that provides DNS filtering, DHCP, routing/NAT, a zone-based firewall, UPnP and monitoring.
- Per-VLAN control: DHCP can be served by Gatehouse or left to another server, and Gatehouse can be the gateway for a VLAN or leave that to another router.
- Safe by default. A bad change must never lock the administrator out of the box.
- A polished, themed console and installer. Every colour, size and spacing value comes from design tokens.

## 2. Target

- x86-64 PC with one or more real network ports (Intel assumed, others supported by the Debian kernel).
- Base OS: Debian 13 (trixie) stable, minimal, no desktop on the installed system. The image is built in GitHub Actions and published as a release.
- Boots UEFI and legacy BIOS.
- Single NIC (VLAN trunk or LAN-host mode) and multiple NICs (classic gateway) both supported.

## 3. Architecture

One Go program, `gatehouse`, with sub-commands. The web console is a React app built into static files and embedded in the binary.

| Process | Runs as | Purpose |
| --- | --- | --- |
| `gatehouse daemon` | user `gatehouse`, with `CAP_NET_BIND_SERVICE` | DNS, DHCP, UPnP, web/API, metrics, scheduling |
| `gatehouse netd` | root | Applies network and firewall changes on request over a unix socket. Validates every request. |
| `gatehouse rollback` | root, started by a systemd timer | Restores the last known good ruleset if a change is not confirmed in time. Independent of the other processes. |
| `gatehouse recover` | root, local console only | Resets the firewall to the baseline, resets an admin password, clears TOTP. |
| `gatehouse installer` | root, live image only | Serves the installer UI and performs the install. |

External components the program orchestrates rather than re-implements:

- `nftables` for firewalling and NAT (rules are generated and loaded with `nft -f`, validated first with `nft -c -f`).
- `systemd-networkd` for addresses, VLANs, DHCPv4/DHCPv6 clients, IPv6 prefix delegation and router advertisements.
- `ppp` (pppd with the rp-pppoe plugin) for PPPoE.

## 4. Storage

- `config.db` (SQLite, WAL): users, sessions, settings, zones, interfaces, WAN links, DHCP scopes/reservations/leases, devices and groups, DNS records and upstreams, blocklist metadata, firewall objects/rules/revisions.
- `logs.db` (SQLite, WAL): DNS query log, firewall events, security events, metric rollups. Retention is configurable and enforced by a background job.
- Not in SQLite: blocklist contents (cached as files, loaded into memory), DNS cache, live counters.
- The active nftables ruleset lives in the kernel and is regenerated from `config.db`.

## 5. Console

Pages: Overview (landing), DNS, DHCP, Firewall (overview and policies), Devices and access, Logs, Settings (access, interfaces and WAN, authentication, system).

The Overview shows service status, internet throughput, per-link state, items needing attention, per-interface throughput, recent activity and link history. Detail lives on the area pages.

Every widget subscribes to its own server-sent-event topic, so areas of the page update independently.

All styling derives from `tokens/tokens.json` (see section 11).

## 6. DNS

- Forwarding resolver with cache, upstreams over UDP/TCP, DNS-over-TLS and DNS-over-HTTPS, health checks and latency tracking.
- Local records and per-device names (from DHCP leases).
- Blocklists: add by URL, upload, or paste; formats domain list, hosts file, AdGuard/ABP style rules; scheduled refresh; last good copy kept on failure; entry counts and duplicate merging.
- Allowlist and custom block rules. A "why was this blocked?" lookup that names the matching list.
- Per-group list assignment.
- Per-client rate limiting (token bucket); throttled clients are logged as security events.
- Query log with filtering, retention, optional anonymisation.

## 7. DHCP

- DHCPv4 scopes bound to an interface or VLAN. Mode per scope: `serve`, `relay` (another router relays to Gatehouse), `off`.
- Reservations, options (router, DNS, domain, NTP), lease times, lease persistence.
- Default gateway option is independent of who serves DHCP.
- Rogue-server probe per VLAN, with a warning when another server is found where Gatehouse serves, or none where one is expected.
- Leases feed DNS names and the device list.

## 8. Gateway, WAN and routing

- Zone role per VLAN: `gateway` (Gatehouse routes and NATs) or `host` (another router stays the gateway).
- WAN link types: DHCP, static, PPPoE, with optional VLAN tag; IPv4 and IPv6 (DHCPv6 prefix delegation to LAN zones, router advertisements, static v6).
- Multi-WAN: priority failover and weighted load balancing, per-link health checks (ICMP or TCP to configurable targets), policy routing so replies leave through the link they arrived on.
- NAT masquerade per WAN link, optional port forwards.
- CGNAT detection: warn when the WAN address is private or in 100.64.0.0/10.

## 9. Firewall

- Zones, each bound to interfaces. Policy per ordered zone pair with a default action and ordered rules (source, destination, service, action, logging, enabled).
- Reusable objects: address groups, domain lists (resolved by Gatehouse DNS), address lists with scheduled refresh, service groups.
- Host firewall for the box itself ("Self" zone). Defaults, inbound drop: DNS 53/tcp+udp and DHCP 67/udp from LAN zones, HTTPS 443 from the management network, SSH 22 off by default, ICMP and ICMPv6 neighbour discovery allowed with limits. Outbound open for upstream DNS, NTP, updates.
- DNS enforcement: redirect client port 53 to Gatehouse, block 853, optional DoH provider IP block.
- Compile to nftables in a dedicated table (`inet gatehouse`). Never touches other tables.

### Anti-lockout

1. A protected management rule is always first. It cannot be edited, reordered or removed through the UI.
2. Pre-apply simulation: a change that would block the administrator's current session or leave no valid management source is refused.
3. Commit with confirm: every apply starts a confirm window (default 60 s). The confirmation must arrive on a new connection. The rollback runs in `gatehouse rollback`, a separate process started by a systemd timer, so it works if the daemon hangs.
4. Last known good ruleset is stored on each confirmation and loaded automatically if the ruleset fails to start. Rules load before the web console listens.
5. `gatehouse recover` at the local console resets to the baseline.
6. Sign-in lockouts are temporary and tracked per source and per account.

### UPnP

- Per WAN link. Off by default. Enabled per zone, never IoT or Guest unless chosen. Device allowlist.
- A mapping must target the requester's own address. No ports below 1024, none that match management or Gatehouse ports. Lease length, mappings per device and request rate limited.
- No event subscriptions. Listens only on internal interfaces.
- Mappings live in nftables sets with element timeouts. Every request is logged. UI lists active mappings with revoke.
- Return traffic is bound to the link the mapping belongs to. Mappings are dropped on failover by default.

## 10. Authentication and security

- Local accounts. Password hashing PBKDF2-HMAC-SHA256 (600,000 iterations), constant-time comparison.
- TOTP (RFC 6238) required for administrators, recovery codes (10, single use, hashed), re-entry of a code to apply firewall changes (configurable).
- Sessions: random 256-bit tokens, stored hashed, `HttpOnly`, `SameSite=Strict`, `Secure` when served over TLS, idle and absolute timeouts, listing and revocation.
- Login rate limiting per source and per account with temporary lockout.
- Console served over HTTPS with a generated self-signed certificate on first boot (replaceable).
- No default credentials. The installer creates the first administrator and enrols TOTP.
- The unprivileged daemon cannot change the system directly; `netd` validates every request.

## 11. Design tokens

- `tokens/tokens.json` is the single source. `build-tokens.mjs` generates `tokens.css`.
- Layers: primitive, semantic, component. Components use only semantic and component tokens.
- `check-tokens.mjs` fails the build on any raw colour or pixel value outside the token files, including inline styles and SVG attributes.
- Dark theme is the default. A light theme exists as a draft.

## 12. Installer and image

- Live ISO built with live-build on Debian 13. Contains the program, systemd units and hardened defaults.
- The installer runs in the live environment: a full-screen themed UI on the console (kiosk browser) and the same UI reachable from another computer over the network for machines without a display.
- Steps: welcome, target disk with erase warning, network port roles (WAN, LAN, management), WAN type, administrator account and TOTP enrolment, hostname and timezone, summary, progress, finish.
- Partitions: EFI system partition, boot, root, and a data partition for the databases and logs.
- Installs the same image to disk, writes the initial configuration, installs the boot loader for UEFI and BIOS.

## 13. Testing

- Unit and integration tests for every package.
- Firewall behaviour tested in network namespaces, including the rollback timer.
- The ISO is booted in an emulator for a smoke test. Real-hardware testing is done by the owner on an x86 PC.

## 14. Deferred

- High availability, VPN, DPI/IDS, mobile app.
- A/B partition updates and an update channel.
- Per-device throughput accounting.

## 15. Open items

- Product name (Gatehouse is a placeholder).
- ISO delivery: GitHub Actions build, downloaded from the repository release.
- Hardware validation on the owner's PC (NIC model, boot mode).
