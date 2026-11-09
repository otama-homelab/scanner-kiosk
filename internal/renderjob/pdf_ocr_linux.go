//go:build linux

package renderjob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdftextlayer"
	"scanner-kiosk/internal/processrun"
	"scanner-kiosk/pkg/ocrplugin"
)

// PDFRasterCommand is supplied only by worker configuration, never by a job.
// Standalone distributions supply their manifest-verified native trampoline.
type PDFRasterCommand struct {
	Path string
	Args []string
}
type PDFRasterCommands interface{ PDFRasterCommand() PDFRasterCommand }

type rasterReply struct {
	Mapping       documents.PDFRasterMapping
	Width, Height int
	Origin        string
}

type rasterDiagnostic struct {
	file      *os.File
	remaining int64
}

func (w *rasterDiagnostic) Write(p []byte) (int, error) {
	n := len(p)
	if int64(len(p)) > w.remaining {
		p = p[:w.remaining]
	}
	if len(p) > 0 {
		written, err := w.file.Write(p)
		w.remaining -= int64(written)
		if err != nil {
			return written, err
		}
	}
	return n, nil
}

func rasterPDF(ctx context.Context, records Records, root string, scope *artifactscope.Scope, id string, options pdfassemble.Options, budget *artifactscope.Budget) (data []byte, reply rasterReply, result error) {
	command := PDFRasterCommand{Path: "/usr/bin/pdftoppm"}
	if port, ok := records.(PDFRasterCommands); ok {
		command = port.PDFRasterCommand()
	}
	if !filepath.IsAbs(command.Path) || filepath.Clean(command.Path) != command.Path || len(command.Args) > 8 {
		return nil, reply, ErrUnsupported
	}
	outputLimit := min(options.MaxOutputBytes, budget.Remaining()/4)
	if outputLimit < 1 {
		return nil, reply, artifactscope.ErrLimit
	}
	reservation, err := scope.ReserveScratch(ctx, id, outputLimit*2, 65536)
	if err != nil {
		return nil, reply, err
	}
	dir, err := os.MkdirTemp(root, ".pdf-raster-")
	if err != nil {
		reservation.ReleaseAfterCleanup()
		return nil, reply, err
	}
	clean := true
	defer func() {
		if clean {
			if e := os.RemoveAll(dir); e != nil {
				result = errors.Join(result, ocrplugin.ErrReconciliation)
			} else {
				reservation.ReleaseAfterCleanup()
			}
		}
	}()
	input, err := scope.OpenInput(ctx, id)
	if err != nil {
		return nil, reply, err
	}
	defer input.Close()
	// OpenInput verifies the exact immutable source before any parser/tool starts.
	source, err := os.OpenFile(filepath.Join(dir, "source.pdf"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, reply, err
	}
	defer source.Close()
	if n, e := io.Copy(source, io.LimitReader(input, options.MaxInputBytes+1)); e != nil || n > options.MaxInputBytes {
		return nil, reply, artifactscope.ErrLimit
	}
	if _, err = source.Seek(0, 0); err != nil {
		return nil, reply, err
	}
	metadata, err := os.OpenFile(filepath.Join(dir, "metadata.json"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, reply, err
	}
	defer metadata.Close()
	self, err := os.Executable()
	if err != nil {
		return nil, reply, err
	}
	args := []string{"__pdf-raster", strconv.FormatInt(options.MaxInputBytes, 10), strconv.FormatInt(outputLimit, 10), strconv.FormatInt(options.MaxPagePixels, 10), command.Path}
	args = append(args, command.Args...)
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stopped := processrun.Run(bounded, processrun.Spec{Path: self, Args: args, Dir: dir, Env: []string{"GOMAXPROCS=2", "GOMEMLIMIT=256MiB", "LANG=C", "HOME=/nonexistent"}, Stdin: source, Stdout: metadata, Grace: 200 * time.Millisecond})
	if !stopped.GroupStopped {
		clean = false
		return nil, reply, ocrplugin.ErrReconciliation
	}
	if stopped.Err != nil {
		return nil, reply, errors.Join(documents.ErrInvalid, stopped.Err)
	}
	if _, err = metadata.Seek(0, 0); err != nil {
		return nil, reply, err
	}
	decoder := json.NewDecoder(io.LimitReader(metadata, 65537))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&reply) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, reply, documents.ErrInvalid
	}
	png, err := os.Open(filepath.Join(dir, "raster.png"))
	if err != nil {
		return nil, reply, err
	}
	defer png.Close()
	data, err = io.ReadAll(io.LimitReader(png, outputLimit+1))
	if err != nil || int64(len(data)) > outputLimit {
		return nil, reply, artifactscope.ErrLimit
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || config.Width != reply.Width || config.Height != reply.Height || int64(config.Width)*int64(config.Height) > options.MaxPagePixels {
		return nil, reply, documents.ErrInvalid
	}
	return data, reply, nil
}

// PDFRasterChild parses PDF metadata and runs Poppler under inherited hard CPU,
// memory, file-size and descriptor limits. The outer supervisor owns this entire
// process group, including Poppler descendants, and retains scratch if stop is uncertain.
func PDFRasterChild(args []string, input, output *os.File) int {
	if len(args) < 4 || len(args) > 12 {
		return 10
	}
	maxInput, e := strconv.ParseInt(args[0], 10, 64)
	if e != nil || maxInput < 1 || maxInput > 1<<30 {
		return 10
	}
	maxOutput, e := strconv.ParseInt(args[1], 10, 64)
	if e != nil || maxOutput < 1 || maxOutput > 1<<30 {
		return 10
	}
	maxPixels, e := strconv.ParseInt(args[2], 10, 64)
	if e != nil || maxPixels < 1 || maxPixels > 250000000 || !filepath.IsAbs(args[3]) {
		return 10
	}
	if processrun.ApplyPDFLimits(maxOutput, processrun.PDFImportMemory) != nil {
		return 10
	}
	source, e := io.ReadAll(io.LimitReader(input, maxInput+1))
	if e != nil || int64(len(source)) > maxInput {
		return 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), 28*time.Second)
	defer cancel()
	conf := model.NewStatelessConfiguration()
	boxes, e := api.Boxes(ctx, bytes.NewReader(source), nil, conf)
	if e != nil || len(boxes) != 1 {
		return 10
	}
	box := boxes[0]
	media, crop := box.MediaBox(), box.CropBox()
	if media == nil || crop == nil || box.Rot%90 != 0 {
		return 10
	}
	values := []float64{media.LL.X, media.LL.Y, media.UR.X, media.UR.Y, crop.LL.X, crop.LL.Y, crop.UR.X, crop.UR.Y}
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 20000 {
			return 10
		}
	}
	if crop.Width() <= 0 || crop.Height() <= 0 || crop.LL.X < media.LL.X || crop.LL.Y < media.LL.Y || crop.UR.X > media.UR.X || crop.UR.Y > media.UR.Y {
		return 10
	}
	w, h := int(math.Ceil(crop.Width()*300/72)), int(math.Ceil(crop.Height()*300/72))
	if w < 1 || h < 1 || int64(w)*int64(h) > maxPixels {
		return 10
	}
	origin, e := pdftextlayer.PDFTextOrigin(ctx, source, maxInput)
	if e != nil {
		return 10
	}
	mapping := documents.PDFRasterMapping{DPI: 300, MediaBox: [4]float64{media.LL.X, media.LL.Y, media.UR.X, media.UR.Y}, CropBox: [4]float64{crop.LL.X, crop.LL.Y, crop.UR.X, crop.UR.Y}, SourceRotation: ((box.Rot % 360) + 360) % 360, ScaleX: float64(w) / crop.Width(), ScaleY: float64(h) / crop.Height()}
	f, e := os.OpenFile("unrotated.pdf", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return 10
	}
	if box.Rot != 0 {
		e = api.Rotate(ctx, bytes.NewReader(source), f, -box.Rot, []string{"1"}, conf)
	} else {
		_, e = f.Write(source)
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return 10
	}
	png, e := os.OpenFile("raster.png", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return 10
	}
	diag, e := os.OpenFile("diagnostics", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		png.Close()
		return 10
	}
	argv := append(append([]string{}, args[4:]...), "-f", "1", "-l", "1", "-singlefile", "-cropbox", "-r", "300", "-png", "unrotated.pdf")
	cmd := exec.CommandContext(ctx, args[3], argv...)
	cmd.Env = []string{"LANG=C", "HOME=/nonexistent"}
	cmd.Stdout = png
	cmd.Stderr = &rasterDiagnostic{file: diag, remaining: 65536}
	e = cmd.Run()
	pe, de := png.Close(), diag.Close()
	if e != nil || pe != nil || de != nil {
		return 10
	}
	info, e := os.Stat("raster.png")
	if e != nil || info.Size() < 1 || info.Size() > maxOutput {
		return 10
	}
	if json.NewEncoder(output).Encode(rasterReply{Mapping: mapping, Width: w, Height: h, Origin: origin}) != nil {
		return 10
	}
	return 0
}
