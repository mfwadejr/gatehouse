# Gatehouse

DNS, DHCP, gateway and firewall for your network in one small appliance, with a web console built to be easy to use. It installs from a USB stick onto an ordinary x86 PC.

Gatehouse is a working name.

## What it does

- **Internet:** connects through DHCP, a fixed address or PPPoE (with VLAN tag if needed), with failover or load sharing across several connections. USB Ethernet adapters work as ports.
- **DNS:** fast caching resolver with ad and tracker blocking, per-device profiles, your own names and a "why was this blocked?" lookup.
- **DHCP:** addresses for each of your networks, reservations, and automatic names for devices.
- **Firewall:** pick a type for each network (Trusted, Private, Guest, Isolated, Custom) and Gatehouse writes the rules. Every change comes with a 60-second safety timer that undoes it unless you confirm, so you cannot lock yourself out.
- **Sign-in:** two-step sign-in is required for the administrator.

## Try it

1. Download `gatehouse-*.iso` from the latest release on the Releases page.
2. Write it to a USB stick (at least 4 GB) with [balenaEtcher](https://etcher.balena.io/) or `dd`.
3. Boot the PC from the stick. If it will not start, turn off Secure Boot in the firmware settings (the image is not signed yet).
4. The screen shows a web address and a one-time code. On another computer on the same network, open that address (accept the certificate warning), enter the code, choose the disk, and install.
5. Restart without the USB stick. Open `https://gatehouse.lan` (or the address shown on the screen) and follow the setup.

The installer erases the disk you choose, and it will not let you choose the stick it started from.

## Building

```
cd web && npm ci && npm run build      # the console
go build ./cmd/gatehouse                # the program (CGO is needed for SQLite)
iso/build.sh ./gatehouse 0.1.0          # the image (root, Debian or Ubuntu with live-build)
```

GitHub Actions does all of this on every push and attaches the image to a release (`nightly` for the latest commit, a numbered release for tags starting with `v`).

Layout: `cmd/gatehouse` (the program), `internal/` (DNS, DHCP, firewall, network, installer, API), `web/` (React console), `tokens/` (design tokens; every colour and size in the console comes from here), `iso/` (live-build configuration), `docs/SPEC.md` (the specification).

## Status

Early. DNS, DHCP, firewall, internet connections, the console and the installer are written and tested in isolation; the image build is new. VPN (WireGuard and OpenVPN) and UPnP are specified in `docs/SPEC.md` and not built yet.
