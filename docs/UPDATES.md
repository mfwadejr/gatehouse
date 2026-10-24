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

Settings, Updates. It checks shortly after start and then twice a day, or press Check now. Press
Update now to install. The console restarts for about a minute, then reports the result.

If the new version does not answer within about two minutes, the old program and service files are
put back automatically and the Updates card says so. Your settings are never touched; a copy of the
settings from before the update is kept in `/var/lib/gatehouse/update/`.

## What an update can and cannot change

It replaces the Gatehouse program and its service files. It does not change the operating system
or the boot loader; those need a fresh image. The optional "Debian security updates" switch handles
the operating system's own fixes.

## How it is kept safe

* Packages are signed with ed25519; the machine checks the signature against the key built into it
  (twice: once in the console, once more in the root helper that installs it).
* Only the program and `gatehouse*.service` files can be installed.
* Only tagged releases of the configured repository are offered.
