# dokky

dokky mounts your cloud storage as normal folders on your computer. Open them in Finder, Explorer or any app, and copy files in and out like on any drive. Uploads happen in the background. It works with Google Drive, SMB, WebDAV, SFTP, OneDrive, Dropbox, Mega and many more. You can sync your settings across devices with a free account, or use it with no account at all.

## Install

```
npm i -g dokky
```

The program itself is saved at `~/.local/bin/dokky` (on Windows, `%USERPROFILE%\.local\bin\dokky.exe`). If that folder is not on your PATH, dokky adds it. After that dokky keeps working even if you change or remove Node.

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

Privacy: the server stores your credentials encrypted, but it can decrypt them to serve your devices.

## Updates

dokky updates itself. To update right now, run `dokky update`. To go back to a specific version, run `dokky update --to <version>`. That also turns auto update off until you run a plain `dokky update`. Setting `DOKKY_NO_UPDATE=1` also turns auto update off, but only when it is set in the environment of the dokky background service, not in your shell.

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
