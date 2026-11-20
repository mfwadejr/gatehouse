# Data-keeping inventory (20 Nov 2026, after 0.9.4.49)

Asked for by Marvin (20 Nov) after the placement audit, because moving Storage to Logs on a thin reason would only move the scatter. Read-only: nothing was changed. Built from the code (tables in internal/store, pruning in internal/app/storage.go, logs.go, devtraffic.go, speedtest.go, overview.go, internal/dns/querylog.go, backup.go, backuphistory.go) and the console (Storage.jsx, Logs.jsx, Backups.jsx). Not agreed, not built.

## 1. What the box stores, how long, and where each keep setting is
"Keep setting" is where a person changes how long it is kept. "Seen" is where the data is shown.

| What | Seen on | Kept for | Keep setting lives in |
|---|---|---|---|
| Connections (firewall log) | Logs (Events, Summary), Reports, Threats "At your front door" | Traffic and events days (1 to 365, default 30); also trimmed first by the size limit (default 200 MB) and a hard cap of 500,000 rows | Settings, Alerts & logs, Storage (days) and Logs, What gets recorded (days and MB) |
| DNS lookups | Logs (Events), DNS pages, Reports | Same days and size limit | Same two places |
| What happened (general activity) | Logs, Events | Same days; trimmed by the size limit | Same two places |
| Admin activity (sign-ins, what people changed) | Logs, Admin activity | Admin days (30 to 3650, default 365); never trimmed by size; cannot be cleared by hand | Storage only |
| Data used by each device, by connection, by country | Logs (Activity), Overview map, Internet, Usage | Traffic and events days; but whole-connection totals are kept at least 100 days whatever you choose (for billing periods and the provider report) | Storage (days) and Logs, What gets recorded |
| Overview and other charts (metrics) | Overview, DNS, DHCP, Networks, Threats, Reports | Four steps: fine 2 h, medium 25 h, hourly 31 / 90 / 365 days, daily 90 days to 10 years or everything; never trimmed by size | Storage |
| Speed tests | Internet, Speed; provider report; Reports | 365 days, fixed in code | Nowhere (no setting) |
| Threat alerts | Threats, Overview | Alerts and outages days (7 to 3650, default 365) | Storage |
| Internet outages | Internet, Overview, Reports, provider report | Same alerts and outages days | Storage |
| Firewall change copies | Firewall, Change history; Review; Settings, Backup, Changes | Newest N (20 to 5000, default 200), plus the one in use and any waiting | Storage |
| Backup files | Settings, Backup & recovery | N copies (3 to 60, default 7) | The Backups card itself |
| Backup history (who made, restored, deleted) | Backups, Backup history | Newest 500 lines, fixed; survives restores | Nowhere (no setting) |
| Saved reports | Logs, Reports | Until removed by an administrator | The report itself |
| Known devices (names, last seen) | Devices, DHCP | Until deleted by hand | The device itself |
| DHCP leases | DHCP | Looks after itself: expired offers go at once, active leases a day after they end | None needed |
| Sign-in sessions | Settings, Account | Expire on their own | None needed |
| Threat engine's alert file (eve.json, written by the engine, not NopeWarden) | Threats, Advanced, Where it writes | Not set by NopeWarden | Nowhere. To check on the box: whether the engine's packaging rotates it. |
| System (journal) log | Diagnostics, Log | Debian's journald defaults, not set by NopeWarden | Nowhere. To check on the box: the journal's size limit. |
| Blocking list downloads | Protection, Lists and labels | Latest copy per list; version history retention not checked | List updates only |
| Syslog, NetFlow, SNMP | not shown | Nothing kept: these send data out | Settings, Alerts & logs (three cards) |

## 2. Findings
**D1. The same two keep settings are editable in two places.** "Keep logs for (days)" and "Never use more than (MB)" are in Logs, in the What gets recorded window, and the day count is also in Settings, Alerts & logs, Storage (as "Keep traffic and events for"). The size limit is editable only in the Logs window; Storage only shows it. Changing the days in one place changes the other. A person looking for "how big may logs get" has to find the Logs window.

**D2. One label covers more than it says.** "Keep traffic and events for (days)" governs six kinds, and Storage describes its range as 1 to 365; but the totals per connection are kept at least 100 days whatever the number, and the size limit can remove connections, lookups and activity earlier than the days say. Neither is stated on the field.

**D3. The Storage card mixes four different kinds of rule in one list.** Days (traffic, alerts, admin), a count of copies (firewall changes), steps of chart history, and a size limit sitting elsewhere. Backups (a count) are on a different page altogether, and backup history and speed tests have no setting.

**D4. Two kinds of record have no setting.** Speed tests (fixed 365 days) and backup history (fixed 500 lines). Speed test length is already on the fix list as a gap.

**D5. Three stores are outside NopeWarden's control and unchecked.** The threat engine's file, the system journal and (less important) blocking list versions. If either of the first two is not rotated by the operating system, it can fill the disk without any NopeWarden setting to help. This needs a look on a real box, not in the demo.

**D6. The keep setting is far from where the data is seen.** For almost everything the setting is in Settings, Alerts & logs, while the data is in Logs, Internet, Threats, Overview and Firewall. That is the "setting lives with the area that uses it" problem, but there is no single owning area: the same numbers feed five pages.

**D7. Outputs and storage share a card group.** Syslog, NetFlow and SNMP send data out; they keep nothing. They sit beside Storage only because both were once called "logs".

## 3. Options for one home (not agreed)
- **A. Storage as a tab of Logs.** Fits the logs, but not chart history (Overview, DNS, DHCP), backups or firewall copies, and Logs would then hold settings for pages that are not Logs.
- **B. Storage as its own Settings tab ("Storage").** Everything that is kept, in one place: What is kept, how long, how big, where you see it, with groups: History you look at (connections, lookups, activity, admin, traffic), Charts, Safety copies (backups, firewall copies, backup history), Records (speed tests, outages, threat alerts). The size limit moves in beside the days. Logs, What gets recorded keeps only the on and off switches (allowed, blocked, name the apps) and a link; Diagnostics, What is using the disk links to it. Settings, Alerts & logs then holds Alerts and, folded, Send data to other tools (syslog, NetFlow, SNMP). Backups keeps its card, with a link and the same "keep this many" shown in the Storage table.
- **C. Keep Storage where it is and tidy it.** Move the size limit in, add the 100-day and size-limit notes, add Speed test length. No new tab; Settings, Alerts & logs stays long (2633 px).

My recommendation: B. Reasons: what is kept is wider than logs; a tab is easier to find than a card on a long page; and it removes D1 (one place for days and size), D6 (one table says where each thing is seen) and D7. Cost: Settings gets another tab (so with the Managing this device tab, eight), which is why the audit's order proposes doing the tab count together.

## 4. Things to do whichever option is chosen
- Say on the days field that connection totals are kept at least 100 days and that the size limit can remove things sooner.
- Add a speed-test length setting (default 365 days).
- Look at the threat engine's file and the system journal on the real box (ACCEPTANCE row to add) and, if either grows unbounded, add a cap or rotation setting.
- Show backup history length (500) in the table, even if fixed.

## 5. Decisions for Marvin
- A, B or C.
- Whether the speed-test length setting is wanted (default 365 days).
- Whether to check the engine's file and the journal on the box before choosing (recommended).
