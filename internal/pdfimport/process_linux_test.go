package pdfimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
	"scanner-kiosk/internal/processrun"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__pdf-import" {
		if os.Getenv("SCANNER_PDF_SECRET_SENTINEL") != "" {
			os.Exit(99)
		}
		os.Exit(Child(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "__pdf-limit-probe" {
		if processrun.ApplyPDFLimits(1<<20, processrun.PDFImportMemory) != nil {
			os.Exit(1)
		}
		var data, core, cpu unix.Rlimit
		if unix.Getrlimit(unix.RLIMIT_DATA, &data) != nil || data.Cur != 512<<20 || unix.Getrlimit(unix.RLIMIT_CORE, &core) != nil || core.Cur != 0 || unix.Getrlimit(unix.RLIMIT_CPU, &cpu) != nil || cpu.Max != 31 {
			os.Exit(2)
		}
		raw, err := unix.Mmap(-1, 0, 600<<20, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
		if err == nil {
			unix.Munmap(raw)
			os.Exit(3)
		}
		if !errors.Is(err, unix.ENOMEM) {
			os.Exit(4)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func processFiles(t *testing.T, raw []byte) (*os.File, *os.File) {
	t.Helper()
	root := t.TempDir()
	input, err := os.OpenFile(filepath.Join(root, "input.pdf"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(filepath.Join(root, "result"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); output.Close() })
	if _, err = input.Write(raw); err != nil {
		t.Fatal(err)
	}
	return input, output
}
func TestPrepareUsesBoundedChildAndPreservesOriginal(t *testing.T) {
	t.Setenv("SCANNER_PDF_SECRET_SENTINEL", "must-not-reach-parser")
	raw := fixturePDF("")
	in, out := processFiles(t, raw)
	pages, err := Prepare(context.Background(), in, out, options())
	if err != nil || len(pages) != 2 || pages[0].WidthPoints != 200 || pages[1].WidthPoints != 400 {
		t.Fatal("child split", len(pages), err)
	}
	for _, page := range pages {
		raw, err := io.ReadAll(io.NewSectionReader(out, page.Offset, page.Size))
		if err != nil || int64(len(raw)) != page.Size || !bytes.HasPrefix(raw, []byte("%PDF-")) || sha256.Sum256(raw) != page.SHA256 {
			t.Fatal("invalid page section", err)
		}
	}
	actual, err := os.ReadFile(in.Name())
	if err != nil || !bytes.Equal(actual, raw) {
		t.Fatal("child changed original", err)
	}
}
func TestPrepareChildRefusalsAndCancellation(t *testing.T) {
	for _, mode := range []string{"malformed", "unsafe", "limit", "cancelled", "same-file", "nonempty-output"} {
		t.Run(mode, func(t *testing.T) {
			raw := fixturePDF("")
			policy := options()
			want := ErrInvalid
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "malformed":
				raw = []byte("broken")
			case "unsafe":
				raw = fixturePDF("/OpenAction << /S /JavaScript /JS (alert) >>")
				want = ErrUnsafe
			case "limit":
				policy.MaxPages = 1
				want = ErrLimit
			case "cancelled":
				cancel()
				want = context.Canceled
			}
			in, out := processFiles(t, raw)
			if mode == "same-file" {
				out = in
			}
			if mode == "nonempty-output" {
				if _, err := out.Write([]byte("retained")); err != nil {
					t.Fatal(err)
				}
			}
			pages, err := Prepare(ctx, in, out, policy)
			if !errors.Is(err, want) || len(pages) != 0 {
				t.Fatal("refusal", err, len(pages))
			}
			if mode == "nonempty-output" {
				actual, _ := os.ReadFile(out.Name())
				if string(actual) != "retained" {
					t.Fatal("clobbered existing file")
				}
			}
		})
	}
}
func TestPDFChildHardMemoryLimit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(exe, "__pdf-limit-probe")
	command.Env = []string{"GOMAXPROCS=2"}
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatal("hard limit probe", err, string(result))
	}
}
func TestDecodeRejectsBrokenOrOversizedChildOutput(t *testing.T) {
	for _, mode := range []string{"valid", "header", "count", "truncated", "dimension", "length", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			var b bytes.Buffer
			b.WriteString("SCNPDF02")
			binary.Write(&b, binary.BigEndian, uint32(1))
			width := 200.0
			if mode == "dimension" {
				width = -1
			}
			binary.Write(&b, binary.BigEndian, width)
			binary.Write(&b, binary.BigEndian, 300.0)
			length := uint64(5)
			if mode == "length" {
				length = 1 << 30
			}
			binary.Write(&b, binary.BigEndian, length)
			digest := sha256.Sum256([]byte("%PDF-"))
			b.Write(digest[:])
			b.WriteString("%PDF-")
			raw := b.Bytes()
			switch mode {
			case "header":
				raw[0] = 'X'
			case "count":
				binary.BigEndian.PutUint32(raw[8:12], 10000)
			case "truncated":
				raw = raw[:20]
			case "trailing":
				raw = append(raw, 'x')
			}
			pages, err := decodePages(bytes.NewReader(raw), int64(len(raw)), options())
			if mode == "valid" {
				if err != nil || len(pages) != 1 || pages[0].Offset != 68 || pages[0].Size != 5 {
					t.Fatal("valid frame rejected", pages, err)
				}
				return
			}
			if err == nil || len(pages) != 0 {
				t.Fatal("invalid child output accepted")
			}
		})
	}
}
