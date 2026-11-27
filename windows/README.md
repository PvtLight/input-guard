# Input Guard for Windows

**Walk away from the PC. Your agents keep working.**

A small notification-area app that blocks this PC's own keyboard, mouse, touchpad and touch screen while the screen stays on and everything keeps running. It's the Windows version of the Mac app in this repository, with the same rules: the passphrase is the only way to unlock, every launch starts unlocked, and safety wins over staying locked.

Version 0.1.0 is a first prototype. It was built and tested on a Mac. **Nothing in it has run on Windows yet**, so start with the [checklist](CHECKLIST.md), which begins with the 15-second trial.

## What it does

- **Lock.** Click the tray icon and choose **Lock Input**, or press **Ctrl+Alt+Shift+L**. Every key, click, scroll and touch from this PC is ignored. The screen stays on, the PC doesn't sleep on its own, and agents, builds and downloads keep going.
- **Unlock prompt.** While locked, press **Ctrl+Alt+Shift+L** again, or **click 5 times within 3 seconds**, anywhere. A prompt appears in the middle of the screen. Type the passphrase and press **Enter**. **Esc** puts the prompt away and stays locked. The clicks only open the prompt, and they stay blocked from apps like every other click.
- **Passphrase only.** It's the only way to unlock. It's stored as a salted PBKDF2-HMAC-SHA256 hash (600,000 rounds, the same as the Mac), sealed with Windows' Data Protection API (DPAPI) for your Windows account. A wrong passphrase makes you wait 2, 4, 8, 16, then 30 seconds before the next try.
- **Trial lock.** **15-Second Trial** locks for 15 seconds, then gives input back by itself. Use it first.
- **Can't lock you out.** Ctrl+Alt+Del always works (see [If something goes wrong](#if-something-goes-wrong)), and a separate recovery helper ends Input Guard if it ever freezes.
- **Never surprises you.** Every launch starts unlocked. No autostart, no service, no driver, nothing installed. Locking Windows, or switching users, turns it off.
- **Private.** No network code, analytics or accounts. Go standard library only, no third-party packages.

## Install

1. Copy `InputGuard.exe` to the PC, for example with Splashtop's file transfer, into any folder you like (Documents is fine).
2. Double-click it. Windows asks **"Do you want to allow this app to make changes?"** Choose **Yes**. Input Guard runs as administrator so it can also block input to admin windows such as Task Manager. On a standard (non-admin) account it runs without the prompt, with that one gap.
3. If **"Windows protected your PC"** appears, choose **More info**, then **Run anyway**. The app isn't signed. If Windows Security flags the file instead, compare its SHA-256 (`Get-FileHash .\InputGuard.exe` in PowerShell) with the one `build.sh` printed before you decide to allow it.
4. The settings window opens and a dark green padlock appears in the notification area. Windows 11 may hide it behind the **^** next to the clock. Drag it onto the taskbar to keep it in view.

## First run

1. In **Passphrase**, type a passphrase twice and click **Save Passphrase**. Use 4 to 128 characters from an English keyboard. Don't reuse your Windows password.
2. Click **15-Second Trial**. The notice says **Input test** and counts down. Try the keyboard and mouse: nothing happens. Click 5 times quickly, type your passphrase and press Enter, or wait for the 15 seconds to run out.
3. When you're ready, click **Lock Input** or press **Ctrl+Alt+Shift+L**. The notice says **Agent working** for a few seconds, then gets out of the way. The tray icon turns amber.

To unlock: Ctrl+Alt+Shift+L or 5 quick clicks, then the passphrase and Enter.

## Remote control with Splashtop

Both locking and unlocking are meant to be done through Splashtop.

**To lock:** click the tray icon (it may be under **^** by the clock) and choose **Lock Input**, or click **Lock Input** in the settings window, or press **Ctrl+Alt+Shift+L**. Nothing is blocked until the lock starts, so this works however Splashtop sends its input.

**While locked**, Splashtop's input is blocked like the PC's own, which is what you want while you're away. The cursor doesn't move, and that's expected. Each click still reaches Input Guard: it brings up the notice and fills one of five dots, so you can see your clicks arriving.

**To unlock through Splashtop:**

- **Click 5 times quickly** in the Splashtop window. On the fifth the prompt opens. Type the passphrase and press Enter. Input Guard reads the keys it blocks, so typing works even though nothing reaches your apps.
- Or send **Ctrl+Alt+Shift+L**, if Splashtop passes it through.
- If neither works: use Splashtop's **Ctrl+Alt+Del** button, choose **Lock**, and sign in to Windows. That always ends the guard.

**Why the clicks and keys still reach it:** every key and click that Splashtop sends on Windows goes through the same low-level hooks as the PC's own input. That's true whether Splashtop injects its input with `SendInput` or uses a driver. Input Guard reads them there before blocking them. **Check Input Sources** in Settings shows in 10 seconds, without locking anything, whether your Splashtop clicks and keys reach the hooks.

Two Splashtop Streamer settings matter. They're on the PC, under **Settings → Security**:

- **Enable lock screen at the end of session.** If it's on, Windows locks when you disconnect. A Windows lock turns Input Guard off, and the Windows lock screen takes over: the screen is hidden and agents that drive apps by clicking or watching the screen stop. Agents in a terminal keep working. Turn it off if you want Input Guard to keep guarding the visible desktop after you disconnect.
- **Lock keyboard and mouse.** Splashtop's own feature blocks the PC's keyboard and mouse, but only during a session and not the touch screen. Input Guard also covers the time when nobody is connected.

## Outside input: what Windows lets the guard tell apart

Input Guard sees every key and mouse event through Windows' low-level hooks (`WH_KEYBOARD_LL` and `WH_MOUSE_LL`) before any app does. For each event, Windows says whether software sent it:

| Where the input comes from | What the hook sees |
| --- | --- |
| This PC's keyboard, mouse and touchpad | No flag |
| Software calling `SendInput`, `keybd_event` or `mouse_event`: agents that click and type, AutoHotkey, PowerShell `SendKeys`, most remote-control apps | **Injected** (`LLKHF_INJECTED` / `LLMHF_INJECTED`, plus a "lower integrity" variant). Input Guard counts either one. |
| Characters typed as text rather than keys | Injected, as the key `VK_PACKET`, with the character attached. The unlock prompt reads these too. |
| Touch screen and pen | Not seen by the hooks at all. Touches go straight to the window under the finger, so Input Guard covers every screen with a clear window that catches them. |

**Splashtop:** Splashtop doesn't document how its Streamer delivers input, and no public report measures it. Comparable apps inject: Chrome Remote Desktop uses `SendInput` and tags its events, and AnyDesk's keys arrive flagged as injected. To see what Splashtop does on your PC, click **Check Input Sources…** in Settings. For 10 seconds it counts keys and clicks by source and lists any tags software put on its input, while blocking nothing. Then type and click through Splashtop. "Sent by software" means Splashtop injects. "From this PC" means it uses a driver that looks like real hardware. The checklist includes this step.

**What the guard does with the flag:**

- **By default, everything is blocked:** the PC's own input, agents' input and Splashtop's. Agents that don't need the keyboard or mouse keep working: terminals, builds, downloads, and browser automation that talks to the browser directly.
- **Allow automation events (experimental)**, in Settings, lets injected input through, except while the unlock prompt is open. The PC's own keyboard and mouse stay blocked. If Splashtop injects, its input passes too, so you can use the PC remotely while hands at the PC can't. In this mode the touch screen isn't blocked, because the cover would also catch the clicks that are let through. Five clicks don't open the prompt either, because stray clicks at the PC would pause the automation. Use Ctrl+Alt+Shift+L, or the tray menu's **Unlock…**, instead.
- **The flag is a hint, not proof.** Any program can inject input, and a driver-level tool (some remappers, virtual keyboards, hardware emulators) can make software input look real, or real input look injected. Don't rely on automation mode against someone determined.

## If something goes wrong

Safety wins over staying locked:

| Situation | What happens |
| --- | --- |
| Forgot the passphrase, or the prompt doesn't work | Press **Ctrl+Alt+Del** (Splashtop has a button for it) and choose **Lock**, then sign back in with your Windows password. Locking Windows turns Input Guard off. Ctrl+Alt+Del always reaches Windows: no app can block it. |
| Input Guard freezes | Windows itself drops a keyboard or mouse hook that stops answering. The recovery helper also ends a frozen Input Guard within about 3 seconds, and Windows removes everything it put on screen. |
| The recovery helper dies or freezes | Input Guard notices within about 2 seconds and gives input back itself. |
| Input Guard's own checks fail | The lock ends and Settings says **Your input is NOT locked**. It never quietly locks again. |
| Windows drops Input Guard's mouse hook | Windows does this to a hook that once took too long. The next click that reaches the touch cover ends the lock, with the same warning. A dropped keyboard hook can't be noticed. |
| A trial goes wrong | It ends after 15 seconds anyway. The helper enforces that even if the app's own timer fails. |
| Everything else | Hold the power button. Input Guard never blocks it. |

Input Guard ends the lock when Windows reports a lock, unlock, sign-out or user switch. It won't lock at all unless Windows has agreed to send those reports, so Ctrl+Alt+Del → Lock is always a way out.

## What Windows won't let it block

- **Ctrl+Alt+Del:** reserved for Windows. Its screen offers Lock, Switch user, Sign out, Change password and Task Manager. Lock and sign out both end the guard. Running as administrator, Input Guard still blocks input to Task Manager.
- **Win+L:** always locks Windows. That ends the guard and leaves whoever pressed it at the Windows lock screen.
- **Windows' own secure screens:** UAC prompts, the Ctrl+Alt+Del screen and the lock screen are on a separate desktop that no app's hooks reach. If an agent triggers a UAC prompt while you're away, someone at the PC can answer it.
- **Swipes in from the screen's edges, and three- or four-finger touchpad gestures:** Windows handles these itself. They can still open its panels (notifications and quick settings, widgets, Task View) or switch desktops.
  - Those panels sit above every window, so **taps on a touch screen work inside them**. Someone at the PC could turn on airplane mode and cut the agents' network. Mouse clicks and typing in them stay blocked.
  - On Windows Pro, the **Allow edge swipe** policy turns edge swipes off. It's in Local Group Policy, under Computer Configuration → Administrative Templates → Windows Components → Edge UI. Input Guard doesn't change it.
- **Xbox Game Bar (Win+G):** Microsoft says hooks don't block its shortcuts. Turn Game Bar off in Settings if that matters.
- **Power, sleep and laptop Fn keys** for brightness, airplane mode and the like: the hardware handles them.
- **Sticky Keys:** pressing Shift five times may still bring up Windows' Sticky Keys question. Turn off its shortcut in Settings → Accessibility → Keyboard if that matters.
- **Admin windows on a standard account:** without administrator rights, a hook isn't called for input to windows running as administrator. Admin accounts don't have this gap, because Input Guard runs elevated.
- **Policy locks:** a screen saver with a password, an organization's inactivity lock, and Dynamic Lock can still lock Windows, which ends the guard.

As on the Mac, the desktop stays visible. Input Guard keeps hands off the PC while you're away. It is not a security lock. For privacy, or against someone determined to get in, use the Windows lock screen.

## Differences from the Mac app

- **Shortcut:** Ctrl+Alt+Shift+L instead of ⌃⌥⌘L, because Windows keeps Win+L for itself.
- **No Windows Hello in this version.** The Mac version has Touch ID, but over Splashtop neither is in reach. Windows Hello's PIN fallback would also need typing into a Windows dialog that the guard blocks. The passphrase is the only way to unlock.
- **Runs as administrator** (one prompt at launch). The Mac app needs Accessibility permission instead.
- **The touch cover** is a window over all screens, like the Mac's touch shield. Edge swipes stay with Windows.
- **Check Input Sources**, to see how remote-control input arrives, is new.

## Remove it

Unlock, then choose **Quit Input Guard** from the tray icon's menu. Delete `InputGuard.exe`, and the folder `%LOCALAPPDATA%\Input Guard`, which holds `passphrase.dat` (the sealed hash) and `settings.json`. Nothing else was written. There's no registry entry, service, driver or autostart. Deleting `passphrase.dat` while unlocked also resets the passphrase.

## How it works

- `app/` is the Windows part, kept thin: two low-level hooks on their own thread, the tray icon and menu, the settings window, the notice, the touch cover, DPAPI, and Windows' lock and user-switch reports.
- `core/` is the logic that doesn't depend on Windows. It covers which events pass while locked, the unlock prompt, the five-click counter, the attempt limits, the passphrase hash and file, and the recovery helper's protocol. The hooks call the same code the tests run.
- While locked, the hooks block every key press and click. Only two kinds of event pass. Releases of keys apps already saw pressed pass, such as the Ctrl+Alt+Shift still held when the shortcut starts the lock; without them, apps would act as if those keys were stuck. With automation allowed, injected events also pass.
- The recovery helper is a second copy of `InputGuard.exe` started with `--recovery` before the hooks go in. The app sends it a heartbeat from its UI thread four times a second. If the heartbeats stop for 3 seconds, or a trial reaches 15 seconds, the helper ends the app, and Windows removes the hooks and windows with it. The helper holds a handle to the app, so it can never end some other process that reused the app's process ID.
- `art/` draws the icons from shapes: the Mac app's mark for the .exe, and padlocks for the tray. It uses no image files.

## Build and test

On any computer with Go 1.25 or later (Mac, Linux or Windows):

```sh
cd windows
bash build.sh
```

It formats and vets the code, runs the tests, writes the Windows resources (manifest, icon and version details), cross-compiles `InputGuard.exe` for 64-bit Windows, and checks the result. The tests never block real input:

- The passphrase: a PBKDF2-SHA256 known answer, matching and mismatching, the 4-to-128 rule, damaged records failing closed, and the wait after each wrong try.
- The unlock prompt: Enter with nothing typed, Enter during a wait, Caps Lock, Backspace, and the 128-character limit.
- Five clicks: quick, slow, bunched-up and expired clicks, clicks staying blocked, and the count the notice shows.
- The input rules, replayed exactly as the hooks call them:
  - every press and release order of Ctrl+Alt+Shift+L across the lock starting (25,920 runs): no key reaches an app and nothing stays held;
  - common Windows shortcuts staying blocked;
  - automation mode;
  - typing the passphrase with Shift, Caps Lock, the numpad and Unicode characters;
  - coming back from the Ctrl+Alt+Del screen;
  - 50,000 random events from the keyboard, the mouse and software.
- The passphrase file, with a stand-in for DPAPI: saving, replacing, a changed file, and a weaker record.
- Seven process-level recovery tests, as on the Mac: a frozen app, the trial's hard deadline, a killed helper, a frozen helper, a clean stop, a missing helper, and a healthy lock with no deadline.
- The icons at every size.
- The built .exe: 64-bit GUI program, manifest present and well-formed, icon in 8 sizes, version details.

Not tested here, and covered by the [checklist](CHECKLIST.md) on the PC: the hooks actually blocking, the touch cover, the windows and tray icon, DPAPI, Windows' lock reports, elevation, and how Splashtop's input arrives.

For an ARM-based Windows PC, build with `GOARCH=arm64` (the x64 .exe also runs there under emulation). The resource file is only generated for x64, so the arm64 build has no icon or manifest.
