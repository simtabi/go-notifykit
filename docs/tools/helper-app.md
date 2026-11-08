# macOS helper app

`<AppName> Notifier.app` is the bundle go-notifykit assembles on macOS so a banner carries the program's own name and icon.

## What it contains

| Path in the bundle | Source |
|---|---|
| `Contents/MacOS/notifier` | the universal notifier embedded in macOS builds, built from `src/macos/notifier.swift` |
| `Contents/Info.plist` | written from `Config`: `CFBundleDisplayName` = `AppName`, `CFBundleIdentifier` = `AppID + ".notifier"`, `LSUIElement` true (no Dock icon) |
| `Contents/Resources/AppIcon.icns` | built from `Config.Icon`; absent when there is no usable icon, and then the plist names none |
| `Contents/Resources/notifykit.stamp` | a hash of the three inputs above |

It lives in the state directory: `~/.local/state/<app>/notifykit/<AppName> Notifier.app` by default.

## Lifecycle

1. The first `Send` (or `Backend()`) compares the stamp with the current inputs. If they match, nothing is written.
2. Otherwise a new bundle is written beside the old one, signed ad hoc (`codesign --force --sign -`), swapped in, and registered with LaunchServices (`lsregister -f`).
3. Each notification runs `Contents/MacOS/notifier` with one JSON object on stdin.

Registration is not optional. Without it the notification center logs `Failed to find or validate client` and refuses the app without asking the user. A failed registration is logged, not fatal.

## Notifier exit status

| Status | Meaning | `Send` returns |
|---|---|---|
| 0 | posted | `Result{Backend: "helper"}` |
| 2 | bad input | an error |
| 3 | the user has not allowed notifications | `ErrDenied` |
| 4 | no answer within two minutes | an error |
| still running at `Timeout` | macOS is showing its permission prompt | `Result{Pending: true}`; the banner follows once the user allows it |

## Permission

The first run shows macOS's "allow notifications" prompt. A dismissed or declined prompt is remembered, and later runs exit 3 until the user turns the app on in System Settings › Notifications, where it is listed under its `AppName`.

## When it is not used

Without `AppID`, or if `codesign` fails, go-notifykit logs why and falls back to `terminal-notifier`, then `osascript`. `NOTIFYKIT_BACKEND=helper` turns that fallback into an error.

---

[← Docs index](../../README.md#documentation)
