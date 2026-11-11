# Internet

## What it is for
Where your internet comes from: the cable from your modem, how your provider connects you, and what to do when it stops.

![The internet page, shown with example data](img/internet.png)

The picture is an example from a new, empty box; yours will show your own devices and numbers.

## What is on the page
Tabs along the top: **Connections**, **Speed**, **Troubleshoot** and **Usage**. Warnings stay above the tabs, and the tab you pick is remembered. **Speed** appears once a connection is switched on; the last two are for administrators.

- **Connections**: **Internet connections**: one row per connection with its state in words (Working, No link, No internet), its port and its delay. **Add connection** adds a second one.
- **Edit** a connection to change its **Name**, the port the cable is plugged into, and **How does your provider connect you?** (**Automatic**, **Username and password**, or **Fixed address**). **Who is your provider?** fills in the usual settings for known providers. Advanced options include **VLAN number**, **IPv6**, **Check by**, **Order**, **Share of traffic** and **Packet size (MTU)**.
- **Speed**: **Speed test history**, **Test servers**, and **Keep delay low when the line is busy**, which stops one big download from slowing everything else. For each connection it shows three speeds: what the plan promises, the measured typical speed from the last 7 days of full tests on a quiet line, and the speed the queue is held to. **Follow my measurements** (the default once it is on) moves the held speed a little each day toward the measured one; **Use fixed numbers** holds it to the speeds you type. **Measure the line automatically** runs a test about every 4 hours, only when the line has been quiet for a minute. It says Learning until there are about 8 good tests over at least 2 days. **Check the difference** measures the delay under load without and with the queue and says in words what changed.
- **Troubleshoot** holds **Test my internet**, **Tools**, and **Why can't it reach that?**. **Usage**: **Provider report card** and **Data use and limit** keep a month of outages and speed to show your provider, and an optional monthly limit.

## Common tasks
1. Add a second connection: **Add connection**, choose the port (NopeWarden notices a cable as you plug it in), choose how your provider connects you, **Save**, then **Review and apply**.
2. Change provider settings: **Edit**, change the type, **Save**, **Review and apply**.
3. Use two connections: give each an **Order** (1 is used first) or a **Share of traffic**.

## Example
Your cable modem needs nothing: choose **Automatic**. A fibre provider that needs a user name uses **Username and password**.

## If something goes wrong
**No link** means nothing answers on the cable: check the cable and the modem's power. **No internet** means the next device answers but nothing beyond it does: check your provider or restart the modem. A second connection takes over on its own.
