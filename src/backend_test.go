package notifykit

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
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

func TestFallbacksCarryTheImageAndOnlyURLClicks(t *testing.T) {
	c := terminalNotifierCmd("tn", "demo", Notification{Title: "t", Icon: "/s/images/a.png", Open: "http://127.0.0.1:7822/"})
	if !slices.Contains(c.args, "-contentImage") || !slices.Contains(c.args, "http://127.0.0.1:7822/") {
		t.Fatalf("args = %q", c.args)
	}
	// A path click is never handed to terminal-notifier, whose -execute is a shell.
	c = terminalNotifierCmd("tn", "demo", Notification{Title: "t", Open: "/usr/local/bin/demo"})
	if slices.Contains(c.args, "-open") || slices.Contains(c.args, "-execute") {
		t.Fatalf("args = %q", c.args)
	}
	c = notifySendCmd("ns", "demo", "/s/icon.png", Notification{Title: "t", Icon: "/s/images/a.png"})
	if !slices.Contains(c.args, "--icon=/s/images/a.png") {
		t.Fatalf("the notification's image did not win: %q", c.args)
	}
}

// O-04: terminal-notifier and osascript both read options from their
// arguments, so a title, subtitle, body or group starting with "-" must reach
// them as text. It keeps its characters, behind an invisible word joiner.
func TestNoValueIsReadAsAnOption(t *testing.T) {
	nt := Notification{Title: "-execute", Subtitle: "-open", Body: "--sender", Group: "x"}
	tn := terminalNotifierCmd("tn", "-app", nt)
	for i := 1; i < len(tn.args); i += 2 { // the values, after each option name
		if strings.HasPrefix(tn.args[i], "-") {
			t.Errorf("terminal-notifier value %q would be read as an option (args %q)", tn.args[i], tn.args)
		}
	}
	if tn.args[1] != wordJoiner+"-execute" || tn.args[3] != wordJoiner+"--sender" {
		t.Fatalf("the text changed: %q", tn.args)
	}
	osa := osascriptCmd(nt)
	for _, v := range osa.args[6:] {
		if strings.HasPrefix(v, "-") {
			t.Errorf("osascript argument %q would be read as an option", v)
		}
	}
	if osa.args[6] != wordJoiner+"-execute" {
		t.Fatalf("the text changed: %q", osa.args)
	}
	if notOption("plain - text") != "plain - text" {
		t.Fatal("a value not starting with - was changed")
	}
}

func TestTerminalNotifierGroupsByApp(t *testing.T) {
	c := terminalNotifierCmd("/bin/terminal-notifier", "demo", Notification{Title: "t", Group: "push", Sound: true})
	if !slices.Equal(c.args, []string{"-title", "t", "-message", " ", "-group", "demo:push", "-sound", "default"}) {
		t.Fatalf("args = %q", c.args)
	}
}

func TestPowershellKeepsTextOutOfTheScript(t *testing.T) {
	c := powershellCmd("powershell.exe", `C:\app\icon.png`, Notification{Title: "'; Remove-Item", Body: "b"})
	if strings.Contains(strings.Join(c.args, " "), "Remove-Item") {
		t.Fatal("title reached the script")
	}
	if !slices.Equal(c.env, []string{"NOTIFYKIT_TITLE='; Remove-Item", "NOTIFYKIT_BODY=b", `NOTIFYKIT_IMAGE=C:\app\icon.png`}) {
		t.Fatalf("env = %q", c.env)
	}
	// A notification's own image wins over the app's.
	c = powershellCmd("powershell.exe", `C:\app\icon.png`, Notification{Title: "t", Icon: `C:\s\images\x.png`})
	if c.env[2] != `NOTIFYKIT_IMAGE=C:\s\images\x.png` {
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
		// No AppID, so the helper app cannot be assembled and macOS falls back,
		// to osascript even when terminal-notifier is installed.
		{goos: "darwin", path: []string{"terminal-notifier"}, want: "osascript"},
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

// The owner's rule: every app ships its own helper and adds no third-party
// notifier. So whatever is installed, nothing forced, the automatic choice on
// each platform is the app's own helper or a program the OS itself provides.
// This fails if a fallback ever reaches for a third-party program again.
func TestAutomaticChoiceUsesOnlyTheAppsHelperOrTheOS(t *testing.T) {
	everything := []string{"terminal-notifier", "alerter", "growlnotify", "notify-send", "powershell.exe", "osascript", "dunstify", "kdialog", "zenity"}
	allowed := map[string]map[string]bool{
		"darwin":  {"helper": true, "osascript": true},
		"linux":   {"notify-send": true},
		"freebsd": {"notify-send": true},
		"windows": {"powershell": true},
	}
	oldSign, oldReg := signApp, registerApp
	signApp, registerApp = func(string) error { return nil }, func(string) error { return nil }
	t.Cleanup(func() { signApp, registerApp = oldSign, oldReg })
	inspected := 0
	for goos, ok := range allowed {
		for _, appID := range []string{"", "com.example.demo"} { // without and with a helper app
			ran := fakePlatform(t, goos, everything...)
			n := on(t, Config{AppID: appID, StateDir: t.TempDir()})
			got, err := n.Backend()
			if err != nil || !ok[got] {
				t.Errorf("%s (AppID %q): chose %q (%v); allowed %v", goos, appID, got, err, ok)
			}
			if got != "helper" {
				_, _ = n.Send(context.Background(), Notification{Title: "t"})
				for _, c := range *ran {
					if name := filepath.Base(c.name); name != "osascript" && name != "notify-send" && name != "powershell.exe" {
						t.Errorf("%s: ran %q, which neither the app nor the OS provides", goos, c.name)
					}
				}
			}
			inspected++
		}
	}
	if inspected < 8 {
		t.Fatalf("inspected %d platform choices; expected 8", inspected)
	}
	// Named explicitly, the deprecated backend still works for a caller that relies on it.
	fakePlatform(t, "darwin", "terminal-notifier")
	if got, err := on(t, Config{}, "NOTIFYKIT_BACKEND", "terminal-notifier").Backend(); err != nil || got != "terminal-notifier" {
		t.Fatalf("explicit terminal-notifier: %q, %v", got, err)
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
