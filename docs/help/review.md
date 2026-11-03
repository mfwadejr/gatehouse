# Review changes

## What it is for
Saving a change to the firewall, the network or a policy puts it live straight away. Sometimes you would rather collect several changes first and put them live together. **Save for later** does that, and this page is where those changes wait.

## How saving works
- **Save and apply** (the main button) saves the change and puts it live after a moment. Gatehouse then checks that you can still reach it and keeps the change. If a change cuts you off, nothing more is needed: it undoes itself within 60 seconds.
- **Save for later** saves the change but leaves it waiting. A small badge showing how many changes are waiting, with a link to **Review changes**, appears at the top of every page.
- Quick actions such as **Pause…**, **Turn off** and **Remove** are applied the same way as **Save and apply**.
- If Gatehouse finds something worth a second look (a warning or a problem), or other changes are already waiting, nothing goes live on its own. The change waits here, with the reason shown.

## What is on the page
- **Changes waiting**: every saved-but-not-live change, in plain words and in the order you made them.
- **Worth knowing before you commit** and **problems**: what Gatehouse found when it checked. A problem has to be fixed before you can commit.
- **Verify and commit** puts everything waiting live in one step, network first and then the firewall, each with the 60-second safety.
- **Discard all changes** puts everything back exactly as it was running. It is offered only when Gatehouse still has the copy it made before the first waiting change.
- **Code from your authenticator app** shows when you asked for a code before changing the firewall. Gatehouse remembers a good code for 15 minutes.

## Common tasks
1. Make three changes and put them live together: choose **Save for later** on each, open **Review**, check the list, **Verify and commit**.
2. Undo a change that just went live: press **Undo now** on the countdown at the bottom of the window.
3. Change your mind about waiting changes: **Discard all changes**.

## If something goes wrong
If you are told other changes are waiting, open **Review** and commit or discard them first. If a commit fails, the message says which part (network or firewall) and what was wrong; the changes stay waiting.
