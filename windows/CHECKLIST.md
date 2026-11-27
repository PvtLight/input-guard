# Trying Input Guard on the Windows PC

About 20 minutes, through Splashtop. Steps marked **At the PC** need someone physically there, so skip them until you are.

**If anything goes wrong:** a trial gives input back by itself after 15 seconds. For a normal lock, use Splashtop's **Ctrl+Alt+Del** button, choose **Lock**, and sign back in. That always turns Input Guard off.

Note anything that doesn't match **Expect**: the step number and what you saw.

## Set up

1. **Copy it over.** Use Splashtop's file transfer to put `InputGuard.exe` in Documents on the PC. If you like, check the copy in PowerShell with `Get-FileHash $HOME\Documents\InputGuard.exe`. It should match the SHA-256 that `build.sh` printed.
2. **Start it.** Double-click `InputGuard.exe` and answer **Yes** to Windows' prompt about allowing changes. If "Windows protected your PC" appears, choose **More info**, then **Run anyway**.
   - Expect: the Input Guard window opens, and a dark green open padlock appears by the clock, maybe behind **^**. Hovering it says "Input Guard: unlocked". The window's subtitle ends with "running as administrator".
3. **Set the passphrase.** Type one twice under **Passphrase** and click **Save Passphrase**.
   - Expect: "✓ Passphrase saved." A **Current passphrase** box now appears above the other two.
4. **Check how Splashtop's input arrives.** Click **Check Input Sources…**. For the next 10 seconds, type a few letters and move and click the mouse in the Splashtop window. Nothing is blocked.
   - Expect: a summary at the top of the window. **Write down the two lines that start with Keys: and Mouse:, plus the Tags line.** If your typing counts as "sent by software", Splashtop injects its input.

## Trials (each one gives input back after 15 seconds)

5. **Hands off.** Click **15-Second Trial**, then try to move the mouse, click and type through Splashtop.
   - Expect: the notice in the middle says **Input test** and counts down. The cursor doesn't move, and clicks and keys do nothing. The tray icon turns amber. After 15 seconds input comes back, and the window says "The 15-second trial finished."
6. **Five clicks.** Start a trial and click 5 times quickly.
   - Expect: each click fills one of five dots under the countdown. That shows Splashtop's clicks are arriving.
   - On the fifth, the notice changes to **Resume input**. Type the passphrase: a dot appears for each character. Press Enter, and input comes back with "Your passphrase unlocked the trial."
7. **Wrong passphrase.** Start a trial, click 5 times, type something wrong and press Enter.
   - Expect: "That passphrase did not match. Wait 2 seconds, then try again." Let the trial run out.
8. **Shortcut and Esc.** Start a trial and press **Ctrl+Alt+Shift+L** through Splashtop.
   - Expect: the prompt opens (if Splashtop doesn't pass the shortcut, note that). Press **Esc**: the countdown comes back and it stays locked until the 15 seconds are up.
9. **Ctrl+Alt+Del.** Start a trial, then use Splashtop's **Ctrl+Alt+Del** button and choose **Cancel**.
   - Expect: back on the desktop, still locked, counting down.
10. **Admin window.** Open Task Manager (Ctrl+Shift+Esc) and click into it. Start a trial and try to use Task Manager with the keyboard through Splashtop.
    - Expect: blocked like everything else.

## A real lock

11. **Lock and leave.** Click **Lock Input** (or press Ctrl+Alt+Shift+L). Disconnect Splashtop, wait a minute, and connect again.
    - Expect: still locked, with an amber icon. If you see the Windows lock screen instead, Splashtop's **Enable lock screen at the end of session** is on. That locked Windows, which turns Input Guard off. Find it in the Splashtop Streamer on the PC, under **Settings → Security**. Leave it on if you prefer the PC locked when you leave. Turn it off if you want Input Guard guarding the visible desktop.
12. **Back in.** Click 5 times, type the passphrase and press Enter.
    - Expect: input comes back and the icon turns green.
13. **Way back in.** Lock again, then use **Ctrl+Alt+Del → Lock** and sign in to Windows.
    - Expect: Input Guard is off after signing in.

## Optional

14. **Automation events.** Tick **Allow automation events** and run a trial. Use the PC through Splashtop.
    - Expect: if step 4 showed Splashtop's input as "sent by software", you can use the PC normally through Splashtop during the trial. A keyboard or mouse at the PC stays blocked. Untick it afterwards unless you want that mode.
15. **At the PC: keyboard, mouse, touchpad.** Run a trial and use everything at the PC.
    - Expect: nothing gets through. Try Alt+Tab, the Windows key, Win+D and three-finger touchpad swipes. Note anything that does get through.
16. **At the PC: touch screen**, if it has one. Run a trial, tap and swipe on the screen, then tap 5 times quickly.
    - Expect: taps do nothing to apps, and 5 quick taps open the prompt. Swipes in from the screen's edges may still open Windows' own panels.
    - If a panel opens, tap something harmless in it, such as a notification, and note whether the tap works. Don't touch airplane mode or Wi-Fi.

## Done

17. **Quit.** Unlock if needed, then choose **Quit Input Guard** from the tray icon's menu. To remove it, delete `InputGuard.exe` and the folder `%LOCALAPPDATA%\Input Guard`.

Send back: the step 4 summary, and the number and what you saw for every step that didn't match.
