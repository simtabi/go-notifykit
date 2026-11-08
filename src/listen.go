package notifykit

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Endpoint is where a running application accepts notifications from other
// local processes: a Unix-domain socket and the file holding the token every
// message must carry. Both live in the application's state directory, which
// only the user can enter.
type Endpoint struct {
	Socket    string
	TokenFile string
}

// Limits on what Listen accepts.
const (
	maxMessage     = 16 << 10        // one message, bytes
	messageTimeout = 5 * time.Second // to read one message and answer it
	maxInFlight    = 8               // connections served at once
	maxSocketPath  = 100             // sun_path is 104 bytes on macOS, 108 on Linux
)

// pingRequest is one line a client writes.
type pingRequest struct {
	Token        string       `json:"token"`
	Notification Notification `json:"notification"`
}

// pingReply is the one line the server writes back.
type pingReply struct {
	OK         bool   `json:"ok"`
	Backend    string `json:"backend,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
	Suppressed bool   `json:"suppressed,omitempty"`
	Pending    bool   `json:"pending,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Endpoint reports where Listen serves, without starting it.
func (n *Notifier) Endpoint() (Endpoint, error) {
	dir, err := n.stateDir()
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{Socket: filepath.Join(dir, "notify.sock"), TokenFile: filepath.Join(dir, "notify.token")}, nil
}

// Listen accepts notifications from other local processes ([Ping], or
// `<app> notify ping`) until ctx ends, and shows each through Send. Every
// start writes a fresh random token; a message without it is refused.
//
// Only the user can reach the socket: the state directory is made 0700, the
// socket and token 0600. A message is one JSON line of at most 16 KiB, read
// within five seconds, and its text is cleaned like any other notification,
// so a sender can change what a banner says but nothing else.
func (n *Notifier) Listen(ctx context.Context) error {
	ep, err := n.Endpoint()
	if err != nil {
		return err
	}
	if len(ep.Socket) > maxSocketPath {
		return fmt.Errorf("notifykit: socket path %s is %d bytes; Unix sockets allow %d, so set a shorter Config.StateDir", ep.Socket, len(ep.Socket), maxSocketPath)
	}
	dir := filepath.Dir(ep.Socket)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if err := clearStaleSocket(ep.Socket); err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(ep.TokenFile, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	ln, err := net.Listen("unix", ep.Socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(ep.Socket, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	defer func() {
		_ = os.Remove(ep.TokenFile)
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close() // a unix listener removes its socket file on Close
	}()
	slots := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			n.reply(conn, pingReply{Error: "busy"})
			_ = conn.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			n.serve(ctx, conn, token)
		}()
	}
}

func (n *Notifier) serve(ctx context.Context, conn net.Conn, token string) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(messageTimeout))
	line, err := bufio.NewReaderSize(io.LimitReader(conn, maxMessage+1), 4096).ReadBytes('\n')
	if len(line) > maxMessage {
		n.reply(conn, pingReply{Error: "message too large"})
		return
	}
	if err != nil && !errors.Is(err, io.EOF) {
		n.reply(conn, pingReply{Error: "read: " + err.Error()})
		return
	}
	var req pingRequest
	dec := json.NewDecoder(strings.NewReader(string(line)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		n.reply(conn, pingReply{Error: "bad message: " + err.Error()})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
		n.logf("refused a ping with a wrong token")
		n.reply(conn, pingReply{Error: "wrong token"})
		return
	}
	res, err := n.Send(ctx, req.Notification)
	if err != nil {
		n.reply(conn, pingReply{Error: err.Error()})
		return
	}
	n.reply(conn, pingReply{OK: true, Backend: res.Backend, Skipped: res.Skipped, Suppressed: res.Suppressed, Pending: res.Pending})
}

func (n *Notifier) reply(conn net.Conn, r pingReply) {
	b, _ := json.Marshal(r)
	_, _ = conn.Write(append(b, '\n'))
}

// Ping hands nt to the application listening at ep and returns what it did
// with it. The application must be running [Notifier.Listen].
func Ping(ctx context.Context, ep Endpoint, nt Notification) (Result, error) {
	token, err := readToken(ep.TokenFile)
	if err != nil {
		return Result{}, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", ep.Socket)
	if err != nil {
		return Result{}, fmt.Errorf("notifykit: nothing is listening at %s (is the application running?): %w", ep.Socket, err)
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(messageTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	b, err := json.Marshal(pingRequest{Token: token, Notification: nt})
	if err != nil {
		return Result{}, err
	}
	if len(b) >= maxMessage {
		return Result{}, errors.New("notifykit: the notification is too large to ping")
	}
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return Result{}, err
	}
	line, err := bufio.NewReader(io.LimitReader(conn, maxMessage)).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Result{}, fmt.Errorf("notifykit: no answer from %s: %w", ep.Socket, err)
	}
	var r pingReply
	if err := json.Unmarshal(line, &r); err != nil {
		return Result{}, fmt.Errorf("notifykit: unreadable answer: %w", err)
	}
	if !r.OK {
		return Result{}, fmt.Errorf("notifykit: refused: %s", r.Error)
	}
	return Result{Backend: r.Backend, Skipped: r.Skipped, Suppressed: r.Suppressed, Pending: r.Pending}, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// readToken refuses a token file anyone but the user could read or write.
func readToken(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("notifykit: no token at %s (is the application running?): %w", path, err)
	}
	if osName() != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("notifykit: %s is readable by others (%v); refusing it", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(b))
	if len(t) != 64 {
		return "", fmt.Errorf("notifykit: %s does not hold a token", path)
	}
	return t, nil
}

// clearStaleSocket removes a socket file left by a process that died, but
// never a live one and never anything that is not a socket.
func clearStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("notifykit: %s exists and is not a socket; not touching it", path)
	}
	if c, err := net.DialTimeout("unix", path, 500*time.Millisecond); err == nil {
		_ = c.Close()
		return fmt.Errorf("notifykit: another process is already listening at %s", path)
	}
	return os.Remove(path)
}
