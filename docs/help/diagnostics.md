# Diagnostics

## What it is for
Shows how each part of Gatehouse is doing, lets you read its log, test the connection, and, if you switch it on, open a command line on the firewall. It is for the day something is not working and there is no screen or keyboard on the machine.

## What is on the page
Tabs along the top:
- **Services**: each part of the system with a state: Running, Starting, Failing or Off. A failing one says why in plain words, such as the last thing it complained about. **Show log** opens its log and **Restart** restarts it. **Save a support bundle** saves a text file with the machine, the network, every service's state and recent log lines. Anything that looks like a password or key is hidden in it. Send it when asking for help.
- **Network tools**: the connection tests, ping, traceroute and name lookups.
- **Terminal**: a command line on the firewall. See below.

The **Log** card under Services has **Show the log of** (one service, or **Warnings from everything**), **How many lines**, **Only lines containing** and **Refresh**.

## Common tasks
1. Find out why a service is not working: **Services**, read the reason, press **Show log**.
2. Restart a service that is stuck: **Restart**.
3. Get help: **Save a support bundle**, and send the file.
4. Run a command by hand: **Terminal**, **Switch the terminal on**, enter your password or code, then **Open a terminal**.

## The terminal
A terminal is a full command line with the highest rights on the machine. Anyone who can sign in as an administrator and pass the check can do anything on it, so Gatehouse treats it carefully:
- It is off until you switch it on, and only administrators can use it.
- Switching it on asks for your password, or your authenticator code if you use one. Opening it asks again only if the last check was more than 15 minutes ago.
- It closes by itself when nobody has typed for 10 minutes, and after two hours at most. Two can be open at once.
- The Logs record who opened it, when, from where, and when it closed. What is typed is not recorded.
- It cannot hold up your traffic by itself. If you break something from it, that is on the command you typed.

## If something goes wrong
If a service shows **Failing**, read its reason and its log; the reason is the last line it printed that looked like an error. If the page itself shows nothing, you may not be signed in as an administrator. If the terminal will not open, check that it is switched on and that two terminals are not already open. If the terminal stops responding, **Close the terminal** and open a new one.
