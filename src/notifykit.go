// Package notifykit posts native desktop notifications under the calling
// application's own name and icon.
//
// On macOS it assembles a small signed helper app, "<AppName> Notifier.app",
// from a prebuilt notifier embedded in this package, so a banner says "sange"
// with sange's icon rather than "Script Editor". It falls back to the
// osascript macOS provides when the helper cannot be used, and never to a
// third-party notifier: every app ships its own helper. On Linux
// and the BSDs it uses notify-send, and on Windows a PowerShell toast.
//
// Desktop delivery switches itself off under `go test` and in CI, so a test
// suite never puts a banner on a developer's screen. Environment variables turn
// it on or off explicitly (see [Config.EnvPrefix]).
package notifykit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Level is how urgent a notification is. Backends that have urgency map it
// (notify-send's --urgency); the others ignore it.
type Level int

const (
	// Info is routine news.
	Info Level = iota
	// Warn needs attention soon.
	Warn
	// Alert needs attention now.
	Alert
)

// String is "info", "warn" or "alert".
func (l Level) String() string {
	switch l {
	case Warn:
		return "warn"
	case Alert:
		return "alert"
	}
	return "info"
}

// Notification is one banner. Only Title has a default (the app name); every
// other field may be left empty.
type Notification struct {
	Title    string `json:"title"`              // the message: the first line; the app name when empty
	Subtitle string `json:"subtitle,omitempty"` // second line (macOS only; folded into Body elsewhere)
	Body     string `json:"body,omitempty"`     // the description: the text under the title
	Level    Level  `json:"level,omitempty"`
	// Group collapses related notifications: on macOS a new notification in a
	// group replaces the previous one in Notification Center, and with
	// [Config.MinInterval] set, repeats within the interval are suppressed.
	Group string `json:"group,omitempty"`
	Sound bool   `json:"sound,omitempty"` // play the default sound, where the backend can
	// Icon is an image for this notification only: an absolute path to a PNG,
	// JPEG or GIF of at most 10 MB, not a symlink. On macOS it is attached to the
	// banner (the app's own icon still marks it); on Linux and Windows it
	// replaces the app's icon. Empty means the app's Config.Icon. It is checked
	// and copied into the state directory, so the file itself is never moved.
	Icon string `json:"icon,omitempty"`
	// Open is run when the notification is clicked, with no shell: an absolute
	// path to this program's own executable (run with no arguments), or an
	// http(s) URL on 127.0.0.1, localhost or ::1. Anything else is refused. Only
	// the macOS helper acts on a click.
	Open string `json:"open,omitempty"`
}

// Config describes the application the notifications come from.
type Config struct {
	// AppName is the name the user sees, e.g. "sange". Required.
	AppName string
	// AppID is a reverse-DNS identifier for the product, from its own domain
	// (e.g. "sh.sange.notify"), else "com.simtabi.<binary>". Required on macOS.
	AppID string
	// BundleID is the macOS helper app's bundle identifier, which is what
	// System Settings > Notifications remembers permission by. Empty means
	// AppID+".notifier". Set it to keep the permission an existing helper app
	// already has.
	BundleID string
	// Icon is a square PNG, ideally 1024x1024. Optional: without it the macOS
	// helper shows the generic app icon and notify-send shows none.
	Icon []byte
	// EnvPrefix names the per-application switches, e.g. "SANGE" reads
	// SANGE_NOTIFY_DESKTOP (1/0, true/false, on/off) and SANGE_NOTIFY_BACKEND.
	// Empty means the application has no switches of its own; the global
	// NOTIFYKIT_DESKTOP and NOTIFYKIT_BACKEND still apply.
	EnvPrefix string
	// Disabled turns desktop delivery off whatever the environment says.
	Disabled bool
	// StateDir is where the macOS helper app and the icon file are written.
	// Empty means $XDG_STATE_HOME/<app>/notifykit, else
	// ~/.local/state/<app>/notifykit (%LocalAppData%\<app>\notifykit on
	// Windows).
	StateDir string
	// MinInterval suppresses a repeat of the same Group within this long.
	// Zero means never suppress.
	MinInterval time.Duration
	// Timeout bounds one delivery. Zero means 10s.
	Timeout time.Duration
	// Backend replaces the platform backend, e.g. a [Recorder] in tests. A
	// Backend set here is always used unless Disabled is true: the test and CI
	// switches only stop real desktop delivery.
	Backend Backend
	// Getenv reads the environment. Nil means os.Getenv.
	Getenv func(string) string
	// Log receives one line per fallback or failure. Nil discards them.
	Log io.Writer
}

// Result says what happened to one notification.
type Result struct {
	Backend    string // the backend that delivered it, empty when none did
	Suppressed bool   // dropped by MinInterval
	Skipped    string // why desktop delivery is off, when it is
	// Pending means macOS is still asking the user whether to allow this
	// app's notifications (the first run). It shows once they allow it.
	Pending bool
}

// Errors returned by Send.
var (
	// ErrUnsupported means the platform has no usable notifier.
	ErrUnsupported = errors.New("notifykit: no desktop notifier is available here")
	// ErrDenied means the user has not allowed notifications for this app.
	ErrDenied = errors.New("notifykit: notifications are not allowed for this app (System Settings > Notifications)")
	// errPending is the helper still waiting on the permission prompt; Send
	// turns it into Result.Pending.
	errPending = errors.New("notifykit: waiting for the user to allow notifications")
)

// Notifier sends notifications for one application. It is safe for
// concurrent use.
type Notifier struct {
	cfg     Config
	getenv  func(string) string
	testing func() bool

	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time

	once    sync.Once
	backend Backend
	berr    error
}

// New checks cfg and returns a Notifier. It does no I/O: the macOS helper is
// assembled on the first Send.
func New(cfg Config) (*Notifier, error) {
	cfg.AppName = strings.TrimSpace(cfg.AppName)
	if cfg.AppName == "" {
		return nil, errors.New("notifykit: Config.AppName is required")
	}
	if strings.ContainsAny(cfg.AppName, `/\:`) || cfg.AppName == "." || cfg.AppName == ".." {
		return nil, fmt.Errorf("notifykit: AppName %q cannot contain a slash, a backslash or a colon", cfg.AppName)
	}
	if cfg.AppID != "" && !validAppID(cfg.AppID) {
		return nil, fmt.Errorf("notifykit: AppID %q must be reverse-DNS, e.g. com.example.app", cfg.AppID)
	}
	if cfg.BundleID != "" && !validAppID(cfg.BundleID) {
		return nil, fmt.Errorf("notifykit: BundleID %q must be reverse-DNS, e.g. com.example.app.notifier", cfg.BundleID)
	}
	if cfg.EnvPrefix != "" && !validPrefix(cfg.EnvPrefix) {
		return nil, fmt.Errorf("notifykit: EnvPrefix %q must be letters, digits and _", cfg.EnvPrefix)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	g := cfg.Getenv
	if g == nil {
		g = os.Getenv
	}
	return &Notifier{cfg: cfg, getenv: g, testing: underTest, last: map[string]time.Time{}, now: time.Now}, nil
}

// Enabled reports whether Send would reach a backend, and if not, why.
func (n *Notifier) Enabled() (bool, string) {
	if n.cfg.Disabled {
		return false, "disabled in the application's config"
	}
	if n.cfg.Backend != nil {
		return true, ""
	}
	for _, name := range n.envNames("NOTIFY_DESKTOP") {
		if v, ok := parseSwitch(n.getenv(name)); ok {
			if !v {
				return false, name + " is off"
			}
			return true, ""
		}
	}
	if n.testing() {
		return false, "running under go test (set NOTIFYKIT_DESKTOP=1 to force)"
	}
	if ci := strings.TrimSpace(n.getenv("CI")); ci != "" && !strings.EqualFold(ci, "false") && ci != "0" {
		return false, "running in CI (set NOTIFYKIT_DESKTOP=1 to force)"
	}
	return true, ""
}

// Send shows nt. A nil error with a non-empty Result.Skipped means delivery is
// switched off, which is not a failure.
func (n *Notifier) Send(ctx context.Context, nt Notification) (Result, error) {
	nt = sanitize(nt)
	if nt.Title == "" {
		nt.Title = n.cfg.AppName
	}
	// Check what the caller (or a ping) asked for before anything else, so a
	// bad icon or click action is refused even when delivery is off.
	open, err := checkOpen(nt.Open)
	if err != nil {
		return Result{}, err
	}
	nt.Open = open
	if ok, why := n.Enabled(); !ok {
		return Result{Skipped: why}, nil
	}
	if n.suppress(nt.Group) {
		return Result{Suppressed: true}, nil
	}
	if nt.Icon != "" {
		if nt.Icon, err = n.prepareImage(nt.Icon); err != nil {
			return Result{}, err
		}
	}
	b, err := n.pick()
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, n.cfg.Timeout)
	defer cancel()
	if err := b.Send(ctx, nt); err != nil {
		if errors.Is(err, errPending) {
			return Result{Backend: b.Name(), Pending: true}, nil
		}
		return Result{}, fmt.Errorf("%s: %w", b.Name(), err)
	}
	return Result{Backend: b.Name()}, nil
}

// Backend reports the backend Send uses, choosing it if it has not been
// chosen yet. Useful for a "doctor" command.
func (n *Notifier) Backend() (string, error) {
	b, err := n.pick()
	if err != nil {
		return "", err
	}
	return b.Name(), nil
}

func (n *Notifier) pick() (Backend, error) {
	n.once.Do(func() {
		if n.cfg.Backend != nil {
			n.backend = n.cfg.Backend
			return
		}
		force := ""
		for _, name := range n.envNames("NOTIFY_BACKEND") {
			if v := strings.TrimSpace(n.getenv(name)); v != "" {
				force = strings.ToLower(v)
				break
			}
		}
		n.backend, n.berr = platformBackend(n, force)
	})
	return n.backend, n.berr
}

func (n *Notifier) suppress(group string) bool {
	if n.cfg.MinInterval <= 0 || group == "" {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	t := n.now()
	if prev, ok := n.last[group]; ok && t.Sub(prev) < n.cfg.MinInterval {
		return true
	}
	n.last[group] = t
	return false
}

// envNames is the per-application name first (<PREFIX>_NOTIFY_DESKTOP), then
// the global one (NOTIFYKIT_DESKTOP). suffix is "NOTIFY_DESKTOP" or
// "NOTIFY_BACKEND".
func (n *Notifier) envNames(suffix string) []string {
	global := "NOTIFYKIT_" + strings.TrimPrefix(suffix, "NOTIFY_")
	if n.cfg.EnvPrefix == "" {
		return []string{global}
	}
	return []string{strings.ToUpper(n.cfg.EnvPrefix) + "_" + suffix, global}
}

func (n *Notifier) logf(format string, args ...any) {
	if n.cfg.Log != nil {
		_, _ = fmt.Fprintf(n.cfg.Log, "notifykit: "+format+"\n", args...)
	}
}

// stateDir resolves Config.StateDir.
func (n *Notifier) stateDir() (string, error) {
	if n.cfg.StateDir != "" {
		return n.cfg.StateDir, nil
	}
	app := strings.ToLower(strings.ReplaceAll(n.cfg.AppName, " ", "-"))
	if d := n.getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, app, "notifykit"), nil
	}
	if d := n.getenv("LOCALAPPDATA"); d != "" && osName() == "windows" {
		return filepath.Join(d, app, "notifykit"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", app, "notifykit"), nil
}

func parseSwitch(v string) (on, set bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	}
	return false, false
}

func validAppID(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r != '-' && !isAlnum(r) {
				return false
			}
		}
	}
	return true
}

func isAlnum(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

func validPrefix(s string) bool {
	for _, r := range s {
		if r != '_' && !isAlnum(r) {
			return false
		}
	}
	return true
}

// maxField bounds a title or body; desktops truncate anyway.
const maxField = 240

// clean replaces control characters (newlines, terminal escapes) with spaces,
// drops bidirectional overrides and other invisible format characters (which
// can make a banner read differently from what it says), collapses runs of
// space and bounds the length.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r) || r == utf8.RuneError:
			return ' '
		case unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxField {
		r := []rune(s)
		s = string(r[:maxField-1]) + "…"
	}
	return s
}

func sanitize(nt Notification) Notification {
	nt.Title, nt.Subtitle, nt.Body, nt.Group = clean(nt.Title), clean(nt.Subtitle), clean(nt.Body), clean(nt.Group)
	// Paths and URLs are checked, not rewritten: trimming is all they get here.
	nt.Icon, nt.Open = strings.TrimSpace(nt.Icon), strings.TrimSpace(nt.Open)
	return nt
}

// joined folds the subtitle into the body for backends without a subtitle.
func joined(nt Notification) string {
	if nt.Subtitle == "" {
		return nt.Body
	}
	if nt.Body == "" {
		return nt.Subtitle
	}
	return nt.Subtitle + ": " + nt.Body
}
