# DNS

## What it is for
DNS turns names into addresses. NopeWarden answers for your devices, blocks ads, trackers and malware names, and lets you add your own names.

![The dns page, shown with example data](img/dns.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
Tabs along the top:
- **Overview**: **Lookups over time**, **How queries were answered**, **Busiest devices**, **Most blocked** and **Most asked**.
- **Blocking**: **Blocklists** you can switch on (which devices get which lists is set per profile, on the Profiles page), **Always allow or block** exceptions that beat every list, and **Why is this blocked?** to find which list blocks a name.
- **Servers**: **Upstream DNS servers** (where NopeWarden asks when it does not know), **Which server is asked first**, **Add a DNS server**, and the encrypted DNS switch.
- **Your own names**: **Give this firewall a name**, and names that point somewhere on your network, like nas.lan to 192.168.1.10.
- **Settings**: **What to do with blocked names**, **Home domain**, **Queries per second per device**, **Remember answers (entries)**, **Keep the query history for (days)**, **Hide device addresses in the history** and **Tell browsers to use this DNS**.

Devices sent through a VPN have their name lookups sent through it as well.

## Common tasks
1. Allow a blocked site: **Always allow or block**, type the name, choose allow, **Add**.
2. Find out why a site is blocked: **Why is this blocked?**, type the name, **Check**.
3. Name a device on your network: **Your own names**, enter the name and address, **Add**.

4. Block an app, a category or a website for one device, a network or at set hours: this is on the Protection page now, under **Blocks**.
5. See what a list blocks: **Blocking**, **View names** on the list. Search, and allow a name that should not be blocked.

## Example
A shopping site does not load. Type its name under **Why is this blocked?**; it names the list. Add the name to **Always allow or block** as allowed.

## If something goes wrong
**No server is answering** under Servers means none of the upstream servers replied. Check the internet. **Clear saved answers** forgets everything NopeWarden remembers and is safe to use.

## Resolve directly (your own resolver)
Under **Servers**, **Resolve directly** turns on NopeWarden's own Unbound resolver. Instead of asking a forwarder such as your provider or Cloudflare, it asks the internet's root servers itself, so no single company sees every name you look up. It is off until you turn it on, and it only listens inside the box (port 5335), never on your network.

- While it switches on, the card says what it is doing (setting up Unbound, waiting for its first answer, switching your devices over) and the choice is locked; it can take up to a minute. If it cannot be done, the message says why and your lookups stay as they were.
- It works on a fresh install: the key Unbound uses to check signatures (the DNSSEC root key) is put in place for you when you first turn it on. If it cannot be, the message says so in plain words and your name lookups stay as they were.
- **Check DNSSEC**: names signed with DNSSEC are verified. When a check fails, the query log says why in plain words (for example, the site's signature has expired) rather than just failing.
- Automatic fallback: if direct lookups stop working, NopeWarden switches to your encrypted forwarders by itself and raises an alert. It switches back when direct lookups work again.
- **Your own names** keep working (split-horizon): names you define, like nas.lan, are answered locally and never sent out.

## Changes wait for Review
DNS changes (lists, groups, servers, your own names) now wait on the **Review changes** page with the firewall and network changes. Apply or discard them together. **Discard** puts DNS back exactly as it was running.

## Schedules
Blocklist refreshes follow the one schedule editor (every N hours, daily, weekly or off). A failed refresh retries after 5 minutes, then 15, 30 and every hour.

## DNS follows the route
A device sent through a VPN or a particular internet connection looks up names through that same path, so a website route does not leak lookups to your normal connection. The Routes tab on Policies lists the addresses learned for each website route.
