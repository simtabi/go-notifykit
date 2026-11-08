package notifykit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// helperVersion is the CFBundleShortVersionString of the assembled app. Bump
// it with any change to the bundle layout; the stamp already covers content.
const helperVersion = "1.0.0"

// helperBackend posts through "<AppName> Notifier.app", which this package
// assembles from the embedded notifier (helperBinary) and signs ad hoc.
type helperBackend struct {
	n   *Notifier
	exe string
}

func (*helperBackend) Name() string { return "helper" }

// newHelperBackend makes sure the helper app is current and returns it.
func newHelperBackend(n *Notifier) (Backend, error) {
	if len(helperBinary) == 0 {
		return nil, errors.New("this build carries no macOS notifier")
	}
	if n.cfg.AppID == "" {
		return nil, errors.New("Config.AppID is required for the macOS notifier app")
	}
	dir, err := n.stateDir()
	if err != nil {
		return nil, err
	}
	app, err := ensureBundle(dir, n.cfg, helperBinary, codesign, lsregister, n.logf)
	if err != nil {
		return nil, err
	}
	return &helperBackend{n: n, exe: filepath.Join(app, "Contents", "MacOS", "notifier")}, nil
}

type helperPayload struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
	Group    string `json:"group,omitempty"`
	Sound    bool   `json:"sound,omitempty"`
}

func (h *helperBackend) Send(ctx context.Context, nt Notification) error {
	group := nt.Group
	if group != "" {
		group = h.n.cfg.AppName + ":" + group
	}
	in, err := json.Marshal(helperPayload{Title: nt.Title, Subtitle: nt.Subtitle, Body: nt.Body, Group: group, Sound: nt.Sound})
	if err != nil {
		return err
	}
	err = runHelper(ctx, command{name: h.exe, stdin: in})
	var ee *exitError
	if errors.As(err, &ee) && ee.code == 3 {
		return fmt.Errorf("%w: %s", ErrDenied, ee.msg)
	}
	return err
}

// runHelper runs the notifier without killing it at the deadline: on the
// first run it is waiting for the user to answer the permission prompt, and
// killing it then would lose the notification. The notifier exits by itself
// within two minutes.
var runHelper = func(ctx context.Context, c command) error {
	cmd := exec.Command(c.name, c.args...) //nolint:gosec // G204: c.name is the notifier inside the app this package assembled
	cmd.Stdin = bytes.NewReader(c.stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &exitError{code: ee.ExitCode(), msg: strings.TrimSpace(out.String())}
		}
		return err
	case <-ctx.Done():
		// The notifier keeps running and posts once the user answers.
		return fmt.Errorf("%w: %w", errPending, ctx.Err())
	}
}

// signer signs the bundle at path.
type signer func(path string) error

func codesign(path string) error {
	return execRun(context.Background(), command{name: "/usr/bin/codesign", args: []string{"--force", "--sign", "-", "--timestamp=none", path}})
}

// lsregisterPath is LaunchServices' own registration tool, present on every Mac.
const lsregisterPath = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// lsregister tells LaunchServices about the app straight away. Without it the
// notification center cannot find the bundle on its first run ("Failed to
// find or validate client") and refuses it without asking the user.
func lsregister(path string) error {
	return execRun(context.Background(), command{name: lsregisterPath, args: []string{"-f", path}})
}

// ensureBundle writes "<dir>/<AppName> Notifier.app" unless an identical one
// is already there, and returns its path. A new bundle is assembled beside the
// old one, signed, swapped in (so a concurrent Send never runs a half-written
// app) and registered with LaunchServices.
func ensureBundle(dir string, cfg Config, binary []byte, sign, register signer, logf func(string, ...any)) (string, error) {
	path := filepath.Join(dir, cfg.AppName+" Notifier.app")
	var icns []byte
	if len(cfg.Icon) > 0 {
		var err error
		if icns, err = makeICNS(cfg.Icon); err != nil {
			logf("the icon is not usable, the notifier app gets the generic one: %v", err)
			icns = nil
		}
	}
	plist, err := infoPlist(cfg, icns != nil)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	for _, part := range [][]byte{binary, plist, icns} {
		_, _ = fmt.Fprintf(sum, "%d:", len(part))
		sum.Write(part)
	}
	stamp := hex.EncodeToString(sum.Sum(nil))
	if old, err := os.ReadFile(filepath.Join(path, "Contents", "Resources", "notifykit.stamp")); err == nil && strings.TrimSpace(string(old)) == stamp {
		return path, nil
	}

	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	_ = os.RemoveAll(tmp)
	defer func() { _ = os.RemoveAll(tmp) }()
	files := []struct {
		rel  string
		data []byte
		perm os.FileMode
	}{
		{"Contents/MacOS/notifier", binary, 0o755},
		{"Contents/Info.plist", plist, 0o644},
		{"Contents/Resources/notifykit.stamp", []byte(stamp + "\n"), 0o644},
	}
	if icns != nil {
		files = append(files, struct {
			rel  string
			data []byte
			perm os.FileMode
		}{"Contents/Resources/AppIcon.icns", icns, 0o644})
	}
	for _, f := range files {
		p := filepath.Join(tmp, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, f.data, f.perm); err != nil {
			return "", err
		}
	}
	if err := sign(tmp); err != nil {
		return "", fmt.Errorf("signing the notifier app: %w", err)
	}
	if err := os.RemoveAll(path); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	if err := register(path); err != nil {
		logf("could not register the notifier app with LaunchServices; the first notification may be refused: %v", err)
	}
	return path, nil
}

// infoPlist is the helper app's Info.plist. The banner shows the display
// name; the bundle identifier is what System Settings > Notifications lists.
func infoPlist(cfg Config, hasIcon bool) ([]byte, error) {
	if cfg.AppID == "" {
		return nil, errors.New("Config.AppID is required for the macOS notifier app")
	}
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	kv := [][2]string{
		{"CFBundleDevelopmentRegion", "en"},
		{"CFBundleDisplayName", cfg.AppName},
		{"CFBundleExecutable", "notifier"},
		{"CFBundleIdentifier", cfg.AppID + ".notifier"},
		{"CFBundleInfoDictionaryVersion", "6.0"},
		{"CFBundleName", cfg.AppName},
		{"CFBundlePackageType", "APPL"},
		{"CFBundleShortVersionString", helperVersion},
		{"CFBundleVersion", helperVersion},
		{"LSMinimumSystemVersion", "11.0"},
		{"NSUserNotificationAlertStyle", "banner"},
	}
	if hasIcon {
		kv = append(kv, [2]string{"CFBundleIconFile", "AppIcon"})
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, p := range kv {
		fmt.Fprintf(&b, "\t<key>%s</key>\n\t<string>%s</string>\n", p[0], esc(p[1]))
	}
	b.WriteString("\t<key>LSUIElement</key>\n\t<true/>\n</dict>\n</plist>\n")
	return []byte(b.String()), nil
}
