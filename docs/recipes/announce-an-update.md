# Announce an update

Show one banner when a newer version is out, and stay quiet about it until the next one.

```go
func announce(ctx context.Context, n *notifykit.Notifier, current, latest string) {
	if latest == "" || latest == current {
		return
	}
	_, _ = n.Send(ctx, notifykit.Notification{
		Title: "authama " + latest + " is available",
		Body:  "You have " + current + ". Run: authama self-update",
		Group: "update",
	})
}
```

The `update` group means a newer announcement replaces the older one in Notification Center. With `Config.MinInterval`, a check that runs every few minutes shows the banner once per interval, not on every check. Record the last version announced in the app's own state if it should show only once per release.

See [Configuration](../configuration.md) for `Group` and `MinInterval`.

---

[← Docs index](../../README.md#documentation)
