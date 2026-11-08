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
| `-title` | the app name | first line |
| `-subtitle` | none | second line |
| `-body` | none | the message |
| `-group` | none | replace or rate-limit within this group |
| `-level` | `info` | `info`, `warn` or `alert` |
| `-sound` | off | play the default sound |
| `-json` | off | read one notification as JSON from stdin instead |

Exit status: 0 done (including "not shown" because delivery is off), 1 failed, 2 usage.

```bash
echo '{"title":"Backup","body":"42 files","group":"backup"}' | authama notify send -json
authama notify doctor
```

---

[← Docs index](../../README.md#documentation)
