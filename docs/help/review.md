# Review changes

## What it is for
Saving a change to the firewall, the network or a policy puts it live straight away. Sometimes you would rather collect several changes first and put them live together. **Save for later** does that, and this page is where those changes wait.

## After an update
Sometimes an update changes how NopeWarden writes the firewall rules without changing what they allow. Review then says **The firewall rules were refreshed** instead of listing changes, and NopeWarden puts them live by itself a short while after the update, with the same checks as any change. You never have to do anything. A change you made yourself is never put live this way.

## How saving works
- **Save and apply** (the main button) saves the change and puts it live after a moment. NopeWarden then checks that you can still reach it and keeps the change. If a change cuts you off, nothing more is needed: it undoes itself within 60 seconds.
- **Save for later** saves the change but leaves it waiting. It is offered on the forms for rules, routes, open ports, services, device groups, internet connections, connection groups, networks, network types, DHCP networks and reservations, VPN settings, letting networks see each other, and who may manage NopeWarden. A small badge showing how many changes are waiting, with a link to **Review changes**, appears at the top of every page.
- Quick actions such as **Pause…**, **Turn off** and **Remove** are applied the same way as **Save and apply**.
- If NopeWarden finds something worth a second look (a warning or a problem), or other changes are already waiting, nothing goes live on its own. The change waits here, with the reason shown.

## What is on the page
- **Changes waiting**: every saved-but-not-live change, in plain words and in the order you made them.
- **Worth knowing before you commit** and **problems**: what NopeWarden found when it checked. A problem has to be fixed before you can commit.
- **Verify and commit** puts everything waiting live in one step, network first and then the firewall, each with the 60-second safety.
- While a change goes live, a list of steps appears on the card at the bottom of the window and here on **Review**, each with a line saying what is going on: checking the change is safe, saving a way back, loading the new rules, filling in the device and country lists, starting the 60-second safety timer, checking you can still reach NopeWarden, and keeping the change or undoing it. A step shows a spinner while it runs and a green tick when it is done. When everything worked the list says **Applied successfully** and stays until you press **Close**. If a step fails it turns red, says why, and the list stays until you press **Close**; the previous settings are still in use. Network changes show their own steps. **Keep this change**, **Undo now** and the countdown stay on the card while the change is on trial.
- **Discard** on a line goes back to just before that change. Later changes can depend on earlier ones, so it also drops every change saved after it, and asks first. On the last line it drops only that change.
- **Discard all changes** puts everything back exactly as it was running. It is offered only when NopeWarden still has the copy it made before the first waiting change.
- **Code from your authenticator app** shows when you asked for a code before changing the firewall. NopeWarden remembers a good code for 15 minutes.

## Common tasks
1. Make three changes and put them live together: choose **Save for later** on each, open **Review**, check the list, **Verify and commit**.
2. Undo a change that just went live: press **Undo now** on the countdown at the bottom of the window.
3. Change your mind about one change: press **Discard** on its line. About all of them: **Discard all changes**.

## If something goes wrong
If you are told other changes are waiting, open **Review** and commit or discard them first. If a commit fails, the message says which part (network or firewall) and what was wrong; the changes stay waiting.

## The DNS area
Review now has three areas: **Networks**, **DNS** and **Firewall**. DNS changes wait here like the others. Applying sends them live; discarding restores DNS exactly as it was running.

## What waits here
Firewall, route, VPN, DHCP, network and DNS changes wait here, and so do Forget a device, adding a network, country rules and changes to blocks. A device you chose to forget shows Forgetting… on the Devices page until the change is applied; discarding the change keeps the device.
