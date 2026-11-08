package notifykit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shortDir is a state directory short enough for a Unix socket path
// (t.TempDir on macOS is too long for sun_path).
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "nk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return filepath.Join(d, "s")
}

// listening starts Listen with a Recorder and waits for its token.
func listening(t *testing.T) (*Notifier, *Recorder, Endpoint) {
	t.Helper()
	rec := &Recorder{}
	n, err := New(Config{AppName: "demo", StateDir: shortDir(t), Backend: rec})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.Listen(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Listen: %v", err)
		}
	})
	ep, _ := n.Endpoint()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(ep.TokenFile); err == nil {
			if c, err := net.Dial("unix", ep.Socket); err == nil {
				_ = c.Close()
				return n, rec, ep
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Listen never came up")
	return nil, nil, Endpoint{}
}

func TestPingShowsTheNotification(t *testing.T) {
	_, rec, ep := listening(t)
	res, err := Ping(context.Background(), ep, Notification{Title: "update\x1b[2J ready", Body: "v1.2.0", Group: "update"})
	if err != nil || res.Backend != "recorder" {
		t.Fatalf("Ping = %+v, %v", res, err)
	}
	got := rec.Sent()
	if len(got) != 1 || got[0].Title != "update [2J ready" || got[0].Group != "update" {
		t.Fatalf("shown %+v", got)
	}
}

func TestListenFilesAreUserOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	_, _, ep := listening(t)
	for _, p := range []string{ep.Socket, ep.TokenFile, filepath.Dir(ep.Socket)} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is %v; want no group or other access", p, fi.Mode().Perm())
		}
	}
}

// raw sends one line and returns the reply.
func raw(t *testing.T, ep Endpoint, line []byte) pingReply {
	t.Helper()
	c, err := net.Dial("unix", ep.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	_, _ = c.Write(line)
	reply, _ := bufio.NewReader(c).ReadBytes('\n')
	var r pingReply
	if err := json.Unmarshal(reply, &r); err != nil {
		t.Fatalf("reply %q: %v", reply, err)
	}
	return r
}

func TestListenRefusesBadMessages(t *testing.T) {
	_, rec, ep := listening(t)
	token, err := readToken(ep.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	msg := func(v any) []byte { b, _ := json.Marshal(v); return append(b, '\n') }
	cases := map[string]struct {
		line []byte
		want string
	}{
		"wrong token":   {msg(pingRequest{Token: strings.Repeat("0", 64), Notification: Notification{Title: "x"}}), "wrong token"},
		"no token":      {msg(map[string]any{"notification": map[string]any{"title": "x"}}), "wrong token"},
		"unknown field": {msg(map[string]any{"token": token, "notification": map[string]any{"title": "x"}, "exec": "rm"}), "bad message"},
		"not JSON":      {[]byte("hello\n"), "bad message"},
		"too large":     {append(bytes.Repeat([]byte("a"), maxMessage+10), '\n'), "too large"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := raw(t, ep, c.line)
			if r.OK || !strings.Contains(r.Error, c.want) {
				t.Fatalf("reply %+v; want an error containing %q", r, c.want)
			}
		})
	}
	if len(rec.Sent()) != 0 {
		t.Fatalf("a refused message was shown: %+v", rec.Sent())
	}
}

func TestPingRefusesALooseTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	dir := t.TempDir()
	tok := filepath.Join(dir, "notify.token")
	_ = os.WriteFile(tok, []byte(strings.Repeat("a", 64)), 0o644)
	if _, err := Ping(context.Background(), Endpoint{Socket: filepath.Join(dir, "x.sock"), TokenFile: tok}, Notification{}); err == nil || !strings.Contains(err.Error(), "readable by others") {
		t.Fatalf("err = %v", err)
	}
}

// A ping is checked like any other notification: a sender cannot make the app
// open a remote URL or run another program.
func TestPingCannotAskForAForeignClickAction(t *testing.T) {
	_, rec, ep := listening(t)
	for _, open := range []string{"https://example.com/", "/bin/sh", "file:///etc/passwd"} {
		if _, err := Ping(context.Background(), ep, Notification{Title: "t", Open: open}); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Errorf("open %q: err = %v", open, err)
		}
	}
	if len(rec.Sent()) != 0 {
		t.Fatalf("shown %+v", rec.Sent())
	}
}

func TestPingWithNobodyListening(t *testing.T) {
	dir := shortDir(t)
	_ = os.MkdirAll(dir, 0o700)
	tok := filepath.Join(dir, "notify.token")
	_ = os.WriteFile(tok, []byte(strings.Repeat("a", 64)), 0o600)
	_, err := Ping(context.Background(), Endpoint{Socket: filepath.Join(dir, "notify.sock"), TokenFile: tok}, Notification{})
	if err == nil || !strings.Contains(err.Error(), "is the application running") {
		t.Fatalf("err = %v", err)
	}
}

func TestListenWillNotClobber(t *testing.T) {
	dir := shortDir(t)
	_ = os.MkdirAll(dir, 0o700)
	// A regular file where the socket goes is never removed.
	_ = os.WriteFile(filepath.Join(dir, "notify.sock"), []byte("keep"), 0o600)
	n, _ := New(Config{AppName: "demo", StateDir: dir, Backend: &Recorder{}})
	if err := n.Listen(context.Background()); err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notify.sock")); string(b) != "keep" {
		t.Fatal("the file was replaced")
	}
}

func TestSecondListenerIsRefused(t *testing.T) {
	n, _, _ := listening(t)
	if err := n.Listen(context.Background()); err == nil || !strings.Contains(err.Error(), "already listening") {
		t.Fatalf("err = %v", err)
	}
}

func TestListenRejectsALongSocketPath(t *testing.T) {
	n, _ := New(Config{AppName: "demo", StateDir: "/" + strings.Repeat("d", 120), Backend: &Recorder{}})
	if err := n.Listen(context.Background()); err == nil || !strings.Contains(err.Error(), "shorter Config.StateDir") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCLI(t *testing.T) {
	n, rec, _ := listening(t)
	run := func(stdin string, args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := RunCLI(context.Background(), n, args, strings.NewReader(stdin), &out, &errb)
		return code, out.String(), errb.String()
	}
	if code, out, _ := run("", "send", "-title", "t", "-body", "b", "-level", "alert"); code != 0 || !strings.Contains(out, "recorder") {
		t.Fatalf("send: %d %q", code, out)
	}
	if code, out, errs := run("", "ping", "-title", "p"); code != 0 || !strings.Contains(out, "recorder") {
		t.Fatalf("ping: %d %q %q", code, out, errs)
	}
	if code, _, _ := run(`{"title":"j","body":"from json","level":1}`, "send", "-json"); code != 0 {
		t.Fatalf("send -json: %d", code)
	}
	icon := filepath.Join(t.TempDir(), "i.png")
	_ = os.WriteFile(icon, testPNG(t, 16), 0o644)
	if code, out, errs := run("", "send", "--message", "M", "--description", "D", "--icon", icon, "--open", "http://127.0.0.1:7822/"); code != 0 {
		t.Fatalf("send --message --description --icon --open: %d %q %q", code, out, errs)
	}
	got := rec.Sent()
	if len(got) != 4 || got[0].Level != Alert || got[1].Title != "p" || got[2].Level != Warn {
		t.Fatalf("shown %+v", got)
	}
	if m := got[3]; m.Title != "M" || m.Body != "D" || m.Open != "http://127.0.0.1:7822/" || m.Icon == icon || m.Icon == "" {
		t.Fatalf("the aliases, icon or open were lost: %+v", m)
	}
	if code, _, errs := run("", "send", "-open", "file:///etc/passwd"); code != 1 || !strings.Contains(errs, "invalid") {
		t.Fatalf("a bad -open gave %d %q", code, errs)
	}
	for _, bad := range [][]string{{}, {"shout"}, {"send", "-level", "loud"}, {"send", "extra"}} {
		if code, _, _ := run(`{"exec":"x"}`, bad...); code != 2 {
			t.Errorf("%q gave %d, want 2", bad, code)
		}
	}
	if code, _, _ := run(`{"exec":"x"}`, "send", "-json"); code != 2 {
		t.Error("an unknown JSON field was accepted")
	}
	if code, out, _ := run("", "doctor"); code != 0 || !strings.Contains(out, "backend: recorder") || !strings.Contains(out, "notify.sock") {
		t.Fatalf("doctor: %d %q", code, out)
	}
}
