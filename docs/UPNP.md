# UPnP

Games, consoles and media apps can ask the gateway to open a port for them. Gatehouse supports this
but keeps it off until you switch it on twice: for a **network** (Firewall → Networks → "Let devices
open ports automatically") and for an **internet connection** (Internet → the connection → "Let
devices open ports on this connection").

What it will and will not do:

- A device can only open ports **to itself**. A request that names another device is refused.
- Ports below 1024, the console, VPN, DNS and any port you forwarded by hand can never be opened this way.
- At most 32 ports per device and 256 in total. Each port closes after a limited time (default one day,
  1 hour to 7 days) unless the device renews it.
- Requests from outside the network are ignored, and each device is rate limited.
- Optionally only listed devices (by MAC address) may use it.
- Open ports are cleared when the machine restarts and when the active internet connection changes.
- Every request, allowed or refused, is listed under Firewall → UPnP, where any port can be closed.

How it works: the daemon answers the standard discovery (SSDP) and control (SOAP) requests on each
allowed network, checks the policy, and asks the privileged helper to load the mappings into a separate
nftables table (`ip gatehouse_upnp`). The main firewall lets connections that were redirected that way
through only while UPnP is on.

Not supported: NAT-PMP / PCP, IPv6 pinholes (IGD2), and event subscriptions.
Not tested on real hardware yet; the logic is covered by unit tests.
