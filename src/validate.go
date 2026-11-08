package notifykit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrInvalid wraps every refusal of a notification's icon or click action.
var ErrInvalid = errors.New("notifykit: invalid notification")

// maxImage bounds a per-notification image.
const maxImage = 10 << 20

// imageTypes are the formats accepted, by their leading bytes.
var imageTypes = []struct {
	ext   string
	magic []byte
}{
	{".png", []byte("\x89PNG\r\n\x1a\n")},
	{".jpg", []byte{0xFF, 0xD8, 0xFF}},
	{".gif", []byte("GIF87a")},
	{".gif", []byte("GIF89a")},
}

// prepareImage checks a per-notification image and copies it into the state
// directory, returning the copy's path.
//
// The path must be absolute and name a regular file, not a symlink, of at most
// 10 MB whose content is a PNG, JPEG or GIF. The file is opened and then
// compared with what was checked, so swapping it for something else between
// the two is caught. The copy matters on macOS, where an attached image file is
// moved into the notification store: the caller's file is never touched.
func (n *Notifier) prepareImage(path string) (string, error) {
	bad := func(why string) error { return fmt.Errorf("%w: icon %s: %s", ErrInvalid, path, why) }
	if !filepath.IsAbs(path) {
		return "", bad("must be an absolute path")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return "", bad("cannot be read")
	}
	switch {
	case before.Mode()&os.ModeSymlink != 0:
		return "", bad("is a symlink; pass the file it points to")
	case !before.Mode().IsRegular():
		return "", bad("is not a regular file")
	case before.Size() == 0:
		return "", bad("is empty")
	case before.Size() > maxImage:
		return "", bad("is larger than 10 MB")
	}
	f, err := os.Open(path) //nolint:gosec // G304: the path is checked above and re-checked against the open file below
	if err != nil {
		return "", bad("cannot be read")
	}
	defer func() { _ = f.Close() }()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", bad("changed while it was being checked")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImage+1))
	if err != nil || len(data) > maxImage {
		return "", bad("cannot be read in full, or grew past 10 MB")
	}
	ext := ""
	for _, t := range imageTypes {
		if bytes.HasPrefix(data, t.magic) {
			ext = t.ext
			break
		}
	}
	if ext == "" {
		return "", bad("is not a PNG, JPEG or GIF")
	}

	dir, err := n.stateDir()
	if err != nil {
		return "", err
	}
	images := filepath.Join(dir, "images")
	if err := os.MkdirAll(images, 0o700); err != nil {
		return "", err
	}
	pruneImages(images, 24*time.Hour)
	sum := sha256.Sum256(data)
	out := filepath.Join(images, hex.EncodeToString(sum[:12])+ext)
	if err := writeFileAtomic(out, data, 0o600); err != nil {
		return "", err
	}
	return out, nil
}

// pruneImages removes copies older than age; a failure only leaves a file.
func pruneImages(dir string, age time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-age)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// selfExecutable is the running program, symlinks resolved. A seam for tests.
var selfExecutable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// checkOpen accepts a click action and returns it in canonical form, or ""
// for none. Two shapes are allowed, and the macOS helper checks the same rules
// again before acting on a click:
//
//   - an http or https URL whose host is 127.0.0.1, localhost or ::1, with no
//     user info: an app's own local web UI;
//   - an absolute path that resolves to this program's own executable, which is
//     run with no arguments and no shell.
func checkOpen(open string) (string, error) {
	if open == "" {
		return "", nil
	}
	bad := func(why string) error { return fmt.Errorf("%w: open %q: %s", ErrInvalid, open, why) }
	if lower := strings.ToLower(open); strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		u, err := url.Parse(open)
		if err != nil || u.Host == "" {
			return "", bad("is not a valid URL")
		}
		if u.User != nil {
			return "", bad("must not carry user info")
		}
		if !loopback(u.Hostname()) {
			return "", bad("must point at 127.0.0.1, localhost or ::1")
		}
		return u.String(), nil
	}
	if strings.Contains(open, "://") {
		return "", bad("only http and https URLs are allowed")
	}
	if !filepath.IsAbs(open) {
		return "", bad("must be an absolute path to this program, or a local http(s) URL")
	}
	resolved, err := filepath.EvalSymlinks(open)
	if err != nil {
		return "", bad("does not exist")
	}
	self, err := selfExecutable()
	if err != nil || resolved != self {
		return "", bad("only this program's own executable may be opened")
	}
	return resolved, nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && (ip.Equal(net.IPv4(127, 0, 0, 1)) || ip.Equal(net.IPv6loopback))
}
