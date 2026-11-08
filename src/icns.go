package notifykit

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// icnsTypes maps each ICNS PNG element to its pixel size. macOS picks the
// element that fits the place the icon is drawn: the 16 and 32 point ones in
// lists, the larger ones in banners and Settings.
var icnsTypes = []struct {
	tag  string
	size int
}{
	{"icp4", 16}, {"ic11", 32}, // 16pt @1x, @2x
	{"icp5", 32}, {"ic12", 64}, // 32pt
	{"ic07", 128}, {"ic13", 256}, // 128pt
	{"ic08", 256}, {"ic14", 512}, // 256pt
	{"ic09", 512}, {"ic10", 1024}, // 512pt
}

// makeICNS turns a square PNG into an .icns file holding a PNG element for
// every size up to the source's own; it never scales up.
func makeICNS(src []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("the icon is not a PNG: %w", err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() {
		return nil, fmt.Errorf("the icon is %dx%d; it must be square", b.Dx(), b.Dy())
	}
	if b.Dx() < 16 {
		return nil, fmt.Errorf("the icon is %dpx; it must be at least 16px", b.Dx())
	}
	cache := map[int][]byte{}
	var body bytes.Buffer
	for _, t := range icnsTypes {
		if t.size > b.Dx() {
			continue
		}
		data, ok := cache[t.size]
		if !ok {
			var enc bytes.Buffer
			if err := png.Encode(&enc, downscale(img, t.size)); err != nil {
				return nil, err
			}
			data = enc.Bytes()
			cache[t.size] = data
		}
		body.WriteString(t.tag)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	if body.Len() == 0 {
		return nil, errors.New("no icon size fits")
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// downscale resizes a square image to size x size by averaging each source
// area (a box filter), in premultiplied alpha so transparent edges do not
// darken.
func downscale(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	n := b.Dx()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0, y1 := b.Min.Y+y*n/size, b.Min.Y+(y+1)*n/size
		if y1 == y0 {
			y1 = y0 + 1
		}
		for x := 0; x < size; x++ {
			x0, x1 := b.Min.X+x*n/size, b.Min.X+(x+1)*n/size
			if x1 == x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, cnt uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA() // premultiplied, 16-bit
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
					cnt++
				}
			}
			if a == 0 {
				continue
			}
			// Un-premultiply: channel = sum(c) / sum(a) * 255.
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r * 255 / a),
				G: uint8(g * 255 / a),
				B: uint8(bl * 255 / a),
				A: uint8(a / cnt >> 8),
			})
		}
	}
	return dst
}
