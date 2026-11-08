# Security

A sender can change what a banner says, and nothing else; only the user who runs the app can be a sender.

## What a notification can contain

Every text field is cleaned before any backend sees it: control characters
(newlines, terminal escapes) become spaces, bidirectional overrides and other
invisible format characters are dropped (so `invoice<RLO>fdp.exe` cannot pass for
`invoiceexe.pdf`), and each field is cut to 240 characters. Text then reaches the
notifier as data:

| Backend | How text travels |
|---|---|
| helper | JSON on the notifier's stdin |
| osascript | `on run argv` arguments; the script itself is a constant |
| notify-send, and the deprecated terminal-notifier (only when named) | argv; `notify-send` gets `--` and its markup escaped |
| powershell | environment variables, XML-escaped by PowerShell; the script is a constant |

No backend runs a shell, so quoting in a title cannot run anything.

## Images

A per-notification `Icon` must be an absolute path to a regular file, not a
symlink, of at most 10 MB, whose content (not its name) is a PNG, JPEG or GIF. The
file is opened and compared with what was checked, so swapping it in between is
caught. It is then copied, `0600`, into the state directory's `images/` folder,
and only the copy reaches a backend: macOS moves an attached file into its own
store, and the caller's file must never be moved. Copies older than a day are
pruned.

## Click actions

`Open` accepts exactly two shapes: an `http` or `https` URL whose host is
`127.0.0.1`, `localhost` or `::1`, with no user info, or an absolute path that
resolves to the running program's own executable. Anything else is `ErrInvalid`.
The macOS helper checks the stored action again when it is clicked, against the
same rules: the executable must equal the `NotifyKitOpenPath` written into its own
`Info.plist`. A path runs with no arguments and no shell; a URL opens in the
browser. `terminal-notifier` is given URLs only, because its `-execute` runs a
shell. A ping goes through the same checks, so a sender cannot make the app open a
remote page or run another program.

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
- **Same rules.** A pinged notification's text, `Icon` and `Open` are cleaned and
  checked exactly as a local one's are.

What the channel does not defend against: a process already running as the same
user can read the token, as it could read any of that user's files. That is the
boundary every per-user socket shares.

## The macOS helper

Each process signs a fresh copy of the helper app at start and compares it, file by
file, with the installed one, replacing any difference. Before each notification
the installed notifier is compared again with the bytes signed at start. A
notifier swapped in by another process is therefore replaced, not run. The app
and the state directory are user-only (`0700` directories, `0600` files, the
notifier `0700`).

terminal-notifier and osascript both read options from their arguments, so a
title, subtitle, body or group starting with `-` gets an invisible word joiner
(U+2060) in front: it reads the same in the banner and cannot be taken for an
option.

The notifier is built from `src/macos/notifier.swift` in this repository, and a
test fails if the embedded binary's recorded source hash does not match the
source. The assembled app is signed ad hoc on the user's machine; it is never
notarized or distributed, and it can only post notifications.

## Reporting

See [SECURITY.md](../SECURITY.md): GitHub private vulnerability reporting, or
security@simtabi.com.

---

[← Docs index](../README.md#documentation)
