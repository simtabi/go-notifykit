# `notify` command

`notifykit.RunCLI` is a ready-made subcommand with three verbs, `send`, `ping` and `doctor`, which an application mounts as `<app> notify`.

## Mounting it

```go
// plain os.Args
if len(os.Args) > 1 && os.Args[1] == "notify" {
	os.Exit(notifykit.RunCLI(ctx, n, os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
}
```

```go
// cobra
cmd := &cobra.Command{
	Use:                "notify",
	Short:              "Show, ping or check desktop notifications",
	DisableFlagParsing: true,
	RunE: func(c *cobra.Command, args []string) error {
		if code := notifykit.RunCLI(c.Context(), n, args, c.InOrStdin(), c.OutOrStdout(), c.ErrOrStderr()); code != 0 {
			os.Exit(code)
		}
		return nil
	},
}
```

## Verbs

| Verb | Does |
|---|---|
| `send` | shows a notification from this process, subject to the usual switches |
| `ping` | hands it to the running app's `Listen` |
| `doctor` | prints whether delivery is on (and why not), the backend, and the ping socket |

## Flags (`send`, `ping`)

| Flag | Default | Meaning |
|---|---|---|
| `-title`, `-message` | the app name | the message: the first line |
| `-body`, `-description` | none | the description: the text under the title |
| `-subtitle` | none | second line |
| `-icon` | the app's icon | an image for this notification (PNG, JPEG or GIF, absolute path, at most 10 MB) |
| `-open` | nothing | on click, a local `http(s)` URL to open, or this program's own path to run |
| `-group` | none | replace or rate-limit within this group |
| `-level` | `info` | `info`, `warn` or `alert` |
| `-sound` | off | play the default sound |
| `-json` | off | read one notification as JSON from stdin instead |

Exit status: 0 done (including "not shown" because delivery is off), 1 failed, 2 usage.

```bash
echo '{"title":"Backup","body":"42 files","group":"backup"}' | authama notify send -json
sange notify send --message "Deploy finished" --description "billing-api is on v1.4.0" \
  --icon /Users/me/Pictures/deploy.png --open http://127.0.0.1:7822/
authama notify doctor
```

---

[← Docs index](../../README.md#documentation)
