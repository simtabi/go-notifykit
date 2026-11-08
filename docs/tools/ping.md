# Ping channel

Three functions let one local process show a notification through another: `Notifier.Listen`, `Notifier.Endpoint` and `Ping`.

## Serve

```go
go func() {
	if err := n.Listen(ctx); err != nil {
		log.Printf("notification listener: %v", err)
	}
}()
```

`Listen` runs until `ctx` ends. Every start writes a fresh token and every stop deletes it, so a ping can only reach a running app.

## Send

```go
ep, _ := n.Endpoint() // or build an Endpoint from the app's known state directory
res, err := notifykit.Ping(ctx, ep, notifykit.Notification{
	Title: "Deploy finished",
	Body:  "billing-api is on v1.4.0",
	Group: "deploy",
})
```

From a shell, through the app's mounted command:

```bash
sange notify ping -title "Deploy finished" -body "billing-api is on v1.4.0" -group deploy
```

## Files

| File | Mode | Content |
|---|---|---|
| `<state>/notify.sock` | `0600` | the Unix-domain socket |
| `<state>/notify.token` | `0600` | 64 hex characters, written fresh on every start |
| `<state>/` | `0700` | the directory both live in |

Unix socket paths are limited to about 100 bytes, so `Listen` refuses a longer path and says to shorten `Config.StateDir`.

## Wire format

One line each way, UTF-8 JSON, at most 16 KiB, within five seconds.

```json
{"token":"<64 hex>","notification":{"title":"Deploy finished","body":"billing-api is on v1.4.0","group":"deploy","level":0}}
```

```json
{"ok":true,"backend":"helper"}
{"ok":false,"error":"wrong token"}
```

The notification may carry every field a local one can, `icon` and `open`
included, and they are checked the same way. The reply may also carry `skipped`, `suppressed` or `pending`, with the same meaning as in `Result`. Unknown fields are refused. See [Security](../security.md) for the reasoning.

---

[← Docs index](../../README.md#documentation)
