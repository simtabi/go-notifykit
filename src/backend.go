package notifykit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Backend delivers a notification. Implement it to send somewhere else, or
// use a [Recorder] in tests.
type Backend interface {
	Name() string
	Send(ctx context.Context, nt Notification) error
}

// Recorder is a Backend that keeps what it is sent, for tests.
type Recorder struct {
	mu   sync.Mutex
	sent []Notification
}

// Name is "recorder".
func (*Recorder) Name() string { return "recorder" }

// Send records nt.
func (r *Recorder) Send(_ context.Context, nt Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, nt)
	return nil
}

// Sent returns a copy of everything recorded so far.
func (r *Recorder) Sent() []Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Notification(nil), r.sent...)
}

// command is one program run with arguments, extra environment and stdin.
type command struct {
	name  string
	args  []string
	env   []string
	stdin []byte
}

// runner runs a command and waits for it; errors carry a little output.
type runner func(ctx context.Context, c command) error

func execRun(ctx context.Context, c command) error {
	cmd := exec.CommandContext(ctx, c.name, c.args...) //nolint:gosec // G204: c.name is a fixed notifier binary chosen by this package; args are data, never a shell line
	if len(c.env) > 0 {
		cmd.Env = append(os.Environ(), c.env...)
	}
	if c.stdin != nil {
		cmd.Stdin = bytes.NewReader(c.stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &exitError{code: ee.ExitCode(), msg: msg}
		}
		if msg != "" {
			return fmt.Errorf("%s: %w: %s", filepath.Base(c.name), err, msg)
		}
		return fmt.Errorf("%s: %w", filepath.Base(c.name), err)
	}
	return nil
}

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("exit status %d", e.code)
	}
	return fmt.Sprintf("exit status %d: %s", e.code, e.msg)
}

// cmdBackend is a backend that is one command per notification.
type cmdBackend struct {
	name  string
	build func(Notification) (command, error)
	run   runner
}

func (b *cmdBackend) Name() string { return b.name }

func (b *cmdBackend) Send(ctx context.Context, nt Notification) error {
	c, err := b.build(nt)
	if err != nil {
		return err
	}
	return b.run(ctx, c)
}

// Seams the tests replace.
var (
	osName           = func() string { return runtime.GOOS }
	lookPath         = exec.LookPath
	run       runner = execRun
	underTest        = testing.Testing
)

// backendNames are the values NOTIFYKIT_BACKEND accepts.
var backendNames = []string{"helper", "terminal-notifier", "osascript", "notify-send", "powershell", "none"}

// platformBackend chooses the backend for this OS, or the one force names.
func platformBackend(n *Notifier, force string) (Backend, error) {
	if force != "" {
		known := false
		for _, b := range backendNames {
			known = known || b == force
		}
		if !known {
			return nil, fmt.Errorf("notifykit: unknown backend %q (want one of %s)", force, strings.Join(backendNames, ", "))
		}
		if force == "none" {
			return nil, ErrUnsupported
		}
	}
	try := func(name string) bool { return force == "" || force == name }
	switch osName() {
	case "darwin":
		if try("helper") {
			b, err := newHelperBackend(n)
			if err == nil {
				return b, nil
			}
			n.logf("the %s notifier app is unavailable, falling back: %v", n.cfg.AppName, err)
			if force != "" {
				return nil, err
			}
		}
		if try("terminal-notifier") {
			if bin, err := lookPath("terminal-notifier"); err == nil {
				return &cmdBackend{name: "terminal-notifier", run: run, build: func(nt Notification) (command, error) {
					return terminalNotifierCmd(bin, n.cfg.AppName, nt), nil
				}}, nil
			} else if force != "" {
				return nil, fmt.Errorf("terminal-notifier not found: %w", ErrUnsupported)
			}
		}
		if try("osascript") {
			return &cmdBackend{name: "osascript", run: run, build: func(nt Notification) (command, error) {
				return osascriptCmd(nt), nil
			}}, nil
		}
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		if try("notify-send") {
			bin, err := lookPath("notify-send")
			if err != nil {
				return nil, fmt.Errorf("notify-send not found: %w", ErrUnsupported)
			}
			icon := n.iconFile()
			return &cmdBackend{name: "notify-send", run: run, build: func(nt Notification) (command, error) {
				return notifySendCmd(bin, n.cfg.AppName, icon, nt), nil
			}}, nil
		}
	case "windows":
		if try("powershell") {
			bin, err := lookPath("powershell.exe")
			if err != nil {
				return nil, fmt.Errorf("powershell.exe not found: %w", ErrUnsupported)
			}
			icon := n.iconFile()
			return &cmdBackend{name: "powershell", run: run, build: func(nt Notification) (command, error) {
				return powershellCmd(bin, icon, nt), nil
			}}, nil
		}
	}
	return nil, ErrUnsupported
}

// iconFile writes Config.Icon as icon.png in the state directory, once, and
// returns its path; "" when there is no icon or it cannot be written.
func (n *Notifier) iconFile() string {
	if len(n.cfg.Icon) == 0 {
		return ""
	}
	dir, err := n.stateDir()
	if err != nil {
		n.logf("no state directory for the icon: %v", err)
		return ""
	}
	p := filepath.Join(dir, "icon.png")
	if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, n.cfg.Icon) {
		return p
	}
	if err := writeFileAtomic(p, n.cfg.Icon, 0o600); err != nil {
		n.logf("cannot write the icon: %v", err)
		return ""
	}
	return p
}

// osascriptCmd shows nt through AppleScript. Title, subtitle and body travel
// as run-handler arguments, never as script text, so no quoting in them can
// change the script. macOS attributes these banners to Script Editor.
func osascriptCmd(nt Notification) command {
	return command{name: "/usr/bin/osascript", args: []string{
		"-e", "on run argv",
		"-e", "display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv)",
		"-e", "end run",
		// osascript keeps reading options after its script, so a value that
		// starts with "-" would be taken for one.
		notOption(nt.Title), notOption(nt.Subtitle), notOption(nt.Body),
	}}
}

// wordJoiner is U+2060: invisible, and not a "-".
const wordJoiner = "\u2060"

// notOption stops a value from being read as a command-line option by the
// notifier it is passed to: one starting with "-" gets an invisible word
// joiner in front, so the banner reads the same and the parser sees text.
func notOption(s string) string {
	if strings.HasPrefix(s, "-") {
		return wordJoiner + s
	}
	return s
}

// terminalNotifierCmd uses terminal-notifier, whose -group replaces an earlier
// banner in the same group.
func terminalNotifierCmd(bin, app string, nt Notification) command {
	// terminal-notifier reads "-name value" pairs, so a value starting with
	// "-" would be taken for an option: notOption keeps every value text.
	args := []string{"-title", notOption(nt.Title), "-message", notOption(nonEmpty(nt.Body, " "))}
	if nt.Subtitle != "" {
		args = append(args, "-subtitle", notOption(nt.Subtitle))
	}
	if nt.Group != "" {
		args = append(args, "-group", notOption(app+":"+nt.Group))
	}
	if nt.Sound {
		args = append(args, "-sound", "default")
	}
	if nt.Icon != "" {
		args = append(args, "-contentImage", nt.Icon)
	}
	// Only a URL: terminal-notifier's -execute runs a shell, which is never used.
	if strings.HasPrefix(nt.Open, "http://") || strings.HasPrefix(nt.Open, "https://") {
		args = append(args, "-open", nt.Open)
	}
	return command{name: bin, args: args}
}

// notifySendCmd uses notify-send, which reads a small markup subset in the
// body; "--" ends its options.
func notifySendCmd(bin, app, icon string, nt Notification) command {
	urgency := map[Level]string{Info: "low", Warn: "normal", Alert: "critical"}[nt.Level]
	args := []string{"--app-name=" + app, "--urgency=" + urgency}
	if nt.Icon != "" {
		icon = nt.Icon // this notification's own image wins over the app's
	}
	if icon != "" {
		args = append(args, "--icon="+icon)
	}
	body := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(joined(nt))
	return command{name: bin, args: append(args, "--", nt.Title, body)}
}

// powershellCmd shows a Windows toast. Title, body and image path travel in
// the environment and are XML-escaped by PowerShell, so they never become
// script text.
func powershellCmd(bin, appIcon string, nt Notification) command {
	image := nt.Icon
	if image == "" {
		image = appIcon
	}
	return command{
		name: bin,
		args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", windowsToast},
		env:  []string{"NOTIFYKIT_TITLE=" + nt.Title, "NOTIFYKIT_BODY=" + joined(nt), "NOTIFYKIT_IMAGE=" + image},
	}
}

const windowsToast = `$ErrorActionPreference = 'Stop'
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$t = [System.Security.SecurityElement]::Escape($env:NOTIFYKIT_TITLE)
$b = [System.Security.SecurityElement]::Escape($env:NOTIFYKIT_BODY)
$i = ''
if ($env:NOTIFYKIT_IMAGE) { $i = '<image placement="appLogoOverride" src="' + [System.Security.SecurityElement]::Escape(([System.Uri]$env:NOTIFYKIT_IMAGE).AbsoluteUri) + '"/>' }
$x = New-Object Windows.Data.Xml.Dom.XmlDocument
$x.LoadXml("<toast><visual><binding template=""ToastGeneric""><text>$t</text><text>$b</text>$i</binding></visual></toast>")
$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($x))`

func nonEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// writeFileAtomic writes through a temporary file and a rename, so a reader
// never sees a half-written file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
