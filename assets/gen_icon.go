//go:build ignore

// gen_icon turns the master artwork into the files a released binary needs.
//
//	go run assets/gen_icon.go                       # from the committed master
//	go run assets/gen_icon.go -in some-artwork.png  # from a new one
//
// It writes assets/tulipe.png (the 512-pixel master, and the icon a Linux
// desktop entry points at) and assets/tulipe.ico (the one Windows Explorer
// shows on the .exe). Turning the .ico into the object the linker embeds is
// one more command, kept out of here because it is someone else's tool:
//
//	go run github.com/akavel/rsrc@v0.10.2 -ico assets/tulipe.ico \
//	    -arch amd64 -o cmd/tulipe/rsrc_windows_amd64.syso
//
// The results are committed, so building Tulipe needs none of this.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

func main() {
	in := flag.String("in", "assets/tulipe.png", "artwork to start from")
	outPNG := flag.String("png", "assets/tulipe.png", "512-pixel master to write")
	outICO := flag.String("ico", "assets/tulipe.ico", "Windows icon to write")
	flag.Parse()

	src, err := readPNG(*in)
	if err != nil {
		log.Fatal(err)
	}
	// Artwork exported as RGB has opaque white where the rounded corners
	// should show nothing. On a dark taskbar that reads as a white square
	// with the icon inside it, so the white is cut away and the result
	// trimmed to what is left.
	trimmed := trim(cutBackground(src))

	master := resize(trimmed, 512)
	if err := writePNG(*outPNG, master); err != nil {
		log.Fatal(err)
	}

	// The sizes Windows actually asks for, largest first, which is the order
	// an .ico is conventionally written in.
	sizes := []int{256, 128, 64, 48, 32, 16}
	images := make([]*image.NRGBA, 0, len(sizes))
	for _, s := range sizes {
		images = append(images, resize(trimmed, s))
	}
	if err := writeICO(*outICO, images); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s (512×512) and %s (%d sizes) written from %s\n", *outPNG, *outICO, len(sizes), *in)
}

func readPNG(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out, nil
}

func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// cutBackground makes transparent the near-white pixels reachable from the
// edges. Flooding inwards rather than testing every pixel is what keeps the
// white inside the drawing — the pages of the book, the lettering — opaque.
func cutBackground(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := image.NewNRGBA(src.Bounds())
	copy(out.Pix, src.Pix)

	white := func(x, y int) bool {
		c := src.NRGBAAt(x, y)
		return c.A > 0 && c.R > 235 && c.G > 235 && c.B > 235
	}
	seen := make([]bool, w*h)
	var queue [][2]int
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= w || y >= h || seen[y*w+x] || !white(x, y) {
			return
		}
		seen[y*w+x] = true
		queue = append(queue, [2]int{x, y})
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		push(x, h-1)
	}
	for y := 0; y < h; y++ {
		push(0, y)
		push(w-1, y)
	}
	for len(queue) > 0 {
		p := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		out.SetNRGBA(p[0], p[1], color.NRGBA{})
		push(p[0]-1, p[1])
		push(p[0]+1, p[1])
		push(p[0], p[1]-1)
		push(p[0], p[1]+1)
	}
	return out
}

// trim crops to what is left visible, then pads back to a square so the icon
// is not stretched by the resize.
func trim(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	minX, minY, maxX, maxY := w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if src.NRGBAAt(x, y).A == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX || maxY < minY {
		return src // nothing opaque: leave it alone rather than crop to nothing
	}

	side := max(maxX-minX+1, maxY-minY+1)
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	offX := (side - (maxX - minX + 1)) / 2
	offY := (side - (maxY - minY + 1)) / 2
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			out.SetNRGBA(offX+x-minX, offY+y-minY, src.NRGBAAt(x, y))
		}
	}
	return out
}

// resize averages the source pixels each output pixel covers. Downscaling is
// all this ever does, and an area average is what keeps thin strokes — the
// leaf edges, the lettering — from breaking up at 16 pixels.
func resize(src *image.NRGBA, side int) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		y0, y1 := y*h/side, (y+1)*h/side
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < side; x++ {
			x0, x1 := x*w/side, (x+1)*w/side
			if x1 <= x0 {
				x1 = x0 + 1
			}
			// Colours are averaged weighted by alpha: a transparent pixel has
			// no colour to contribute, and letting its zeroes in would darken
			// every edge.
			var r, g, b, a, weight float64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := src.NRGBAAt(sx, sy)
					af := float64(c.A) / 255
					r += float64(c.R) * af
					g += float64(c.G) * af
					b += float64(c.B) * af
					a += float64(c.A)
					weight += af
				}
			}
			n := float64((y1 - y0) * (x1 - x0))
			if weight == 0 {
				out.SetNRGBA(x, y, color.NRGBA{})
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{
				R: clamp8(r / weight),
				G: clamp8(g / weight),
				B: clamp8(b / weight),
				A: clamp8(a / n),
			})
		}
	}
	return out
}

func clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	default:
		return uint8(v + 0.5)
	}
}

// writeICO writes an icon directory whose entries are PNG images. Windows has
// read PNG-compressed entries since Vista, and they are a fraction of the size
// of the DIB form.
func writeICO(path string, images []*image.NRGBA) error {
	var payloads [][]byte
	for _, img := range images {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		payloads = append(payloads, buf.Bytes())
	}

	var out bytes.Buffer
	// ICONDIR: reserved, type 1 (icon), count.
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(images))})

	const dirEntry = 16
	offset := 6 + dirEntry*len(images)
	for i, img := range images {
		side := img.Bounds().Dx()
		// 256 is written as 0: the field is one byte.
		dim := byte(side)
		out.WriteByte(dim)
		out.WriteByte(dim)
		out.WriteByte(0)                                    // palette colours: none, the image is true colour
		out.WriteByte(0)                                    // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1))  // colour planes
		binary.Write(&out, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&out, binary.LittleEndian, uint32(len(payloads[i])))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(payloads[i])
	}
	for _, p := range payloads {
		out.Write(p)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}
