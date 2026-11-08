package notifykit

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// RunCLI is a ready-made `notify` command for an application to mount, so
// every app offers the same three verbs:
//
//	<app> notify send   [flags]   show a notification from this process
//	<app> notify ping   [flags]   hand one to the running app (its Listen)
//	<app> notify doctor           say whether notifications work, and why not
//
// Flags: -title, -subtitle, -body, -group, -level info|warn|alert, -sound,
// and -json to read one notification as JSON from stdin instead. It returns
// the process exit status: 0 done, 1 failed, 2 usage.
func RunCLI(ctx context.Context, n *Notifier, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := func() int {
		_, _ = fmt.Fprintf(stderr, "usage: %s notify send|ping|doctor [-title T] [-subtitle S] [-body B] [-group G] [-level info|warn|alert] [-sound] [-json]\n", n.cfg.AppName)
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "doctor":
		return doctor(n, stdout)
	case "send", "ping":
	default:
		return usage()
	}

	fs := flag.NewFlagSet(n.cfg.AppName+" notify "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var nt Notification
	var level string
	var fromJSON bool
	fs.StringVar(&nt.Title, "title", "", "the first line (default: the app name)")
	fs.StringVar(&nt.Subtitle, "subtitle", "", "the second line")
	fs.StringVar(&nt.Body, "body", "", "the message")
	fs.StringVar(&nt.Group, "group", "", "replace an earlier notification in this group")
	fs.StringVar(&level, "level", "info", "info, warn or alert")
	fs.BoolVar(&nt.Sound, "sound", false, "play the default sound")
	fs.BoolVar(&fromJSON, "json", false, "read one notification as JSON from stdin")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if fromJSON {
		dec := json.NewDecoder(io.LimitReader(stdin, maxMessage))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&nt); err != nil {
			_, _ = fmt.Fprintln(stderr, "bad JSON on stdin:", err)
			return 2
		}
	} else {
		lv, err := ParseLevel(level)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		nt.Level = lv
	}

	var res Result
	var err error
	if verb == "send" {
		res, err = n.Send(ctx, nt)
	} else {
		var ep Endpoint
		if ep, err = n.Endpoint(); err == nil {
			res, err = Ping(ctx, ep, nt)
		}
	}
	switch {
	case err != nil:
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	case res.Skipped != "":
		_, _ = fmt.Fprintln(stdout, "not shown:", res.Skipped)
	case res.Pending:
		_, _ = fmt.Fprintf(stdout, "macOS is asking whether to allow notifications from %s; this one shows once you allow it\n", n.cfg.AppName)
	case res.Suppressed:
		_, _ = fmt.Fprintln(stdout, "not shown: a notification in this group was shown moments ago")
	default:
		_, _ = fmt.Fprintln(stdout, "shown through", res.Backend)
	}
	return 0
}

func doctor(n *Notifier, stdout io.Writer) int {
	ok, why := n.Enabled()
	if ok {
		_, _ = fmt.Fprintln(stdout, "desktop notifications: on")
	} else {
		_, _ = fmt.Fprintln(stdout, "desktop notifications: off -", why)
	}
	backend, err := n.Backend()
	switch {
	case err != nil:
		_, _ = fmt.Fprintln(stdout, "backend: none -", err)
	default:
		_, _ = fmt.Fprintln(stdout, "backend:", backend)
	}
	if ep, err := n.Endpoint(); err == nil {
		_, _ = fmt.Fprintln(stdout, "ping socket:", ep.Socket)
	}
	if err != nil || !ok {
		return 1
	}
	return 0
}

// ParseLevel reads "info", "warn" or "alert".
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return Info, nil
	case "warn", "warning":
		return Warn, nil
	case "alert":
		return Alert, nil
	}
	return Info, errors.New("level must be info, warn or alert")
}
