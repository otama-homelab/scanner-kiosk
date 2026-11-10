//go:build linux

// Package photonormalize decodes untrusted camera images only in its child.
package photonormalize

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"scanner-kiosk/internal/processrun"
)

const MaxInputBytes int64 = 32 << 20
const MaxNormalizedBytes int64 = 200 << 20
const MaxPixels int64 = 40_000_000
const MaxDimension = 12000
const maxMetadata = 1 << 20
const headerSize = 64

var ErrInvalid = errors.New("invalid photo")
var ErrLimit = errors.New("photo limit exceeded")
var ErrUnstopped = errors.New("photo child stop unknown")
var ErrProcess = errors.New("photo child failed")

type Prepared struct {
	Width, Height, Orientation int
	Offset, Size               int64
	SHA256                     [32]byte
}

// Prepare checks descriptors and framed output; it never decodes source pixels.
func Prepare(ctx context.Context, input, output *os.File, media string, maxOutput int64) (Prepared, error) {
	return prepare(ctx, input, output, media, maxOutput, nil)
}

// PrepareCrop reuses the isolated normalizer protocol; input is a frozen normalized PNG.
func PrepareCrop(ctx context.Context, input, output *os.File, crop image.Rectangle, maxOutput int64) (Prepared, error) {
	if crop.Min.X < 0 || crop.Min.Y < 0 || crop.Dx() < 1 || crop.Dy() < 1 || crop.Max.X > MaxDimension || crop.Max.Y > MaxDimension {
		return Prepared{}, ErrInvalid
	}
	return prepare(ctx, input, output, "image/png", maxOutput, []string{strconv.Itoa(crop.Min.X), strconv.Itoa(crop.Min.Y), strconv.Itoa(crop.Dx()), strconv.Itoa(crop.Dy())})
}
func prepare(ctx context.Context, input, output *os.File, media string, maxOutput int64, extra []string) (Prepared, error) {
	var p Prepared
	if input == nil || output == nil || (media != "image/jpeg" && media != "image/png") || maxOutput < 1 || maxOutput > 1<<30 {
		return p, ErrInvalid
	}
	in, e := input.Stat()
	inputLimit := MaxInputBytes
	if len(extra) != 0 {
		inputLimit = MaxNormalizedBytes
	}
	if e != nil || !in.Mode().IsRegular() || in.Size() < 1 || in.Size() > inputLimit {
		return p, ErrLimit
	}
	out, e := output.Stat()
	if e != nil || !out.Mode().IsRegular() || os.SameFile(in, out) || out.Size() != 0 || out.Mode().Perm()&0077 != 0 {
		return p, ErrInvalid
	}
	if _, e = input.Seek(0, 0); e != nil {
		return p, e
	}
	exe, e := os.Executable()
	if e != nil {
		return p, e
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r := processrun.Run(bounded, processrun.Spec{Path: exe, Args: append([]string{"__photo-normalize", media, strconv.FormatInt(maxOutput, 10)}, extra...), Env: []string{"GOMAXPROCS=2", "GOMEMLIMIT=512MiB"}, Stdin: input, Stdout: output, Grace: 200 * time.Millisecond})
	if !r.GroupStopped {
		return p, ErrUnstopped
	}
	if bounded.Err() != nil {
		return p, bounded.Err()
	}
	if r.Err != nil {
		var exit *exec.ExitError
		if errors.As(r.Err, &exit) {
			if exit.ExitCode() == 10 {
				return p, ErrInvalid
			}
			if exit.ExitCode() == 11 {
				return p, ErrLimit
			}
		}
		return p, ErrProcess
	}
	out, e = output.Stat()
	if e != nil || out.Size() < headerSize || out.Size() > headerSize+maxOutput {
		return p, ErrProcess
	}
	var h [headerSize]byte
	if _, e = output.ReadAt(h[:], 0); e != nil || string(h[:8]) != "PHOTO001" {
		return p, ErrProcess
	}
	p.Width = int(binary.BigEndian.Uint32(h[8:12]))
	p.Height = int(binary.BigEndian.Uint32(h[12:16]))
	p.Orientation = int(binary.BigEndian.Uint32(h[16:20]))
	p.Size = int64(binary.BigEndian.Uint64(h[24:32]))
	p.Offset = headerSize
	copy(p.SHA256[:], h[32:64])
	if !dimensions(p.Width, p.Height) || p.Orientation < 1 || p.Orientation > 8 || p.Size < 1 || p.Size != out.Size()-headerSize {
		return Prepared{}, ErrProcess
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, io.NewSectionReader(output, p.Offset, p.Size)); e != nil || !bytes.Equal(hash.Sum(nil), p.SHA256[:]) {
		return Prepared{}, ErrProcess
	}
	return p, nil
}
func dimensions(w, h int) bool {
	return w > 0 && h > 0 && w <= MaxDimension && h <= MaxDimension && int64(w)*int64(h) <= MaxPixels
}

// Child changes process-wide limits and must never be called by the API process.
func Child(args []string, input, output *os.File) int {
	if (len(args) != 2 && len(args) != 6) || input == nil || output == nil {
		return 10
	}
	limit, e := strconv.ParseInt(args[1], 10, 64)
	if e != nil || limit < 1 || limit > 1<<30 {
		return 10
	}
	if processrun.ApplyPDFLimits(limit+headerSize, processrun.PDFImportMemory) != nil {
		return 14
	}
	inputLimit := MaxInputBytes
	if len(args) == 6 {
		inputLimit = MaxNormalizedBytes
	}
	raw, e := io.ReadAll(io.LimitReader(input, inputLimit+1))
	if e != nil {
		return 10
	}
	if int64(len(raw)) > inputLimit {
		return 11
	}
	var data []byte
	var p Prepared
	if len(args) == 2 {
		data, p, e = normalize(raw, args[0], limit)
	} else {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || format != "png" || !dimensions(cfg.Width, cfg.Height) {
			return 10
		}
		p.Width, p.Height = cfg.Width, cfg.Height
		coords := [4]int{}
		for i := range coords {
			v, err := strconv.Atoi(args[i+2])
			if err != nil || v < 0 || v > MaxDimension {
				return 10
			}
			coords[i] = v
		}
		rect := image.Rect(coords[0], coords[1], coords[0]+coords[2], coords[1]+coords[3])
		if args[0] != "image/png" || rect.Empty() || !rect.In(image.Rect(0, 0, p.Width, p.Height)) {
			return 10
		}
		im, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return 10
		}
		cropped := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
		draw.Draw(cropped, cropped.Bounds(), im, rect.Min, draw.Src)
		buffer := &boundedBuffer{limit: limit}
		e = png.Encode(buffer, cropped)
		data = buffer.Bytes()
		p.Width = rect.Dx()
		p.Height = rect.Dy()
		p.Orientation = 1
	}
	if errors.Is(e, ErrLimit) {
		return 11
	}
	if e != nil {
		return 10
	}
	var h [headerSize]byte
	copy(h[:], "PHOTO001")
	binary.BigEndian.PutUint32(h[8:12], uint32(p.Width))
	binary.BigEndian.PutUint32(h[12:16], uint32(p.Height))
	binary.BigEndian.PutUint32(h[16:20], uint32(p.Orientation))
	binary.BigEndian.PutUint64(h[24:32], uint64(len(data)))
	digest := sha256.Sum256(data)
	copy(h[32:64], digest[:])
	if _, e = output.Write(h[:]); e != nil {
		return 14
	}
	if _, e = output.Write(data); e != nil {
		return 14
	}
	return 0
}

type boundedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-int64(b.Len()) {
		return 0, ErrLimit
	}
	return b.Buffer.Write(p)
}
func normalize(raw []byte, media string, limit int64) ([]byte, Prepared, error) {
	var p Prepared
	if int64(len(raw)) > MaxInputBytes {
		return nil, p, ErrLimit
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || "image/"+format != media || (format != "jpeg" && format != "png") {
		return nil, p, ErrInvalid
	}
	if !dimensions(cfg.Width, cfg.Height) {
		return nil, p, ErrLimit
	}
	if format == "jpeg" && (len(raw) < 4 || raw[len(raw)-2] != 0xff || raw[len(raw)-1] != 0xd9) {
		return nil, p, ErrInvalid
	}
	orientation, e := orientation(raw, format)
	if e != nil {
		return nil, p, e
	}
	im, actual, e := image.Decode(bytes.NewReader(raw))
	if e != nil || actual != format || im.Bounds().Dx() != cfg.Width || im.Bounds().Dy() != cfg.Height {
		return nil, p, ErrInvalid
	}
	w, h := cfg.Width, cfg.Height
	if orientation >= 5 {
		w, h = h, w
	}
	normalized := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < cfg.Height; y++ {
		for x := 0; x < cfg.Width; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = cfg.Width - 1 - x
			case 3:
				dx, dy = cfg.Width-1-x, cfg.Height-1-y
			case 4:
				dy = cfg.Height - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = cfg.Height-1-y, x
			case 7:
				dx, dy = cfg.Height-1-y, cfg.Width-1-x
			case 8:
				dx, dy = y, cfg.Width-1-x
			}
			normalized.Set(dx, dy, im.At(x+im.Bounds().Min.X, y+im.Bounds().Min.Y))
		}
	}
	b := &boundedBuffer{limit: limit}
	if e = png.Encode(b, normalized); e != nil {
		return nil, p, e
	}
	p.Width, p.Height, p.Orientation = w, h, orientation
	return b.Bytes(), p, nil
}

// Only one bounded IFD0 and its orientation SHORT are interpreted. Other tags
// are ignored and every derived image is encoded afresh without metadata.
func tiffOrientation(raw []byte) (int, error) {
	if len(raw) < 8 {
		return 1, nil
	}
	var order binary.ByteOrder
	switch string(raw[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1, nil
	}
	if order.Uint16(raw[2:4]) != 42 {
		return 1, nil
	}
	offset := uint64(order.Uint32(raw[4:8]))
	if offset < 8 || offset+2 > uint64(len(raw)) {
		return 0, ErrInvalid
	}
	count := uint64(order.Uint16(raw[offset : offset+2]))
	if count > 256 {
		return 0, ErrLimit
	}
	if offset+2+12*count+4 > uint64(len(raw)) {
		return 0, ErrInvalid
	}
	found := false
	orientation := 1
	for i := uint64(0); i < count; i++ {
		entry := raw[offset+2+i*12 : offset+14+i*12]
		if order.Uint16(entry[:2]) != 0x112 {
			continue
		}
		if found || order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 0, ErrInvalid
		}
		found = true
		orientation = int(order.Uint16(entry[8:10]))
		if orientation < 1 || orientation > 8 {
			return 0, ErrInvalid
		}
	}
	return orientation, nil
}
func orientation(raw []byte, format string) (int, error) {
	result, seen, total := 1, false, 0
	accept := func(data []byte) error {
		if seen {
			return ErrInvalid
		}
		seen = true
		var e error
		result, e = tiffOrientation(data)
		return e
	}
	if format == "jpeg" {
		for pos := 2; pos < len(raw); {
			if raw[pos] != 0xff {
				return 0, ErrInvalid
			}
			for pos < len(raw) && raw[pos] == 0xff {
				pos++
			}
			if pos >= len(raw) {
				return 0, ErrInvalid
			}
			marker := raw[pos]
			pos++
			if marker == 0xda || marker == 0xd9 {
				break
			}
			if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
				continue
			}
			if pos+2 > len(raw) {
				return 0, ErrInvalid
			}
			length := int(binary.BigEndian.Uint16(raw[pos : pos+2]))
			if length < 2 || length > len(raw)-pos {
				return 0, ErrInvalid
			}
			segment := raw[pos+2 : pos+length]
			pos += length
			if marker >= 0xe0 && marker <= 0xef || marker == 0xfe {
				total += length
				if total > maxMetadata {
					return 0, ErrLimit
				}
			}
			if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
				if e := accept(segment[6:]); e != nil {
					return 0, e
				}
			}
		}
	} else {
		for pos := 8; pos+12 <= len(raw); {
			length := uint64(binary.BigEndian.Uint32(raw[pos : pos+4]))
			if length > uint64(len(raw)-pos-12) {
				return 0, ErrInvalid
			}
			kind := string(raw[pos+4 : pos+8])
			data := raw[pos+8 : pos+8+int(length)]
			pos += 12 + int(length)
			if kind != "IDAT" {
				total += int(length)
				if total > maxMetadata {
					return 0, ErrLimit
				}
			}
			if kind == "eXIf" {
				if e := accept(data); e != nil {
					return 0, e
				}
			}
			if kind == "IEND" {
				if pos != len(raw) {
					return 0, ErrInvalid
				}
				break
			}
		}
	}
	return result, nil
}
