# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Notifications under the program's own name and icon.** On macOS the first
  `Send` assembles `<AppName> Notifier.app` from an embedded universal notifier,
  signs it ad hoc, registers it with LaunchServices and posts through
  `UNUserNotificationCenter`, so a banner no longer says "Script Editor". It falls
  back to `terminal-notifier`, then `osascript`. Linux and the BSDs use
  `notify-send` with the app's icon; Windows uses a PowerShell toast.
- **Silent under tests and in CI.** Desktop delivery is off under `go test` and
  when `CI` is set, unless `NOTIFYKIT_DESKTOP=1`. Per-app switches
  (`<PREFIX>_NOTIFY_DESKTOP`, `<PREFIX>_NOTIFY_BACKEND`) and `Config.Disabled`
  sit above the global ones. A `Recorder` backend lets tests assert on what would
  have been shown.
- **One banner per event.** `Group` and `MinInterval` suppress repeats; on macOS
  a newer banner in a group replaces the older one.
- **A secure ping channel.** `Listen` serves a user-only Unix socket in the state
  directory (`0700` directory, `0600` socket and token) and requires a token
  written fresh on every start; `Ping` sends one bounded JSON message to it.
- **A shared `notify` command.** `RunCLI` gives every app `notify send`,
  `notify ping` and `notify doctor`.
- **First run reports `Result.Pending`** while macOS shows its permission
  prompt, rather than an error.
