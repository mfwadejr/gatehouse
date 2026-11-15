# Fix list (after 0.9.4.19) - started 11 Nov 2026

Continues claude/fix-list-after-0.9.4.11.md, which still holds AW, AM, AT, AU, AV and the hardware checklist. Status from there: AP, AS (0.9.4.17), AQ (0.9.4.18, installed) and AR (0.9.4.19) are BUILT.

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

## Agreed 11 Nov: release 0.9.4.20 (builds right after 0.9.4.19; not built yet)
1. Themed controls (you asked for the dropdowns to be included). The dropdown list is the native macOS list, which CSS cannot style, so the token checks never saw it. A themed Select replaces every raw select; a themed time picker; a themed date and date-range picker; a build check that fails on any raw select or native date/time input. The same pass is added to AT ("does every control look like our own").
2. Usage redesign (Internet, Usage).
   - "Set a limit" moves out of the numbers card into its own Data limit card (limit, billing day, warning level, what happens at the limit).
   - Period chooser: This billing period, This month, Last month, Last 30 days, Last 7 days, and a custom from and to range.
   - Stage 1: per-connection one-minute totals from the per-link byte counters; limit and billing day per connection; an All connections view with calendar ranges only.
   - Stage 2 (agreed): per-device-per-connection counting with nft counters keyed on the outgoing interface, correct with routes and failover. Older history shows as "all connections, before 0.9.4.20".

## Order from here
0.9.4.20 (above), then AW in three stages, AM, AT, AU, AV (details in the earlier list).

## Waiting on you
Results of the 0.9.4.18 Keep delay low checks (ACCEPTANCE 78) and 0.9.4.17 (76 and 77).

## Agreed 11 Nov: AT result, release 0.9.4.27 (not built; waits for "go")
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

## Open, to decide: What's new is too long to scroll (raised 11 Nov, after 0.9.4.27)
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

## Release 0.9.4.28: agreed package (12 Nov; not built, waits for "go")
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

## APPROVED fix list for 0.9.4.34 (Marvin, 13 Nov; not built, waits for "go")
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


## Rule groups (asked by Marvin 13 Nov; proposal, not agreed)
Question: can rules be grouped, for example Media (Plex, Sonarr, Prowlarr, Radarr, SABnzbd) or Databases (Postgres access plus the application)? Answer today: no. A rule has a name, a zone pair, an order and a switch; nothing groups them. Firewall > Groups & lists groups devices and addresses, not rules.
Proposal: a rule (and any other policy: open port, route, block, speed rule) can be put in one named group. Policies and Firewall > Rules show a group as a folded section with a count and a live match total; filter by group; Pause, Turn off or Turn on a whole group at once; rename and delete a group (members are kept, ungrouped). A group changes nothing about what a rule does or the order it is checked in; it is only for organising and for acting on many at once. Members may come from different zone pairs and different kinds (the Postgres rule and the application's open port together). Open decisions: (1) one group per policy, like a folder (my recommendation, simple to see), or several tags per policy (OPNsense categories work this way, more flexible, harder to show as sections); (2) whether groups span all policy kinds or only firewall rules (my recommendation: all kinds, because Plex needs a rule and an open port).

## Rule groups: decisions (Marvin, 13 Nov)
1. One group per policy for now. Tags are to be discussed separately (see "Tags: to discuss" below), so the group design must not block adding tags later (store the group as its own thing; do not bake the one-group limit into the layout).
2. Groups hold all kinds of policy: firewall rules, open ports, routes, blocks, speed rules.
Status: approved in principle; mockup and write-up first, then it joins the 0.9.4.34/.35 list on his "go".

## Tags: to discuss (raised by Marvin 13 Nov)
Questions to bring: what a tag would do that a group does not (cross-cutting labels such as "Remote access" or "Kids" across several groups; filter and search by tag; a tag on devices, profiles and networks as well as policies, so "show everything about Plex"); where tags would show (chips on rows, filter bar, search); whether a tag can be acted on like a group (pause) or is label-only (my lean: label and filter only, so a tag never changes what the firewall does); limits (how many per item, naming rules, colours kept to the one status palette).

## Tags: decisions so far (Marvin, 13 Nov)
- A tag is label and filter only. Pausing, turning off or turning on is done by group or by the single policy, never by tag ("pausing is a deliberate action").
- Where tags apply: my recommendation is policies (all kinds) and devices first; profiles and networks later if wanted (few of them, already named and searchable). Awaiting his reply on that.
- Tags are not built; they come after groups.

## Tags: APPROVED to build with groups (Marvin, 13 Nov: "we can add tags now")
Scope (my recommendation, taken as approved unless he says otherwise): tags on all kinds of policy (firewall rules, open ports, routes, blocks, speed rules) and on devices. Label and filter only: a tag never pauses, turns off or changes what anything does. Shown as chips on rows, a tag filter next to the group filter, found by search ("show everything tagged plex", across policies and devices). Plain names, a small limit per item (five), colours stay inside the one status palette (no tag colours of their own). Profiles and networks not tagged in this release.
Order inside the release: groups first, tags on the same plumbing. Mockup of groups and tags before anything is built. Joins the 0.9.4.34/.35 list; he says "go".
