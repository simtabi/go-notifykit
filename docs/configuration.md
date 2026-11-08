# Configuration

Twelve `Config` fields and four environment variables decide whether, how and from whom a notification is shown.

## `Config`

| Field | Default | Meaning |
|---|---|---|
| `AppName` | required | The name on every banner, e.g. `sange`. No `/`, `\` or `:`. |
| `AppID` | none | Reverse-DNS from the product's own domain (`sh.sange.notify`), else `com.simtabi.<binary>`. Required for the macOS helper. Without it macOS falls back to `terminal-notifier` or `osascript`. |
| `BundleID` | `AppID + ".notifier"` | The helper app's bundle identifier, which macOS remembers notification permission by. Set it to keep a permission an existing helper already has. |
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

Every field but `Title` may be left empty.

| Field | Default | Meaning |
|---|---|---|
| `Title` | the app name | The message: the first line. |
| `Body` | empty | The description: the text under the title. |
| `Subtitle` | empty | Second line on macOS; joined to the body elsewhere. |
| `Icon` | the app's `Config.Icon` | An image for this notification: an absolute path to a PNG, JPEG or GIF of at most 10 MB, not a symlink. Attached on macOS; replaces the app icon on Linux and Windows. |
| `Open` | nothing | Run on click: this program's own executable (no arguments, no shell), or an `http(s)` URL on `127.0.0.1`, `localhost` or `::1`. macOS helper, and `terminal-notifier` for URLs. |
| `Group` | none | Collapses related banners (see `MinInterval`); on macOS a newer one in a group replaces the older. |
| `Level` | `Info` | `Info`, `Warn`, `Alert`; `notify-send` maps it to urgency. |
| `Sound` | off | Play the default sound where the backend can. |

Text fields lose control characters (replaced with spaces) and bidirectional
overrides and other invisible format characters (dropped), and are cut to 240
characters, so text from logs, git or a remote system cannot break or disguise a
banner. An `Icon` or `Open` that breaks the rules makes `Send` return
`ErrInvalid`, even when delivery is switched off. See [Security](security.md).

---

[← Docs index](../README.md#documentation)
