package notifykit

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// helperVersion is the CFBundleShortVersionString of the assembled app. Bump
// it with any change to the bundle layout; content is compared file by file.
const helperVersion = "1.0.0"

// helperBackend posts through "<AppName> Notifier.app", which this package
// assembles from the embedded notifier (helperBinary) and signs ad hoc.
type helperBackend struct {
	n        *Notifier
	app      string // the bundle, for re-registering it
	exe      string
	signed   []byte // the notifier this build installed, signed
	rebuild  func() (string, []byte, error)
	register signer
	fallback Backend // osascript, for when macOS refuses the app

	mu sync.Mutex
}

func (*helperBackend) Name() string { return "helper" }

// newHelperBackend makes sure the helper app is current and returns it.
func newHelperBackend(n *Notifier) (Backend, error) {
	if n.cfg.AppID == "" {
		return nil, errors.New("Config.AppID is required for the macOS notifier app")
	}
	if len(helperBinary) == 0 {
		return nil, errors.New("this build carries no macOS notifier")
	}
	dir, err := n.stateDir()
	if err != nil {
		return nil, err
	}
	rebuild := func() (string, []byte, error) {
		return ensureBundle(dir, n.cfg, helperBinary, codesign, lsregister, n.logf)
	}
	app, signed, err := rebuild()
	if err != nil {
		return nil, err
	}
	return &helperBackend{
		n: n, app: app, exe: filepath.Join(app, "Contents", "MacOS", "notifier"),
		signed: signed, rebuild: rebuild, register: lsregister,
		fallback: &cmdBackend{name: "osascript", run: run, build: func(nt Notification) (command, error) { return osascriptCmd(nt), nil }},
	}, nil
}

type helperPayload struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
	Group    string `json:"group,omitempty"`
	Sound    bool   `json:"sound,omitempty"`
	Image    string `json:"image,omitempty"`
	Open     string `json:"open,omitempty"`
}

// Send posts through the helper app. When macOS answers "not allowed", the
// usual cause is a bundle LaunchServices has not caught up with (a new or
// moved app is refused without the user ever being asked), so the app is
// registered again and the notification retried once. If macOS still refuses,
// it is shown through osascript instead, which macOS attributes to Script
// Editor, and ErrDenied is returned only when that fails too.
func (h *helperBackend) Send(ctx context.Context, nt Notification) error {
	group := nt.Group
	if group != "" {
		group = h.n.cfg.AppName + ":" + group
	}
	in, err := json.Marshal(helperPayload{
		Title: nt.Title, Subtitle: nt.Subtitle, Body: nt.Body, Group: group, Sound: nt.Sound,
		Image: nt.Icon, Open: nt.Open,
	})
	if err != nil {
		return err
	}
	app, exe, err := h.verify()
	if err != nil {
		return err
	}
	err = runHelper(ctx, command{name: exe, stdin: in})
	if !notAllowed(err) {
		return err
	}
	if rerr := h.register(app); rerr != nil {
		h.n.logf("re-registering the notifier app failed: %v", rerr)
	} else if err = runHelper(ctx, command{name: exe, stdin: in}); !notAllowed(err) {
		return err
	}
	h.n.logf("macOS does not allow notifications from the %s notifier app (System Settings > Notifications); falling back to osascript", h.n.cfg.AppName)
	if ferr := h.fallback.Send(ctx, nt); ferr != nil {
		var ee *exitError
		errors.As(err, &ee)
		return fmt.Errorf("%w: %s (osascript: %v)", ErrDenied, ee.msg, ferr)
	}
	return nil
}

// verify re-reads the installed notifier before it is run and rebuilds the
// app when it is not the one this process signed, so a notifier replaced after
// start is never executed.
func (h *helperBackend) verify() (app, exe string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.signed == nil {
		return h.app, h.exe, nil // built by a test without a signed binary
	}
	if got, err := os.ReadFile(h.exe); err == nil && bytes.Equal(got, h.signed) {
		return h.app, h.exe, nil
	}
	h.n.logf("the %s notifier app changed since it was installed; rebuilding it", h.n.cfg.AppName)
	app, signed, err := h.rebuild()
	if err != nil {
		return "", "", fmt.Errorf("rebuilding the notifier app: %w", err)
	}
	h.app, h.exe, h.signed = app, filepath.Join(app, "Contents", "MacOS", "notifier"), signed
	return h.app, h.exe, nil
}

// notAllowed is the notifier's exit status 3.
func notAllowed(err error) bool {
	var ee *exitError
	return errors.As(err, &ee) && ee.code == 3
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

// ensureBundle makes "<dir>/<AppName> Notifier.app" exactly what this build
// would produce, and returns its path and the signed notifier's bytes.
//
// The installed app is never trusted on its own say-so. A fresh bundle is
// written and signed beside it on every start, and the two trees are compared
// file by file: a notifier, plist or icon that anything has altered since (a
// process running as the same user, say) does not match, and the fresh bundle
// replaces it and is registered with LaunchServices. Ad-hoc signing is
// deterministic, so an untouched app compares equal and nothing is swapped.
// The signed bytes are returned so each Send can re-check the notifier it is
// about to run (see helperBackend.Send).
//
// Everything is user-only: directories 0700, the notifier 0700, the rest 0600.
func ensureBundle(dir string, cfg Config, binary []byte, sign, register signer, logf func(string, ...any)) (string, []byte, error) {
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
		return "", nil, err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	_ = os.RemoveAll(tmp)
	defer func() { _ = os.RemoveAll(tmp) }()
	type file struct {
		rel  string
		data []byte
		perm os.FileMode
	}
	files := []file{
		{"Contents/MacOS/notifier", binary, 0o700},
		{"Contents/Info.plist", plist, 0o600},
	}
	if icns != nil {
		files = append(files, file{"Contents/Resources/AppIcon.icns", icns, 0o600})
	}
	for _, f := range files {
		p := filepath.Join(tmp, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", nil, err
		}
		if err := os.WriteFile(p, f.data, f.perm); err != nil {
			return "", nil, err
		}
	}
	if err := sign(tmp); err != nil {
		return "", nil, fmt.Errorf("signing the notifier app: %w", err)
	}
	signed, err := os.ReadFile(filepath.Join(tmp, "Contents", "MacOS", "notifier"))
	if err != nil {
		return "", nil, err
	}
	if same, _ := sameTree(tmp, path); same {
		return path, signed, nil
	}
	if _, err := os.Lstat(path); err == nil {
		logf("the installed %s notifier app differs from this build; replacing it", cfg.AppName)
	}
	if err := os.RemoveAll(path); err != nil {
		return "", nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", nil, err
	}
	if err := register(path); err != nil {
		logf("could not register the notifier app with LaunchServices; the first notification may be refused: %v", err)
	}
	return path, signed, nil
}

// sameTree reports whether two directories hold the same files, with the same
// bytes and permission bits, and nothing else. A symlink anywhere is a
// difference.
func sameTree(a, b string) (bool, error) {
	list := func(root string) (map[string]os.FileInfo, error) {
		out := map[string]os.FileInfo{}
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			out[rel] = info
			return nil
		})
		return out, err
	}
	la, err := list(a)
	if err != nil {
		return false, err
	}
	lb, err := list(b)
	if err != nil || len(la) != len(lb) {
		return false, err
	}
	for rel, ia := range la {
		ib, ok := lb[rel]
		if !ok || ia.Mode() != ib.Mode() {
			return false, nil
		}
		if !ia.Mode().IsRegular() {
			if !ia.IsDir() {
				return false, nil // a symlink or anything else
			}
			continue
		}
		da, err := os.ReadFile(filepath.Join(a, rel))
		if err != nil {
			return false, err
		}
		db, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || !bytes.Equal(da, db) {
			return false, err
		}
	}
	return true, nil
}

// bundleID is Config.BundleID, else AppID+".notifier".
func bundleID(cfg Config) string {
	if cfg.BundleID != "" {
		return cfg.BundleID
	}
	return cfg.AppID + ".notifier"
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
		{"CFBundleIdentifier", bundleID(cfg)},
		{"CFBundleInfoDictionaryVersion", "6.0"},
		{"CFBundleName", cfg.AppName},
		{"CFBundlePackageType", "APPL"},
		{"CFBundleShortVersionString", helperVersion},
		{"CFBundleVersion", helperVersion},
		{"LSMinimumSystemVersion", "11.0"},
		{"NSUserNotificationAlertStyle", "banner"},
	}
	if self, err := selfExecutable(); err == nil {
		// The one executable a click may run (see Notification.Open).
		kv = append(kv, [2]string{"NotifyKitOpenPath", self})
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
