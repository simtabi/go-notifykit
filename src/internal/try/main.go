// Command try posts one real notification from a throwaway app called
// "notifykit-try", so a developer can see the banner, its name and its icon.
// Run it with `make try` (which sets NOTIFYKIT_DESKTOP=1) or
// `NOTIFYKIT_DESKTOP=1 go -C src run ./internal/try -body "hello"`.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	notifykit "github.com/simtabi/go-notifykit/src"
)

func main() {
	title := flag.String("title", "notifykit-try", "the first line")
	subtitle := flag.String("subtitle", "", "the second line")
	body := flag.String("body", "If this banner says notifykit-try with a blue icon, the helper app works.", "the message")
	group := flag.String("group", "try", "replace an earlier banner in this group")
	backend := flag.String("backend", "", "force a backend: helper, osascript, notify-send, powershell")
	id := flag.String("id", "com.simtabi.notifykit-try", "bundle ID; a new one makes macOS ask for permission afresh")
	image := flag.String("image", "", "attach this image (absolute path to a PNG, JPEG or GIF)")
	open := flag.String("open", "", "on click, open this local http(s) URL")
	flag.Parse()

	if *backend != "" {
		_ = os.Setenv("NOTIFYKIT_BACKEND", *backend)
	}
	n, err := notifykit.New(notifykit.Config{
		AppName: "notifykit-try",
		AppID:   *id,
		Icon:    icon(),
		Log:     os.Stderr,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	res, err := n.Send(context.Background(), notifykit.Notification{Title: *title, Subtitle: *subtitle, Body: *body, Group: *group, Sound: true, Icon: *image, Open: *open})
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	case res.Skipped != "":
		fmt.Println("skipped:", res.Skipped)
	case res.Pending:
		fmt.Println("macOS is asking whether to allow notifications from notifykit-try; click Allow and the banner follows")
	default:
		fmt.Println("sent through", res.Backend)
	}
}

// icon draws a 1024px rounded blue square with a white bell-ish dot, so the
// helper has something recognisable to show.
func icon() []byte {
	const size, radius = 1024, 220
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	blue := color.NRGBA{R: 0x06, G: 0x6f, B: 0xd1, A: 0xff}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	inside := func(x, y int) bool {
		cx, cy := clamp(x, radius, size-radius), clamp(y, radius, size-radius)
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= radius*radius
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !inside(x, y) {
				continue
			}
			dx, dy := x-size/2, y-size/2
			if dx*dx+dy*dy <= 220*220 {
				img.SetNRGBA(x, y, white)
			} else {
				img.SetNRGBA(x, y, blue)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
