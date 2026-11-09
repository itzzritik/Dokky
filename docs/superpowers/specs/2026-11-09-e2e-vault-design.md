# End-to-end encrypted vault

Status: approved for build (decisions made by Claude while the owner was away; see "Calls made").

## Why

Today the server encrypts drive logins with one server key (`ENCRYPTION_KEY`) and decrypts them to hand them to devices. Anyone with the database plus that key, or a compromised server, reads every user's cloud credentials. The landing page already promises a zero-knowledge design. This makes it true, at the level of Forged (our SSH key manager) and better where Forged has known gaps.

The end goal is an enterprise-grade app that still feels effortless: drives keep mounting at login with no prompt by default, Touch ID or Windows Hello for anything sensitive, and a master password typed rarely.

## Goals

- The server stores only ciphertext for drive logins and service options. It can never decrypt them.
- One master password per account, never sent anywhere. Argon2id stretched.
- A recovery key, so a forgotten password is not data loss (Forged has none).
- Local store encrypted with the account key, unlockable by: automatic device key (default), system auth (Touch ID, Windows Hello, polkit), or master password.
- Manual lock, window idle lock, re-auth before sensitive actions, password re-entry interval.
- Smooth migration for existing local-only users and signed-in accounts, no lost drives, no downtime for mounts.

## Non-goals (this version)

- Key rotation on password change or device removal (data model carries `keyId` so it can come later).
- Browser-side decryption in the web dashboard (the dashboard never needs secrets).
- Hardware-bound keys (Secure Enclave, TPM). System auth is a policy gate, like Forged, documented honestly.

## Anti-goals

- Drives that stop mounting at login after an update because a prompt is waiting, unless the user chose that.
- Any path where plaintext credentials or the master password reach the server or logs.
- A migration that strands a signed-in device or loses a service.

## Cryptography

All new crypto lives in `cli/internal/vault` (Go). Constants are part of the format; changing them needs a new version.

| Thing | Choice |
|---|---|
| AEAD | XChaCha20-Poly1305 (`golang.org/x/crypto/chacha20poly1305.NewX`), 24-byte random nonce |
| Envelope | `0x01 ‖ nonce(24) ‖ ciphertext‖tag`, base64 std encoding on the wire and in JSON |
| Password KDF | Argon2id, salt 32 random bytes, t=3, m=65536 KiB, p=4, 32-byte output, password NFKC normalised |
| KDF caps (from disk or server) | t 1..10, m 19456..262144 KiB, p 1..16, salt exactly 32 bytes, alg must be `argon2id` |
| Password KEK | HKDF-SHA256(ikm=argon2 output, salt=nil, info=`dokky/v1/password-kek`) |
| Account key (AK) | 32 random bytes. Encrypts everything. |
| Recovery key | 20 random bytes, text format below |
| Recovery KEK | HKDF-SHA256(ikm=recovery bytes, salt=kdf salt, info=`dokky/v1/recovery-kek`) |
| Device key | 32 random bytes in the OS keyring (service `dokky`, user `device-key`), file fallback on headless Linux exactly like today's `store-key` |
| Device KEK | HKDF-SHA256(ikm=device key, salt=nil, info=`dokky/v1/device-kek`) |
| keyId | first 16 hex chars of SHA-256(`dokky/v1/key-id` ‖ AK) |

Recovery key text: 20 bytes → 32 Crockford base32 chars (alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ`), plus 2 checksum chars (first byte of SHA-256 of the 20 bytes, base32), shown as `DK1-` then 34 chars in groups of 4 or less separated by `-`. Parsing ignores case, spaces and dashes, maps `O→0`, `I/L→1`, and rejects a bad checksum with a clear error.

AAD strings (bind every ciphertext to its role, so a server cannot swap blobs):

- wrap with password: `dokky/v1/wrap|password|<keyId>`
- wrap with recovery key: `dokky/v1/wrap|recovery|<keyId>`
- wrap with device key: `dokky/v1/wrap|device|<keyId>`
- service config: `dokky/v1/service|<serviceId>|config`
- service OAuth token: `dokky/v1/service|<serviceId>|token`
- local store: `dokky/v1/store`

Zero every key and password byte slice after use where Go allows it. Passwords arrive as strings over IPC; convert to `[]byte` once and zero that.

Password rule (`vault.CheckPassword`): at least 8 characters, nothing else. The TUI shows a small count inside the field until it is long enough, then a tick.

## Files on a device

State dir as today (`platform.StateDir()`).

### `vault.json` (0600, durable atomic write)

```json
{
  "version": 1,
  "keyId": "3f2a9c...",
  "kdf": {"alg": "argon2id", "salt": "<b64>", "t": 3, "m": 65536, "p": 4},
  "wrappedKey": "<envelope b64>",
  "recoveryWrappedKey": "<envelope b64>",
  "deviceWrappedKey": "<envelope b64>",
  "unlock": "auto",
  "passwordDays": 14,
  "idleLockMinutes": 10,
  "passwordAt": "2026-11-09T02:00:00Z",
  "createdAt": "2026-11-09T02:00:00Z",
  "account": false
}
```

- `unlock`: `auto` (device key, no prompt, default), `system` (Touch ID / Windows Hello / polkit, then device key), `password`.
- `passwordDays`: 0 never, else 7, 14 or 30. After this many days since `passwordAt`, system auth stops being accepted for unlock and sensitive actions; the password is needed once, which resets `passwordAt`. Does not affect `auto` startup unlock.
- `idleLockMinutes`: 0 never, else 5, 10, 30, 60. TUI window lock only.
- `account`: true once these wraps match the server vault of the signed-in account.

### `store.enc` v2

`"DKY2" ‖ envelope(AK, json(Data), aad=dokky/v1/store)`. A file without the magic is v1 (AES-GCM with keyring `store-key`), read only for migration.

## Daemon states

`api.Vault.State` (new):

- `setup`: no `vault.json`. Store is v1 (legacy keyring key) or fresh. Daemon runs normally, drives mount. TUI requires creating a master password before the dashboard (a fresh install picks local or sign in first, then creates it).
- `locked`: `vault.json` exists and the AK is not in memory. Daemon serves a minimal IPC surface; drives are not mounted. On a GUI session with `unlock=system` the daemon proactively shows one system auth prompt about 5 seconds after start ("Unlock dokky to mount your drives"); dismissed means stay locked and send one OS notification.
- `unlocked`: normal operation.

Startup: read `vault.json`. If absent → `setup` using v1 store. If `unlock=auto` and the device key opens `deviceWrappedKey` → `unlocked`. Otherwise → `locked`. A restart out of the unlocked phase (update, `daemon.restart`, reinstall; never a lock, a stop or a pending removal) of a `system` or `password` device hands the AK to the next start through `handoff.json` plus a one-use `restart-key` in the keyring; startup deletes both first and accepts them only within 2 minutes for the same keyId, accepting that the two together open the vault for those seconds.

`Run` becomes two phases on the same socket path: a locked phase with a small handler map, then the existing unlocked run. Manual lock (`vault.lock`) ends the unlocked phase (drives detach as on shutdown, services are deleted from the rclone memory config, keys zeroed) and returns to the locked phase without auto unlock. The rclone engine starts once per process and survives phase changes.

Methods allowed while locked: `state`, `vault.unlock`, `vault.recover`, `daemon.restart`, `update.check`, plus the events stream (state only). Everything else returns coded error `locked`.

## IPC methods (new)

Defined in `cli/internal/api/vault.go`, client wrappers in `cli/internal/client/vault.go`.

| Method | Params | Reply | Notes |
|---|---|---|---|
| `vault.setup` | `{password}` | `{recoveryKey}` | only in `setup`. Creates AK (or adopts the server AK flow, see sign in), writes vault.json, rewrites store v2, deletes keyring `store-key`. Signed-in: also creates the server vault and seals and uploads every service. |
| `vault.unlock` | `{method: "password"\|"system", password?}` | `{}` | `system` refused when `passwordDays` elapsed or over SSH or no GUI; coded errors `password_required`, `wrong_password`, `system_unavailable`, `cancelled`. 5 wrong passwords → 30 s cooldown, doubling to 5 min (`too_many_attempts`, message says how long). |
| `vault.recover` | `{recoveryKey, newPassword}` | `{}` | works locked or unlocked; rewraps with new password, keeps the same recovery key; signed-in: updates the server vault. Unlocks. |
| `vault.lock` | `{}` | `{}` | unmounts and locks |
| `vault.authorize` | `{method, password?}` | `{grant}` | one-shot grant valid 2 minutes for a sensitive method |
| `vault.settings` | `{grant, unlock?, passwordDays?, idleLockMinutes?}` | `{}` | switching to `system` checks availability first |
| `vault.changePassword` | `{current, next}` | `{}` | rewrap only (no rotation), server vault updated when signed in |
| `vault.newRecoveryKey` | `{password}` | `{recoveryKey}` | replaces the recovery wrap locally and on the server |
| `vault.join` | `{password}` | `{}` | sign in to an account whose server vault already exists: unwrap the server AK, re-encrypt the local store with it, replace local wraps with the server's plus a new device wrap, then continue the pending sign in |
| `vault.joinRecover` | `{recoveryKey, newPassword}` | `{}` | as `vault.join` using the recovery key, and rewraps the account with the new password |

`api.State` gains:

```go
Vault Vault `json:"vault"`

type Vault struct {
    State          string `json:"state"`          // setup | locked | unlocked
    Need           string `json:"need,omitempty"` // "" | create | join: what sign in waits for
    Unlock         string `json:"unlock"`         // auto | system | password
    PasswordDays   int    `json:"passwordDays"`
    IdleLockMinutes int   `json:"idleLockMinutes"`
    PasswordDue    bool   `json:"passwordDue"`    // system auth no longer accepted until the password is typed
    System         string `json:"system"`         // "" when unavailable, else the name to show: "Touch ID", "Windows Hello", "your system password"
    Synced         bool   `json:"synced"`         // the account's server vault matches this device
    Legacy         int    `json:"legacy"`         // services on the server still waiting to be end-to-end encrypted
    CooldownUntil  time.Time `json:"cooldownUntil,omitempty"`
}
```

## System auth (`cli/internal/sysauth`)

`Available() (name string, ok bool)` and `Prompt(ctx, reason string) error` (nil = verified, `ErrCancelled`, `ErrUnavailable`). Runs inside the daemon so the daemon, not the TUI, decides. Builds stay `CGO_ENABLED=0` (release runs on Linux and signs macOS with rcodesign), so no Swift helper:

- macOS: `osascript -l JavaScript` running JXA that uses the ObjC bridge for `LAContext` `evaluatePolicy(LAPolicyDeviceOwnerAuthentication)` (Touch ID with password fallback), pumping the run loop until the reply block fires, printing `ok`/`cancel`/`error:<msg>`. Name: "Touch ID" when `canEvaluatePolicy(LAPolicyDeviceOwnerAuthenticationWithBiometrics)` else "your Mac password".
- Windows: PowerShell 5.1 by absolute path calling WinRT `UserConsentVerifier` (port Forged's `cli/cmd/forged-auth/platform_windows.go` script and window-foregrounding). Name "Windows Hello".
- Linux: `pkexec --disable-internal-agent /bin/true` when `DISPLAY` or `WAYLAND_DISPLAY` is set (Forged's approach). Name "your system password".
- Unavailable over SSH (`SSH_CONNECTION`/`SSH_TTY` in the *TUI's* environment is passed as a `remote` flag on the request) and in a session with no GUI.

## Server (web/, Next.js + MongoDB)

New collection `vaults`, `_id` = userId string:

```ts
{ _id, version, keyId, kdf: {alg, salt, t, m, p}, wrappedKey: Box, recoveryWrappedKey: Box,
  createdAt, updatedAt, passwordChangedAt, createdBy /* deviceId */ }
```

`wrappedKey` and `recoveryWrappedKey` are additionally encrypted at rest with `ENCRYPTION_KEY` (existing `secrets.ts`, AAD `userId|vault|wrappedKey`), so a database-only leak cannot start an offline password guess. This outer layer is removed before serving; the inner envelope is opaque to the server.

Device API (Bearer `dky_`). Devices that understand vaults send `X-Dokky-Vault: 1`.

- `GET /api/v1/vault` → `200 {vault: {version, keyId, kdf, wrappedKey, recoveryWrappedKey, createdAt, passwordChangedAt}}` or `404 {error: {code: "no_vault"}}`. Rate limit 20/min per device.
- `POST /api/v1/vault` `{keyId, kdf, wrappedKey, recoveryWrappedKey}` → `201 {vault}`; `409 vault_exists` with `current`.
- `PUT /api/v1/vault` `{baseVersion, keyId, kdf, wrappedKey, recoveryWrappedKey}` → `200 {vault}`; keyId must equal the stored one (`409 key_mismatch`); `409 conflict` with `current` on version mismatch. Audit `vault.rekey`.
- Validation: kdf within the caps above, base64 envelopes 41..512 bytes decoded, keyId `/^[0-9a-f]{16}$/`.

Services, new wire shape (both directions):

```json
{"id","name","type","version","updatedAt","keyId","sealed","sealedToken","tokenExpiry"}
```

- `sealed`: envelope of JSON `{"options":{...},"secrets":{... without "token"}}`.
- `sealedToken`: envelope of the token string, or null. `tokenExpiry`: ISO time or null.
- `PUT /services/{id}` with `X-Dokky-Vault: 1` takes `{name, type, keyId, sealed, sealedToken, tokenExpiry, baseVersion}`. Requires the account vault and matching keyId (`409 key_mismatch`, `409 no_vault`). Unsets legacy `options` and `secrets`.
- `POST /services/{id}/token` with the header takes `{keyId, sealedToken, expiry}`; newest expiry wins exactly as today.
- `GET /sync` with the header returns new-shape services. A service still holding legacy fields additionally returns `legacy: {options, secrets}` (decrypted as today) so the first vault device can seal it. Once every service is sealed the server never decrypts a secret again.
- Without the header (old dokky): unchanged behaviour while the account has no vault. Once it has one, `GET /sync`, service PUT and token POST return `426 {error: {code: "update_required", message: "Update dokky to keep syncing: npm i -g dokky"}}`.
- Dashboard: account page gains a Security panel (end-to-end encrypted since date, key id fingerprint, number of services still waiting to be encrypted, plain explanation). Remove any copy claiming things the code does not do; landing page and docs describe the real design (XChaCha20-Poly1305, Argon2id, recovery key, Touch ID/Windows Hello).

## Sync on the device

Seal and open at the wire boundary in `cli/internal/sync`; the daemon keeps plaintext `store.Service` in memory as today. The client gets a `Sealer` (AK + keyId). Pull opens `sealed`/`sealedToken` (or takes `legacy`, and marks the service for re-upload). Put and token write-back seal. A `409 key_mismatch` or a vault with a different keyId pauses sync and sets `Vault.Need = "join"`.

Sync runs only when the vault is unlocked and `account` is true. A signed-in device still in `setup` keeps its drives but does not sync until the password exists.

## Sign in

Fresh install gate: "Sign in with browser" or "Use on this computer only".

- Local: create master password → recovery key screen → dashboard.
- Sign in: device code approval (unchanged) → daemon checks `GET /vault`:
  - none: create master password (it becomes the account password) → server vault created from local wraps → services sealed and uploaded → recovery key screen.
  - exists, same keyId: continue.
  - exists, different keyId or local `setup`: "Enter the master password for <email>" (or use the recovery key) → `vault.join` → continue upload and clash resolution as today.

`Account.Pending` stays true until the vault step finishes; `api.Vault.Need` tells the TUI which screen.

## Migration

- Local-only v1 store: daemon runs as before in `setup`. On TUI open a required screen "Protect dokky with a master password". Setup rewrites the store as v2 and removes the old keyring key.
- Signed-in v1 store: same screen, but if the server already has a vault (another device went first) the screen asks for that password (`join`). After setup or join, the device seals every service, including `legacy` ones from the server, and uploads them.
- Old dokky devices keep syncing in legacy mode until the account has a vault, then get `update_required` (they auto-update hourly).

## TUI

New screens under `cli/internal/tui/screen/vault/`: create password (password and confirm, no labels, a count then a tick inside the field, a tick or cross on confirm), recovery key (shown once, a Download button saves it to a text file in Downloads, Continue without a download asks once more), unlock (big system auth button when available plus password field, "forgot password" → recovery flow, cooldown message), join (account email shown), recover (recovery key + new password). Settings gains a Security section: unlock at startup (Automatically, Touch ID/Windows Hello/system, Master password), ask for password every (7, 14, 30 days, never), lock dokky after (5, 10, 30, 60 min, never), change master password, new recovery key, lock dokky now. Changing security settings asks for system auth or password first (`vault.authorize`). Window idle lock covers the whole TUI with the unlock screen (view lock; the daemon stays unlocked). Locking dokky, by `ctrl+l`, "Lock dokky now" or the idle timer, is that same lock screen: drives and syncing keep running. Every vault screen is keyboard first: tab and the arrows walk fields, links, the system auth tile and buttons, and enter acts on what has focus. Over SSH the TUI never offers system auth.

## Calls made

- Mandatory master password for every user, created after choosing local or sign in, so nobody types two passwords.
- XChaCha20-Poly1305 over AES-GCM: random 24-byte nonces have no practical collision risk, matches what the landing page promises.
- Recovery key added (Forged has none).
- Server wraps encrypted at rest with the server key, so a database leak alone cannot be brute forced.
- No Swift helper: osascript JXA on macOS keeps builds cgo-free and the release pipeline unchanged.
- Change password rewraps without rotating the key, so other devices keep working; rotation is a later feature.
- Locking dokky from the TUI is view-only, like a lock screen, so drives and background uploads never stop. The daemon's `vault.lock` (unmount and drop keys) stays for the API but the TUI no longer offers it.
