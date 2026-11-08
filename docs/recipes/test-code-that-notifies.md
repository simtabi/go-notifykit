# Test code that notifies

Inject a `Recorder` and assert on what it recorded; no test ever shows a real banner.

```go
func TestPushFindingsNotifyOnce(t *testing.T) {
	rec := &notifykit.Recorder{}
	n, _ := notifykit.New(notifykit.Config{AppName: "sange", Backend: rec})

	reportPush(context.Background(), n, findings(5))

	sent := rec.Sent()
	if len(sent) != 1 || sent[0].Title != "sange: 5 findings in billing-api" {
		t.Fatalf("sent %+v", sent)
	}
}
```

Without a `Backend`, a `Notifier` built inside `go test` is off (`testing.Testing()`), so code paths you never meant to test cannot show a banner either.

An end-to-end test that runs the built binary is a separate process, so set the switch in its environment:

```go
cmd.Env = append(os.Environ(), "NOTIFYKIT_DESKTOP=0")
```

---

[← Docs index](../../README.md#documentation)
