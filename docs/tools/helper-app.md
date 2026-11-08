# macOS helper app

`<AppName> Notifier.app` is the bundle go-notifykit assembles on macOS so a banner carries the program's own name and icon.

## What it contains

| Path in the bundle | Source |
|---|---|
| `Contents/MacOS/notifier` | the universal notifier embedded in macOS builds, built from `src/macos/notifier.swift` |
| `Contents/Info.plist` | written from `Config`: `CFBundleDisplayName` = `AppName`, `CFBundleIdentifier` = `BundleID` (default `AppID + ".notifier"`), `LSUIElement` true (no Dock icon), and `NotifyKitOpenPath`, the one executable a click may run |
| `Contents/Resources/AppIcon.icns` | built from `Config.Icon`; absent when there is no usable icon, and then the plist names none |

It lives in the state directory: `~/.local/state/<app>/notifykit/<AppName> Notifier.app` by default.

## Lifecycle

1. The first `Send` (or `Backend()`) in a process writes a fresh bundle beside the installed one and signs it ad hoc (`codesign --force --sign -`, deterministic).
2. The two are compared file by file, bytes and permission bits. If they match, the fresh copy is discarded. Otherwise, because this build changed or because something altered the installed app, the fresh one is swapped in and registered with LaunchServices (`lsregister -f`). The installed app is never trusted just because it is there.
   Before each notification, the installed notifier is read again and compared with the bytes signed at start; a mismatch rebuilds the app first, so a notifier replaced after start never runs.
   Everything is user-only: directories `0700`, the notifier `0700`, the other files `0600`.
   The plist names the program's own executable (the one a click may run), so a program started
   with `go run`, which gets a new temporary path each time, has its app rebuilt on every run. An
   installed binary keeps its path, and its app is kept.
3. Each notification runs `Contents/MacOS/notifier` with one JSON object on stdin:
   title, subtitle, body, group, sound, the checked image copy and the checked click action.
4. A click relaunches the app with nothing on stdin. It reads the action from the
   clicked notification, checks it again, runs or opens it, and quits.

Registration is not optional. Without it the notification center logs `Failed to find or validate client` and refuses the app without asking the user, which is also why a "not allowed" answer triggers one more registration and retry before falling back. A failed registration is logged, not fatal.

## Notifier exit status

| Status | Meaning | `Send` returns |
|---|---|---|
| 0 | posted | `Result{Backend: "helper"}` |
| 2 | bad input | an error |
| 3 | macOS does not allow notifications from the app | the app is registered again and the notification retried once; if it is still refused, it is shown through `osascript`, and `ErrDenied` comes back only if that fails too |
| 4 | no answer within two minutes | an error |
| still running at `Timeout` | macOS is showing its permission prompt | `Result{Pending: true}`; the banner follows once the user allows it |

## Permission

The first run shows macOS's "allow notifications" prompt. A dismissed or declined prompt is remembered, and later runs exit 3 until the user turns the app on in System Settings › Notifications, where it is listed under its `AppName`.

## When it is not used

Without `AppID`, or if `codesign` fails, go-notifykit logs why and falls back to `osascript`, which macOS provides. It never falls back to a third-party notifier. `NOTIFYKIT_BACKEND=helper` turns that fallback into an error.

---

[← Docs index](../../README.md#documentation)
