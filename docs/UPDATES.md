# Updating an installed Gatehouse

Installed machines update themselves from a signed package attached to a GitHub release. The
installer image is only for a fresh install.

## One-time setup (about five minutes)

1. Make a signing key. It stays on your Mac and in GitHub; never share the private half.

   ```
   ssh-keygen -t ed25519 -N "" -C gatehouse-update -f ~/gatehouse-update-key
   ```

2. Give GitHub the private key as a secret named `UPDATE_SIGNING_KEY`:

   ```
   gh secret set UPDATE_SIGNING_KEY --repo mfwadejr/gatehouse < ~/gatehouse-update-key
   ```

   (Or in the browser: repository, Settings, Secrets and variables, Actions, New repository
   secret, paste the whole contents of `gatehouse-update-key`.)

3. Put the public half into the program, so every machine knows which updates to trust:

   ```
   cp ~/gatehouse-update-key.pub ~/Developer/gatehouse/internal/update/update-key.pub
   ```

   Commit and push it. Back up `gatehouse-update-key` somewhere safe. If it is lost, machines
   already installed can never be updated again and need a new installer image.

4. Install once from a fresh image built after step 3 (the first image with the key). That
   machine, and every one installed from it, can now update itself.

## Publishing an update

Tag a version higher than what is installed (test builds are `0.1.N`, so start at `0.2.0`):

```
git tag v0.2.0 && git push origin v0.2.0
```

The build makes the image and a signed `gatehouse-update-0.2.0.tar.gz` (+ `.sig`) and attaches both
to the release. Ordinary pushes only publish the `nightly` test image; machines never update to it.

## On the machine

Settings, Updates. One button updates Gatehouse and the operating system underneath it together.
Press Check now, or let it check twice a day. When something is ready the card lists it ("Gatehouse
0.3.0" and "12 system updates"); press Update now. A step list shows where it is: download, check
the signature, save a copy of the settings, install system updates, install Gatehouse, restart,
check that it runs. The console restarts near the end and the page reconnects by itself.

Order matters. The system updates go first. If they fail, Gatehouse has not been touched and the
card says so. If the new Gatehouse does not answer within about two minutes, the old program and
service files are put back automatically (the system updates stay installed and the card says
so). Your settings are never touched; a copy of the settings from before the update is kept in
`/var/lib/gatehouse/update/`.

A kernel or core library update only takes effect after a restart; the card and the Overview page
show "A restart is needed" until the machine has been restarted.

### Off, Notify, Automatic

- **Off:** nothing is checked on its own; use Check now.
- **Notify** (the default): checks twice a day and shows a notice on Overview.
- **Automatic:** also installs by itself, once a day at most, in the hour you choose. Gatehouse
  restarts for about a minute. It does not restart the machine; the "restart needed" notice stays
  until you do.

## What an update contains

- The Gatehouse program and its service files (signed).
- The Debian packages the release needs. They are listed in `iso/update-packages.list`, which
  `update-build` puts into the signed package, so a feature that needs extra software (high
  availability, threat detection, application identification) arrives as an ordinary update.
  Add a package name there only when a release needs it. If Debian's servers cannot be reached,
  a release that needs a package waits; a release that needs none still installs and the card says
  the system updates were skipped.
- The Debian updates for the release the machine is on (security and point-release fixes).

## Staying on Debian 13

The installed image names its apt sources by release name (`trixie`, `trixie-updates`,
`trixie-security`), never `stable`. Before any system step the machine checks that it reports
Debian 13 and that every apt source still names `trixie`; if not, the system step is refused with
that reason. It never moves to a newer Debian by itself. Debian's own unattended upgrades are
switched off, so this is the one update path.

An update never changes the boot loader or the operating system image; those need a fresh
install.
