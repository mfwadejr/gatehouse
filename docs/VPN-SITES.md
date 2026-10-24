# VPN choices and site-to-site

## Connect from outside (WireGuard and OpenVPN servers)

Two plain choices when the VPN is created:

- **What goes through the VPN**: *Everything* (default) or *Only my home network*.
- **Can reach devices in my home network** (default on). Off gives connected devices the internet through the router and nothing else. It cannot be off when only the home network goes through the VPN.

They map onto the existing model: reach on = the `VPN` zone (trusted), reach off = `VPN internet only` (private); everything = `route_all`. The old `access` and `route_all` fields are still accepted when `route` is not sent.

## Through a VPN provider

At setup: *Everything on my network* (default; all networks are ticked) or *Only the networks and devices I pick* (opens the picker).

## Site-to-site (WireGuard)

1. First site: VPN, Add a VPN, Join two sites together. Enter the other site's network (for example `192.168.50.0/24`) and the public address of this site. A code is shown once.
2. Second site: same menu, "Second site: I have a code", paste the code.

Both ends then route to each other's networks and allow traffic both ways, using a zone called **Remote sites** (trusted, no internet through this router). The second site connects out and keeps the link alive, so it can sit behind another router. Overlapping address ranges are refused with a plain message. The code holds the link's keys: send it privately.
