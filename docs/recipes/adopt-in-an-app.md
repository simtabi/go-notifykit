# Adopt it in an app

Replace a hand-rolled `osascript`, `terminal-notifier`, `notify-send` or PowerShell call with one `Notifier`.

1. Find the old calls:

   ```bash
   grep -rnE 'osascript|terminal-notifier|notify-send|ToastNotification' --include='*.go' src | grep -v _test.go
   ```

2. Build one `Notifier` where the app wires its dependencies:

   ```go
   n, err := notifykit.New(notifykit.Config{
   	AppName:   "authama",
   	AppID:     "com.simtabi.authama",
   	Icon:      brand.IconPNG, // the web UI logo mark at 1024px
   	EnvPrefix: "AUTHAMA",
   	Disabled:  !cfg.Notifications.Desktop,
   	MinInterval: 30 * time.Second,
   })
   ```

3. Pass it to whatever notified, and make each event one summarised notification with a `Group`.
4. Keep the old function, deprecated and forwarding to the `Notifier`, until its callers are gone.
5. Mount `notifykit.RunCLI` as `<app> notify`, and run `Listen` in any long-running mode.
6. In end-to-end tests that run the binary, set `NOTIFYKIT_DESKTOP=0`.

The full rule is in [Standard](../standard.md).

---

[← Docs index](../../README.md#documentation)
