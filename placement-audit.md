# Placement audit: where each setting lives (20 Nov 2026, after 0.9.4.49)

Asked for by Marvin (20 Nov): "a full blown audit" after the management-port discussion. Read-only: nothing was changed. Method: every page and tab was opened in the demo console (1280 x 900) and its cards, controls and height recorded, then checked against the code (`web/src/pages`). Heights are on demo data; a real box with more devices and rules is longer. Not agreed, not built: every proposal below waits for Marvin's decisions and a "go". Rule applied (13 Nov): a setting lives with the area that uses it; before anything moves, say who uses it and from where, and keep a link from the old place.

## 1. How long each page is (pixels, demo data)
Tallest first. Anything over about 1700 is a lot of scrolling.
- Overview 3577 (14 cards; the two top ones are being made collapsible)
- Settings, Alerts & logs 2633
- Diagnostics, Services 2510
- Threats, Rules 2496 (one card, Groups of checks, is 1233 tall with 43 controls)
- Internet, Speed 2135
- Settings, General 1840
- Threats, Overview 1784
- Logs, Summary 1756
- Settings, Advanced 1706
- Firewall, Countries 1677 (one card, 253 controls)
- Firewall, Protection 1645 (plus the Protected box above the tabs, about 300)
Everything else is under 1550. Pages with one or two short cards are the best-balanced (DHCP leases, Policies, VPN).

## 2. Findings, by theme

**F1. Managing the device is spread over five places.**
- Settings, General: Device mode, Management port (with Close the console on the LAN), Console address, Power.
- Settings, Account & sign-in: Remote login (SSH), Two-factor, Password. (Correction to my message of 20 Nov: Remote login is in Account, not General.)
- Top of Firewall (the Protected box, above the tabs): Who may manage, Test my access, and the list of always-on rules.
- Settings, Advanced: Bridge mode, which has its own separate "networks that may manage" box.
- Diagnostics, Services: Console connections (who connected).
Proposal: one Managing this device group holding Management port, Who may manage (moved from Firewall), Test my access, Close the console on the LAN, Console address and Remote login. Bridge mode's own list reuses the same editor. Firewall keeps a one-line pointer and the always-on list. This also shrinks the Protected box. Placement (folded group in Settings, General, or its own Settings tab) is decided by measuring height first; Settings, General is 1840 already.

**F2. Forced DNS is in two places.** The full card (ForcedDns.jsx, with exceptions) is on Firewall, Protection; DNS, Servers has a short Forced DNS card with a Change link to it. DNS is where people look. Proposal: the full card moves to DNS, Servers; Firewall, Protection keeps a pointer. Open question for Marvin: it is enforced by firewall rules, but he uses it as a DNS choice.

**F3. Two different things are both called lists.** DNS, Blocklists (ad, tracker and malware names; add a list, pick when it updates) and Protection, Lists used for blocking (what is checked when a block names an app, category or site; Run now, update times) are separate, but the names and the update controls look alike. Also two exception systems: DNS, Always allow or always block (names) and Protection, Exceptions (per device, profile or network); and two why-checkers: DNS, Why is this blocked? and Internet, Why can't it reach that?. Proposal: do not merge; rename so the two kinds read differently, and add a link between each pair. To verify before deciding: whether any list is shown in both.

**F4. The same cards appear on two pages.** Test my internet and Tools are on Internet, Troubleshoot and also Diagnostics, Tools (same component). The provider report is on Internet, Usage (Provider report card), in Logs, Reports (ISP report) and as Report on this link on Diagnostics. Proposal: one home each (tools in Diagnostics; reports in Logs, Reports) with a pointer on the Internet pages. Open question for Marvin: Internet, Troubleshoot is where people go when the internet breaks, so a duplicate there may be wanted. Alternative: keep both and say so.

**F5. Networks appear on four pages.** Your networks (add, edit, remove) is on Networks and again on Firewall, Networks; a read-only copy is on Overview; Network ports is on Networks while the management port is in Settings. Proposal: Networks owns add, edit, remove. Firewall, Networks keeps Who can reach whom and In plain words, with Your networks reduced to a short list that opens the Networks page.

**F6. Schedules live in five places.** Backups (Settings), blocklists (DNS), country lists (Firewall), threat rules (Threats) and updates, with a central list in Updates, Schedule, Other recurring jobs. This follows the rule (each stays with its area) and the central list is the overview, so no move is proposed. Only action: make sure the central list says "edit it here or where it is used" for each row.

**F7. Settings, Alerts & logs mixes four jobs.** Alerts (who is told), Send logs elsewhere (syslog), Send traffic records (NetFlow) and Let a monitoring tool read this device (SNMP) are about sending things out; Storage (what is kept and for how long) is what the Logs page uses. At 2633 it is the longest settings tab. Proposal: Storage moves to Logs as its own tab (it is Logs' keeping periods, and Logs already owns history), with a pointer from Settings. Syslog, NetFlow and SNMP become one folded group, Send data to other tools. Alerts stays. Diagnostics, What is using the disk links to Storage.

**F8. Threats settings are in four tabs and the page is long.** Threats has Settings (mode, how closely, limits, the program), Rules (update schedule, Groups of checks, Single checks, Allowed checks, Devices never checked) and Advanced (save and load tuning, reset, program log, where it writes). The mode is also shown on Firewall, Protection and Protection, Overview as pointers, which is fine. Problems: Rules is 2496 tall; Allowed checks and Devices never checked are exceptions but sit under Rules. Proposal: fold Groups of checks to a summary line with the controls inside; keep the rest.

**F9. Five places keep a history of changes.** Firewall, Change history; Settings, Backup, Changes (revert points) and Backup history; Review changes; Logs, Admin activity. Each answers a different question, so nothing moves. Proposal: one line on each saying which question it answers and linking to the others.

**F10. Open ports exist in three places.** Firewall, Ports (Open a port, UPnP), Policies (open ports are one kind of policy) and Firewall, Rules. To verify before deciding whether Firewall, Ports and Policies list the same items; if they do, one list with a link.

**F11. Smaller items.**
- Appearance (Settings, General) duplicates Light or Dark in the profile menu. Keep both; low priority.
- Devices, Profiles shows one card per profile; with more profiles it grows. A list with Edit would be shorter.
- Settings, General opens with Finish setting up and Common setups before the device settings; the order could put device settings first once setup is done.
- Firewall, Countries is one card with 253 controls: fold the country list behind a search.

## 3. Proposed order (not agreed)
1. Managing this device (F1), with the editor, the comma/space-separated entry, pick-from-known-networks including VPN and aliases, and ping following the limit.
2. Settings, Alerts & logs split (F7) and the tallest-page folds (Threats Rules, Diagnostics Services, Countries).
3. Forced DNS (F2), duplicated cards (F4, F5), naming of lists (F3).
4. Wording and links (F6, F9, F10, F11).
Each is its own release with a pointer left behind, search entries for old and new names, redirects for old links, ACCEPTANCE rows and docs.

## 4. Decisions needed from Marvin
- F1: folded group in Settings, General, or its own Settings tab (I will measure and report the height).
- F2: should Forced DNS live on DNS, Servers?
- F4: remove the duplicate cards on Internet, Troubleshoot, or keep them there on purpose?
- F7: Storage as a tab of Logs, and syslog, NetFlow and SNMP folded together?
- Whether F3, F9 and F10 (naming and links only) can go with whichever release touches those pages.
