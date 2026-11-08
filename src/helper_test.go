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
	app, err := ensureBundle(dir, cfg, []byte("BIN"), sign, reg, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if app != filepath.Join(dir, "Sange & Co Notifier.app") || len(signed) != 1 || !strings.Contains(signed[0], ".tmp-") {
		t.Fatalf("app %q, signed %q", app, signed)
	}
	// LaunchServices learns the final path, not the temporary one.
	if len(registered) != 1 || registered[0] != app {
		t.Fatalf("registered %q, want [%q]", registered, app)
	}
	exe, err := os.Stat(filepath.Join(app, "Contents", "MacOS", "notifier"))
	if err != nil || exe.Mode().Perm()&0o100 == 0 {
		t.Fatalf("notifier: %v %v", exe, err)
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

	// Same inputs: nothing is rewritten or re-signed.
	if _, err := ensureBundle(dir, cfg, []byte("BIN"), sign, reg, t.Logf); err != nil || len(signed) != 1 || len(registered) != 1 {
		t.Fatalf("an identical bundle was rebuilt (%d signs, %d registrations, %v)", len(signed), len(registered), err)
	}
	// A new binary (a newer notifykit) rebuilds it, and registers it again.
	if _, err := ensureBundle(dir, cfg, []byte("BIN2"), sign, reg, t.Logf); err != nil || len(signed) != 2 || len(registered) != 2 {
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

func TestEnsureBundleSignFailureLeavesTheOldApp(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{AppName: "demo", AppID: "com.example.demo"}
	ok := func(string) error { return nil }
	app, err := ensureBundle(dir, cfg, []byte("v1"), ok, ok, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ensureBundle(dir, cfg, []byte("v2"), func(string) error { return errors.New("no codesign") }, ok, t.Logf); err == nil {
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
	app, err := ensureBundle(t.TempDir(), Config{AppName: "demo", AppID: "com.example.demo", Icon: []byte("not a png")}, []byte("x"), func(string) error { return nil }, func(string) error { return nil }, logf)
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
	if _, err := ensureBundle(t.TempDir(), Config{AppName: "demo", AppID: "com.example.demo"}, []byte("x"), ok, func(string) error { return errors.New("no lsregister") }, logf); err != nil {
		t.Fatal(err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "LaunchServices") {
		t.Fatalf("logged %q", logged)
	}
}

func TestHelperSendPassesJSONAndMapsDenial(t *testing.T) {
	var got command
	old := runHelper
	t.Cleanup(func() { runHelper = old })
	runHelper = func(_ context.Context, c command) error { got = c; return nil }
	n, _ := New(Config{AppName: "sange", AppID: "com.sangehq.sange"})
	h := &helperBackend{n: n, exe: "/x/notifier"}
	if err := h.Send(context.Background(), Notification{Title: "t", Subtitle: "s", Body: "b", Group: "push", Sound: true}); err != nil {
		t.Fatal(err)
	}
	var p helperPayload
	if err := json.Unmarshal(got.stdin, &p); err != nil || got.name != "/x/notifier" || len(got.args) != 0 {
		t.Fatalf("ran %+v (%v)", got, err)
	}
	if p != (helperPayload{Title: "t", Subtitle: "s", Body: "b", Group: "sange:push", Sound: true}) {
		t.Fatalf("payload %+v", p)
	}
	runHelper = func(context.Context, command) error { return &exitError{code: 3, msg: "denied"} }
	if err := h.Send(context.Background(), Notification{Title: "t"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("exit 3 gave %v, want ErrDenied", err)
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
