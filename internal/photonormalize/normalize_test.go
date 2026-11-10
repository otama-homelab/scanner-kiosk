//go:build linux

package photonormalize

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPhotoCropChildBounds(t *testing.T) {
	for _, mode := range []string{"pixels", "negative", "outside", "empty", "output-limit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			im := image.NewNRGBA(image.Rect(0, 0, 8, 6))
			for y := 0; y < 6; y++ {
				for x := 0; x < 8; x++ {
					im.SetNRGBA(x, y, color.NRGBA{uint8(x * 20), uint8(y * 30), 80, 255})
				}
			}
			var b bytes.Buffer
			if e := png.Encode(&b, im); e != nil {
				t.Fatal(e)
			}
			input, e := os.OpenFile(filepath.Join(dir, "input.png"), os.O_CREATE|os.O_RDWR, 0600)
			if e != nil {
				t.Fatal(e)
			}
			defer input.Close()
			if _, e = input.Write(b.Bytes()); e != nil {
				t.Fatal(e)
			}
			output, e := os.OpenFile(filepath.Join(dir, "output"), os.O_CREATE|os.O_RDWR, 0600)
			if e != nil {
				t.Fatal(e)
			}
			defer output.Close()
			rect := image.Rect(2, 1, 6, 5)
			limit := int64(1 << 20)
			ctx := context.Background()
			switch mode {
			case "negative":
				rect.Min.X = -1
			case "outside":
				rect.Max.X = 9
			case "empty":
				rect.Max = rect.Min
			case "output-limit":
				limit = 1
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			p, e := PrepareCrop(ctx, input, output, rect, limit)
			if mode != "pixels" {
				if e == nil {
					t.Fatal("invalid crop accepted")
				}
				if mode == "output-limit" && !errors.Is(e, ErrLimit) {
					t.Fatal(e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			data, e := io.ReadAll(io.NewSectionReader(output, p.Offset, p.Size))
			if e != nil {
				t.Fatal(e)
			}
			cropped, e := png.Decode(bytes.NewReader(data))
			if e != nil {
				t.Fatal(e)
			}
			if p.Width != 4 || p.Height != 4 {
				t.Fatal(p)
			}
			for _, point := range []image.Point{{0, 0}, {3, 0}, {0, 3}, {3, 3}} {
				got := color.NRGBAModel.Convert(cropped.At(point.X, point.Y))
				want := im.NRGBAAt(point.X+2, point.Y+1)
				if got != want {
					t.Fatal("wrong child crop pixels", point, got, want)
				}
			}
		})
	}
}

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__photo-normalize" {
		os.Exit(Child(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}
func exif(o int) []byte {
	return []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, byte(o), 0, 0, 0, 0, 0, 0, 0}
}
func chunk(kind string, data []byte) []byte {
	out := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(out[:4], uint32(len(data)))
	copy(out[4:8], kind)
	copy(out[8:], data)
	binary.BigEndian.PutUint32(out[len(out)-4:], crc32.ChecksumIEEE(out[4:len(out)-4]))
	return out
}
func withEXIF(raw []byte, format string, data []byte) []byte {
	if format == "jpeg" {
		segment := append([]byte("Exif\x00\x00"), data...)
		out := []byte{0xff, 0xd8, 0xff, 0xe1, byte((len(segment) + 2) >> 8), byte(len(segment) + 2)}
		out = append(out, segment...)
		return append(out, raw[2:]...)
	}
	out := append([]byte(nil), raw[:33]...)
	out = append(out, chunk("eXIf", data)...)
	return append(out, raw[33:]...)
}
func synthetic(t *testing.T, format string, im image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&b, im)
	} else {
		err = jpeg.Encode(&b, im, &jpeg.Options{Quality: 100})
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func quadrants() image.Image {
	im := image.NewNRGBA(image.Rect(0, 0, 80, 48))
	colors := []color.NRGBA{{240, 20, 20, 255}, {20, 230, 20, 255}, {20, 20, 240, 255}, {240, 220, 20, 255}}
	for y := 0; y < 48; y++ {
		for x := 0; x < 80; x++ {
			im.SetNRGBA(x, y, colors[(y/24)*2+x/40])
		}
	}
	return im
}
func TestPhotoNormalizeOrientations(t *testing.T) {
	// Independent clockwise corner order, including transpose/transverse mirrors.
	corners := [][4]int{{0, 1, 2, 3}, {1, 0, 3, 2}, {3, 2, 1, 0}, {2, 3, 0, 1}, {0, 2, 1, 3}, {2, 0, 3, 1}, {3, 1, 2, 0}, {1, 3, 0, 2}}
	for _, format := range []string{"jpeg", "png"} {
		for o := 1; o <= 8; o++ {
			t.Run(format+string(rune('0'+o)), func(t *testing.T) {
				raw := synthetic(t, format, quadrants())
				base, _, err := image.Decode(bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				data, p, err := normalize(withEXIF(raw, format, exif(o)), "image/"+format, 1<<20)
				if err != nil {
					t.Fatal(err)
				}
				w, h := 80, 48
				if o >= 5 {
					w, h = h, w
				}
				if p.Width != w || p.Height != h || p.Orientation != o {
					t.Fatal("orientation geometry", p)
				}
				im, _, err := image.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				source := []image.Point{{20, 12}, {60, 12}, {20, 36}, {60, 36}}
				output := []image.Point{{w / 4, h / 4}, {3 * w / 4, h / 4}, {w / 4, 3 * h / 4}, {3 * w / 4, 3 * h / 4}}
				for i, at := range output {
					want := color.NRGBAModel.Convert(base.At(source[corners[o-1][i]].X, source[corners[o-1][i]].Y))
					got := color.NRGBAModel.Convert(im.At(at.X, at.Y))
					if got != want {
						t.Fatalf("corner %d: %v want %v", i, got, want)
					}
				}
				if bytes.Contains(data, []byte("eXIf")) || bytes.Contains(data, []byte("Exif")) {
					t.Fatal("metadata retained")
				}
			})
		}
	}
	for _, mode := range []string{"absent", "ignored-malformed", "palette", "alpha", "grayscale", "gray16", "rgba16", "big-endian"} {
		t.Run(mode, func(t *testing.T) {
			var im image.Image = quadrants()
			switch mode {
			case "palette":
				p := image.NewPaletted(image.Rect(0, 0, 3, 2), color.Palette{color.NRGBA{20, 40, 60, 255}, color.NRGBA{70, 80, 90, 255}})
				p.SetColorIndex(2, 1, 1)
				im = p
			case "alpha":
				p := image.NewNRGBA(image.Rect(0, 0, 3, 2))
				p.SetNRGBA(2, 1, color.NRGBA{20, 40, 60, 80})
				im = p
			case "grayscale":
				p := image.NewGray(image.Rect(0, 0, 3, 2))
				p.SetGray(2, 1, color.Gray{Y: 127})
				im = p
			case "gray16":
				p := image.NewGray16(image.Rect(0, 0, 3, 2))
				p.SetGray16(2, 1, color.Gray16{Y: 32768})
				im = p
			case "rgba16":
				p := image.NewNRGBA64(image.Rect(0, 0, 3, 2))
				p.SetNRGBA64(2, 1, color.NRGBA64{R: 10000, G: 20000, B: 30000, A: 40000})
				im = p
			}
			raw := synthetic(t, "png", im)
			if mode == "ignored-malformed" {
				raw = withEXIF(raw, "png", []byte("uninterpreted-GPS-text"))
			}
			if mode == "big-endian" {
				raw = withEXIF(raw, "png", []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 1, 0x12, 0, 3, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0})
			}
			data, p, err := normalize(raw, "image/png", 1<<20)
			if err != nil || p.Orientation != 1 {
				t.Fatal(p, err)
			}
			decoded, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < im.Bounds().Dy(); y++ {
				for x := 0; x < im.Bounds().Dx(); x++ {
					want := color.NRGBAModel.Convert(im.At(x, y))
					got := color.NRGBAModel.Convert(decoded.At(x, y))
					if got != want {
						t.Fatalf("pixel %d,%d: %v != %v", x, y, got, want)
					}
				}
			}
			if bytes.Contains(data, []byte("GPS")) {
				t.Fatal("private metadata retained")
			}
		})
	}
}
func TestPhotoNormalizeRejectsInvalidAndBounded(t *testing.T) {
	pngRaw := synthetic(t, "png", quadrants())
	jpegRaw := synthetic(t, "jpeg", quadrants())
	oversized := append([]byte(nil), pngRaw...)
	binary.BigEndian.PutUint32(oversized[16:20], MaxDimension+1)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	pixels := append([]byte(nil), pngRaw...)
	binary.BigEndian.PutUint32(pixels[16:20], 10000)
	binary.BigEndian.PutUint32(pixels[20:24], 10000)
	binary.BigEndian.PutUint32(pixels[29:33], crc32.ChecksumIEEE(pixels[12:29]))
	offset := exif(1)
	binary.LittleEndian.PutUint32(offset[4:8], 0xfffffffe)
	duplicate := append([]byte(nil), exif(1)...)
	binary.LittleEndian.PutUint16(duplicate[8:10], 2)
	duplicate = append(duplicate[:22], append(duplicate[10:22], 0, 0, 0, 0)...)
	cases := []struct {
		name, media string
		raw         []byte
		limit       int64
	}{
		{"heic", "image/jpeg", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00heicmif1"), 1 << 20},
		{"wrong-media", "image/jpeg", pngRaw, 1 << 20}, {"truncated-png", "image/png", pngRaw[:len(pngRaw)-7], 1 << 20}, {"truncated-jpeg", "image/jpeg", jpegRaw[:len(jpegRaw)/2], 1 << 20},
		{"trailing-jpeg", "image/jpeg", append(append([]byte(nil), jpegRaw...), []byte("unaccepted trailing payload")...), 1 << 20},
		{"byte-bound", "image/png", make([]byte, MaxInputBytes+1), 1 << 20}, {"dimension-bound", "image/png", oversized, 1 << 20}, {"pixel-bound", "image/png", pixels, 1 << 20},
		{"invalid-offset", "image/jpeg", withEXIF(jpegRaw, "jpeg", offset), 1 << 20}, {"duplicate-tag", "image/jpeg", withEXIF(jpegRaw, "jpeg", duplicate), 1 << 20},
		{"duplicate-exif", "image/png", withEXIF(withEXIF(pngRaw, "png", exif(1)), "png", exif(1)), 1 << 20},
		{"invalid-orientation", "image/png", withEXIF(pngRaw, "png", exif(9)), 1 << 20},
		{"metadata-bound", "image/png", append(append(append([]byte(nil), pngRaw[:33]...), chunk("tEXt", make([]byte, maxMetadata+1))...), pngRaw[33:]...), 1 << 20},
		{"output-bound", "image/png", pngRaw, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, _, err := normalize(c.raw, c.media, c.limit)
			if err == nil || len(data) != 0 {
				t.Fatal("unsafe photo accepted", c.name)
			}
		})
	}
	t.Run("child-timeout", func(t *testing.T) {
		root := t.TempDir()
		input, err := os.OpenFile(filepath.Join(root, "in"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		input.Write(pngRaw)
		output, err := os.OpenFile(filepath.Join(root, "out"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer output.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		_, err = Prepare(ctx, input, output, "image/png", 1<<20)
		if err == nil {
			t.Fatal("timed child accepted")
		}
		if stat, e := output.Stat(); e != nil || stat.Size() != 0 {
			t.Fatal("timeout published output", e)
		}
	})
}
