package notifykit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// env is a fake environment.
func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

// noRun fails the test if anything would be executed.
func noRun(t *testing.T) {
	t.Helper()
	old, oldH := run, runHelper
	run = func(context.Context, command) error { t.Error("a desktop notifier was executed"); return nil }
	runHelper = run
	t.Cleanup(func() { run, runHelper = old, oldH })
}

// The guard this package exists for: under `go test`, with nothing set, no
// notifier program runs and Send reports why.
func TestDesktopIsOffUnderGoTest(t *testing.T) {
	noRun(t)
	n, err := New(Config{AppName: "demo", AppID: "com.example.demo", EnvPrefix: "DEMO", Getenv: env()})
	if err != nil {
		t.Fatal(err)
	}
	ok, why := n.Enabled()
	if ok || !strings.Contains(why, "go test") {
		t.Fatalf("Enabled() = %v, %q; want off because of go test", ok, why)
	}
	res, err := n.Send(context.Background(), Notification{Title: "t", Body: "b"})
	if err != nil || res.Skipped == "" || res.Backend != "" {
		t.Fatalf("Send = %+v, %v; want skipped", res, err)
	}
}

func TestSwitches(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		env     []string
		test    bool
		want    bool
		whyPart string
	}{
		{"off under go test", Config{}, nil, true, false, "go test"},
		{"global forces on under test", Config{}, []string{"NOTIFYKIT_DESKTOP", "1"}, true, true, ""},
		{"app switch beats global", Config{EnvPrefix: "DEMO"}, []string{"DEMO_NOTIFY_DESKTOP", "off", "NOTIFYKIT_DESKTOP", "1"}, false, false, "DEMO_NOTIFY_DESKTOP"},
		{"app switch forces on", Config{EnvPrefix: "demo"}, []string{"DEMO_NOTIFY_DESKTOP", "true", "NOTIFYKIT_DESKTOP", "0"}, true, true, ""},
		{"global off", Config{}, []string{"NOTIFYKIT_DESKTOP", "no"}, false, false, "NOTIFYKIT_DESKTOP"},
		{"junk value is ignored", Config{}, []string{"NOTIFYKIT_DESKTOP", "maybe"}, false, true, ""},
		{"off in CI", Config{}, []string{"CI", "true"}, false, false, "CI"},
		{"CI=false is not CI", Config{}, []string{"CI", "false"}, false, true, ""},
		{"on by default outside tests", Config{}, nil, false, true, ""},
		{"Disabled beats everything", Config{Disabled: true}, []string{"NOTIFYKIT_DESKTOP", "1"}, false, false, "config"},
		{"a custom backend runs under test", Config{Backend: &Recorder{}}, nil, true, true, ""},
		{"Disabled stops a custom backend", Config{Backend: &Recorder{}, Disabled: true}, nil, true, false, "config"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := c.cfg
			cfg.AppName, cfg.Getenv = "demo", env(c.env...)
			n, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			n.testing = func() bool { return c.test }
			ok, why := n.Enabled()
			if ok != c.want || !strings.Contains(why, c.whyPart) {
				t.Fatalf("Enabled() = %v, %q; want %v containing %q", ok, why, c.want, c.whyPart)
			}
		})
	}
}

func TestRecorderSeesSanitizedNotifications(t *testing.T) {
	rec := &Recorder{}
	n, err := New(Config{AppName: "demo", Backend: rec})
	if err != nil {
		t.Fatal(err)
	}
	res, err := n.Send(context.Background(), Notification{Title: "a\nb\x1b[31m", Body: strings.Repeat("x", 500)})
	if err != nil || res.Backend != "recorder" {
		t.Fatalf("Send = %+v, %v", res, err)
	}
	got := rec.Sent()
	if len(got) != 1 || got[0].Title != "a b [31m" || len([]rune(got[0].Body)) != maxField || !strings.HasSuffix(got[0].Body, "…") {
		t.Fatalf("recorded %+v", got)
	}
	// An empty title falls back to the app name.
	_, _ = n.Send(context.Background(), Notification{Body: "x"})
	if got := rec.Sent(); got[1].Title != "demo" {
		t.Fatalf("empty title became %q", got[1].Title)
	}
}

func TestMinIntervalSuppressesRepeatsInAGroup(t *testing.T) {
	rec := &Recorder{}
	n, _ := New(Config{AppName: "demo", Backend: rec, MinInterval: time.Minute})
	now := time.Unix(1_000, 0)
	n.now = func() time.Time { return now }
	send := func(group string) Result {
		res, err := n.Send(context.Background(), Notification{Title: "t", Group: group})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if send("push").Suppressed {
		t.Fatal("the first in a group was suppressed")
	}
	if !send("push").Suppressed {
		t.Fatal("a repeat within the interval was sent")
	}
	if send("other").Suppressed || send("").Suppressed || send("").Suppressed {
		t.Fatal("another group, or no group, was suppressed")
	}
	now = now.Add(time.Minute)
	if send("push").Suppressed {
		t.Fatal("a repeat after the interval was suppressed")
	}
	if len(rec.Sent()) != 5 {
		t.Fatalf("sent %d, want 5", len(rec.Sent()))
	}
}

func TestNewValidatesConfig(t *testing.T) {
	for _, cfg := range []Config{
		{},
		{AppName: "a/b"},
		{AppName: "ok", AppID: "nodots"},
		{AppName: "ok", AppID: "com..x"},
		{AppName: "ok", AppID: "com.ex ample"},
		{AppName: "ok", EnvPrefix: "BAD-PREFIX"},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%+v) accepted it", cfg)
		}
	}
	if _, err := New(Config{AppName: "My App", AppID: "com.example.my-app", EnvPrefix: "MY_APP"}); err != nil {
		t.Fatal(err)
	}
}

func TestStateDir(t *testing.T) {
	n, _ := New(Config{AppName: "My App", Getenv: env("XDG_STATE_HOME", "/state")})
	if d, _ := n.stateDir(); d != filepath.Join("/state", "my-app", "notifykit") {
		t.Fatalf("stateDir = %q", d)
	}
	n, _ = New(Config{AppName: "x", StateDir: "/custom"})
	if d, _ := n.stateDir(); d != "/custom" {
		t.Fatalf("stateDir = %q", d)
	}
}

func TestPermissionPromptIsPendingNotAnError(t *testing.T) {
	n, _ := New(Config{AppName: "demo", Backend: pendingBackend{}})
	res, err := n.Send(context.Background(), Notification{Title: "t"})
	if err != nil || !res.Pending || res.Backend != "pending" {
		t.Fatalf("Send = %+v, %v; want pending", res, err)
	}
}

type pendingBackend struct{}

func (pendingBackend) Name() string { return "pending" }
func (pendingBackend) Send(context.Context, Notification) error {
	return fmt.Errorf("%w: %w", errPending, context.DeadlineExceeded)
}

func TestBackendErrorsAreNamed(t *testing.T) {
	n, _ := New(Config{AppName: "demo", Backend: failing{}})
	if _, err := n.Send(context.Background(), Notification{Title: "t"}); err == nil || !strings.HasPrefix(err.Error(), "failing: ") {
		t.Fatalf("err = %v", err)
	}
}

type failing struct{}

func (failing) Name() string                             { return "failing" }
func (failing) Send(context.Context, Notification) error { return errors.New("boom") }

func TestPickIsDoneOnce(t *testing.T) {
	var calls atomic.Int32
	b := countingBackend{&calls}
	n, _ := New(Config{AppName: "demo", Backend: b})
	for i := 0; i < 3; i++ {
		if name, err := n.Backend(); err != nil || name != "counting" {
			t.Fatal(name, err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("Name called %d times", calls.Load())
	}
}

type countingBackend struct{ n *atomic.Int32 }

func (c countingBackend) Name() string                           { c.n.Add(1); return "counting" }
func (countingBackend) Send(context.Context, Notification) error { return nil }
