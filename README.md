# dokky

dokky mounts your cloud storage as native drives on your computer. Open them in Finder, Explorer or any app, and copy files in and out like on any other drive. Uploads happen in the background. It works with Google Drive, SMB, WebDAV, SFTP, OneDrive, Dropbox, Mega and many more. You can sync your settings across devices with a free account, or use it with no account at all.

## Install

```
npm i -g dokky
```

The first time you run `dokky`, the program is saved at `~/.local/bin/dokky` (on Windows, `%USERPROFILE%\.local\bin\dokky.exe`). If that folder is not on your PATH, dokky adds it. After that dokky keeps working even if you change or remove Node.

## First mount

1. Run `dokky` to open the app.
2. Add a service: pick a type (the common ones are at the top, press `/` to search all of them) and fill in the form.
3. Add a mount: pick the service, choose a folder, give it a name.
4. Mount it. Your files show up in `~/dokky/<name>`.

You can also run `dokky mount <name>`, `dokky unmount <name>` and `dokky status`.

## macOS, Linux and Windows

- macOS: needs macOS 13 or newer. Nothing extra to install.
- Linux: needs FUSE 3. Install it with one of:
  - `sudo apt install fuse3`
  - `sudo dnf install fuse3`
  - `sudo pacman -S fuse3`
  - `sudo zypper install fuse3`
- Windows: needs WinFsp. Install it with `winget install -e --id WinFsp.WinFsp`.

## Sync

Sync is optional. Run `dokky login` to sign in with GitHub.

- Syncs: your services, your mounts and their saved logins, across all your devices.
- Does not sync: your files and the cache.
- `dokky logout` removes your account's data from this device and clears the local cache. It waits for queued uploads first. If a mount that is not mounted still has files waiting to upload, it stops and asks you to mount it so they can finish. `--force` skips all of this, and any uploads that have not been sent are lost.
- If you revoke a device from another device, it unmounts everything and wipes its dokky data the next time it talks to the server.
- Signing in on another device asks for the same master password.

Privacy: saved logins are end-to-end encrypted. dokky seals them on your device with XChaCha20-Poly1305, using a key that only your master password (stretched with Argon2id) or your recovery key can open. The server stores only ciphertext and can't decrypt it. It does see the names of your services and mounts, their folders and cache settings, so the dashboard can show them. Nobody can reset your master password, so keep the recovery key dokky shows you somewhere safe. By default dokky unlocks by itself at startup so drives mount at login. You can ask for Touch ID, Windows Hello, your system password or the master password instead.

## Updates

dokky updates itself in the background with npm: every hour it checks for a new version, runs `npm i -g dokky@latest` with the npm that installed it, downloads the new program, and switches once nothing is uploading. Turn it off in Settings, Automatic updates. You can also update with `npm i -g dokky` yourself; the next time you run `dokky` it switches to the new version. To update right now, run `dokky update`. To go back to a specific version, run `dokky update --to <version>`. That also turns auto update off until you run a plain `dokky update`. Setting `DOKKY_NO_UPDATE=1` also turns auto update off, but only when it is set in the environment of the dokky background service, not in your shell.

## Uninstall

```
npm rm -g dokky
```

About 10 minutes later dokky cleans up after itself. It stops transfers, unmounts everything, removes the mount folders and `~/dokky` if empty, the background service, the program, the PATH entry it added, the cache and the logs. Uploads that have not been sent by then are dropped. If you were logged in, it also removes this device from your account and deletes your settings from this device. If you were local-only, it keeps your encrypted services and mounts, so a reinstall brings them all back.

## Doctor

If something does not work, run:

```
dokky doctor
```

It checks FUSE or WinFsp, the background service, your keychain, the network, free disk space and shows the exact fix for anything wrong, plus where the log file is.
