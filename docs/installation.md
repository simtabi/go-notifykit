# Installation

go-notifykit is a Go library; it installs with `go get` and needs nothing else on most machines.

## Add the module

```bash
go get github.com/simtabi/go-notifykit/src
```

```go
import notifykit "github.com/simtabi/go-notifykit/src"
```

The module lives in `src/`, so the import path ends in `/src` and the package is
named `notifykit`. Go 1.23 or newer.

## What each platform needs

| Platform | Backend | Needs |
|---|---|---|
| macOS 11+ | `<AppName> Notifier.app`, assembled at run time | nothing: `codesign` and `lsregister` ship with macOS |
| macOS, fallback | `osascript` | nothing: built into macOS. No third-party notifier is ever used automatically. |
| Linux, FreeBSD, OpenBSD, NetBSD, DragonFly | `notify-send` | `libnotify` (`libnotify-bin` on Debian and Ubuntu) and a running notification daemon |
| Windows 10+ | PowerShell toast | Windows PowerShell 5.1 (built in) |

The macOS helper is embedded only in macOS builds, so a Linux or Windows binary
does not carry it (about 150 KB).

## Version

Until the first tag, depend on a commit:

```bash
go get github.com/simtabi/go-notifykit/src@main
```

---

[← Docs index](../README.md#documentation)
