# Fix list (after 0.9.4.19) - started 11 Nov 2026

Continues claude/fix-list-after-0.9.4.11.md, which still holds AW, AM, AT, AU, AV and the hardware checklist. Status from there: AP, AS (0.9.4.17), AQ (0.9.4.18, installed) and AR (0.9.4.19) are BUILT.

## Current status (updated 15 Nov 2026, after 0.9.4.42)
Everything below that says "not built" or "waits for go" is old history unless it is listed here. Built and delivered: 0.9.4.37 to 0.9.4.42.

Built in 0.9.4.42 "Look and feel" (front end only; ACCEPTANCE 172 to 177): pager (Previous, page numbers, Next, Rows per page 10/25/50 remembered, count, Latest tag only on newest) on Backups, Backup history, Changes and every What changed card; Networks tags open the page that explains them and Networks What changed has its own 1 h/24 h/7 d; tab dots on Internet, Firewall and Updates (VPN has no tabs, so it stays a menu dot only); profile menu at the bottom of the menu (items 7); six-box code input in every authenticator-code place with recovery-code fallback (item 8); sliding menu highlight (item 9). Tested in the demo with 27 backups, a real authenticator enrol, typed and pasted codes, and a recovery-code sign-in. The still-open items under 0.9.4.42 above (Threats edge numbers, the DNS live feed and Devices over time wording) were not part of this release and remain waiting for "go".

Agreed 15 Nov by Marvin ("we should probably include these"); build waits for his "go". Proposed split into two releases:

0.9.4.42:
1. Threats edge numbers on Threats Overview, above the install card: who is probing you (attempts, ports, plain reason, Copy address only), ports being probed, blocked by network, a 30-day strip with each day's worst moment. First check what Threats already covers and add only the gaps. Mock is in the scratchpad (mock40.png).
2. The small gaps from 0.9.4.39: Networks number cards that open the page that explains them, and a time selector for the Networks What changed strip; Devices over time and the DNS live feed to say plainly how far back they go (or fill in what the box can) instead of leaving it unexplained.
3. Tab dots on Internet, Firewall, VPN and Updates, by giving their attention items a tab.
3b. Pages instead of Show more (Marvin, 15 Nov, after seeing 0.9.4.41): the shared list component gets Previous, Page N of M, Next and numbered pages, a rows-per-page choice (10, 25, 50, remembered), a fixed page height, a reset to page 1 when the list changes, and a count like Showing 11 to 19 of 19 backups. Applies to Backups, Backup history and every other long list at once. Removes the Show more wording.
   Also (Marvin, 15 Nov): the What changed cards (Networks, DNS, DHCP, Threats) show 10, then pages, instead of four; they load the whole period (not just the latest 100) and page through it, with no bar when one page is enough. Marvin's Changes section is a different card: Settings, Backup & recovery, "Changes" (revert points, pages/Changes.jsx), which shows every saved copy with no limit (the keep setting is 20 to 5000). It gets the same pager, 10 per page; the Latest tag must stay on the newest copy only (use the overall index, not the row on the page). Both this card and the What changed cards get pages.

Reports and long history (Marvin, 15 Nov 2026; agreed in principle, build waits for "go"):
- Chart history becomes a Storage period you set (default 365 days, up to 10 years) by adding a daily step (low, average, high per day per series) to the existing three. Optional longer hourly detail with its size shown. Outages (already 365 days, adjustable with alerts) and speed tests (fixed 365 days: make adjustable) stay in Storage.
- A general Reports tool: pick a subject (Internet links, DNS, DHCP, Firewall connections, Threats, Devices, VPN, Speed tests, System, Backups and updates), a period (24 h, 7, 30, 60, 90 days, 1 year, or exact from and to dates), filters that fit the subject (links: latency, loss, drop length, which link; DNS: blocked or allowed, list, device, name, slow, failed; threats: rule, device, severity; firewall and devices: device, port, network, direction, country) and a grouping (hour, day, week, month, or by device, name, port, link). Result: summary, chart and matching rows; export as a spreadsheet or print; save a report by name and run it again. First template: ISP report per link (uptime %, outages and longest, average and worst delay, loss, median speed test down and up, day-by-day strip).
- Home (decided 15 Nov, after Marvin said it could fit Logs): a Reports tab in Logs, after Activity and before Admin activity, because Logs already owns the history, filters, summary, export and the Storage periods. Diagnostics (Internet troubleshooting and Network tools) gets a "Report on this link" shortcut that opens Reports prefilled; Logs rows get "Make a report from this search". Search finds it as Reports, ISP report, uptime, drops. (First leaning was a Diagnostics tab; changed.)
- Chart history length (Marvin, 15 Nov): an admin setting in Storage under advanced: 90 days, 1 year (default), 2 years, 5 years, 10 years, or Everything (never removed; only the admin or a Danger zone reset clears it). Show the current size, the growth per extra year and free disk beside the choice. Longer hourly detail is its own choice (default 31 days) with its size shown. Never trimmed by the size limit.
- Report exports (Marvin, 15 Nov): every report offers Print (always; a print layout without menus), PDF (shield logo and NopeWarden name at the top, title, period, filters used, date and time made, box name, chart with legend, summary first, then the table, page numbers), spreadsheet (CSV, and an Excel file with the summary and rows on separate sheets) and JSON. PDF is made in the program so it looks the same from any device: needs a small PDF library (check its licence, no downloads at run time) and a bundled open-licence font; the logo comes from the file the brand check protects.
- Honest limit: a report only covers what the box keeps. The period picker shows "data available from <date>" per subject and warns past it, linking to the Storage period that controls it. No scheduled email (alerts are off).
- Order: 0.9.4.42 = adjustable chart history with its daily step, plus the Threats numbers, the Networks and tab-dot gaps. 0.9.4.43 = Reports tab in Logs on the subjects the data supports now (Internet links with outages and speed tests, DNS, Threats, Firewall connections) with custom dates, filters, grouping, export, print and saved reports. 0.9.4.44 = remaining subjects (DHCP, Devices, VPN, System, Backups and updates), then certificate countdowns, the morning summary and disk health.

Moved to 0.9.4.44 (Marvin: "I like all 3"; short design note for each first, then his go):
4. Certificate countdowns: days left on the OpenVPN CA, server and each user certificate (and the high-availability link certificate); the console's own certificate renews itself under 30 days so it is shown only if it ever fails to. Blue suggestion about 60 days out, amber closer, red once expired.
5. A plain-words morning summary built from fixed rules (no AI), a card on Overview from the same list as the menu dots plus a few yesterday numbers. Open question: also a morning message? (needs alerts, which are off on his box).
6. Disk health and wear, shown for whatever the hardware reports. Marvin's idea: a blue popout with the drive details when SMART data exists. Plan: the card says one plain word (Healthy, Worn, Warning, Not reported); select it for a blue read-only popout with model, size, temperature, power-on time, wear used, reallocated sectors and last self-test, each line saying "not reported by this hardware" when missing, and what kind of disk was detected (NVMe, SATA SSD, hard drive, virtual disk). Amber or red only when the drive itself reports a problem. New code: nothing reads SMART today; needs the SMART tool on the box (offer to install), and must cope with virtual machines.

Console polish (Marvin, 15 Nov 2026: "I like your suggestions for all 3"; ideas taken from a UI-upgrade video, not copied; build waits for his "go"; proposed as its own small release after 0.9.4.42, so 0.9.4.43 or earlier as he prefers):
7. Profile dropdown (do first): the avatar at the bottom of the menu opens a plain dropdown with name and role, Account & sign-in, Help, What's new, Appearance (light or dark), the version and Sign out. Replaces the bare theme button, sign-out icon and tiny Version and What's new text. Opens and closes by keyboard, closes on a click away, keeps Appearance one click away, no heavy shadows.
8. Six-box code input (one shared component): six digit boxes that auto-advance, accept a pasted code across all six, show the numeric keypad, submit on the sixth digit, and show a clear wrong-code state. Keeps autocomplete one-time-code so password managers can fill an authenticator code. Skip the clipboard Auto Fill button and the Resend code link (nothing is sent). Replaces the code fields in Sign in, turning on two-factor, Review, Updates, the Danger zone step-up and the other places that ask for a code (about eight; found in ui.jsx, Auth.jsx, PowerModal.jsx, Settings.jsx, Review.jsx, Updates.jsx).
9. Sliding highlight on the menu: the active page highlight slides to the new page over about 200 ms. No chevrons on every row, no gooey blur filter. Must cope with group headings, status dots, resize and zoom, and do nothing when reduced motion is on. Dots stay steady.
- Rules for all three: every icon from our own set, nothing loaded from the internet, colours from tokens, the shield logo untouched, no change to what any page does.

Recording rule (Marvin, 15 Nov): write as much useful data to the database as we sensibly can.
- Today: metrics keeps 30 second points for 2 hours, 5 minute points for 25 hours, hourly points for 31 days (series: devices.online, dhcp.used, dns.qps/blocked/cached/ms, fw.blocked, net.*, vpn.*, wan.*, sys.cpu/mem/disk/temp, threat.*). Nothing is kept past 31 days, so a 30-day strip needs its own table.
- Proposed: a small daily summary table kept for a year (tiny, one row per day per key), written once a day: blocked inbound, scans, most ports by one scanner, top scanning addresses and ports, alerts and outages with the day's worst status, DNS totals and blocked share, devices online (min, max), peak speeds, pool fullness, disk readings, certificate days left, backup checked or not. Shown in Storage as its own period (default 365 days) beside the other keeping periods, never removed early by the size limit.
- New hourly series where useful: disk temperature and wear, certificate days left, scans per hour, per-upstream DNS answer time, per-link delay and loss.
- Care: scanner addresses are outside addresses; respect the "hide device addresses" setting for any inside address; the daily table is cleared by the Danger zone resets like the other logs.

Still open from the checks: the 10-at-a-time backup list was not seen with more than one backup on screen (test with a seeded demo).

To confirm with Marvin (could not tell from this list whether they shipped): AM step 4 (Logs, Diagnostics, Overview), README revamp, Backups "Keep this many" alignment. Overview map size: corrected (Marvin, 15 Nov).

Marvin's to run: hardware ACCEPTANCE rows from earlier releases and rows 160 to 171.

Parked, not to be raised again by Claude: see the parked idea section.


## Built in 0.9.4.19 (11 Nov)
AR. Management port. Firewall, Protection tab, card "Management port".
- Choose a free wired port (not used by a network, an internet connection, HA or the heart cable), From DHCP or Fixed address with an optional gateway. Static addresses that overlap a served network, DHCP scope or internet address are refused. Refused in bridge mode.
- The ruleset text is static. The port name sits in the nft set mgmt_if and the console ports in lan_console (only when "Close the console on the LAN" is on), so only set contents change at runtime and no system-initiated apply is needed.
- Rules: console, SSH (only if on), ping and the DHCP reply in; the forward chain drops anything in or out of the port.
- networkd file 06-nopewarden-mgmt.network; a static gateway uses its own routing table 195, only for replies; a DHCP route never becomes the default route.
- Change confirm: kept when a signed-in request arrives on the management address, otherwise undone after 60 s (Undo now also works).
- Close the console on the LAN: can only be switched on from the management address with the port healthy. Closes after 30 s healthy, opens after 60 s unhealthy (carrier, address, and a neighbour or the gateway seen).
- Listed under the protected rules; kept by backup restore and network reset.
- First apply: the rules reach the live firewall with the next firewall apply, and the card says so.
- Tests: rules and sets, file rendering, validation, confirm and revert, hysteresis, close-LAN refusal. Full Go tests and web checks pass.
- Yours to check on the box (ACCEPTANCE 79): a real cable, DHCP and a fixed address, the 60 s undo, closing and reopening the LAN console, nothing routed through the port.
- HA use of the management port stays open until the HA appliances arrive.

## Agreed 11 Nov: release 0.9.4.20 (builds right after 0.9.4.19; not built yet) [SHIPPED: versions have moved past it]
1. Themed controls (you asked for the dropdowns to be included). The dropdown list is the native macOS list, which CSS cannot style, so the token checks never saw it. A themed Select replaces every raw select; a themed time picker; a themed date and date-range picker; a build check that fails on any raw select or native date/time input. The same pass is added to AT ("does every control look like our own").
2. Usage redesign (Internet, Usage).
   - "Set a limit" moves out of the numbers card into its own Data limit card (limit, billing day, warning level, what happens at the limit).
   - Period chooser: This billing period, This month, Last month, Last 30 days, Last 7 days, and a custom from and to range.
   - Stage 1: per-connection one-minute totals from the per-link byte counters; limit and billing day per connection; an All connections view with calendar ranges only.
   - Stage 2 (agreed): per-device-per-connection counting with nft counters keyed on the outgoing interface, correct with routes and failover. Older history shows as "all connections, before 0.9.4.20".

## Order from here [OLD: see Current status at the top]
0.9.4.20 (above), then AW in three stages, AM, AT, AU, AV (details in the earlier list).

## Waiting on you [OLD: see Current status at the top]
Results of the 0.9.4.18 Keep delay low checks (ACCEPTANCE 78) and 0.9.4.17 (76 and 77).

## Agreed 11 Nov: AT result, release 0.9.4.27 (not built; waits for "go") [SHIPPED: see Built in 0.9.4.27]
Findings are in claude/at-audit-findings.md. Decisions: fix all 20 findings in one release.
1. Updates becomes its own page (System group, before Settings), themed like the rest. Tabs: Updates, Schedule, What's new, Advanced (repository, channel). Duplicate "How often to check" removed. Nav dot: blue available, yellow restart needed, red failed. Settings loses its blue dot. Sidebar footer says "Update available" when there is one.
2. Overview banner at the top, shown in manual and automatic mode. Buttons: Update now, What's new, Remind me later, Dismiss. Remind me later hides it until tomorrow morning; Dismiss hides it for that version only; both are remembered per account and a newer version shows it again. Yellow restart and red failed banners cannot be dismissed.
3. Blocking lists and Traffic labels move from Settings > Advanced to Protection (new tab "Lists and labels"). High availability and Bridge mode stay in Advanced, which then holds only the box-level tools.
4. All other findings (Activity pickers and axis, Admin activity wording, bridge switch vs status, list badges and empty version, alert text pointing to the wrong card, session names, tooltips on nav dots, unnamed buttons, shorter intros, System log empty state) in the same release.

## Built in 0.9.4.27 (11 Nov): AT result
- Updates is its own page (System group, before Settings): tabs Updates, Schedule, What's new, Advanced. The Updates card left Settings, General. The duplicate "How often to check" is gone (Schedule tab has the update check; "Other recurring jobs" lists the rest).
- Overview: update notice at the top whatever the update mode: blue (Update now, What's new, Remind me later until 08:00 tomorrow, Dismiss for that version; kept per account, a newer version shows it again), yellow restart notice, red failed-update notice (3 days; neither can be dismissed). Status line still counts the restart and the failure.
- Menu: Updates dot blue/yellow/red; Settings lost its blue dot; footer says Update available.
- Protection, new tab Lists and labels (blocking lists and Traffic labels moved from Settings, Advanced). A list that could not be downloaded says "Could not update" with the reason in words; empty version hidden; the duplicate labels row removed. My network moved to Diagnostics, Health and services.
- Logs, Activity: one picker, axis in min/h, speeds in kb/s when small. Bridge mode pill says Saved, not applied. Alerts: data limit text points to Internet, Usage; the "switch on a way" note only shows when an alert is on. Sessions named in words. System log empty state with Show everything. Two unlabelled selects named. Backup note shortened; Logs intro shortened.
- Not changed: finding 4 (Admin activity wording): the wording is already "<Thing> created" with the name; the demo data had no name. Findings 8 (four Save buttons), 15 (Review changes empty page) and 17 (menu dot tooltips already exist) left as they are. Search now finds Updates, Update schedule, What is new, Where releases come from.
- Yours to check (ACCEPTANCE 108-113).

## Open, to decide: What's new is too long to scroll (raised 11 Nov, after 0.9.4.27) [RESOLVED: What's new redesign, built in 0.9.4.28]
Problem: Updates, What's new prints every release in full, newest first, with every bullet open. It is a very long page, the version you care about scrolls away, and the lines run very wide. The same list opens from the menu footer.
Proposal (not agreed yet):
1. Newest release open, every older one collapsed to one line: version and title, click to open. "Expand all" and "Collapse all" at the top.
2. A search box that filters releases by words in their notes (for example "firewall" or "DNS"); matching releases open.
3. Older releases grouped by line (0.9.4.x, 0.9.3 and so on) so 0.9.4.10 to 0.9.4.26 is one block to open, not a scroll.
4. Lines capped to a readable width.
5. Same component for the menu footer window and the Overview "What's new in X" card, so all three stay in step.

## Agreed 12 Nov: Lists used for blocking, say it updates itself (for 0.9.4.28 with the What's new fix)
The Block lists job already exists (Updates, Schedule, Other recurring jobs; also on DNS): daily at 04:10 by default, changeable, Run now, cannot be turned off. It refreshes only lists that are switched on; the others download the first time a block uses them. Nothing new to build for the updating itself; the card just never said so.
1. Add a line to the card: "Updated automatically every day at 04:10. Next: <date and time>." with a Change link (to the schedule) and a Run now button on the card.
2. Reword "not downloaded yet" to "Not in use yet" with "Downloaded when a block uses it, then kept up to date daily."
3. Lists in use show "Last updated 16h ago. Next: tomorrow 04:10."
Not doing: downloading unused lists ahead of time (disk and bandwidth for lists no block uses).

## Agreed 12 Nov: What's new redesign (for 0.9.4.28)
Decisions: only the newest release open by default; search box yes. Collapse older releases to one line, group older releases by line (0.9.4.x, 0.9.3, 0.9.2 and so on), Expand all and Collapse all, readable line width, one component for the Updates tab, the menu footer window and the Overview card.
Question raised: the in-app notes start at 0.8.12 (65 entries; 0.8.43 to 0.8.48 and 0.9.4.13 have no entry). Whether to backfill 0.5.1 to 0.8.11 from the old tarballs and fix lists is open.

## Agreed 12 Nov: backfill What's new (for 0.9.4.28)
Marvin: fill the blanks as best I can. Draft written from the old fix lists and a comparison of what changed between each old build: docs in project doc claude/whats-new-backfill-draft.md. Covers 0.8.44 to 0.8.47 and 0.8.49 (inside the list), 0.5.1 to 0.8.11 (before it) and one grouped entry for the first builds. 0.9.4.13 was never built and nothing was released between 0.9.4 and 0.9.4.10, so those stay without entries. Early entries that rest on little evidence (boot menu, installer screens, 0.8.1 to 0.8.3, 0.7.x) are kept short and say so. Approve or edit the draft, then it goes in with 0.9.4.28.

## Release 0.9.4.28: agreed package (12 Nov; not built, waits for "go") [SHIPPED: see Built in 0.9.4.28]
1. What's new redesign: newest release open, older ones collapsed to a line, grouped by line (0.9.4.x, 0.9.3 and so on), search box, Expand all and Collapse all, readable width; one component for the Updates tab, the menu footer window and the Overview card.
2. What's new backfill: the draft in claude/whats-new-backfill-draft.md goes in as written (0.8.44 to 0.8.47, 0.8.49, 0.5.1 to 0.8.11 and one grouped entry for the first builds). Marvin: include it. He can still edit the draft before the build.
3. Lists used for blocking: "Updated automatically every day at 04:10. Next: ...", Change link, Run now, "Not in use yet" wording, "Next:" on lists in use.
Also in the release: release notes for 0.9.4.28, ACCEPTANCE rows, website refresh.
Still waiting on you: hardware checks (ACCEPTANCE 76 to 113, new hardware not here yet).

## Built in 0.9.4.28 (12 Nov)
- What's new (Updates tab, menu footer window): newest release open, older ones one line each, grouped by line (Earlier in 0.9.4.x, 0.9.3.x, 0.9.x, 0.8.x, 0.7.x, 0.6.x, 0.5.x, Earliest builds), Expand all, Collapse all, a search box (every word must appear; matches shown open), bullets capped to a readable width. One component for both places; the Overview card is unchanged and its All release notes link opens the same list.
- Backfill: 103 entries now, from the draft in claude/whats-new-backfill-draft.md, entered as written (no edits came back). Early entries that rest on little evidence say so.
- Lists used for blocking (Protection, Lists and labels): "Updated automatically: every day at 04:10. Next: ..." with Change when (to Updates, Schedule) and Run now; "Not in use yet" with "Downloaded when a block uses it, then kept up to date every day"; lists in use show Next.
- Docs: updates.md and protection.md; ACCEPTANCE rows 114 to 117.

## AM research done (proposal, not agreed)
Full write-up in claude/am-screen-reorganisation-proposal.md: ten findings from reviews and one academic study, ten recommendations (search covers every setting, one home per setting, "Who can reach what" matrix, show all built-in rules, menu regrouping, first tab "now"), and a five-release order (0.9.4.29 Settings and search; .30 Firewall; .31 Network; .32 Security and People; .33 Logs, Diagnostics, Overview). Waiting on Marvin's decisions.

## AM decisions (from Marvin)
- Order accepted: .29 Settings and search; .30 Firewall; .31 Network group; .32 Security and People; .33 Logs, Diagnostics, Overview clean-up.
- Matrix wanted. It already exists (Firewall > Networks, clickable); promote it to the first thing on Firewall.
- VPN stays its own page. Not merged or moved into another page.
- Hidden-rules audit done: baseline hygiene rules (invalid, replies, ICMP, ND, router ping, DHCP/DNS to router, mDNS, DoT refusal, default drop) are not listed anywhere. Proposed: folded "Always on" list on Firewall. Awaiting yes/no.

## Built in 0.9.4.29 (AM, step 1: Settings and search)
- Audit found no duplicated settings (only repeated labels like Kill switch per route), so the work was search coverage: 39 cards that held settings or controls had no search entry. 24 search rows added (Management port, New devices approval, Filtering profiles, DNS settings, Resolve directly, Monthly data limit, Speed test history and servers, Connection groups, Add a profile, Your services, VPN test addresses, threat limits and single checks and program log, Open now, What the firewall is doing right now, and more). Old-name words added for moved items (blocking lists, traffic labels, how often to check).
- New build check in check-search.mjs: every Card with a literal title on a page must be reachable from search or be listed in src/search-display.json with a reason (18 display-only cards listed). 136 cards reachable.
- Settings tabs left as they are (General, Account, Alerts, Backup, Advanced): after 0.9.4.27 they hold what they should, and renaming General would break docs and habits for no evidence-backed gain. Note: Management port lives on Firewall > Protection, not Settings; found via search now.
- Docs: Overview help describes Search. ACCEPTANCE rows 118-119.
- Test fix (found by CI on v0.9.4.28): TestAlertsSendOnlyWhatIsOnAndHideSecrets used a quiet-hours window of 00:00 to 23:59, which does not cover 23:59 itself, so it failed if it ran in that minute. It now uses one hour either side of now. No product code changed.

## Agreed (Marvin): Management port moves to Settings
- The management port card is a device-level setting (which physical port manages this box), so it belongs in Settings, General, next to Device mode and Console address, not on Firewall > Protection. 0.9.4.29 wrongly described it as living under Firewall.
- Do: move the card (MgmtPort.jsx) to Settings > General; search row points to settings/general; Firewall > Protection keeps a one-line link ("The management port is now under Settings, General"); update docs (firewall.md, settings help), ACCEPTANCE rows 79 and 118, release note. Old search words stay.
- Build with 0.9.4.30 (the Firewall release) unless Marvin wants it sooner.

## Agreed (Marvin): 0.9.4.30 contents (Firewall)
- Matrix "Who can reach whom" becomes the first thing on Firewall.
- Management port card moves to Settings, General (pointer left on Firewall > Protection).
- Folded "Always on: how NopeWarden behaves before your rules" card on Firewall > Rules, seven read-only behaviours with plain reasons and live counts (mockup approved).
- Services as a Firewall tab: to decide when we reach it (not yet agreed).
- Build waits for Marvin's "go".

## Built in 0.9.4.30 (AM, step 2: Firewall)
- Who can reach whom is now the first card on Firewall > Networks (above Your networks).
- New folded card "Always on: how NopeWarden behaves before your rules" at the bottom of Firewall > Rules: seven read-only behaviours with plain reasons and packet counts. To count them, the baseline rules in the compiled ruleset now carry counters and tags (builtin:invalid, replies, health, ping, dhcpdns, dot; the final drop is counted as "default"). Behaviour of the firewall is unchanged. Counters() and /api/fw/rules/counts include these tags. Two tests that matched the exact established-traffic text were updated; new test TestBuiltInBehavioursAreCounted.
- Management port card moved from Firewall > Protection to Settings > General; Protection shows a pointer note; docs moved (settings.md), search row points to settings/general; ACCEPTANCE row 79 updated, rows 120-122 added.
- Note for later: the Protected box above the tabs takes about 300 px before the matrix. Option: fold it to one line. Not done.
- Not done: Services as a Firewall tab (not agreed yet; decide with 0.9.4.31).

## Found (Marvin, after 0.9.4.30): "1 change waiting" appears after every update (PROPOSAL, not agreed)
- Cause: the console marks the firewall as having changes waiting whenever the rule text compiled from the current settings differs from the text of the last confirmed apply (routes_fw.go fwStatus: dirty = compile(config) != lastApplied). Releases that change what the compiler writes (0.9.4.30 added counters to the built-in rules; earlier releases changed rule text too) therefore leave one "change" waiting after the update even though the person changed nothing and what is allowed is identical. The Review page then lists the plain-words summary of the whole firewall, not what differs, so it looks like the same change every time.
- Proposed fix A: Review says what differs. If the settings are unchanged since the last apply, it says "NopeWarden was updated and writes its rules a little differently. What is allowed has not changed."
- Proposed fix B: store a fingerprint of the settings with every confirmed revision. After an update, if the fingerprint of today's settings equals the last confirmed one but the rule text differs, apply the refreshed rules automatically through the normal safe path (60-second undo), record it in Admin activity as "Updated by NopeWarden", and show no waiting change. The first update after this ships still needs one manual commit, because older revisions have no fingerprint.
- Not changing anything about changes the person made: those still wait or apply as they choose.

## Agreed (Marvin): "1 change waiting" after updates
- Fixes A and B above are agreed and go in the next release (0.9.4.31). Build waits for "go".

## Built in 0.9.4.31
- "Changes waiting" after an update (fixes A and B): every applied firewall revision now stores a fingerprint of the settings it came from (fw_revisions.model) and the program version (setting fw.rules_version). fwStatus has refresh_only. Review shows "The firewall rules were refreshed" with the plain sentence; the menu dot and badge are quiet; Overview shows a blue suggestion instead of an amber warning. refreshRulesIfNeeded (40 s after start and hourly): when the version differs, the settings fingerprint matches and the text differs, it applies the refreshed rules (restore point, Apply with the usual timer, keeps only if the firewall answers with counters loaded, refuses if a protected rule would go missing), records "Updated by NopeWarden" as a revision and in the log. Never touches a person's own changes. Revisions made before this release have no fingerprint, so the first update after it needs one manual commit. Test: TestRefreshAfterUpdateOnlyWhenSettingsAreUnchanged.
- Note: the auto-apply keeps the rules after a 3 second check rather than waiting a person's confirmation; there is no person to confirm, so the helper's timer is the undo if the firewall does not answer. This could not be tested against a real nft here; check on hardware (ACCEPTANCE 123).
- Network group, as agreed in principle and chosen by me where not specified: Routes is a tab of Policies (All policies, Routes); #/routes redirects; docs merged into policies.md; DHCP opens on a new Overview tab; Internet > Connections shows Traffic first. DNS already opens on its overview, unchanged. Services as a Firewall tab: not done (never agreed).
- ACCEPTANCE rows 123-127.

## Agreed (Marvin, 12 Nov): AM step 3, with C changed
All of A to F agreed. C changed after Marvin asked whether removing was right on a near-default install: the DNS Profiles tab is REMOVED (not a pointer), with a one-line pointer to Profiles on DNS > Blocking, and #/dns/groups redirects to #/profiles. Condition met first: the Profiles editor could not pick individual block lists, so it gained "Advanced: pick block lists".

## Built in 0.9.4.32 (AM, step 3: Protection, Threats, Profiles)
- C: Profiles editor gets Advanced: pick block lists (profileOut.lists / profileIn.lists; nil keeps the profile's lists, [] clears them; category lists are still chosen by level). DNS Groups tab and its editor removed; note on DNS > Blocking; redirect in App.jsx. /api/dns/groups stays (Devices, Blocks and Speed pickers still use it). Test: TestProfileKeepsAndEditsItsOwnBlockLists.
- F: Pause is one PauseMenu drop-down; device count links to #/devices; level pill says Custom filtering, N categories (+ N lists) instead of "Choose"; the level button label is now Custom.
- A: Protection Overview = Security check first, then "Also protecting you" (Blocks, Threat watching). The "Protection at a glance" card is gone.
- B: "Where do I block things?" card at the top of Protection > Blocks (links to DNS Blocking, Profiles, Firewall rules).
- D: Threats > Overview is the install page when not installed or installing (What threat watching does + Install card). Other tabs point to Overview. Settings no longer doubles as the install page.
- E: Allowed checks and Devices never checked moved from Advanced to Rules. Advanced keeps save/load tuning, reset, program log, where it writes.
- Search rows, docs (dns, profiles, protection, threats), release note, website, ACCEPTANCE rows 128-132.
- Next in order: Logs, Diagnostics and Overview clean-up; AU; AV.

## Built in 0.9.4.33
- Websites accept a leading "*." (dnssrv.NormalizeSite) in Blocks, Speed control and DNS exceptions; "a*b.com" is still refused. Test TestNormalizeSiteAcceptsAWildcardPrefix.
- Update notice: GitHub release text is shown as plain lines (plainNotes in whatsnew.jsx): no **, no "Full Changelog" link, no "by @name in URL". Used by Updates and the Overview notice.
- ACCEPTANCE rows 133-134.

## AM step 4 proposal: Logs, Diagnostics, Overview (not agreed yet)
Full write-up: claude/am-logs-diagnostics-overview-proposal.md. G small map on Overview, full map on Logs; H plain country-data message; I lighter This machine card; J Logs tabs renamed Events, Summary, Data use, Admin activity; K Console connections one row per visit, latest 10; L Services empty state. Open question: keep Data use next to Internet > Usage? Awaiting decisions.

## Agreed (Marvin, 13 Nov): "console" becomes a real built-in service
Found: the Service column of the built-in rows (Firewall > Rules, S1 to S3) shows a chip "console" that is only a hard-coded word in RuleLines.jsx; it is not in the Services list, cannot be picked in a rule, and S3 "The port you set this up from" is labelled From "Management" although it is not a management network.
Decision: make "console" a real built-in service, listed on the Services page and pickable in rules. It is worked out when the firewall is compiled from the console's real ports (fw.ConsolePortsFrom: the saved port, plus a new one while it is being switched), so it follows the console port setting; it is not a fixed port list. Open point for Marvin: the box serves only HTTPS on the console port. Port 80 is open only for the installer and redirects to HTTPS, so plain http is not part of "console" unless he wants the redirect included. Also fix From/To for S3. Not built; waits for "go" (planned with 0.9.4.34 or its own build).

## APPROVED fix list for 0.9.4.34 (Marvin, 13 Nov; not built, waits for "go") [SHIPPED: see Built in 0.9.4.34]
1. "console" becomes a real built-in service (see the agreed entry above). Built from the console's real ports; the built-in rows link to it; S3 From/To corrected. Only the console port, not port 80.
2. AM step 4, Logs, Diagnostics and Overview, items G to L as written in claude/am-logs-diagnostics-overview-proposal.md: G small map on Overview, full map on Logs > Summary; H plain country-data message; I lighter "This machine" card; J Logs tabs Events, Summary, Data use, Admin activity; K Console connections one row per visit, latest 10; L Services empty state. Data use stays next to Internet > Usage (my recommendation, taken as approved).
3. Package-cache self-repair: when the system-update check reports a corrupted package cache, remove /var/cache/apt/pkgcache.bin and srcpkgcache.bin, retry once, and warn only if it still fails. (Marvin's 0.9.4.32 to .33 update repaired it by itself; this stops it showing a warning at all.)
4. Bug (found by Marvin, 13 Nov): Review/Settings access check said "You are connected through ;" with the port name missing. Cause: App.sessionSafeVia had an unnamed result and a bare return, so the port name was always empty (the same empty name showed in the firewall preview message). Fixed in the tree (named result), and the line now reads "You are connected from <your address> through <port name>; it can manage this device." Test: TestSessionCheckNamesThePort.
   Agreed (Marvin, 13 Nov): the wording stays "You are connected from <address> through <port name>"; the port name is the one he gave the port (Name (enp0s20f1)), looked up at check time so a rename shows next run; an unnamed port shows its plain name. The address is the one the box sees for his browser (differs behind a VPN or router).
5. Added by Marvin 13 Nov ("include all of those in the next push"), from the audit of the step-3 proposal and the wider reorganisation list:
   a. Profiles: the device count links to Devices already filtered to that profile (needs the Devices page to take a profile filter from the link).
   b. R10: every simple page ends with a clear link to its Advanced tab, and Advanced tabs are named the same everywhere (audit, then fix).
   c. Profiles as a tab of Devices & access (R7). Design check first: where each page's content goes and what old links and search rows redirect.
   d. Services as a tab of Firewall (R7), with the old menu entry redirecting and search aliases kept.
   e. R9: tools for big rule sets: warn about duplicate or shadowed rules, and group rules (see "Rule groups" below, which is a separate proposal waiting for decisions).
   f. R5: a check of every form for options whose choices change depending on another choice; where they do, say why in one line.
   g. R6: a "hide a card" control on Overview (not a widget gallery).
   Scope warning: 0.9.4.34 is now large; if it gets too big to verify properly, I will propose splitting it into 0.9.4.34 and 0.9.4.35 rather than shipping it half-checked. Items with design choices (c, d, e) get a mockup or a written proposal before they are built.
Then, as before: AU (docs refresh with new screenshots), AV ("How it works").


## Rule groups (asked by Marvin 13 Nov; proposal, not agreed) [SHIPPED as rule sets, 0.9.4.37]
Question: can rules be grouped, for example Media (Plex, Sonarr, Prowlarr, Radarr, SABnzbd) or Databases (Postgres access plus the application)? Answer today: no. A rule has a name, a zone pair, an order and a switch; nothing groups them. Firewall > Groups & lists groups devices and addresses, not rules.
Proposal: a rule (and any other policy: open port, route, block, speed rule) can be put in one named group. Policies and Firewall > Rules show a group as a folded section with a count and a live match total; filter by group; Pause, Turn off or Turn on a whole group at once; rename and delete a group (members are kept, ungrouped). A group changes nothing about what a rule does or the order it is checked in; it is only for organising and for acting on many at once. Members may come from different zone pairs and different kinds (the Postgres rule and the application's open port together). Open decisions: (1) one group per policy, like a folder (my recommendation, simple to see), or several tags per policy (OPNsense categories work this way, more flexible, harder to show as sections); (2) whether groups span all policy kinds or only firewall rules (my recommendation: all kinds, because Plex needs a rule and an open port).

## Rule groups: decisions (Marvin, 13 Nov)
1. One group per policy for now. Tags are to be discussed separately (see "Tags: to discuss" below), so the group design must not block adding tags later (store the group as its own thing; do not bake the one-group limit into the layout).
2. Groups hold all kinds of policy: firewall rules, open ports, routes, blocks, speed rules.
Status: approved in principle; mockup and write-up first, then it joins the 0.9.4.34/.35 list on his "go".

## Tags: to discuss (raised by Marvin 13 Nov) [SHIPPED, 0.9.4.37]
Questions to bring: what a tag would do that a group does not (cross-cutting labels such as "Remote access" or "Kids" across several groups; filter and search by tag; a tag on devices, profiles and networks as well as policies, so "show everything about Plex"); where tags would show (chips on rows, filter bar, search); whether a tag can be acted on like a group (pause) or is label-only (my lean: label and filter only, so a tag never changes what the firewall does); limits (how many per item, naming rules, colours kept to the one status palette).

## Tags: decisions so far (Marvin, 13 Nov)
- A tag is label and filter only. Pausing, turning off or turning on is done by group or by the single policy, never by tag ("pausing is a deliberate action").
- Where tags apply: my recommendation is policies (all kinds) and devices first; profiles and networks later if wanted (few of them, already named and searchable). Awaiting his reply on that.
- Tags are not built; they come after groups.

## Tags: APPROVED to build with groups (Marvin, 13 Nov: "we can add tags now")
Scope (my recommendation, taken as approved unless he says otherwise): tags on all kinds of policy (firewall rules, open ports, routes, blocks, speed rules) and on devices. Label and filter only: a tag never pauses, turns off or changes what anything does. Shown as chips on rows, a tag filter next to the group filter, found by search ("show everything tagged plex", across policies and devices). Plain names, a small limit per item (five), colours stay inside the one status palette (no tag colours of their own). Profiles and networks not tagged in this release.
Order inside the release: groups first, tags on the same plumbing. Mockup of groups and tags before anything is built. Joins the 0.9.4.34/.35 list; he says "go".

## Built in 0.9.4.34
- console is a real built-in service (Services page, rule picker, From labels Setup port / Management port); access check reads "You are connected from <address> through <port name>".
- Overview: smaller map with Top countries, plain country-data message (reason under Show details), lighter This machine card (What's using it? opens Diagnostics), Hide or show cards (remembered per person).
- Logs tabs: Events, Summary, Data use, Admin activity. Diagnostics: console connections one row per visit (10, Show older), below Services; new What is using the disk card; Services empty state fixed.
- Package-cache self-repair in the update check (removes pkgcache.bin/srcpkgcache.bin, retries once).
- Profiles device count opens Devices filtered to the profile (new Profile filter).
- R10 audit: only Settings, Updates and Threats have Advanced tabs, all named Advanced. Updates and Threats now end with a link to it. Settings left alone (five peer tabs).
- R5 audit: one form changed options silently (Forwards, internet connection choice); now says why. Others already explain themselves.
- ACCEPTANCE rows 135-142.
- Still to do in 0.9.4.35 (mockup first): Profiles tab in Devices, Services tab in Firewall, rule groups and tags, duplicate/shadowed rule warnings.

## README revamp (agreed 13 Nov 2026)
- Done: README rewritten with logo, name, slogan, feature sections, five screenshots (docs/img), install, build and status. Commit messages carry no co-author or session lines from now on.
- Follow-on: add a Website and Documentation link at the top of the README once the domain is bought.

## Standing design rule (Marvin, 13 Nov 2026)
Ease of use stays the test for everything. A setting lives with the area that uses it, or stays there. Before any page or tab is moved, write down who uses it and from where, and keep a link from the old place. Applies to 0.9.4.35 (Profiles into Devices, Services into Firewall) and every later change.

## Agreed plan for 0.9.4.35 and 0.9.4.36 (13 Nov 2026)
Mockup: placement-mockup-0.9.4.35.png. Rule: setting lives with the area that uses it (see standing design rule).

### 0.9.4.35 (placement and rule tools)
1. Services becomes a tab of Firewall; the Services sidebar item goes; old #/services links open it. Rule editor service picker gets "Make a new service"; each service shows how many rules use it; link from the policy editor.
2. Profiles becomes a tab of Devices & access; Profiles sidebar item goes; old #/profiles links open it. Device editor gets "Manage profiles"; links from DNS and Protection stay.
3. Firewall tab "Groups & lists" is renamed "Device groups" (it only holds device groups). Check first that nothing else lives in it.
4. Policies moves from the Network to the Security group (next to Firewall).
5. Duplicate and shadowed rule warnings (R9).
6. Not moved: VPN stays in Security.
7. Docs, search rows, ACCEPTANCE rows, release note, website as usual. No co-author or session lines in commits.

### 0.9.4.36 (page length, then rule sets and tags)
- Page length pass, mockup of the worst pages (before and after) first. Measured heights at 900 px: Settings General 2960, Settings Alerts 2720, Overview 2450, DNS Overview 2200, Diagnostics Health 1900, Firewall Protection 1740, Settings Advanced 1660, Services 1620, Logs Summary 1545. Approach: fold rarely used cards, show the latest few with Show more, fold Your networks on Overview to one line, show your own services before the built-in list. Re-measure on a box with more devices.
- Rule sets (name agreed, replaces "rule groups"): one set per policy, all policy kinds, pause/turn off/on a whole set, folded with counts, filter. Tags as agreed (label and filter only, max five, no own colours, search finds them), on the same plumbing.

## Built in 0.9.4.35
- Services is a tab of Firewall (usage counts, Make a new service in the picker, link from Policies); Profiles is a tab of Devices & access (Manage profiles in the device editor, pointer to Device groups). Old #/services, #/profiles and #/dns/groups links redirect. Help chapters for both merged into Firewall and Devices & access.
- Firewall tab "Groups & lists" renamed Device groups (checked: it only holds device groups). Policies moved to the Security group below Firewall. VPN stays.
- Overlapping rules: FindOverlaps (duplicate, redundant, shadowed) compares what is written only (same networks, names, ports, addresses; skips matrix, paused, rate-limited, timed rules). Rule lines show Duplicate / Not needed / Never applies; a count note on the Rules tab. CIDR containment is not compared, so some overlaps are missed on purpose.
- ACCEPTANCE rows 143-146.
- Next: 0.9.4.36, page length pass (mockup first), then rule sets and tags.

## Backups: Keep this many alignment (13 Nov 2026)
Fixed in the working tree, ships with the next build: the three fields (Make a backup, At, Keep this many backups) now line up at the top, and the Keep box is narrower.

## Site Gateway review (13 Nov 2026), proposal, not agreed [DECIDED: see the Agreed section below]
Reviewed /Users/marvinwade/Downloads/site-gateway2 (backup, restore, log retention and pruning, reset and defaults). NopeWarden already has: scheduled backups, restore check, hand-check, encrypted download, upload, restore, reset to network defaults and factory reset (with step-up), log retention in days and MB, keep-alerts days, Clean up now. Candidates to bring over are in the chat message of 13 Nov; decisions to be recorded here.

## Agreed from the Site Gateway review (13 Nov 2026), for 0.9.4.36 with the page-length pass
Mockup: danger-zone-mockup-0.9.4.36.png. Decisions (Marvin: 1-4 and 6 ok, 5 ok if something similar):
1. Backup history: every backup, restore, upload and delete, including failed ones and who did it, as a folded list under Backups.
2. Log clean-up preview: "If you clean up now, this would be removed: ..." before the button, plus last run and what it removed.
3. Restore default settings: a middle reset that puts preferences back (backup schedule, alerts and notifications, log keeping, update schedule, console display choices such as hidden Overview cards and table columns). Keeps networks, firewall, DNS, DHCP, VPNs, accounts and sign-in. Same step-up confirmation and a safety copy first. Scope is preferences only (my suggestion; DNS and DHCP belong to Reset network settings).
4. Include history (logs) in a backup: a checkbox, off by default, for manual and scheduled backups.
5. Retention, a similar but smaller version: three periods instead of two: Traffic and events (days), Alerts and outages (days), Admin activity (days, default 365). Plus the preview from item 2.
6. No change to safety snapshots (already visible in the backup list).
7. Danger zone theming from Site Gateway: a red-bordered Danger zone block at the bottom of Settings, Backup & recovery, with three cards of rising severity: yellow Restore default settings, orange Reset network settings, red Factory reset. Each says what stays. Uses the existing status colour tokens only.
8. Danger zone confirmation (agreed 13 Nov 2026): all three cards (Restore default settings, Reset network settings, Factory reset) ask for the administrator password every time, plus the authenticator code when two-factor sign-in is on. Today a code replaces the password; that changes for these three only. Administrator accounts only, a safety copy first, refused while an update runs, and the word "reset" typed for Factory reset (as now). Other serious changes keep today's rule (code if two-factor is on, otherwise password). Needs backend change in internal/app/reset.go and a StepUp variant that shows both fields.
9. Manual clear of logs (agreed 13 Nov 2026): in the Storage table each kind of record gets a tick box, with "Select all" and a Clear selected button. The confirmation names each kind and how many entries go, and asks for the administrator password plus the authenticator code if two-factor is on (same as the Danger zone). Admin activity is not offered for manual clearing: it is the record of who did what, and only ages out by its own retention period (default 365 days). The clearing itself is written to admin activity (who, what kinds, how many). Applies to traffic and events, alerts and outages, and other history; firewall change copies are not cleared this way.

## Overview map size (13 Nov 2026)
0.9.4.34 capped the map height (13rem) and narrowed its column, which left blank space either side of a small map. Reverted in the working tree to the original size (column ratio 2.2 to 1, no height cap); ships in 0.9.4.36. If a smaller map is still wanted, do it by lowering the card's own height with the map filling the width, and show a mockup first.

## Built in 0.9.4.36 (Phase 1, without the page-length folds)
- Danger zone (Settings, Backup & recovery, bottom): yellow Restore default settings (preferences only, store.ResetPreferences), orange Reset network settings, red Factory reset. All three need the password every time plus the authenticator code when two-factor is on (a.dangerous; /api/stepup reports danger "both" or "password"). Each window says what stays. Restore default settings does not reconnect or reload.
- Backup history: a file (backups/history.jsonl, newest 500) beside the backups, so restores and resets do not rewind it. Records created, failed, restored, restore-failed, uploaded, downloaded, deleted, with who. Folded "Backup history" in Backups. "Include history (logs)" (backup config include_history, off by default) adds logs.db to manual and scheduled backups; restore ignores it.
- Retention: three periods (Traffic and events, Alerts and outages, Admin activity default 365). Admin rows (area admin or actor a user) are pruned only by admin_days and never by the size limit. Storage splits "What happened" and "Admin activity". Clean-up preview line plus last run. Clear by hand: tick boxes, Select all, Clear selected, password + code, writes "Logs were cleared by hand" to admin activity; admin activity and firewall change copies not offered. Old DELETE /api/logs and the Delete all log entries button removed (it would have wiped admin activity).
- Overview map: back to full size (cap removed).
- ACCEPTANCE rows 147-153. Tests: DangerZone, RestoreDefaults, BackupHistory, Retention.
- NOT in this build: the page-length folds. The mockup (page-length-mockup-0.9.4.36.png) is waiting for approval; folds go in the next release once approved. Rule sets and tags remain Phase 2.

## Phase 2 for 0.9.4.37: rule sets and tags (13 Nov 2026), mockup sent, awaiting approval [SHIPPED: see Built in 0.9.4.37]
Mockup: rule-sets-tags-mockup-0.9.4.37.png. Marvin's brief: rule sets apply across all policy kinds; a whole set can be paused, turned off or turned on; sets are folded with counts and can be filtered. Tags label and filter only, never change behaviour, at most five per item, no colours of their own, search finds them, same plumbing as sets, on all policy kinds and devices.
Proposed (not built): sets and tags live on the Policies list (rule, route, block, open port, speed) with filters for kind, set, tag and text; Set and Tags lines in each policy editor; Tags line in the device editor; Devices list tag filter; search results for tags (open the list filtered) and sets.
Open questions in the mockup: (1) a policy is in at most one set and a set may mix kinds (alternative: one kind per set); (2) tags are shared names, lowercase, up to 24 characters; (3) filters only on Policies and Devices, other pages show chips; (4) administrators manage sets and tags, others see them. Turn on restores only the policies that were on before the set was turned off.

### Decisions for 0.9.4.37 (13 Nov 2026; Marvin: "open to your suggestions", so my recommendations stand)
1. A policy is in at most one set; a set may mix all five policy kinds.
2. Tags are shared names across policies and devices, lowercase, up to 24 characters, five per item, no colours.
3. Set and tag filters on Policies and Devices; other pages show chips only.
4. Administrators manage sets and tags; others see them.
Also: deleting a set keeps its policies; Turn on restores only what was on before the set was turned off; Pause on a set uses the same time choices as a single policy. Waiting for "go" before building.

## Danger zone as its own tab (13 Nov 2026, Marvin)
Settings gets a sixth tab, "Danger zone", with the tab label in the bad (red) status colour, administrators only, holding the three reset cards. It leaves Backup & recovery. Search row and help chapter updated; built in the working tree, ships with the next release (0.9.4.37) because 0.9.4.36 is already delivered.

## Status (14 Nov 2026)
0.9.4.36 is pushed and running on Marvin's machine (confirmed). The Danger zone tab therefore needs a release of its own; proposed 0.9.4.37 = Danger zone tab (small), 0.9.4.38 = rule sets and tags, pending his go.

## Built in 0.9.4.37 (14 Nov 2026, one large push at Marvin's request)
- Rule sets and tags: tables policy_sets, policy_set_members, item_tags (own schema migration, included in backups and resets); sets span all five kinds; one "Set & tags" button per policy row (deviation from the mockup, which had a line in each of the five editors); devices get Tags in the device window; filters on Policies and Devices; search finds tags and sets; admin-only management. Known limit: undoing a policy delete re-creates it with a new id, so its set and tags are not restored.
- Page-length folds on Overview map location, Get started, Alerts (quiet hours), Storage, Services, Diagnostics, Forced DNS.
- Danger zone is its own red tab in Settings (seven tabs now).
- Docs refresh (AU): new screenshots, policies/danger-zone/backups pictures referenced, README pictures retaken. "How it works" chapter (AV) added to help and website.
- Website: whats-new and documentation rebuilt for 0.9.4.37.
- Still waiting: README Website/Documentation link (until the domain is bought). Hardware ACCEPTANCE rows 76-158 are Marvin's to run.

## Parked idea: a new look as a selectable theme (15 Nov 2026, Marvin: "just curious, track it for a possible future change")
Not agreed, not scheduled. Nothing to build until Marvin says go.
- Why: the console looks like many generated apps (dark, sidebar, rounded cards, one blue accent). Marvin wants something distinct, taking ideas from Untangle/Arista and Sophos dashboards without copying them. Renders (nopewarden-redesign-mock-1 to 7) were shown; the last one (7) is the merged direction.
- Direction in render 7: light paper canvas, deep violet-ink sidebar grouped Monitor / Protect / Configure / System, the existing blue-to-purple shield kept exactly and brand violet as the accent (status colours stay green/amber/red/blue), a numbers-first strip (Internet, Devices, Protection, Policies) with click-through, network layout widget, traffic by network, policies by set, Needs a look, and the Overview map kept as a wide panel with top countries (never smaller than today; keep a minimum size).
- How, if done: (1) a new theme in tokens.json beside Dark and Light, choosable in Settings, General, Appearance; the original stays. (2) A separate Layout choice, Classic or New, for the grouped sidebar and the new Overview. (3) Other pages follow one at a time. Staged so each release is safe.
- Rule: fonts must be bundled files (woff2, open licence such as OFL, licence text shipped, Latin subset, one or two weights), embedded in the binary and the website folder. No Google Fonts, CDN, hosted icons or remote images. Add a build check that fails on any external address in the console or website (allowing the user-triggered OpenStreetMap place search). Today the console uses system fonts and the website loads nothing external (checked 15 Nov 2026).
- Open questions: theme name; per-policy hit counts and per-network traffic counters (may need small backend additions); font choice.

## Built in 0.9.4.38 (15 Nov 2026): hotfix, Threats Rules tab
Threats, Rules showed "Exceptions is not defined" when threat watching was installed: the Allowed checks and Devices never checked cards had been moved from Advanced to Rules as a component called Exceptions that was never written. Component written (also uses "Quieted by NopeWarden"). New build check web/check-defined.mjs fails the build when a page uses a component that is not imported or defined (it flagged exactly this bug before the fix; 88 files clean after). Verified in the demo console with threat watching faked as installed. ACCEPTANCE row 159. Lesson: the demo has no installed-engine scene, so engine-only screens were never exercised; consider adding a demo scene with the engine installed.

## Built in 0.9.4.39 (15 Nov 2026): livelier Networks, DNS, DHCP, Threats
- Networks: Network layout card (internet at the top, each network below, live down/up every second, devices online, tags, pool fullness; select to open the editor).
- DNS: lookups stacked by outcome, live feed, trend lines in the cards, Answer time, What blocked it, plain zero messages, upstream health dot; Live stays the default.
- DHCP: four numbers, Devices over time, pool bars with Nearly full past 80%, Last seen, Lease ends, Name button (finds or adds the device by its hardware address).
- Threats: Status says Starting until the first reading (no more Watching beside Waiting for the program); the engine notice uses the real engine state; the old "What it has been doing" card is now "What changed".
- Shared pattern: one 1 h / 24 h / 7 d choice remembered between pages (Live only on DNS, remembered per page); What changed strip on Networks, DNS, DHCP; number cards open the page that explains them (DHCP, DNS, Threats).
- Demo: new scene `NW_SCENE=engine` (main plus the threat engine installed and watching).
- Not recorded, so not faked: devices-over-time only goes back as far as the box has been saving history; the live feed shows only lookups since the page opened.
- Hardware ACCEPTANCE rows 160 to 165 are yours to run.

## Built in 0.9.4.40 (15 Nov 2026): DNS split into more tabs
- Raised by Marvin: the DNS page reads long. He left the one open choice to me; decided: Servers is its own tab, not part of Settings.
- DNS now has Overview, Activity, Servers, Blocking, Your names, Settings. Overview keeps the number cards, Lookups over time, How queries were answered, Answer time, What changed.
- Activity (new): Busiest devices, What blocked it, Most blocked, Most asked, with a 1 h / 24 h / 7 d choice (remembered, shared with other pages).
- Servers (new): Forced DNS, Encrypted DNS, Resolve directly, Upstream DNS servers. Search entries point there; #/dns/settings still opens Settings.
- No Go code changed since 0.9.4.39 (full go test passed there); the console build chain passes.
- ACCEPTANCE rows 166 to 168 are yours to run.

## Built in 0.9.4.41 (15 Nov 2026): dots on tabs, shorter backup lists
- Raised by Marvin: the menu dots do not say which tab holds the reason; the Backup & recovery list grows long. Also noticed the backup restore reminder already exists on that page, so it was dropped from the Threats idea list.
- Attention items can name a tab (for example dhcp/scopes, dns/servers, protection/overview, settings/general, settings/backup, threats/alerts). The menu dot uses the page, the tab bar shows the same dot in the same colour on that tab, with the reason on hover. A tab dot only exists when the menu has one.
- New items: no backup has passed its check (warn), and no backup checked by hand in 90 days (blue suggestion), both on Backup & recovery.
- Lists: Limited now shows 10, with "Showing 10 of N" and Show more; Backups and Backup history use it.
- Protection: both Open links look the same.
- No Go code changed since 0.9.4.39 (full go test passed there); console build chain passes.
- Decided: Threats edge numbers (who is probing, ports, blocks by network, 30-day strip) wait for 0.9.4.42, placed on Threats Overview above the install card unless Marvin says otherwise. The mock is in the scratchpad.
- ACCEPTANCE rows 169 to 171 are yours to run.
