# How it works

## What it is for
A plain-language tour of what happens inside NopeWarden when a device asks for a website, when you change a setting, and when something goes wrong. You never need this to use the console; it is here so nothing feels like magic.

## One box, several jobs
NopeWarden is one program on one machine. The same program hands out addresses (DHCP), answers name lookups (DNS), decides what each device may reach (the firewall), sends traffic down the connection or VPN you chose (routes), and serves this console. Because it is one program, a change in one place is seen by the others straight away: a device that gets an address is known by name in the DNS and in the firewall.

## What happens when a device joins
The device asks for an address. NopeWarden gives it one from the pool of that network, or the address you reserved for it, and remembers its name and its hardware address. It then shows up on the Devices page. If **New device approval** is on, it waits there until you allow it.

## What happens when a device opens a website
1. The device asks NopeWarden's DNS for the address of the site.
2. DNS checks your own always-allow and always-block exceptions first, then the lists the device's profile uses, then its hours. A blocked name gets no answer; anything else is looked up and passed back.
3. The device connects. The firewall checks the connection against the rules for its network, from the top, and the first match wins.
4. If a route matches, the connection leaves through the connection or VPN that route names. If none matches, it uses the normal connection.

## What happens when you press Apply
Changes to networks and the firewall are saved first and only take effect when you press **Review and apply**. NopeWarden checks the new firewall before loading it, takes a copy of the old settings, loads the new ones, and starts a 60-second countdown. If your browser can still reach the console, the change is kept. If not, it is undone by itself. That is why a wrong rule cannot lock you out for good.

## Where your settings live
Your settings are kept in one small database on the box, and your logs in another. Backups copy the settings, and the logs too if you turn on **Include history (logs)**. Policies, rule sets and tags are only labels and groupings over the same settings, so removing a set or a tag never removes a policy or a device.

## Rule sets and tags
A policy can be put in one set, and a set can hold any kind of policy. Pausing, turning off or turning on a set does it for every policy in it. Turning a set back on only turns on the policies that were on before. Tags are plain labels on policies and devices; they help you filter and search and never change what anything does.

## What keeps working when something fails
Protection features only add protection. If blocking apps or watching threats stops, your internet carries on and the Logs say so. If the connection drops and a **Kill switch** is on for a VPN, the devices using that VPN lose internet on purpose rather than falling back to the normal connection.

## Who can change what
Administrators can change everything, including the Danger zone and rule sets and tags. Other users can see sets and tags but not change them, and cannot reach the resets. Every change is written to the activity log with who did it.

## Where to read more
The Reference chapter lists the ports in use and the order things are checked in. The Recovery chapter covers being locked out.
