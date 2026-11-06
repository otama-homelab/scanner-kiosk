//go:build linux

// Package tesseract implements local OCR through the installed upstream CLI.
package tesseract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"scanner-kiosk/internal/processrun"
	"scanner-kiosk/pkg/ocrplugin"
)

var ErrReconciliation = ocrplugin.ErrReconciliation

type Options struct {
	Binary, DataDir, WorkRoot string
	// RuntimeRevision pins a distributed image's shared libraries and implementation.
	RuntimeRevision string
	// LanguageFiles maps public language IDs to installed dictionary basenames.
	LanguageFiles                                map[string]string
	MaxInputBytes, MaxResultBytes, MaxPagePixels int64
	MaxPages                                     int
	Timeout, StopGrace                           time.Duration
}
type Provider struct {
	options      Options
	caps         ocrplugin.Capabilities
	dictionaries map[string]string
}

// New snapshots installed dictionaries and fingerprints binary + traineddata.
// Runtime images must keep these files immutable. It performs no recognition.
func New(options Options) (*Provider, error) {
	for _, path := range []string{options.Binary, options.DataDir, options.WorkRoot} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, errors.New("invalid Tesseract paths")
		}
	}
	if options.MaxInputBytes < 1 || options.MaxInputBytes > 1<<40 || options.MaxResultBytes < 1 || options.MaxResultBytes > 1<<40 || options.MaxPagePixels < 1 || options.MaxPages < 1 || options.Timeout <= 0 || options.StopGrace <= 0 {
		return nil, errors.New("invalid Tesseract limits")
	}
	if options.RuntimeRevision != "" {
		if _, err := hex.DecodeString(options.RuntimeRevision); err != nil || len(options.RuntimeRevision) != 64 {
			return nil, errors.New("invalid Tesseract runtime revision")
		}
	}
	if len(options.LanguageFiles) == 0 {
		options.LanguageFiles = map[string]string{"en": "eng", "ja": "jpn"}
	}
	p := &Provider{options: options, dictionaries: map[string]string{}}
	hash := sha256.New()
	if options.RuntimeRevision != "" {
		_, _ = io.WriteString(hash, "runtime\x00"+options.RuntimeRevision+"\x00")
	}
	fingerprint := func(path string) error {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
			return errors.New("invalid Tesseract dependency")
		}
		_, err = io.Copy(hash, file)
		return err
	}
	if err := fingerprint(options.Binary); err != nil {
		return nil, errors.New("Tesseract binary unavailable")
	}
	_, _ = io.WriteString(hash, "\x00osd\x00")
	if err := fingerprint(filepath.Join(options.DataDir, "osd.traineddata")); err != nil {
		return nil, errors.New("Tesseract orientation model unavailable")
	}
	keys := make([]string, 0, len(options.LanguageFiles))
	for key := range options.LanguageFiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	languages := []string{}
	for _, key := range keys {
		name := options.LanguageFiles[key]
		if name == "" || strings.ContainsAny(name, "/\\+\x00") || name == "." || name == ".." {
			return nil, errors.New("invalid Tesseract dictionary")
		}
		path := filepath.Join(options.DataDir, name+".traineddata")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		_, _ = io.WriteString(hash, "\x00"+key+"\x00"+name+"\x00")
		if err := fingerprint(path); err != nil {
			return nil, errors.New("Tesseract dictionary unavailable")
		}
		languages = append(languages, key)
		p.dictionaries[key] = name
	}
	if len(languages) == 0 {
		return nil, errors.New("no Tesseract dictionaries installed")
	}
	version := "sha256-" + hex.EncodeToString(hash.Sum(nil))
	p.caps = ocrplugin.Capabilities{APIVersion: ocrplugin.APIVersion, ID: "tesseract", Name: "Tesseract", Version: "1", Models: []ocrplugin.Model{{ID: "installed", Name: "Installed dictionaries", Version: version, Languages: languages, QualityPresets: []string{"auto", "block"}, Outputs: []ocrplugin.OutputKind{ocrplugin.Text, ocrplugin.PDFTextLayer}, MaxPages: options.MaxPages, MaxPagePixels: options.MaxPagePixels}}}
	return p, nil
}
func (p *Provider) Capabilities(context.Context) (ocrplugin.Capabilities, error) {
	// Use the registry's snapshot cloning rather than expose provider-owned slices.
	c := p.caps
	c.Models = append([]ocrplugin.Model(nil), c.Models...)
	c.Models[0].Languages = append([]string(nil), c.Models[0].Languages...)
	c.Models[0].QualityPresets = append([]string(nil), c.Models[0].QualityPresets...)
	c.Models[0].Outputs = append([]ocrplugin.OutputKind(nil), c.Models[0].Outputs...)
	return c, nil
}
func (p *Provider) Run(ctx context.Context, request ocrplugin.Request, access ocrplugin.Access) (result ocrplugin.Result, err error) {
	registry, err := ocrplugin.New(ctx, p)
	if err != nil {
		return result, err
	}
	if _, err = registry.Select("tesseract", request); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.options.Timeout)
	defer cancel()
	root, err := os.MkdirTemp(p.options.WorkRoot, "ocr-")
	if err != nil {
		return result, errors.New("OCR workspace unavailable")
	}
	clean := true
	var reservations []ocrplugin.ScratchReservation
	defer func() {
		if clean {
			if cleanupErr := removeAndSync(root, p.options.WorkRoot); cleanupErr != nil {
				result = ocrplugin.Result{}
				err = ErrReconciliation
			} else {
				for _, reservation := range reservations {
					reservation.ReleaseAfterCleanup()
				}
			}
		}
	}()
	names := make([]string, 0, len(request.Languages))
	for _, lang := range request.Languages {
		names = append(names, p.dictionaries[lang])
	}
	psm := "1"
	if request.QualityPreset == "block" {
		psm = "6"
	}
	result.ModelVersion = request.ModelVersion
	for i, page := range request.Pages {
		if err = ctx.Err(); err != nil {
			return ocrplugin.Result{}, err
		}
		dir := filepath.Join(root, strconv.Itoa(i))
		if err = os.Mkdir(dir, 0700); err != nil {
			return ocrplugin.Result{}, errors.New("OCR workspace unavailable")
		}

		var reservation ocrplugin.ScratchReservation
		inputLimit := p.options.MaxInputBytes
		if resources, ok := access.(ocrplugin.ScratchAccess); ok {
			reservation, err = resources.ReserveScratch(ctx, page.ArtifactID, p.options.MaxResultBytes, 1<<20)
			if err != nil {
				return ocrplugin.Result{}, err
			}
			reservations = append(reservations, reservation)
			inputLimit = min(inputLimit, reservation.InputBytes())
		}
		input := filepath.Join(dir, "input")
		source, e := access.OpenInput(ctx, page.ArtifactID)
		if e != nil {
			return ocrplugin.Result{}, errors.New("OCR input unavailable")
		}
		target, e := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			source.Close()
			return ocrplugin.Result{}, errors.New("OCR staging failed")
		}
		n, copyErr := io.Copy(target, io.LimitReader(source, inputLimit+1))
		closeErr := target.Close()
		source.Close()
		if copyErr != nil || closeErr != nil || n > inputLimit {
			return ocrplugin.Result{}, errors.New("OCR input exceeds limits or is unreadable")
		}
		file, e := os.Open(input)
		if e != nil {
			return ocrplugin.Result{}, errors.New("OCR input unavailable")
		}
		geometry, format, e := image.DecodeConfig(file)
		file.Close()
		if e != nil || (format != "png" && format != "jpeg") || geometry.Width != page.Width || geometry.Height != page.Height {
			return ocrplugin.Result{}, errors.New("OCR input geometry mismatch")
		}
		base := filepath.Join(dir, "result")
		args := []string{input, base, "--tessdata-dir", p.options.DataDir, "-l", strings.Join(names, "+"), "--psm", psm, "--dpi", strconv.Itoa(page.DPI)}
		extension := ".txt"
		if request.Output == ocrplugin.PDFTextLayer {
			args = append(args, "-c", "tessedit_create_pdf=1", "-c", "textonly_pdf=1")
			extension = ".pdf"
		} else {
			args = append(args, "-c", "tessedit_create_txt=1")
		}
		// Stderr is a private staging file, never included in errors or logs.
		stderr, e := os.OpenFile(filepath.Join(dir, "stderr"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return ocrplugin.Result{}, errors.New("OCR staging failed")
		}
		run, limitErr := runBounded(ctx, processrun.Spec{Path: p.options.Binary, Args: args, Env: []string{"OMP_THREAD_LIMIT=1", "TMPDIR=" + dir, "LC_ALL=C"}, Dir: dir, Stderr: stderr, Grace: p.options.StopGrace}, base+extension, filepath.Join(dir, "stderr"), p.options.MaxResultBytes)
		stderr.Close()
		if !run.GroupStopped {
			clean = false
			return ocrplugin.Result{}, ErrReconciliation
		}
		if limitErr != nil {
			return ocrplugin.Result{}, limitErr
		}
		if run.Err != nil {
			if ctx.Err() != nil {
				return ocrplugin.Result{}, ctx.Err()
			}
			return ocrplugin.Result{}, errors.New("Tesseract recognition failed")
		}
		artifact, e := os.Open(base + extension)
		if e != nil {
			return ocrplugin.Result{}, errors.New("OCR output unavailable")
		}
		info, e := artifact.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() > p.options.MaxResultBytes {
			artifact.Close()
			return ocrplugin.Result{}, errors.New("OCR output exceeds limits")
		}
		if err = ctx.Err(); err != nil {
			artifact.Close()
			return ocrplugin.Result{}, err
		}
		output, e := access.CreateOutput(ctx, request.Output)
		if e != nil {
			artifact.Close()
			return ocrplugin.Result{}, errors.New("OCR output allocation failed")
		}
		_, copyErr = io.Copy(output, io.LimitReader(artifact, p.options.MaxResultBytes+1))
		artifact.Close()
		closeErr = output.Close()
		if copyErr != nil || closeErr != nil {
			return ocrplugin.Result{}, errors.New("OCR output write failed")
		}
		if err = ctx.Err(); err != nil {
			return ocrplugin.Result{}, err
		}

		// No owned process remains and every per-page descriptor is closed here.
		if err = removeAndSync(dir, root); err != nil {
			return ocrplugin.Result{}, errors.New("OCR page cleanup failed")
		}
		if reservation != nil {
			reservation.ReleaseAfterCleanup()
			reservations = reservations[:len(reservations)-1]
		}
		result.Pages = append(result.Pages, ocrplugin.PageResult{PageID: page.ID, ArtifactID: output.Reference(), Kind: request.Output})
	}
	return result, nil
}

// This watchdog bounds staging growth by stopping the process when observed over
// budget. It is not a filesystem quota: bounded overshoot and disk-full handling
// still belong to the worker/container storage boundary.
func runBounded(ctx context.Context, spec processrun.Spec, output, stderr string, maxBytes int64) (processrun.Result, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	check := func() error {
		for path, limit := range map[string]int64{output: maxBytes, stderr: 1 << 20} {
			info, err := os.Stat(path)
			if err == nil && info.Size() > limit {
				return errors.New("OCR staging output exceeds limits")
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return errors.New("OCR staging output unavailable")
			}
		}
		return nil
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := check(); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	result := processrun.Run(ctx, spec)
	cause := context.Cause(ctx)
	cancel(nil)
	<-stopped
	if cause != nil && result.Cancelled {
		return result, cause
	}
	return result, check()
}

func removeAndSync(directory, parent string) error {
	if err := os.RemoveAll(directory); err != nil {
		return err
	}
	file, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(syncErr, closeErr)
}
