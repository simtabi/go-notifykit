# Standard

Every Simtabi Go program that tells its user something through the operating system does it through go-notifykit.

## The rule

A Go binary in any Simtabi-owned org that shows a desktop notification, for any
reason (a finished job, a failed check, an update being available, a security
event), uses `github.com/simtabi/go-notifykit/src`. It does not call
`osascript`, `terminal-notifier`, `notify-send` or PowerShell itself.

## What each program provides

1. **Identity.** `AppName` is the binary's name; `AppID` is
   `com.simtabi.<binary>` for every org; `Icon` is the program's web UI logo mark
   rendered as a 1024px PNG, or its own icon when it has no web UI.
2. **Switches.** `EnvPrefix` is the program's existing environment prefix (`SANGE`,
   `AUTHAMA`). The program's own config can turn notifications off through
   `Disabled`.
3. **One banner per event.** Summarise ("5 findings in billing-api") rather than
   send one banner per item, set `Group`, and set `MinInterval` (30 seconds is a good
   start).
4. **Short text.** Name the repository or host by its short name, not a full path;
   shorten `refs/heads/main` to `main` and SHAs to 7 characters; end with the
   command that shows more (`Run: sange triage list`).
5. **A `notify` command.** Mount `notifykit.RunCLI` as `<app> notify`, so `send`,
   `ping` and `doctor` work the same in every program.
6. **A listener, when long-running.** A server, daemon or watcher runs `Listen` so
   scripts and hooks can `ping` it.
7. **Tests that cannot notify.** Nothing to do for in-process tests. An end-to-end
   test that runs the built binary sets `NOTIFYKIT_DESKTOP=0`. Assert on
   notifications with a `Recorder`.

## Checking a program against it

```bash
# a program that notifies on its own, outside go-notifykit
grep -rnE 'osascript|terminal-notifier|notify-send|ToastNotification' --include='*.go' src \
  | grep -v _test.go
```

Any hit outside go-notifykit is a finding.

---

[← Docs index](../README.md#documentation)
