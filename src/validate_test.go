package notifykit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrepareImageAcceptsPNGJPEGAndGIFAndCopiesThem(t *testing.T) {
	n, _ := New(Config{AppName: "demo", StateDir: t.TempDir()})
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"a.png": testPNG(t, 16),
		"b.jpg": append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 32)...),
		"c.gif": append([]byte("GIF89a"), bytes.Repeat([]byte{1}, 32)...),
	} {
		src := filepath.Join(dir, name)
		if err := os.WriteFile(src, data, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := n.prepareImage(src)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out == src || !strings.HasPrefix(out, filepath.Join(n.cfg.StateDir, "images")) || filepath.Ext(out) != filepath.Ext(name) {
			t.Fatalf("%s: copy at %q", name, out)
		}
		if got, _ := os.ReadFile(out); !bytes.Equal(got, data) {
			t.Fatalf("%s: the copy differs", name)
		}
		if _, err := os.Stat(src); err != nil {
			t.Fatalf("%s: the original was touched: %v", name, err)
		}
		if fi, _ := os.Stat(out); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s: copy is %v, want 0600", name, fi.Mode().Perm())
		}
	}
}

func TestPrepareImageRefuses(t *testing.T) {
	n, _ := New(Config{AppName: "demo", StateDir: t.TempDir()})
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	png := write("ok.png", testPNG(t, 16))
	big, err := os.Create(filepath.Join(dir, "big.png"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = big.Write(testPNG(t, 16))
	_ = big.Truncate(maxImage + 1)
	_ = big.Close()

	cases := map[string]struct{ path, want string }{
		"relative":  {"ok.png", "absolute"},
		"missing":   {filepath.Join(dir, "nope.png"), "cannot be read"},
		"directory": {dir, "not a regular file"},
		"empty":     {write("empty.png", nil), "empty"},
		"too large": {big.Name(), "10 MB"},
		"not image": {write("fake.png", []byte("#!/bin/sh\nrm -rf ~\n")), "PNG, JPEG or GIF"},
		"svg":       {write("x.svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")), "PNG, JPEG or GIF"},
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link.png")
		if err := os.Symlink(png, link); err != nil {
			t.Fatal(err)
		}
		cases["symlink"] = struct{ path, want string }{link, "symlink"}
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := n.prepareImage(c.path)
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v; want ErrInvalid containing %q", err, c.want)
			}
		})
	}
	if entries, _ := os.ReadDir(filepath.Join(n.cfg.StateDir, "images")); len(entries) != 0 {
		t.Fatalf("a refused image was copied: %d files", len(entries))
	}
}

func TestCheckOpen(t *testing.T) {
	self := filepath.Join(t.TempDir(), "demo")
	if err := os.WriteFile(self, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	self, _ = filepath.EvalSymlinks(self)
	other := filepath.Join(t.TempDir(), "other")
	_ = os.WriteFile(other, []byte("x"), 0o755)
	old := selfExecutable
	t.Cleanup(func() { selfExecutable = old })
	selfExecutable = func() (string, error) { return self, nil }

	for _, ok := range []string{"", "http://127.0.0.1:7822/", "https://localhost/x?y=1", "http://[::1]:8080/", "HTTP://LOCALHOST/", self} {
		if _, err := checkOpen(ok); err != nil {
			t.Errorf("checkOpen(%q) refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"http://example.com/", "https://127.0.0.2/", "http://user:pw@127.0.0.1/", "file:///etc/passwd",
		"javascript:alert(1)", "ftp://127.0.0.1/", "demo", "./demo", other, "/bin/sh", "http://127.0.0.1.evil.example/",
	} {
		if _, err := checkOpen(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("checkOpen(%q) = %v; want ErrInvalid", bad, err)
		}
	}
}

// A bad icon or click action is refused even with delivery off, so a caller
// learns of it in tests rather than in the field.
func TestSendRefusesABadOpenOrIcon(t *testing.T) {
	rec := &Recorder{}
	n, _ := New(Config{AppName: "demo", StateDir: t.TempDir(), Backend: rec})
	if _, err := n.Send(context.Background(), Notification{Title: "t", Open: "http://example.com/"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := n.Send(context.Background(), Notification{Title: "t", Icon: "relative.png"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if len(rec.Sent()) != 0 {
		t.Fatal("a refused notification was shown")
	}
	off, _ := New(Config{AppName: "demo", Getenv: env()})
	if _, err := off.Send(context.Background(), Notification{Title: "t", Open: "file:///etc/passwd"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("with delivery off: err = %v", err)
	}
}

func TestSendCopiesThePerCallIcon(t *testing.T) {
	rec := &Recorder{}
	n, _ := New(Config{AppName: "demo", StateDir: t.TempDir(), Backend: rec})
	src := filepath.Join(t.TempDir(), "a.png")
	_ = os.WriteFile(src, testPNG(t, 16), 0o644)
	if _, err := n.Send(context.Background(), Notification{Title: "t", Icon: src}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Sent()[0].Icon; got == src || !strings.HasPrefix(got, n.cfg.StateDir) {
		t.Fatalf("the backend got %q, want the copy in the state directory", got)
	}
}

func TestCleanDropsBidiOverrides(t *testing.T) {
	// U+202E reverses what follows, so "invoice" + RLO + "fdp.exe" displays as "invoiceexe.pdf".
	if got := clean("invoice\u202efdp.exe \u2066x\u2069 \u200b"); got != "invoicefdp.exe x" {
		t.Fatalf("clean = %q", got)
	}
}
