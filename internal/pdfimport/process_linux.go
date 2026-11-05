//go:build linux

package pdfimport

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
	"scanner-kiosk/internal/processrun"
)

var ErrProcess = errors.New("PDF processing child failed")

// ErrUnstopped requires reconciliation before releasing the job or reservation.
var ErrUnstopped = errors.New("PDF processing child group could not be confirmed stopped")

// PreparedPage refers to a validated standalone PDF inside the caller-owned
// output file. Copy its section into registered artifacts without loading all
// imported pages into the parent process's heap.
type PreparedPage struct {
	WidthPoints, HeightPoints float64
	Offset, Size              int64
}

// Prepare runs the same executable with empty credentials/configuration and hard
// resource limits. The caller owns sealed private input and an empty private
// scratch output, including their reservation, cancellation and recovery lifetime.
// It never parses PDF content in the caller or returns partially prepared pages.
func Prepare(ctx context.Context, input, output *os.File, options Options) ([]PreparedPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validOptions(options) || input == nil || output == nil {
		return nil, ErrInvalid
	}
	in, err := input.Stat()
	if err != nil {
		return nil, ErrInvalid
	}
	out, err := output.Stat()
	if err != nil {
		return nil, ErrInvalid
	}
	if !in.Mode().IsRegular() || !out.Mode().IsRegular() || os.SameFile(in, out) || out.Size() != 0 || out.Mode().Perm()&0077 != 0 {
		return nil, ErrInvalid
	}
	if in.Size() > options.MaxInputBytes {
		return nil, ErrLimit
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return nil, ErrInvalid
	}
	if _, err = output.Seek(0, io.SeekStart); err != nil {
		return nil, ErrInvalid
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, ErrProcess
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := processrun.Run(bounded, processrun.Spec{Path: executable, Args: []string{"__pdf-import", strconv.FormatInt(options.MaxInputBytes, 10), strconv.FormatInt(options.MaxOutputBytes, 10), strconv.Itoa(options.MaxPages)}, Env: []string{"GOMAXPROCS=2", "GOMEMLIMIT=256MiB"}, Stdin: input, Stdout: output, Grace: 200 * time.Millisecond})
	if !result.GroupStopped {
		return nil, ErrUnstopped
	}
	if bounded.Err() != nil {
		return nil, bounded.Err()
	}
	if result.Err != nil {
		var exit *exec.ExitError
		if errors.As(result.Err, &exit) {
			switch exit.ExitCode() {
			case 10:
				return nil, ErrInvalid
			case 11:
				return nil, ErrLimit
			case 12:
				return nil, ErrUnsafe
			case 13:
				return nil, ErrEncrypted
			}
		}
		return nil, ErrProcess
	}
	size, err := output.Stat()
	if err != nil || size.Size() > frameLimit(options) {
		return nil, ErrLimit
	}
	if _, err = output.Seek(0, io.SeekStart); err != nil {
		return nil, ErrProcess
	}
	return decodePages(output, size.Size(), options)
}

func frameLimit(options Options) int64 {
	return 12 + int64(options.MaxPages)*24 + options.MaxOutputBytes
}

// Child is exclusively the internal command entry point. It changes limits for
// its entire process and must never run inside an API/worker process.
func Child(args []string, input, output *os.File) int {
	if len(args) != 3 || input == nil || output == nil {
		return 10
	}
	inputBytes, e1 := strconv.ParseInt(args[0], 10, 64)
	outputBytes, e2 := strconv.ParseInt(args[1], 10, 64)
	maxPages, e3 := strconv.Atoi(args[2])
	options := Options{MaxInputBytes: inputBytes, MaxOutputBytes: outputBytes, MaxPages: maxPages}
	if e1 != nil || e2 != nil || e3 != nil || !validOptions(options) {
		return 10
	}
	if applyChildLimits(frameLimit(options)) != nil {
		return 14
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := io.ReadAll(io.LimitReader(input, options.MaxInputBytes+1))
	if err != nil {
		return 10
	}
	pages, err := Split(ctx, raw, options)
	if err != nil {
		switch {
		case errors.Is(err, ErrLimit):
			return 11
		case errors.Is(err, ErrUnsafe):
			return 12
		case errors.Is(err, ErrEncrypted):
			return 13
		default:
			return 10
		}
	}
	var header bytes.Buffer
	header.WriteString("SCNPDF01")
	_ = binary.Write(&header, binary.BigEndian, uint32(len(pages)))
	if _, err = output.Write(header.Bytes()); err != nil {
		return 14
	}
	for _, page := range pages {
		header.Reset()
		_ = binary.Write(&header, binary.BigEndian, page.WidthPoints)
		_ = binary.Write(&header, binary.BigEndian, page.HeightPoints)
		_ = binary.Write(&header, binary.BigEndian, uint64(len(page.PDF)))
		if _, err = output.Write(header.Bytes()); err != nil {
			return 14
		}
		if _, err = output.Write(page.PDF); err != nil {
			return 14
		}
	}
	return 0
}

func applyChildLimits(outputBytes int64) error {
	for resource, limit := range map[int]unix.Rlimit{
		unix.RLIMIT_CORE:   {Cur: 0, Max: 0},
		unix.RLIMIT_CPU:    {Cur: 30, Max: 31},
		unix.RLIMIT_DATA:   {Cur: 512 << 20, Max: 512 << 20},
		unix.RLIMIT_AS:     {Cur: 2 << 30, Max: 2 << 30},
		unix.RLIMIT_FSIZE:  {Cur: uint64(outputBytes), Max: uint64(outputBytes)},
		unix.RLIMIT_NOFILE: {Cur: 32, Max: 32},
	} {
		if err := unix.Setrlimit(resource, &limit); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	debug.SetMemoryLimit(256 << 20)
	debug.SetMaxThreads(16)
	return nil
}

func decodePages(reader io.ReadSeeker, size int64, options Options) ([]PreparedPage, error) {
	if !validOptions(options) || size > frameLimit(options) || size < 12 {
		return nil, ErrProcess
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, ErrProcess
	}
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil || string(header[:8]) != "SCNPDF01" {
		return nil, ErrProcess
	}
	count := binary.BigEndian.Uint32(header[8:])
	if count < 1 || uint64(count) > uint64(options.MaxPages) {
		return nil, ErrProcess
	}
	pages := make([]PreparedPage, 0, int(count))
	var total int64
	offset := int64(12)
	for range int(count) {
		var width, height float64
		var length uint64
		if binary.Read(reader, binary.BigEndian, &width) != nil || binary.Read(reader, binary.BigEndian, &height) != nil || binary.Read(reader, binary.BigEndian, &length) != nil {
			return nil, ErrProcess
		}
		offset += 24
		if !finiteDimension(width) || !finiteDimension(height) || length < 5 || offset > size || length > uint64(size-offset) || length > uint64(options.MaxOutputBytes-total) {
			return nil, ErrProcess
		}
		var prefix [5]byte
		if _, err := io.ReadFull(reader, prefix[:]); err != nil || string(prefix[:]) != "%PDF-" {
			return nil, ErrProcess
		}
		pages = append(pages, PreparedPage{WidthPoints: width, HeightPoints: height, Offset: offset, Size: int64(length)})
		if _, err := reader.Seek(int64(length)-5, io.SeekCurrent); err != nil {
			return nil, ErrProcess
		}
		total += int64(length)
		offset += int64(length)
	}
	end, err := reader.Seek(0, io.SeekEnd)
	if err != nil || end != size || offset != size {
		return nil, ErrProcess
	}
	return pages, nil
}
