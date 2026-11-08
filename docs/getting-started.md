# Getting started

From `go get` to a banner that carries your program's name and icon, in five steps.

## 1. Describe the app

```go
//go:embed icon.png
var iconPNG []byte

n, err := notifykit.New(notifykit.Config{
	AppName:   "sange",            // what the banner says
	AppID:     "com.simtabi.sange", // what System Settings lists (+ ".notifier")
	Icon:      iconPNG,             // square PNG, 1024px is best
	EnvPrefix: "SANGE",             // SANGE_NOTIFY_DESKTOP / SANGE_NOTIFY_BACKEND
})
```

`New` does no I/O. Create one `Notifier` per program and share it; it is safe for
concurrent use.

## 2. Send

```go
res, err := n.Send(ctx, notifykit.Notification{
	Title: "Backup finished",
	Body:  "42 files, 1.2 GB",
	Group: "backup",
})
switch {
case err != nil:
	log.Printf("notification failed: %v", err) // never fatal: it is a courtesy
case res.Skipped != "":
	// off: under go test, in CI, or switched off; res.Skipped says which
case res.Pending:
	// macOS is asking the user to allow notifications; it shows once allowed
}
```

## 3. Try it

`make try` in this repository posts a real banner from a throwaway app called
`notifykit-try`, so you can see the result before wiring it in. Tests never show
a banner; see [Test code that notifies](recipes/test-code-that-notifies.md).

## 4. Give users a switch

Read your own config and pass `Disabled`, and document the environment switches
(`<PREFIX>_NOTIFY_DESKTOP=0`). See [Configuration](configuration.md).

## 5. Mount the `notify` command

```go
case "notify":
	os.Exit(notifykit.RunCLI(ctx, n, args[1:], os.Stdin, os.Stdout, os.Stderr))
```

That gives `yourapp notify send`, `notify ping` and `notify doctor`. If the program
runs for a while (a server, a daemon, a watcher), also run `n.Listen(ctx)` so other
programs can `ping` it. See [Ping channel](tools/ping.md).

---

[← Docs index](../README.md#documentation)
