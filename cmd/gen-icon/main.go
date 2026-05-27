// Generates build/appicon.png — a 1024x1024 rounded-corner PNG used by Wails
// to derive every Windows icon size in build/windows/icon.ico.
//
// One-shot tool. Run from the repo root:
//
//	go run ./cmd/gen-icon
//
// Re-run whenever the design changes; commit the resulting build/appicon.png.
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

const size = 1024
const corner = 180

// Brand colours mirrored from frontend/tailwind.config.js (accent → accent2).
var (
	bgStart = color.RGBA{0x7c, 0x5c, 0xff, 0xff} // #7c5cff
	bgEnd   = color.RGBA{0x22, 0xd3, 0xee, 0xff} // #22d3ee
	white   = color.RGBA{0xff, 0xff, 0xff, 0xff}
	dark    = color.RGBA{0x18, 0x1a, 0x22, 0xff}
	label   = color.RGBA{0xd2, 0xd5, 0xde, 0xff}
)

func main() {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Diagonal gradient background.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			t := float64(x+y) / float64(2*size)
			img.SetRGBA(x, y, lerpRGBA(bgStart, bgEnd, t))
		}
	}

	// Floppy disk silhouette — classic "save" icon, drawn as filled rectangles.
	rect := func(x, y, w, h int, c color.RGBA) {
		draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{c}, image.Point{}, draw.Src)
	}
	// Body
	rect(212, 212, 600, 600, white)
	// Metal slider (top strip with notch)
	rect(292, 212, 440, 180, dark)
	rect(332, 232, 40, 140, white)
	// Label area with three lines of "text"
	rect(272, 492, 480, 280, label)
	rect(312, 532, 400, 40, dark)
	rect(312, 592, 400, 40, dark)
	rect(312, 652, 240, 40, dark)

	// Round the four corners by erasing pixels outside the corner radius.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if outsideRoundedCorner(x, y) {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}

	out, err := os.Create("build/appicon.png")
	must(err)
	defer out.Close()
	must(png.Encode(out, img))
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

func outsideRoundedCorner(x, y int) bool {
	check := func(cx, cy int) bool {
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy > corner*corner
	}
	switch {
	case x < corner && y < corner:
		return check(corner, corner)
	case x >= size-corner && y < corner:
		return check(size-corner, corner)
	case x < corner && y >= size-corner:
		return check(corner, size-corner)
	case x >= size-corner && y >= size-corner:
		return check(size-corner, size-corner)
	}
	return false
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
