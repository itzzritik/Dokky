# Dokky works like Forged

The user picked, item by item, which parts of Forged's experience Dokky copies. Item numbers below are from the picker. Anything not listed stays as Dokky has it today. Mouse support stays everywhere.

Standing rule: every piece of UI is a self-contained, data-driven component with one implementation per concept. Dokky and Forged will later share one component library.

## 1. The control session (items 1, 3, 4, 5, 10, 11, 12, 13)

Drives and file use never stop. Changes need Dokky open and unlocked.

- The daemon keeps the vault key in memory as today, so drives mount and keep working whether the TUI is open, closed or locked.
- **Starting a session.** The daemon also keeps one control session, live for 4 hours (`api.SessionFor`). The `session.open` call starts it after proving the user, with system auth (Touch ID, Windows Hello or polkit) or the master password.
  - If a session is already live and `force` is false, it succeeds with no prompt. That is how reopening Dokky within 4 hours goes straight in, as in Forged.
- **Ending a session.** A session ends when:
  - the computer's screen locks or it sleeps;
  - `session.close` is called (the TUI's `l`, the menu's Lock, or the 4-minute idle lock);
  - the master password changes;
  - 4 hours of wall time pass.

  Wall time matters because the monotonic clock stops during sleep.
- **Gating.** Every daemon method that changes anything refuses with code `session_required` unless a session is live.
  - Open without a session: methods the lock screen needs, read-only views, and the launch handshake. That means `state`, `status`, `activity`, `events`, `events.seen`, `sync.kick`, `update.check`, `update.restart`, `oauth.status`, `providers` and `catalog`, plus `session.open`, `vault.unlock`, `vault.recover` and `vault.setup` when there is no vault yet.
  - Anything that returns secrets or changes state needs the session.
  - The daemon's own work (auto mount at start, sync, auto update) never needs one.
- **Command line.** `dokky mount`, `unmount`, `login`, `logout`, `update` and `fix` are removed. `dokky` (the TUI), `status`, `doctor`, `daemon` and `selftest` stay.
- **Startup.** The vault always opens with the device key at daemon start, so drives mount after a reboot with no prompt. The "Unlock at startup" choice is gone.
  - A vault saved as `password` or `system` opens like `auto` once a device key exists. The next typed password recreates a missing device key, which `mendDevice` already does.
  - The locked phase stays only for a computer with no keychain or a lost device key.
- **Password reminder.**
  - Choices are 7, 15 and 30 days, 7 by default. Old values map 14 → 15 and 0 → 30.
  - Each successful system auth slides the window. The password is due once the interval has passed since the later of the last typed password and the last system auth, or 90 days after the last typed password, whichever comes first.
  - System auth stamps are throttled to one save per hour.
- **After a password change**, the session ends. The next unlock needs the new master password once, which also restarts system-auth trust, like Forged's re-enrolment.
- **No cooldown.** The wrong-password cooldown is gone.
- **Idle lock** is fixed at 4 minutes and lives in the TUI. The setting is gone.

## 2. Lock behaviour in the TUI (items 2, 6, 7, 8, 9)

- **Opening Dokky.** If the vault is unlocked and a session is live, Dokky goes straight in. Otherwise it shows the lock screen and starts system auth at once, with `force` set when the view was locked, as Forged's `startStartupUnlock` does.
- **Locking.** `l` locks from every screen and every window, except while a text field has focus (there it types). `ctrl+l` locks from anywhere, text fields included. Lock also sits in the menu.
- **What locking does.** Closes every window and overlay, cancels a sign-in in progress, wipes anything typed, calls `session.close` and shows the lock screen with "Please authenticate to continue using Dokky.", or "Enter your master password to continue using Dokky." when system auth can't be used.
- **Losing the session.** When the polled state shows the session ended (screen lock or sleep), the TUI locks itself the same way.
- **Idle lock.** 4 minutes with no input locks. A gate mid-flow (a sign-in waiting for the browser) is never locked.

## 3. The lock screen and gate (items 14 to 20)

All of this follows Forged's `screen/gate` layout:

- No footer bar.
- Dokky's still wordmark at the top.
- A centred card, `min(52, w-4)` wide.
- React Bits' Topography ported to braille: a contour map of mountains the camera glides over, filling the whole screen behind the lock screen, its cards, the startup screen and Updating, in place of Forged's fire. Lines fade to a whisper wherever text sits, so everything in the middle keeps its contrast.

The cards:

- **Unlock.** Bold "Unlock Dokky", one muted line, a "Master password" label above a full-width field, and a "Forgot password?" link that stays (item 22). The card footer has key hints on the left and one Unlock button on the right.
  - While system auth waits, the card shows the system name ("Touch ID", "Windows Hello" or "System Auth"), a spinner and "Waiting for confirmation". `esc` switches to the password field and `tab` asks for system auth again.
- **Welcome (first run).** Two choices, each with one line under it: "Sign in" (sync drives across devices) and "Use on this device" (create a master password here).
- **Create.** Labels above both fields, plus the warning "If you lose this password, only your recovery key can open your drives."
- **Sign in.** Inside the same card: the large spaced code, "Waiting for approval", the link, `c` to copy it and an Open link button. Signing in from the dashboard uses this same full-screen card.
- **Not changed:** Dokky's connecting and startup screen (item 21), "Forgot password?" (22) and the teal palette (23).

## 4. Chrome (items 24, 25, 26, 34, 37, 40)

- **Icons** on card titles, settings rows and menu items, with plain-text fallbacks where the terminal lacks the glyphs, as Dokky's glyph files do now.
- **Footer.** At most 4 hints on the left, each starting with a capital ("Mount", "Edit"). On the right: `m More`, `u Update` (only when an update is ready, as now) and `q Quit`. No version number.
- **Menu.** Opens with `m`, drawn exactly as Forged's: rounded border with the title in it, and rows of icon, label and key. It ends with Lock and Quit.
  - `?` still opens it.
  - The Drives page's mount toggle moves from `m` to `space`.
- **Windows.** Forged's plain rounded box with no shaded outer margin, buttons right-aligned, and everything behind faded, footer included. Window titles keep Dokky's style.
- **Settings rows.** An icon, a text toggle (`● On` / `○ Off`) and a value with `▾` that opens a small dropdown anchored under its row, in place of a full window.
- **Wording.** Hints start with a capital. Toasts are short past tense ("Drive removed").

## Calls made while building

- **`l` ends the session.** Locking with `l`, the menu or the idle timer closes the control session too, not only the view. Nothing in Dokky's background needs it, unlike Forged's SSH agent, so a locked Dokky can't be controlled until someone authenticates again. Quitting Dokky keeps the session, so reopening within 4 hours goes straight in, as in Forged.
- **Sign-in on the gate.** The sign-in card and its master password step are full-screen cards on the gate, opened as windows that cover everything (`core.Full`), whether they start from the welcome card or from Settings.
- **Forgot password and recovery key.** "Forgot password?" is reached with down from the password field, because tab means system auth as in Forged. The recovery and join cards also put labels above their fields.
- **Settings rows removed.** "Unlock at startup", "Lock dokky after" and "Lock dokky now" are gone. The password reminder is a dropdown.
- **Window colour.** Windows use `Palette.Window`: the screen colour on light and a shade raised on dark, as Forged's `Surface` is. Fields are a tint of it.

## 5. Kept as Dokky

Item numbers in brackets:

- Enter on an item opens Edit (33).
- One Settings tab (35).
- The Health page (36).
- "I understand" only on harmful actions such as unmount (38).
- Text field look (39).
- Updates (27).
- The window-too-small message (28).
- Toasts for long errors (29).
- The Overview layout (31).
- The Drives page layout (32).

## Contract between daemon and TUI

- `api.MSessionOpen = "session.open"` with `api.SessionParams{Method, Password, Force, Remote}`. Errors: `password_required` (its message is the prompt to show), `cancelled`, `wrong_password` and `system_unavailable`.
- `api.MSessionClose = "session.close"`.
- `api.Vault.Session bool`: a session is live.
- `api.ErrSessionRequired = "session_required"`.
- `client.SessionOpen(ctx, p)` and `client.SessionClose(ctx)`.
- Removed:
  - `vault.authorize` and grants: `vault.settings` needs the session instead.
  - `Vault.CooldownUntil`, `Vault.IdleLockMinutes`, `Vault.Unlock`, `IdleLockChoices`, `ErrTooManyAttempts` and `ErrGrantRequired`.
  - The `Unlock` and `IdleLockMinutes` params of `vault.settings`.
