# Reference

## What is not included

- **Wireless (Wi-Fi):** not included. There are no Wi-Fi drivers or firmware on the installer, and wireless cards are blocked from loading. Use a separate access point.
- **ARM builds and Secure Boot signing:** not built yet.

## Ports NopeWarden uses

- **443**: the console (HTTPS). You can change it under **Settings, General, Console address**.
- **53**: DNS, on your networks only.
- **67 and 68**: DHCP, on the networks where it is switched on.
- **22**: remote login (SSH). Off unless you turn on **Remote login** under **Settings, Account & sign-in**. It is never reachable from the internet side.

## What Apply does

Changes to networks and the firewall are saved first and applied when you press **Review and apply**. NopeWarden checks the new firewall before loading it. After loading, a 60-second countdown starts. If your browser can still reach the console, the change is kept automatically. If not, it is undone. **Undo now** and **Keep this change** let you decide sooner.

## The order things are checked in

- **Firewall:** each network has a type. The type decides what it can reach. Rules you add are checked from the top; the first match wins.
- **DNS:** your own always-allow and always-block exceptions beat every list.
- **Routes:** the first route that matches is used, so put the more specific ones at the top.
- **VPN routing:** a device sent through a VPN is routed by its address. Its name lookups go through the VPN too.

## When a protection feature fails

Blocking apps and topics, and threat watching, only add protection. If they fail, your internet carries on and the protection is lost; your traffic is never held up. A block that cannot be worked out lets the lookup through, and the Logs say so (once an hour). Threat watching reads copies of the traffic, so if it stops, crashes or is removed nothing changes for your devices; if it uses all of its processor share for ten minutes NopeWarden switches it off and says why. Details are in the Protection chapter.

## Updates

NopeWarden looks for updates every 12 hours unless you change **How often to check** (6 hours, 12 hours, a day, a week, or your own number of hours from 1 to 720). Under **Settings, General, Updates** you choose **Off** (only when you press Check now), **Notify** (it tells you) or **Automatic** (it installs once a day in the hour you choose). Updates are signed; NopeWarden installs only releases signed with its own key. If a new version fails to start, the previous one comes back.

## Backups

Backups are made on a schedule under **Settings, Backup & recovery**. A backup is only worth having if it restores, so each one is checked as it is made. Keep a copy on another computer or drive.

## Words used in the console

- **Network:** a separate group of devices, such as Home or Guests. Each is one port, several ports, or a VLAN.
- **Profile:** which blocklists and hours apply to a device.
- **Kill switch:** if a VPN drops, the devices using it lose internet instead of falling back to the normal connection.
- **Pair:** two NopeWarden machines working together; if one fails, the other takes over.
