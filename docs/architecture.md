# Architecture

One `Notifier` per program chooses a backend once, cleans every notification, and hands it to that backend.

## Layers

```
Notifier.Send ── switches (Disabled, env, go test, CI) ── MinInterval per Group
      │
      └── Backend (chosen once)
            ├── helper            macOS: "<AppName> Notifier.app", UNUserNotificationCenter
            ├── osascript         macOS fallback, built into macOS (shows as "Script Editor")
            ├── notify-send       Linux and the BSDs
            ├── powershell        Windows toast
            └── any Backend       e.g. Recorder in tests

Notifier.Listen ── user-only Unix socket + per-start token ── Notifier.Send
Ping            ── reads the token, writes one JSON line, reads one back
RunCLI          ── send | ping | doctor, for an app to mount as `<app> notify`
```

Every command is built by a pure function (`osascriptCmd`, `notifySendCmd`, ...),
so the tests check exact arguments on any OS, and text always travels as data:
argv, stdin JSON or environment, never as script source.

## Why a helper app on macOS?

macOS attributes a notification to the bundle that posts it. `osascript display
notification` is posted by Script Editor, so every banner said "Script Editor" with
its icon, whatever the program was. Showing the program's own name and icon needs a
bundle with that name and icon, which is what the helper is.

It is assembled at run time rather than shipped because Simtabi's Go programs are
built from source or cross-compiled on Linux: there is no macOS build step that
could produce a per-app `.app`. One universal notifier (arm64 and x86_64, about
150 KB) is embedded in macOS builds; the first `Send` writes it into a bundle with
the app's `Info.plist` and `.icns`, signs it ad hoc with `codesign`, and registers it
with LaunchServices. Each later start signs a fresh copy and compares it with the
installed app, which is replaced only when something differs: a new build, a new
name, ID or icon, or a file something else altered. Details: [macOS helper app](tools/helper-app.md).

## Why no third-party notifier?

Every app ships its own helper, so a notification never depends on what else
happens to be installed on the machine. The automatic chain is the app's helper,
then what the operating system itself provides (`osascript`, `notify-send`, a
PowerShell toast), and nothing else; `TestAutomaticChoiceUsesOnlyTheAppsHelperOrTheOS`
fails if a fallback ever reaches for anything more. There is no third-party
backend to name, either: the `terminal-notifier` one was removed.

## Why off under tests and in CI?

Because otherwise every test run that reaches a notifying code path puts a real
banner on the developer's screen. That is how this package started: sange's test
suite posted five `osascript` banners per simulated push, every run.
`testing.Testing()` catches every in-process test with no setup; an end-to-end
test that runs the built binary sets `NOTIFYKIT_DESKTOP=0` or `CI`.

## Why a socket and a token for ping?

So scripts, hooks and other programs can raise a notification as the app,
without each one needing the app's icon and identity, and without anyone else on
the machine being able to. See [Security](security.md).

## Why one banner per group?

A burst of five banners for one event is noise. `Group` plus `MinInterval` lets the
caller summarise ("5 findings in billing-api") and keeps the next repeat quiet, and
on macOS a newer banner in a group replaces the older one in Notification Center.

---

[← Docs index](../README.md#documentation)
