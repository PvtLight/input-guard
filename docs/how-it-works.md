# How Input Guard works

The full details behind the [README](../README.md): every shortcut, every
safeguard, and every limit. Input Guard is a dependency-free macOS menu-bar
utility with no network code, analytics, downloads, accounts, or third-party
packages. It requires Apple Silicon and macOS 14 or later. Version 0.3 is a
prototype, not a replacement for macOS screen locking.

## Appearance

A shared, locally drawn vector mark appears in the app icon, header, and working
notice. Overview contains the shortcut, access, and lock controls; Settings
contains passphrase, automation, and recovery information. Native semantic colors
support light and dark appearances. The notice continues to say **Agent working**.
Branding does not change input filtering, authentication, or recovery behavior.
Accessibility shows its current permission status and refreshes when the app or
its window becomes active. Saving a first passphrase confirms **Passphrase saved**;
successfully replacing an existing one confirms **Passphrase reset** in its card.

## Keyboard shortcut

With Input Guard running, **Control–Option–Command–L** locks input directly
from any app. Clicking **Lock Input** does the same. There is no confirmation
or countdown. Normal sessions have no automatic timeout. The 15-second trial is
optional and is available from Settings or the menu bar. There is no trial
requirement on first use, relaunch, or settings changes.

While guarded, the same shortcut opens the passphrase/Touch ID unlock prompt;
it never unlocks by itself. While activation is already in progress, repeated activations do not start additional sessions. The shortcut cannot launch
a closed app. If another app owns the shortcut, setup reports the conflict and
the menu-bar control remains available. No ordinary typing is monitored while
unguarded; macOS registers just this specific shortcut.

**Control–Command–Q** is allowed through while guarded so you can request the
built-in macOS lock. Releases of modifier keys that other apps already saw
pressed also pass, such as the Control–Option–Command still held when the
shortcut starts the lock. Without them, apps could keep treating a modifier as
held after unlock until it was pressed again. New key and modifier presses
stay blocked. Input Guard ends its session when it receives a screen-lock
or screen-unlock notification. After unlocking macOS, Input Guard stays off;
there is no automatic re-arming. Pending activation is also canceled. Switching away from or back to the macOS user session also
clears the guard. Display sleep/wake alone does not clear it.

Screen-lock/unlock uses the macOS distributed notification names
`com.apple.screenIsLocked` and `com.apple.screenIsUnlocked`. These are not a
documented stable AppKit API or an authentication boundary; receipt is only a
safety reset. The independent freeze recovery is retained. A real macOS
lock/unlock cycle still needs an attended verification; the automated notification
tests use isolated in-process centers and do not lock the computer.

## Touch screens

Touches on a touch screen never reach the session event tap: macOS hands each
touch to the window under the finger, and AppKit turns it into clicks inside
that app. So while guarded, Input Guard also covers every screen (built-in,
external, or a Sidecar iPad) with a clear window above all other windows, menu
bar and Dock included. Touches land on it and do nothing. It never takes
keyboard focus or activates Input Guard, so the agent's app stays in front. On
macOS 27 it also turns off system touch gestures, such as edge swipes, for
touches that start on it. The health check keeps it over every screen when
displays change and puts it back in front if another window gets above it.

It is only a window, not an input filter: when Input Guard unlocks, quits,
crashes, or is ended by its recovery helper, macOS removes it with the process.
The camera notch and the rounded display corners, which have no pixels, stay
with macOS. With **Allow automation events** on, the shield is not used,
because it would also catch automated clicks; touches then pass. Blocking real
touches was confirmed by hand in version 0.3.7, during the 15-second trial on a
touch-screen MacBook Pro.

## Recovery safeguards

Every launch starts unlocked. No login item or launch service is installed.
The app explicitly opts out of macOS Resume using `disableRelaunchOnLogin()`,
so “Reopen windows when logging back in” does not request its relaunch.
App state saving/restoration and window restoration are disabled. Lock state and
pending activations exist only in memory. Even if the app
is opened by an external launcher, it cannot restore a guarded session: manual
activation is required again.

Safety takes priority over maintaining the lock. **Automatic recovery restores
input without a password and ends protection.** Do not treat this version as an
unattended security lock.

- A separate `InputGuardRecovery` executable must complete a readiness handshake
  **before** the app creates its input tap. Failure refuses to start the lock.
- The main UI loop and helper exchange heartbeats. If the UI stops responding
  for about 3 seconds, the helper force-terminates only its own parent Input
  Guard process. This releases that process's event tap, touch shield, and sleep assertions;
  it does not restart the Mac or terminate your other work.
- If the helper dies or stops acknowledging heartbeats, the responsive main app
  removes its tap within about 2 seconds. Pipes are nonblocking so a stalled
  helper cannot freeze the UI. Teardown removes the input tap before disarming
  the helper, and wakes a suspended helper so it can exit.
- Tests have a hard 15-second limit, enforced by the independent helper even if
  the UI timer fails. The test deadline includes sleep and cannot be extended
  by changing the system time. **Normal sessions have no timeout in either the
  app or its helper.** Their helper checks liveness until the session ends.
  Liveness clocks exclude system sleep, so a sleeping Mac is not mistaken for
  a frozen app.
- Clicking **Lock Input** or using the shortcut activates directly, without an
  app confirmation or countdown. Accessibility, saved passphrase, and recovery
  readiness checks still run before input blocking starts. macOS may separately
  request Keychain permission when reading the saved passphrase verifier.
- Normal locking requires Accessibility permission, a saved passphrase, and a
  healthy recovery helper. The optional trial is never a prerequisite, and no
  separate confirmation of physical blocking is required. A macOS lock/unlock
  cycle ends the guard; the next activation remains a manual choice.
- macOS screen lock/unlock and user switching remove the custom lock. Disabled event taps are
  never automatically re-enabled. The power key remains available.

This does not guarantee recovery from an OS-wide freeze, simultaneous failure
of both processes, or hardware failure. macOS Force Quit/restart remains the
last-resort recovery path. Physical input blocking was confirmed by hand in
version 0.2.2 after enabling Accessibility. The new macOS lock/unlock reset
still needs a live attended verification in this version.

## First use

1. Open `Input Guard.app`. Its unlocked padlock appears in the menu bar.
2. Click **Open Accessibility Settings**, then add/enable **Input Guard** in
   System Settings → Privacy & Security → Accessibility. Grant this to the app,
   not to Terminal. If macOS additionally requests Input Monitoring,
   enable Input Guard there and relaunch it.
3. In **Settings**, set a separate Input Guard passphrase, at least 4 characters. Use printable
   ASCII characters and the English keyboard layout when unlocking. Do not enter
   your Mac login password. To change a saved passphrase, enter the current one.
4. If desired, choose **Optional 15-Second Trial** from Settings or the menu
   bar to check your keyboard, pointer, touch screen, and passphrase. It releases input after
   15 seconds and can be ended sooner with your code. You can skip this step.
5. Choose **Lock Input** from the menu bar. The menu-bar label
   says **Agent working** with no expiry timer. The on-screen notice also says
   **Agent working** and disappears after four seconds, leaving the
   desktop visible and its focus unchanged. The Mac and display stay awake while
   guarded. Closing a laptop lid, manual sleep, power loss, and OS policy can
   still suspend work. macOS screen lock/unlock or user switching releases this custom lock.
6. Press **Control–Option–Command–L** to request unlock. If Touch ID is
   available it starts right away: scan an enrolled finger. Or type your Input
   Guard passphrase and press Return; typing closes the Touch ID request, and
   Tab starts it again. No shortcut alone unlocks a normal session. Escape dismisses the
   unlock attempt and leaves input guarded. The prompt says when Caps Lock is on.
   Password entry does not support paste, input-method composition, or
   Option-based character entry.

## Automation

Background file operations, builds, and network work do not require input events.
Browser automation that operates directly through a browser protocol may also
continue without them. These paths still need an actual end-to-end check on the
intended workload.

The optional **Allow automation events (experimental)** setting passes events
whose Core Graphics source PID is greater than zero. This is a compatibility
heuristic, **not a trustworthy distinction between a person and software**.
Remappers, accessibility utilities, remote-control software, and other processes
can generate or relabel input. Software may bypass the tap entirely. This mode
is off by default. Do not rely on it for adversarial physical access protection.
Synthetic events are suspended during passphrase entry. Touch screens are not
blocked in this mode: the window that blocks them would also catch automated clicks.

Test the desired automation during a 15-second trial. Also test physical input
with every connected keyboard, mouse, trackpad, and remapping tool. A successful
build or unit test does not establish compatibility with your input devices or
agent's automation tools.

## Security and recovery limits

- The passphrase is never intentionally written to disk or logged. A random
  32-byte salt and PBKDF2-HMAC-SHA256 verifier (600,000 rounds) are stored in the
  local login Keychain. Wrong attempts receive an increasing delay up to 30
  seconds; Return with nothing typed does not count. Return during the delay
  keeps what you typed, so press Return again when it ends. The app holds the candidate briefly in process memory; Swift does not
  guarantee zeroization of string copies.
- Touch ID uses Apple's LocalAuthentication biometric policy. The app does not
  collect your Mac password or provide a Mac-password fallback dialog.
- Input is filtered at the macOS session event tap; touch screens are covered
  by a window instead (see Touch screens). System-reserved controls,
  secure input contexts, some gestures, power/Touch ID, other accessibility
  software, and remote access are outside its guarantees. The power key is
  intentionally not blocked.
- A process crash, force quit, revoked permission, or tap failure
  releases the input filter. The app checks health every quarter second and shows
  an explicit **NOT locked** warning when it detects a failure. A tap timeout
  ends the lock; the app does not re-arm it. This is not a fail-closed OS lock.
- Normal Quit is refused during a guarded session, but OS-level termination
  cannot be prevented. Restart is a last-resort recovery path if you forget the
  passphrase and both recovery mechanisms fail; unsaved work may be lost. In
  a trial, wait for the 15-second safety timeout instead. Normal sessions do
  not expire: unlock with your code/Touch ID or the macOS lock screen. There is
  no hidden password or emergency bypass hotkey. A forced restart leaves the
  app unguarded until manually activated.
- The desktop is visible. Use the built-in macOS lock for private information or
  when defending against someone intentionally trying to access the machine.
- No login item is installed, no security settings are weakened, and nothing
  locks automatically on launch. After a rebuild, macOS may require renewed
  Accessibility/Keychain approval because the app is locally ad-hoc signed.

## Build and verify

From the repository folder:

```sh
bash build.sh
```

The script compiles the sources using Xcode's Swift compiler, builds and ad-hoc
signs `build/Input Guard.app`, verifies its signature, and runs self-tests for a
known PBKDF2 vector, password verification, invalid records, attempt limiting,
and event policy, a replay of every press and release order of the shortcut
(plus Control–Command–Q and 50,000 random key events) through the tap's filter,
the unlock prompt (Return with nothing typed, Return during a delay, Caps Lock),
and four isolated notification-observer checks. It then runs
seven process-level recovery tests: a frozen UI process (`SIGSTOP`) in an
untimed session, independent trial deadline expiry, killed helper, frozen helper,
clean disarm, missing helper, and a healthy session with no deadline.
The recovery helper terminates only disposable
fixture processes during these tests. The tests do not activate any global input
filter or change Keychain entries.

For a setup-screen preview without permissions or locking (close other copies first):

```sh
'build/Input Guard.app/Contents/MacOS/InputGuard' --preview /tmp/input-guard-preview.png
```

Append `--preview-settings`, `--preview-notice`, or `--preview-unlock` (the unlock
prompt with its longest message and Caps Lock on) to capture those views instead.
These previews never activate the guard.

With other copies of Input Guard closed, run a real launch check (it opens setup,
checks that no input tap, touch shield, recovery helper, or sleep assertion exists,
checks restoration is disabled, and quits without activating input blocking):

```sh
'build/Input Guard.app/Contents/MacOS/InputGuard' --startup-check
```

This verifies fresh launch behavior; it does not restart or log out of the Mac.

To check where clicks and touches would land while guarded, run the touch shield
check. It raises the shield for about a second, with no input tap, asks macOS
which window a click or touch would reach on a grid over every screen, and
lowers it again. It also works while another copy is running:

```sh
'build/Input Guard.app/Contents/MacOS/InputGuard' --touch-shield-check
```

The setup screen can be opened safely; input is blocked only after the user
chooses **Optional 15-Second Trial** or **Lock Input**, and the required permission is granted. Physical input blocking,
real touches, Touch ID, sleep/wake, and live automation
compatibility require attended testing on the Mac.

## Uninstall

Unlock and quit the app, then remove Input Guard.app. Remove its permission in
System Settings if desired. Its passphrase verifier is labeled **Input Guard
passphrase verifier** in Keychain Access and can be deleted there. Deleting the
verifier is only a reset while the app is unguarded; an active guard holds its
existing verifier in memory until it ends.

## Apple references

- [Core Graphics event taps](https://developer.apple.com/documentation/coregraphics/cgevent/tapcreate(tap:place:options:eventsofinterest:callback:userinfo:))
- [Event source process ID](https://developer.apple.com/documentation/coregraphics/cgeventfield/eventsourceunixprocessid)
- [LocalAuthentication](https://developer.apple.com/documentation/localauthentication)
- [Keychain services](https://developer.apple.com/documentation/security/keychain-services)
