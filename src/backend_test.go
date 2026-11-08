package notifykit

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// fakePlatform pins the OS and the programs on PATH, and records commands.
func fakePlatform(t *testing.T, goos string, onPath ...string) *[]command {
	t.Helper()
	oldOS, oldLook, oldRun := osName, lookPath, run
	var ran []command
	osName = func() string { return goos }
	lookPath = func(name string) (string, error) {
		if slices.Contains(onPath, name) {
			return "/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	run = func(_ context.Context, c command) error { ran = append(ran, c); return nil }
	t.Cleanup(func() { osName, lookPath, run = oldOS, oldLook, oldRun })
	return &ran
}

// on returns a Notifier with desktop delivery forced on.
func on(t *testing.T, cfg Config, kv ...string) *Notifier {
	t.Helper()
	if cfg.AppName == "" {
		cfg.AppName = "demo"
	}
	cfg.Getenv = env(append([]string{"NOTIFYKIT_DESKTOP", "1"}, kv...)...)
	n, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOsascriptPassesTextAsDataNeverAsScript(t *testing.T) {
	c := osascriptCmd(Notification{Title: `he said "quit" & end run`, Subtitle: "s", Body: `do shell script "rm"`})
	script := strings.Join(c.args[:6], " ")
	if strings.Contains(script, "quit") || strings.Contains(script, "rm") {
		t.Fatalf("user text reached the script: %q", script)
	}
	if got := c.args[6:]; !slices.Equal(got, []string{`he said "quit" & end run`, "s", `do shell script "rm"`}) {
		t.Fatalf("argv = %q", got)
	}
}

func TestNotifySendEscapesMarkupAndMapsUrgency(t *testing.T) {
	c := notifySendCmd("/bin/notify-send", "demo", "/s/icon.png", Notification{Title: "--help", Subtitle: "in repo", Body: "<b>a & b</b>", Level: Alert})
	want := []string{"--app-name=demo", "--urgency=critical", "--icon=/s/icon.png", "--", "--help", "in repo: &lt;b&gt;a &amp; b&lt;/b&gt;"}
	if !slices.Equal(c.args, want) {
		t.Fatalf("args = %q\nwant   %q", c.args, want)
	}
	if c := notifySendCmd("x", "demo", "", Notification{Title: "t"}); slices.ContainsFunc(c.args, func(a string) bool { return strings.HasPrefix(a, "--icon") }) {
		t.Fatalf("no icon still passed --icon: %q", c.args)
	}
}

func TestTerminalNotifierGroupsByApp(t *testing.T) {
	c := terminalNotifierCmd("/bin/terminal-notifier", "demo", Notification{Title: "t", Group: "push", Sound: true})
	if !slices.Equal(c.args, []string{"-title", "t", "-message", " ", "-group", "demo:push", "-sound", "default"}) {
		t.Fatalf("args = %q", c.args)
	}
}

func TestPowershellKeepsTextOutOfTheScript(t *testing.T) {
	c := powershellCmd("powershell.exe", Notification{Title: "'; Remove-Item", Body: "b"})
	if strings.Contains(strings.Join(c.args, " "), "Remove-Item") {
		t.Fatal("title reached the script")
	}
	if !slices.Equal(c.env, []string{"NOTIFYKIT_TITLE='; Remove-Item", "NOTIFYKIT_BODY=b"}) {
		t.Fatalf("env = %q", c.env)
	}
}

func TestBackendChoicePerPlatform(t *testing.T) {
	cases := []struct {
		goos   string
		path   []string
		cfg    Config
		force  string
		want   string
		errIs  error
		errHas string
	}{
		// No AppID, so the helper app cannot be assembled and macOS falls back.
		{goos: "darwin", path: []string{"terminal-notifier"}, want: "terminal-notifier"},
		{goos: "darwin", want: "osascript"},
		{goos: "darwin", path: []string{"terminal-notifier"}, force: "osascript", want: "osascript"},
		{goos: "darwin", force: "terminal-notifier", errIs: ErrUnsupported},
		{goos: "darwin", force: "helper", errHas: "AppID"},
		{goos: "linux", path: []string{"notify-send"}, want: "notify-send"},
		{goos: "linux", errIs: ErrUnsupported},
		{goos: "freebsd", path: []string{"notify-send"}, want: "notify-send"},
		{goos: "windows", path: []string{"powershell.exe"}, want: "powershell"},
		{goos: "plan9", errIs: ErrUnsupported},
		{goos: "linux", path: []string{"notify-send"}, force: "none", errIs: ErrUnsupported},
		{goos: "linux", force: "carrier-pigeon", errHas: "unknown backend"},
	}
	for _, c := range cases {
		t.Run(c.goos+"/"+c.force+"/"+strings.Join(c.path, ","), func(t *testing.T) {
			fakePlatform(t, c.goos, c.path...)
			n := on(t, c.cfg, "NOTIFYKIT_BACKEND", c.force)
			got, err := n.Backend()
			switch {
			case c.errIs != nil || c.errHas != "":
				if err == nil || (c.errIs != nil && !errors.Is(err, c.errIs)) || !strings.Contains(err.Error(), c.errHas) {
					t.Fatalf("Backend() = %q, %v; want error %v %q", got, err, c.errIs, c.errHas)
				}
			case err != nil || got != c.want:
				t.Fatalf("Backend() = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestAppBackendSwitchBeatsGlobal(t *testing.T) {
	fakePlatform(t, "darwin", "terminal-notifier")
	n := on(t, Config{EnvPrefix: "DEMO"}, "DEMO_NOTIFY_BACKEND", "osascript", "NOTIFYKIT_BACKEND", "terminal-notifier")
	if got, err := n.Backend(); err != nil || got != "osascript" {
		t.Fatalf("Backend() = %q, %v", got, err)
	}
}

func TestSendRunsTheChosenCommand(t *testing.T) {
	ran := fakePlatform(t, "linux", "notify-send")
	n := on(t, Config{StateDir: t.TempDir(), Icon: testPNG(t, 64)})
	res, err := n.Send(context.Background(), Notification{Title: "t", Body: "b"})
	if err != nil || res.Backend != "notify-send" || len(*ran) != 1 {
		t.Fatalf("Send = %+v, %v, ran %d", res, err, len(*ran))
	}
	if !slices.ContainsFunc((*ran)[0].args, func(a string) bool { return strings.HasPrefix(a, "--icon=") && strings.HasSuffix(a, "icon.png") }) {
		t.Fatalf("the icon was not passed: %q", (*ran)[0].args)
	}
}
