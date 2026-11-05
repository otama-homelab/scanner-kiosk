//go:build linux

package pdfassemble

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/processrun"
	"scanner-kiosk/pkg/ocrplugin"
)

// ProcessPage is a trusted snapshot and a confined immutable input capability.
// Imported PDFs have no invented raster geometry or OCR layer.
type ProcessPage struct {
	ID, Size, Color, MediaType string
	Width, Height, Rotation    int
	Input                      artifactread.Input
	Layer                      *artifactread.Input `json:",omitempty"`
}
type ProcessRequest struct {
	Pages    []ProcessPage
	Encoding string
	OCR      bool
	Options  Options
}

const maxProcessManifest = 16 << 20

func validProcessRequest(r ProcessRequest) bool {
	o := r.Options
	return len(r.Pages) > 0 && len(r.Pages) <= o.MaxPages && o.MaxPages >= 1 && o.MaxPages <= 10000 && o.MaxInputBytes >= 1 && o.MaxInputBytes <= 1<<30 && o.MaxOutputBytes >= 1 && o.MaxOutputBytes <= 1<<30 && o.MaxPagePixels >= 1 && o.JPEGQuality >= 1 && o.JPEGQuality <= 100 && (r.Encoding == "lossless" || r.Encoding == "jpeg")
}

// GenerateIsolated parses/merges only in the same executable's limited child.
// available covers the manifest, child output and copied artifact simultaneously.
// The caller owns the scope and stops/reconciles its job; unknown cleanup or
// child stop returns ErrReconciliation and must never trigger blind scope.Close.
func GenerateIsolated(ctx context.Context, rootPath string, scope *artifactscope.Scope, request ProcessRequest, available int64) (reference string, result error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if scope == nil || !validProcessRequest(request) || available < 1 {
		return "", ErrInvalid
	}
	manifest, err := json.Marshal(request)
	if err != nil || len(manifest) > maxProcessManifest {
		return "", ErrLimit
	}
	// Re-encoding a smaller numeric bound cannot increase manifest length.
	request.Options.MaxOutputBytes = min(request.Options.MaxOutputBytes, (available-int64(len(manifest)))/2)
	if request.Options.MaxOutputBytes < 1 {
		return "", ErrLimit
	}
	manifest, err = json.Marshal(request)
	if err != nil {
		return "", ErrInvalid
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", errors.Join(ocrplugin.ErrReconciliation, err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			result = errors.Join(result, ocrplugin.ErrReconciliation, err)
		}
	}()
	var nonce [24]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return "", err
	}
	dir := "work_" + hex.EncodeToString(nonce[:])
	if err = root.Mkdir(dir, 0700); err != nil {
		return "", errors.Join(ocrplugin.ErrReconciliation, err)
	}
	var manifestFile, output *os.File
	unstopped := false
	defer func() {
		var closeErr error
		if manifestFile != nil {
			closeErr = errors.Join(closeErr, manifestFile.Close())
		}
		if output != nil {
			closeErr = errors.Join(closeErr, output.Close())
		}
		if unstopped {
			result = errors.Join(result, ocrplugin.ErrReconciliation, closeErr)
			return
		}
		removeErr := root.RemoveAll(dir)
		parent, openErr := root.Open(".")
		var syncErr error
		if openErr == nil {
			syncErr = errors.Join(parent.Sync(), parent.Close())
		}
		if cleanupErr := errors.Join(closeErr, removeErr, openErr, syncErr); cleanupErr != nil {
			result = errors.Join(result, ocrplugin.ErrReconciliation, cleanupErr)
		}
	}()
	manifestFile, err = root.OpenFile(dir+"/manifest", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	if _, err = manifestFile.Write(manifest); err != nil {
		return "", err
	}
	if _, err = manifestFile.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	output, err = root.OpenFile(dir+"/result", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	completed := processrun.Run(bounded, processrun.Spec{Path: executable, Args: []string{"__pdf-assemble", strconv.FormatInt(request.Options.MaxOutputBytes, 10)}, Dir: rootPath, Env: []string{"GOMAXPROCS=2", "GOMEMLIMIT=256MiB"}, Stdin: manifestFile, Stdout: output, Grace: 200 * time.Millisecond})
	if !completed.GroupStopped {
		unstopped = true
		return "", ocrplugin.ErrReconciliation
	}
	if bounded.Err() != nil {
		return "", bounded.Err()
	}
	if completed.Err != nil {
		return "", ErrFailed
	}
	info, err := output.Stat()
	if err != nil || info.Size() < 5 || info.Size() > request.Options.MaxOutputBytes {
		return "", ErrFailed
	}
	var prefix [5]byte
	if _, err = output.ReadAt(prefix[:], 0); err != nil || string(prefix[:]) != "%PDF-" {
		return "", ErrFailed
	}
	writer, err := scope.CreateArtifact(ctx, artifactscope.PDF)
	if err != nil {
		return "", err
	}
	n, copyErr := io.CopyBuffer(writer, io.NewSectionReader(output, 0, info.Size()), make([]byte, 64<<10))
	closeErr := writer.Close()
	if err = errors.Join(copyErr, closeErr); err != nil {
		return "", err
	}
	if n != info.Size() {
		return "", io.ErrUnexpectedEOF
	}
	return writer.Reference(), nil
}

// AssemblyChild runs before service configuration. CWD is the private document
// root; only manifest-bound paths/digests become parser input streams.
func AssemblyChild(args []string, input, output *os.File) int {
	if len(args) != 1 || input == nil || output == nil {
		return 10
	}
	maxOutput, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || maxOutput < 1 || maxOutput > 1<<30 || processrun.ApplyPDFLimits(maxOutput) != nil {
		return 10
	}
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxProcessManifest {
		return 10
	}
	var request ProcessRequest
	decoder := json.NewDecoder(io.LimitReader(input, maxProcessManifest+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || !validProcessRequest(request) || request.Options.MaxOutputBytes != maxOutput {
		return 10
	}
	rootPath, err := os.Getwd()
	if err != nil {
		return 10
	}
	root, err := artifactread.OpenRoot(rootPath, request.Options.MaxInputBytes)
	if err != nil {
		return 10
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pages := make([]Page, len(request.Pages))
	var layers []TextLayer
	for i, p := range request.Pages {
		bound := p.Input
		pages[i] = Page{ID: p.ID, Size: p.Size, Color: p.Color, MediaType: p.MediaType, Width: p.Width, Height: p.Height, Rotation: p.Rotation, Open: func(ctx context.Context) (io.ReadCloser, error) { return root.Open(ctx, bound) }}
		if p.Layer != nil {
			if !request.OCR || p.MediaType == "application/pdf" {
				return 10
			}
			layer := *p.Layer
			layers = append(layers, TextLayer{PageID: p.ID, Open: func(ctx context.Context) (io.ReadCloser, error) { return root.Open(ctx, layer) }})
		}
	}
	var data []byte
	if request.OCR {
		data, err = BuildWithLayers(ctx, pages, layers, request.Encoding, request.Options)
	} else {
		data, err = Build(ctx, pages, request.Encoding, request.Options)
	}
	if err != nil {
		return 11
	}
	if _, err = output.Write(data); err != nil {
		return 12
	}
	return 0
}
