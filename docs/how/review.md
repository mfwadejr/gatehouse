# Review changes

## What the page does
Review is where a change to the firewall, the networks or a policy is checked, put live and either kept or undone. A change is saved first. It only takes effect when it is applied, either at once by **Save and apply** or later from this page with **Verify and commit**. Whenever a change goes live it goes live on trial: if it is not kept in time, NopeWarden puts the previous settings back by itself.

## Every tab and control
- **Save and apply** and **Save for later** are on the forms. The first applies after a short pause; the second leaves the change waiting here.
- **Verify and commit** applies everything waiting in one step, network settings first and then the firewall.
- **Keep this change** and **Undo now** are on the card at the bottom of the window while a change is on trial. **Why this time?** on that card opens the section on the undo timer below.
- The list of steps (checking the change is safe, saving a way back, loading the rules, filling in the device and country lists, starting the safety timer, checking you can still reach NopeWarden, keeping or undoing) shows what is happening and stays until you press **Close**.

## Timers, thresholds and limits
### The undo timer
A firewall change is put live with a safety window of {{go:internal/app/routes_fw.go#confirmWindow}}. If the change has not been confirmed when the window ends, the previous rules are loaded again. The same window is used for network changes. The confirmation has to arrive on a new connection, not the one that asked for the change, because connections that were already open survive a rule set that blocks new ones, so an old connection proves nothing about whether you can still reach NopeWarden.

The rollback does not depend on the console staying alive. It is carried out by a separate process that a systemd timer starts, so it still happens if the main program hangs or loses its connection.

Needs confirming: the written record gives the value and what it protects against, but not why sixty seconds was chosen over thirty or two minutes. The owner is to say whether that was a decision or a default.

### The history grace period
The change history marks an unconfirmed change as undone only once its window is {{go:internal/app/routes_fw.go#settleGrace}} past, so a change that is still being decided is never marked as undone early.

## How it is measured and reported
Each applied change is recorded in the change history with who made it, a plain summary of what it does, the exact rules, and one of three states: waiting, kept or undone. A change that was undone by the timer is recorded as undone the next time the history is checked, and the Admin activity log says it was not kept in time. The step list on the card and on Review comes from the progress the apply itself reports, so each tick is a step that really finished.

## What it reads, writes and calls
Applying reads the saved settings and builds the firewall rules from them. Before loading anything it checks that the change would not cut off the connection that asked for it, refuses if the country lists are still being downloaded, and saves a restore point. It then hands the rules to the privileged helper with the window above, writes a history entry, and closes the requesting connection so that the confirmation has to come from a fresh one. If two-factor sign-in is on and the setting that asks for a code on everyday changes is switched on, a current code from your authenticator app is required first, and NopeWarden remembers it for 15 minutes. Only one change can be on trial at a time; a second apply is refused until the first is kept or undone.

## Options considered and not taken
- A single **Save and apply** button on network and firewall pages only was dropped because it was inconsistent with the other pages. Every form has both buttons instead.
- Asking "Are you sure?" before every change was replaced by applying on trial, because the undo timer makes a mistake cheap, and a confirmation box does not.
- Applying every save at once with nothing left to review was set aside in favour of two buttons, so a change you want to check before it goes live can wait.

## When something goes wrong
If a step fails, it turns red, says why, and the previous settings stay in use. If the change would cut you off, it is not applied and the message says so. If the window runs out, the previous settings come back and the history shows the change as undone. If the privileged helper is not running, nothing is applied and the console says so.
