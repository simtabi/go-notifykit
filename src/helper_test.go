package notifykit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testPNG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 6, G: 111, B: 209, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The embedded notifier must be built from the Swift source in the tree, and
// be one binary for both Mac architectures.
func TestHelperIsBuiltFromSource(t *testing.T) {
	src, err := os.ReadFile("macos/notifier.swift")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("macos/notifier.swift.sha256")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(src)
	if hex.EncodeToString(sum[:]) != strings.TrimSpace(string(want)) {
		t.Fatal("macos/notifier.swift changed since the binary was built: run `make helper` on a Mac")
	}
	bin, err := os.ReadFile("macos/notifier")
	if err != nil {
		t.Fatal(err)
	}
	// A universal Mach-O starts with the fat magic and lists its slices.
	if len(bin) < 8 || binary.BigEndian.Uint32(bin) != 0xcafebabe {
		t.Fatal("macos/notifier is not a universal Mach-O binary")
	}
	const cpuX86_64, cpuARM64 = 0x01000007, 0x0100000c
	n := binary.BigEndian.Uint32(bin[4:])
	found := map[uint32]bool{}
	for i := uint32(0); i < n && int(8+20*i+4) <= len(bin); i++ {
		found[binary.BigEndian.Uint32(bin[8+20*i:])] = true
	}
	if !found[cpuX86_64] || !found[cpuARM64] {
		t.Fatalf("macos/notifier slices %v; want arm64 and x86_64", found)
	}
}

func TestEnsureBundleWritesSignsAndReuses(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{AppName: "Sange & Co", AppID: "com.sangehq.sange", Icon: testPNG(t, 64)}
	var signed, registered []string
	sign := func(p string) error { signed = append(signed, p); return nil }
	reg := func(p string) error { registered = append(registered, p); return nil }
	app, signedBin, err := ensureBundle(dir, cfg, []byte("BIN"), sign, reg, t.Logf)
	if err != nil || string(signedBin) != "BIN" {
		t.Fatal(signedBin, err)
	}
	if app != filepath.Join(dir, "Sange & Co Notifier.app") || len(signed) != 1 || !strings.Contains(signed[0], ".tmp-") {
		t.Fatalf("app %q, signed %q", app, signed)
	}
	// LaunchServices learns the final path, not the temporary one.
	if len(registered) != 1 || registered[0] != app {
		t.Fatalf("registered %q, want [%q]", registered, app)
	}
	exe, err := os.Stat(filepath.Join(app, "Contents", "MacOS", "notifier"))
	if err != nil || (runtime.GOOS != "windows" && exe.Mode().Perm()&0o100 == 0) {
		t.Fatalf("notifier is missing or not executable: %v", err)
	}
	plist, _ := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	for _, want := range []string{
		"<string>Sange &amp; Co</string>", "<string>com.sangehq.sange.notifier</string>",
		"<key>CFBundleIconFile</key>\n\t<string>AppIcon</string>", "<key>LSUIElement</key>\n\t<true/>",
	} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist lacks %q", want)
		}
	}
	if icns, _ := os.ReadFile(filepath.Join(app, "Contents", "Resources", "AppIcon.icns")); !bytes.HasPrefix(icns, []byte("icns")) {
		t.Error("no AppIcon.icns")
	}

	// Same inputs: a fresh copy is signed to compare with, and as the two
	// match, the installed app is kept and not registered again.
	if _, _, err := ensureBundle(dir, cfg, []byte("BIN"), sign, reg, t.Logf); err != nil || len(signed) != 2 || len(registered) != 1 {
		t.Fatalf("an identical bundle was replaced (%d signs, %d registrations, %v)", len(signed), len(registered), err)
	}
	// A new binary (a newer notifykit) replaces it, and registers it again.
	if _, _, err := ensureBundle(dir, cfg, []byte("BIN2"), sign, reg, t.Logf); err != nil || len(signed) != 3 || len(registered) != 2 {
		t.Fatalf("a changed binary was not rebuilt (%d signs, %d registrations, %v)", len(signed), len(registered), err)
	}
	if b, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "notifier")); string(b) != "BIN2" {
		t.Fatalf("notifier is %q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("left behind %d entries in the state dir", len(entries))
	}
}

// O-03: the installed app is checked against what this build produces, not
// trusted because it is there. A notifier, plist or extra file put in place by
// anything else is replaced, and the replacement registered.
func TestEnsureBundleReplacesATamperedApp(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{AppName: "demo", AppID: "com.example.demo"}
	ok := func(string) error { return nil }
	var regs int
	reg := func(string) error { regs++; return nil }
	app, _, err := ensureBundle(dir, cfg, []byte("GOOD"), ok, reg, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(app, "Contents", "MacOS", "notifier")
	for name, tamper := range map[string]func(){
		"notifier": func() { _ = os.WriteFile(exe, []byte("EVIL"), 0o700) },
		"plist":    func() { _ = os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("<plist/>"), 0o600) },
		"extra":    func() { _ = os.WriteFile(filepath.Join(app, "Contents", "MacOS", "dylib"), []byte("x"), 0o700) },
		"mode": func() {
			if runtime.GOOS != "windows" {
				_ = os.Chmod(exe, 0o777)
			} else {
				_ = os.WriteFile(exe, []byte("EVIL"), 0o700)
			}
		},
	} {
		before := regs
		tamper()
		if _, _, err := ensureBundle(dir, cfg, []byte("GOOD"), ok, reg, t.Logf); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b, _ := os.ReadFile(exe); string(b) != "GOOD" || regs != before+1 {
			t.Fatalf("%s: notifier %q, registrations %d -> %d", name, b, before, regs)
		}
		if _, err := os.Stat(filepath.Join(app, "Contents", "MacOS", "dylib")); err == nil {
			t.Fatalf("%s: an extra file survived the rebuild", name)
		}
	}
}

// O-03: Send re-checks the notifier it is about to run, so one replaced after
// start is never executed.
func TestSendNeverRunsANotifierReplacedAfterStart(t *testing.T) {
	dir := t.TempDir()
	n, _ := New(Config{AppName: "demo", AppID: "com.example.demo"})
	ok := func(string) error { return nil }
	rebuild := func() (string, []byte, error) { return ensureBundle(dir, n.cfg, []byte("GOOD"), ok, ok, t.Logf) }
	app, signed, err := rebuild()
	if err != nil {
		t.Fatal(err)
	}
	h := &helperBackend{n: n, app: app, exe: filepath.Join(app, "Contents", "MacOS", "notifier"), signed: signed, rebuild: rebuild, register: ok}
	var ran []string
	old := runHelper
	t.Cleanup(func() { runHelper = old })
	runHelper = func(_ context.Context, c command) error {
		b, _ := os.ReadFile(c.name)
		ran = append(ran, string(b))
		return nil
	}
	_ = os.WriteFile(h.exe, []byte("EVIL"), 0o700)
	if err := h.Send(context.Background(), Notification{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "GOOD" {
		t.Fatalf("ran %q; want only the rebuilt notifier", ran)
	}
}

func TestBundleAndStateAreUserOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "state")
	ok := func(string) error { return nil }
	app, _, err := ensureBundle(dir, Config{AppName: "demo", AppID: "com.example.demo", Icon: testPNG(t, 32)}, []byte("x"), ok, ok, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, _ error) error {
		checked++
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is %v; want no group or other access", p, info.Mode().Perm())
		}
		return nil
	})
	if checked < 6 {
		t.Fatalf("inspected %d paths under %s; expected the whole bundle", checked, app)
	}
	n, _ := New(Config{AppName: "demo", StateDir: filepath.Join(t.TempDir(), "s2"), Icon: testPNG(t, 32)})
	icon := n.iconFile()
	for _, p := range []string{icon, filepath.Dir(icon)} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: %v %v", p, fi.Mode(), err)
		}
	}
}

func TestEnsureBundleSignFailureLeavesTheOldApp(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{AppName: "demo", AppID: "com.example.demo"}
	ok := func(string) error { return nil }
	app, _, err := ensureBundle(dir, cfg, []byte("v1"), ok, ok, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ensureBundle(dir, cfg, []byte("v2"), func(string) error { return errors.New("no codesign") }, ok, t.Logf); err == nil {
		t.Fatal("a signing failure was not reported")
	}
	if b, _ := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "notifier")); string(b) != "v1" {
		t.Fatalf("the working app was replaced by an unsigned one: %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left %d entries behind", len(entries))
	}
}

func TestBadIconStillGivesAnApp(t *testing.T) {
	var logged bytes.Buffer
	logf := func(f string, _ ...any) { logged.WriteString(f) }
	app, _, err := ensureBundle(t.TempDir(), Config{AppName: "demo", AppID: "com.example.demo", Icon: []byte("not a png")}, []byte("x"), func(string) error { return nil }, func(string) error { return nil }, logf)
	if err != nil {
		t.Fatal(err)
	}
	// The plist must not name an icon file that was never written.
	if plist, _ := os.ReadFile(filepath.Join(app, "Contents", "Info.plist")); strings.Contains(string(plist), "CFBundleIconFile") {
		t.Fatal("Info.plist names an icon that is not there")
	}
	if _, err := os.Stat(filepath.Join(app, "Contents", "Resources", "AppIcon.icns")); !os.IsNotExist(err) {
		t.Fatal("an unusable icon was written")
	}
	if logged.Len() == 0 {
		t.Fatal("the unusable icon was not logged")
	}
}

// A failed registration is logged, not fatal: the app still works once
// LaunchServices finds it on its own.
func TestRegisterFailureIsOnlyLogged(t *testing.T) {
	var logged []string
	logf := func(f string, _ ...any) { logged = append(logged, f) }
	ok := func(string) error { return nil }
	if _, _, err := ensureBundle(t.TempDir(), Config{AppName: "demo", AppID: "com.example.demo"}, []byte("x"), ok, func(string) error { return errors.New("no lsregister") }, logf); err != nil {
		t.Fatal(err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "LaunchServices") {
		t.Fatalf("logged %q", logged)
	}
}

func TestHelperSendPassesJSON(t *testing.T) {
	var got command
	old := runHelper
	t.Cleanup(func() { runHelper = old })
	runHelper = func(_ context.Context, c command) error { got = c; return nil }
	n, _ := New(Config{AppName: "sange", AppID: "sh.sange.notify"})
	h := &helperBackend{n: n, exe: "/x/notifier"}
	nt := Notification{Title: "t", Subtitle: "s", Body: "b", Group: "push", Sound: true, Icon: "/s/images/a.png", Open: "http://127.0.0.1:7822/"}
	if err := h.Send(context.Background(), nt); err != nil {
		t.Fatal(err)
	}
	var p helperPayload
	if err := json.Unmarshal(got.stdin, &p); err != nil || got.name != "/x/notifier" || len(got.args) != 0 {
		t.Fatalf("ran %+v (%v)", got, err)
	}
	want := helperPayload{Title: "t", Subtitle: "s", Body: "b", Group: "sange:push", Sound: true, Image: "/s/images/a.png", Open: "http://127.0.0.1:7822/"}
	if p != want {
		t.Fatalf("payload %+v", p)
	}
}

// "Not allowed" is usually a bundle LaunchServices has not caught up with: the
// app is registered again and retried once, and only then shown through
// osascript.
func TestNotAllowedReregistersRetriesThenFallsBack(t *testing.T) {
	old := runHelper
	t.Cleanup(func() { runHelper = old })
	n, _ := New(Config{AppName: "demo", AppID: "com.example.demo"})
	setup := func(answers ...error) (*helperBackend, *int, *[]string, *Recorder) {
		calls, regs, fb := 0, []string{}, &Recorder{}
		runHelper = func(context.Context, command) error { e := answers[calls]; calls++; return e }
		h := &helperBackend{n: n, app: "/s/demo Notifier.app", exe: "/x/notifier", fallback: fb,
			register: func(p string) error { regs = append(regs, p); return nil }}
		return h, &calls, &regs, fb
	}
	denied := &exitError{code: 3, msg: "Notifications are not allowed for this application"}

	// Registered again, retried, and the retry is posted by the helper.
	h, calls, regs, fb := setup(denied, nil)
	if err := h.Send(context.Background(), Notification{Title: "t"}); err != nil || *calls != 2 || len(*regs) != 1 || (*regs)[0] != "/s/demo Notifier.app" || len(fb.Sent()) != 0 {
		t.Fatalf("err %v, calls %d, registrations %q, fallback %d", err, *calls, *regs, len(fb.Sent()))
	}
	// Still refused: shown through the fallback instead, with no error.
	h, calls, regs, fb = setup(denied, denied)
	if err := h.Send(context.Background(), Notification{Title: "t"}); err != nil || *calls != 2 || len(*regs) != 1 || len(fb.Sent()) != 1 {
		t.Fatalf("err %v, calls %d, registrations %d, fallback %d", err, *calls, len(*regs), len(fb.Sent()))
	}
	// Another failure is reported, not retried.
	h, calls, _, fb = setup(&exitError{code: 1, msg: "boom"})
	if err := h.Send(context.Background(), Notification{Title: "t"}); err == nil || *calls != 1 || len(fb.Sent()) != 0 {
		t.Fatalf("err %v, calls %d, fallback %d", err, *calls, len(fb.Sent()))
	}
	// When the fallback fails too, the refusal is ErrDenied.
	h, _, _, _ = setup(denied, denied)
	h.fallback = failing{}
	if err := h.Send(context.Background(), Notification{Title: "t"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("err %v, want ErrDenied", err)
	}
}

func TestPlistNamesTheBundleIDAndTheClickableExecutable(t *testing.T) {
	old := selfExecutable
	t.Cleanup(func() { selfExecutable = old })
	selfExecutable = func() (string, error) { return "/opt/sange/bin/sange", nil }
	plist, err := infoPlist(Config{AppName: "sange", AppID: "sh.sange.notify", BundleID: "sh.sange.notify"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<string>sh.sange.notify</string>", "<key>NotifyKitOpenPath</key>\n\t<string>/opt/sange/bin/sange</string>"} {
		if !strings.Contains(string(plist), want) {
			t.Errorf("Info.plist lacks %q", want)
		}
	}
	if strings.Contains(string(plist), "sh.sange.notify.notifier") {
		t.Error("BundleID did not replace the default")
	}
	plist, _ = infoPlist(Config{AppName: "x", AppID: "com.example.x"}, false)
	if !strings.Contains(string(plist), "<string>com.example.x.notifier</string>") {
		t.Error("the default bundle ID is not AppID.notifier")
	}
}

func TestMakeICNS(t *testing.T) {
	icns, err := makeICNS(testPNG(t, 128))
	if err != nil {
		t.Fatal(err)
	}
	if string(icns[:4]) != "icns" || int(binary.BigEndian.Uint32(icns[4:])) != len(icns) {
		t.Fatal("bad icns header")
	}
	sizes := map[string]int{}
	for off := 8; off < len(icns); {
		tag, n := string(icns[off:off+4]), int(binary.BigEndian.Uint32(icns[off+4:]))
		cfg, err := png.DecodeConfig(bytes.NewReader(icns[off+8 : off+n]))
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		sizes[tag] = cfg.Width
		off += n
	}
	want := map[string]int{"icp4": 16, "ic11": 32, "icp5": 32, "ic12": 64, "ic07": 128}
	if len(sizes) != len(want) {
		t.Fatalf("elements %v, want %v (never scaled up)", sizes, want)
	}
	for k, v := range want {
		if sizes[k] != v {
			t.Fatalf("%s is %dpx, want %d", k, sizes[k], v)
		}
	}
	for _, bad := range [][]byte{[]byte("nope"), testPNGRect(t, 32, 16), testPNG(t, 8)} {
		if _, err := makeICNS(bad); err == nil {
			t.Error("an unusable icon was accepted")
		}
	}
}

func testPNGRect(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func TestDownscaleKeepsColourAndTransparency(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 2; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	out := downscale(img, 2)
	if c := out.NRGBAAt(0, 0); c != (color.NRGBA{R: 200, G: 100, B: 50, A: 255}) {
		t.Fatalf("opaque half became %v", c)
	}
	if c := out.NRGBAAt(1, 0); c.A != 0 {
		t.Fatalf("transparent half became %v", c)
	}
}
