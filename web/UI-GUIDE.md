# Keeping the console simple

The rule for every screen: **the common choice is visible, the rare one is one click away.**

1. Show what most people need first. Anything that most people never change goes under a
   `<details className="more"><summary>Advanced</summary>…</details>` or the existing
   "Show advanced options" link, so the page reads as a few plain choices.
2. Never hide something that can lock you out or cost you data. Warnings stay visible.
3. Name things by what they do for the person, not by the protocol ("Block encrypted DNS to other
   servers", not "Drop 853").
4. Every setting that can break connectivity goes through Review and apply, with the 60-second way
   back, and a restore point is saved before it.
5. If a feature needs a fact the person might not know (a provider's VLAN, a port), offer a preset
   and mark it unverified until it has been tested.

Where this is applied so far: internet connection (VLAN, IPv6, checks under Advanced), Devices
(details, rules), Firewall (Protection toggles). New screens follow the rule from the start.
