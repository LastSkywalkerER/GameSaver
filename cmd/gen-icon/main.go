// Generates icon artefacts for the app:
//   build/appicon.png         — 1024×1024 PNG; Wails uses this as the source
//                                of build/windows/icon.ico on `wails build`.
//   internal/tray/icon.ico    — multi-resolution ICO (16/32/48/256) used as
//                                the system-tray icon (getlantern/systray
//                                wants ICO on Windows).
//
// Also deletes any stale build/windows/icon.ico so Wails regenerates from the
// fresh PNG (otherwise its cache keeps the old icon and the exe ships with
// the old face — exactly the bug we hit between v0.3.0 and v0.3.1).
//
// Run from repo root:
//
//	go run ./cmd/gen-icon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	xdraw "golang.org/x/image/draw"
)

const corner = 180

var (
	bgStart = color.RGBA{0x7c, 0x5c, 0xff, 0xff}
	bgEnd   = color.RGBA{0x22, 0xd3, 0xee, 0xff}
	white   = color.RGBA{0xff, 0xff, 0xff, 0xff}
	dark    = color.RGBA{0x18, 0x1a, 0x22, 0xff}
	label   = color.RGBA{0xd2, 0xd5, 0xde, 0xff}
)

func main() {
	master := render(1024)

	must(os.MkdirAll("build/windows", 0o755))
	must(writePNG("build/appicon.png", master))
	// Force Wails to regenerate build/windows/icon.ico from our PNG on the
	// next `wails build`; otherwise cached old .ico ends up in the exe.
	_ = os.Remove("build/windows/icon.ico")

	must(os.MkdirAll("internal/tray", 0o755))
	// Multi-resolution ICO for the system tray (Windows picks the closest
	// to the current DPI/notification-area size).
	must(writeICO("internal/tray/icon.ico", master, []int{16, 32, 48, 256}))
}

// render produces an RGBA icon at the given square size.
func render(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// Gradient background.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			t := float64(x+y) / float64(2*size)
			img.SetRGBA(x, y, lerpRGBA(bgStart, bgEnd, t))
		}
	}
	// Floppy disk silhouette, scaled to the requested size.
	s := float64(size) / 1024.0
	rect := func(x, y, w, h int, c color.RGBA) {
		x1 := int(float64(x) * s)
		y1 := int(float64(y) * s)
		x2 := int(float64(x+w) * s)
		y2 := int(float64(y+h) * s)
		draw.Draw(img, image.Rect(x1, y1, x2, y2), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	rect(212, 212, 600, 600, white)
	rect(292, 212, 440, 180, dark)
	rect(332, 232, 40, 140, white)
	rect(272, 492, 480, 280, label)
	rect(312, 532, 400, 40, dark)
	rect(312, 592, 400, 40, dark)
	rect(312, 652, 240, 40, dark)

	// Round corners (radius proportional to size).
	r := int(float64(corner) * s)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if outsideRoundedCorner(x, y, size, r) {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}
	return img
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// writeICO encodes the source image at each requested side length and packs
// the resulting PNG blobs into one .ico file. PNG-inside-ICO is supported by
// Windows Vista+ and reads cleanly on Win10/11.
func writeICO(path string, src image.Image, sizes []int) error {
	type entry struct {
		size int
		data []byte
	}
	entries := make([]entry, 0, len(sizes))
	for _, sz := range sizes {
		scaled := image.NewRGBA(image.Rect(0, 0, sz, sz))
		xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), xdraw.Over, nil)
		var buf bytes.Buffer
		if err := png.Encode(&buf, scaled); err != nil {
			return err
		}
		entries = append(entries, entry{size: sz, data: buf.Bytes()})
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// ICONDIR (6 bytes).
	_ = binary.Write(f, binary.LittleEndian, uint16(0))             // reserved
	_ = binary.Write(f, binary.LittleEndian, uint16(1))             // type = icon
	_ = binary.Write(f, binary.LittleEndian, uint16(len(entries))) // count

	// Each ICONDIRENTRY is 16 bytes; the image data follows after all entries.
	headerSize := 6 + 16*len(entries)
	offsets := make([]int, len(entries))
	cursor := headerSize
	for i, e := range entries {
		offsets[i] = cursor
		cursor += len(e.data)
	}
	for i, e := range entries {
		w := uint8(e.size)
		h := uint8(e.size)
		if e.size >= 256 {
			w, h = 0, 0 // ICO encodes 256 as 0
		}
		_ = binary.Write(f, binary.LittleEndian, w)
		_ = binary.Write(f, binary.LittleEndian, h)
		_ = binary.Write(f, binary.LittleEndian, uint8(0))               // palette size
		_ = binary.Write(f, binary.LittleEndian, uint8(0))               // reserved
		_ = binary.Write(f, binary.LittleEndian, uint16(1))              // planes
		_ = binary.Write(f, binary.LittleEndian, uint16(32))             // bpp
		_ = binary.Write(f, binary.LittleEndian, uint32(len(e.data)))   // size
		_ = binary.Write(f, binary.LittleEndian, uint32(offsets[i]))    // offset
	}
	for _, e := range entries {
		if _, err := f.Write(e.data); err != nil {
			return err
		}
	}
	return nil
}

func lerpRGBA(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	mix := func(av, bv uint8) uint8 { return uint8(float64(av) + (float64(bv)-float64(av))*t) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 0xff}
}

func outsideRoundedCorner(x, y, size, r int) bool {
	check := func(cx, cy int) bool {
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy > r*r
	}
	switch {
	case x < r && y < r:
		return check(r, r)
	case x >= size-r && y < r:
		return check(size-r, r)
	case x < r && y >= size-r:
		return check(r, size-r)
	case x >= size-r && y >= size-r:
		return check(size-r, size-r)
	}
	return false
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
