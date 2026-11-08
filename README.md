# go-notifykit

[![Go Reference](https://pkg.go.dev/badge/github.com/simtabi/go-notifykit/src.svg)](https://pkg.go.dev/github.com/simtabi/go-notifykit/src)
[![Tests](https://github.com/simtabi/go-notifykit/actions/workflows/ci.yml/badge.svg)](https://github.com/simtabi/go-notifykit/actions/workflows/ci.yml)
[![Static analysis](https://github.com/simtabi/go-notifykit/actions/workflows/static-analysis.yml/badge.svg)](https://github.com/simtabi/go-notifykit/actions/workflows/static-analysis.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

> Native desktop notifications for Go programs, under the program's own name and icon, that stay silent under `go test` and in CI.

Works on macOS 11+, Linux and the BSDs (`notify-send`), and Windows 10+ (PowerShell toast), with Go 1.23 or newer.

## Install

```bash
go get github.com/simtabi/go-notifykit/src
```

The module lives in the repository's `src/` directory, so the import path ends in
`/src`; import it under the name `notifykit`. There is no binary to install.

## Quick start guide and usage

### Getting started

Nothing to set up beyond the import. On macOS the first notification assembles a
small helper app, `<AppName> Notifier.app`, in the application's state directory,
and macOS asks once whether to allow it. Give the notifier a square PNG icon, or
banners show the generic app icon.

### Usage

```go
import notifykit "github.com/simtabi/go-notifykit/src"

n, err := notifykit.New(notifykit.Config{
	AppName:   "sange",
	AppID:     "com.simtabi.sange",
	Icon:      sangeIconPNG,        // square PNG, ideally 1024px
	EnvPrefix: "SANGE",             // SANGE_NOTIFY_DESKTOP=0 turns it off
	MinInterval: 30 * time.Second, // one banner per group per 30s
})
if err != nil {
	return err
}
res, err := n.Send(ctx, notifykit.Notification{
	Title: "sange: 5 findings in billing-api",
	Body:  "dev pushed bca8339 to main; already on the host. Run: sange triage list",
	Level: notifykit.Alert,
	Group: "push:billing-api",
})
```

Let other local programs and scripts post through a running app, securely:

```go
go n.Listen(ctx) // a user-only socket; every message carries a per-start token
```

```bash
sange notify ping -title "Update ready" -body "v1.4.0 is available" -group update
```

The full walkthrough is in [Getting started](docs/getting-started.md); everything
else is in the [documentation](#documentation).

## <a name="documentation"></a>Documentation

Hosted at [opensource.simtabi.com/documentation/simtabi/go-notifykit](https://opensource.simtabi.com/documentation/simtabi/go-notifykit/).

### Guides

- [Installation](docs/installation.md) — add the module, and what each platform needs.
- [Getting started](docs/getting-started.md) — from `go get` to a banner with your icon.
- [Configuration](docs/configuration.md) — every `Config` field and environment switch.
- [Architecture](docs/architecture.md) — backends, the macOS helper app, and why it is built this way.
- [Security](docs/security.md) — what a sender can and cannot do, and how the channel is protected.
- [Standard](docs/standard.md) — the Simtabi rule for any Go program that notifies the user.
- [Release](docs/release.md) — versioning, and rebuilding the macOS helper.

### Reference

- [macOS helper app](docs/tools/helper-app.md) — how `<AppName> Notifier.app` is built, signed and registered.
- [Ping channel](docs/tools/ping.md) — `Listen`, `Ping` and the wire format.
- [`notify` command](docs/tools/notify-command.md) — `RunCLI`: `send`, `ping` and `doctor`.

### Recipes

- [Announce an update](docs/recipes/announce-an-update.md) — one banner when a new version is out.
- [Test code that notifies](docs/recipes/test-code-that-notifies.md) — assert on notifications with a `Recorder`.
- [Adopt it in an app](docs/recipes/adopt-in-an-app.md) — replace a hand-rolled osascript or notify-send call.

## License

MIT. See [LICENSE](LICENSE). Contributions: [CONTRIBUTING.md](CONTRIBUTING.md).
Security reports: [SECURITY.md](SECURITY.md).
