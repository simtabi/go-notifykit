# Configuration

Eleven `Config` fields and four environment variables decide whether, how and from whom a notification is shown.

## `Config`

| Field | Default | Meaning |
|---|---|---|
| `AppName` | required | The name on every banner, e.g. `sange`. No `/`, `\` or `:`. |
| `AppID` | none | Reverse-DNS, e.g. `com.simtabi.sange`. Required for the macOS helper, whose bundle ID is `AppID + ".notifier"`. Without it macOS falls back to `terminal-notifier` or `osascript`. |
| `Icon` | none | A square PNG, ideally 1024px. macOS gets an `.icns` built from it; `notify-send` gets the PNG. |
| `EnvPrefix` | none | Names the per-app switches, e.g. `SANGE`. |
| `Disabled` | `false` | Off, whatever the environment says. Wire your own config setting here. |
| `StateDir` | `$XDG_STATE_HOME/<app>/notifykit`, else `~/.local/state/<app>/notifykit` (`%LocalAppData%\<app>\notifykit` on Windows) | Where the helper app, icon, socket and token live. Keep it under about 80 characters: Unix socket paths are limited. |
| `MinInterval` | `0` (never) | Suppress a repeat of the same `Group` within this long. |
| `Timeout` | `10s` | Bounds one delivery. |
| `Backend` | platform | Replace the backend, e.g. a `Recorder` in tests. Runs under `go test` too; only `Disabled` stops it. |
| `Getenv` | `os.Getenv` | Read the environment (tests inject a map). |
| `Log` | discard | One line per fallback or failure. |

## When desktop delivery is on

The first rule that matches wins:

1. `Disabled` is true: **off**.
2. `Backend` is set: **on**.
3. `<PREFIX>_NOTIFY_DESKTOP` is set to `1/true/on/yes` or `0/false/off/no`: that.
4. `NOTIFYKIT_DESKTOP` is set to one of those: that.
5. Running under `go test` (`testing.Testing()`): **off**.
6. `CI` is set to anything but `false` or `0`: **off**.
7. Otherwise **on**.

So a test suite or a CI job never puts a banner on anyone's screen unless it says
`NOTIFYKIT_DESKTOP=1`, and a user silences one app with `SANGE_NOTIFY_DESKTOP=0`
or every app with `NOTIFYKIT_DESKTOP=0`.

## Forcing a backend

`<PREFIX>_NOTIFY_BACKEND`, then `NOTIFYKIT_BACKEND`: one of `helper`,
`terminal-notifier`, `osascript`, `notify-send`, `powershell`, or `none`. A forced
backend that is unavailable is an error rather than a silent fallback.

## Notification fields

| Field | Meaning |
|---|---|
| `Title` | First line; the app name when empty. |
| `Subtitle` | Second line on macOS; joined to the body elsewhere. |
| `Body` | The message. |
| `Level` | `Info`, `Warn`, `Alert`; `notify-send` maps it to urgency. |
| `Group` | Collapses related banners (see `MinInterval`); on macOS a newer one in a group replaces the older. |
| `Sound` | Play the default sound where the backend can. |

Control characters are replaced with spaces and each field is cut to 240
characters, so text from logs, git or a remote system cannot break a banner.

---

[← Docs index](../README.md#documentation)
