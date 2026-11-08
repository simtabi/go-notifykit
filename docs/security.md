# Security

A sender can change what a banner says, and nothing else; only the user who runs the app can be a sender.

## What a notification can contain

Every field is cleaned before any backend sees it: control characters (newlines,
terminal escapes) become spaces and each field is cut to 240 characters. Text then
reaches the notifier as data:

| Backend | How text travels |
|---|---|
| helper | JSON on the notifier's stdin |
| osascript | `on run argv` arguments; the script itself is a constant |
| terminal-notifier, notify-send | argv; `notify-send` gets `--` and its markup escaped |
| powershell | environment variables, XML-escaped by PowerShell; the script is a constant |

No backend runs a shell, so quoting in a title cannot run anything.

## The ping channel

`Listen` accepts notifications from other processes of the same user:

- **Location.** `notify.sock` and `notify.token` sit in the app's state directory,
  which `Listen` makes `0700`; the socket and the token are `0600`. On Windows they
  inherit the profile directory's per-user ACL.
- **Token.** 32 random bytes, written fresh every time `Listen` starts and deleted
  when it stops. Every message must carry it; comparison is constant-time. `Ping`
  refuses a token file that group or others can read.
- **One message, bounded.** One JSON line of at most 16 KiB, read within five
  seconds; unknown fields are refused. At most eight connections are served at
  once; the rest are told "busy".
- **No takeover.** `Listen` removes a stale socket only if nothing answers on it,
  and never removes a path that is not a socket.
- **Rate.** `MinInterval` applies to pinged notifications like any other.

What the channel does not defend against: a process already running as the same
user can read the token, as it could read any of that user's files. That is the
boundary every per-user socket shares.

## The macOS helper

The notifier is built from `src/macos/notifier.swift` in this repository, and a
test fails if the embedded binary's recorded source hash does not match the
source. The assembled app is signed ad hoc on the user's machine; it is never
notarized or distributed, and it can only post notifications.

## Reporting

See [SECURITY.md](../SECURITY.md): GitHub private vulnerability reporting, or
security@simtabi.com.

---

[← Docs index](../README.md#documentation)
