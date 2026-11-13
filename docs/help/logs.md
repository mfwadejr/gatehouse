# Logs

## What it is for
What happened on your network and why: connections the firewall allowed or blocked, names your devices looked up, and changes.

![The logs page, shown with example data](img/logs.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
- Tabs: **Events**, **Summary** (with the full map), **Data use** and, for administrators, **Admin activity**.
- The search words are highlighted in each row, and the search is part of the address (for example #/logs/log?q=printer), so a search can be bookmarked or sent to someone.
- **Admin activity** answers who did what to NopeWarden: sign-ins and failed attempts, and the changes people made, each with who, what, when and from which address. Search with several words (every word must match a person, an action or an address) and choose a time range; the count says how many match and how far back it searched.
- Three places hold three kinds of log. **Logs** is what your network did. **Diagnostics** is how NopeWarden itself is running (its system log). **Admin activity** is who changed what.
- The search box and filters: **Allowed and blocked**, a time range (1 h, 24 h, 7 d, 30 d, or custom) and **Everything**, **Connections**, **Names (DNS)** or **Events**. Live updates can be turned off.
- Every row is at most two lines: the app or website on top, the port and service below. **Out through** shows which connection or VPN it left by. Identical neighbouring entries become one row with a count.
- Click a row (or press Enter) for the rest: why, the rule, the host and address, the country, the networks, the times, and **Block this** or **Allow this**.
- **Columns & rows** chooses columns and how many rows load at a time. **Download as spreadsheet (CSV)** exports what you see: the same search, filters and kind, lookups and connections together, one row for each entry (a run of repeats on screen is one row each in the file). The first columns are the same for every row; the rest are the details below, left empty where they do not apply, plus the rule's technical tag. The rule's match count is the total at the moment you download.
- Open a row to see more. A DNS lookup shows **Response** (the code, with what it means), **Answer time** and **Answered by** (an upstream DNS server, or NopeWarden itself for a name from memory, one of your own names or a blocked name). A connection shows **Outcome** (Completed, Reset, Timed out, Still open or Blocked), **Sent** and **Received** (bytes each way), **Lasted** and the **Rule** that decided it with how many connections that rule has matched so far, counting live while Live is on. A blocked connection shows the outcome and the rule only.
- Outcome, bytes and time are read from the router's list of live connections every few seconds, so a very short connection shows "under 10 seconds", they cover IPv4 only, and connections logged before 0.9.4.26 show "not known yet".
- **Recording & storage** chooses what is recorded and how long it is kept.

## Common tasks
1. Find out why something was blocked: search for the device or site, click the row, read **Why**.
2. Block a destination: click its row, then **Block this**.
3. See which devices use a VPN: look at **Out through**.

Each row shows the internet connection or VPN it left through. For a DNS lookup that is the connection NopeWarden itself used to ask the DNS server; a lookup answered from memory shows none.

4. Protection features that failed leave a line here: a block that went wrong (once an hour), threat watching that stopped, or threat watching that NopeWarden switched off for using too much of the machine. Your traffic is not held up in any of these cases.
5. Opening and closing the terminal, switching it on or off, restarting a service and saving a support bundle are recorded here too, under Diagnostics.
6. NopeWarden's own health checks are hidden. To see them, turn on **Show NopeWarden's own checks**.

## Example
Search for a game console, set the range to 1 h, and click a blocked row. **Allow this** makes a rule for exactly that traffic.

## If something goes wrong
If nothing is recorded, check **Recording & storage**; allowed and blocked connections can be switched off separately. Only the first packet of each connection is recorded, and a flood is capped.
