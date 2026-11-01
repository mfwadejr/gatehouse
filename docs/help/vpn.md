# VPN

## What it is for
Send some devices through a VPN provider, let your phone reach home from anywhere, or join two sites together.

![The vpn page, shown with no data yet](img/vpn.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
- **Add a VPN** starts a guided choice: use a provider's file, let devices reach home, or **Join two sites**.
- Each VPN has a card with its state in words. For a provider VPN: **Who uses it?** (**Whole networks**, **Device groups** or **Individual devices**), **Kill switch**, **Test address**, **Internet sees you as** and **Checked**.
- **Check** beside a device shows, by itself every few seconds, whether its connections and name lookups really leave through the VPN.
- For a VPN to reach home: **Add a device** gives a file and QR code for a phone or laptop. **Devices connect to** shows the address they use.
- **Join two sites** makes a code you take to the other site.

## Common tasks
1. Send your computer through a provider: **Add a VPN**, drop the provider's file, choose **Individual devices** and pick the computer, **Add and connect**.
2. Reach home from your phone: **Add a VPN**, choose to reach home, name the first device, scan the QR code.
3. Check it works: press **Check** next to the device.

## Example
Set **Kill switch** on for a device that must never use your normal connection. If the VPN drops, that device loses internet rather than leaking.

## If something goes wrong
**Needs attention** says what failed. If a device shows your normal connection under **Internet sees you as**, press Check, then **Review and apply** if it says the address changed. With the kill switch off, name lookups fall back to the normal connection while the VPN is down, and the card says so.
