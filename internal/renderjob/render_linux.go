//go:build linux

// Package renderjob coordinates PDF and single-image work for a claimed job.
// Scheduling, persistent cancellation monitoring and process isolation belong to
// the executable worker, not this synchronous component.
package renderjob

import (
	"context"
	"errors"
	"image"
	"io"
	"sync"
	"time"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/captureimage"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/imageexport"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/thumbnail"
	"scanner-kiosk/pkg/ocrplugin"
)

var (
	ErrUnsupported = errors.New("render pipeline unavailable for requested settings")
	ErrPending     = errors.New("render publication requires reconciliation")
	ErrClosed      = errors.New("render work already discarded")
)

type Records interface {
	LoadRenderWork(context.Context, string, string, string) (documents.RenderWork, error)
	PublishExport(context.Context, string, string, string, int64, documents.Export) error
	ExportRecorded(context.Context, string, string, string) (bool, error)
}

// OCRResultRecords completes an immutable candidate, never an adopted output.
type OCRResultRecords interface {
	CompleteOCRResult(context.Context, string, string, string, int64, documents.OCRResultBundle) error
	OCRResultRecorded(context.Context, string, string, documents.OCRResultBundle) (bool, error)
}

// ThumbnailRecords is a worker-only opt-in extension. Publication remains one
// transaction with the export, sharing its durable reservation and read-back.
type ThumbnailRecords interface {
	PublishExportWithThumbnails(context.Context, string, string, string, int64, documents.Export, []documents.ThumbnailRecord) error
	ThumbnailOptions() thumbnail.Options
}

type Prepared struct {
	mu                            sync.Mutex
	records                       Records
	scope                         *artifactscope.Scope
	extraScopes                   []*artifactscope.Scope
	thumbnailScope                *artifactscope.Scope
	thumbnails                    []documents.ThumbnailRecord
	candidate                     *documents.OCRResultBundle
	executionUncertain            bool
	owner, job, runner            string
	revision                      int64
	artifact                      documents.Export
	pending, published, discarded bool
	publicationError              error
	stoppedAtMS                   int64
	cleanupPending, finished      bool
}

// Prepare loads immutable metadata, verifies actual inputs, discovers bounded
// image geometry, generates a plain PDF or one image and seals it. No job is claimed here.
// OCR requires an explicit selector and compatible native text-layer capability.
func Prepare(ctx context.Context, records Records, root, owner, job, runner string, options pdfassemble.Options) (prepared *Prepared, err error) {

	clean := true
	var failedScope *artifactscope.Scope
	var scopes []*artifactscope.Scope
	uncertain := false
	defer func() {
		if err != nil {
			now := time.Now().UnixMilli()
			err = &preparationFailure{cause: err, clean: clean, stoppedAtMS: now, scope: failedScope, extras: otherScopes(scopes, failedScope), uncertain: uncertain}
		}
	}()
	if records == nil {
		return nil, documents.ErrInvalid
	}
	work, err := records.LoadRenderWork(ctx, owner, job, runner)
	if err != nil {
		return nil, err
	}
	format := work.Snapshot.Settings.Format
	if format != "pdf" && format != "jpeg" && format != "png" {
		return nil, ErrUnsupported
	}

	thumbRecords, withThumbnails := records.(ThumbnailRecords)
	hasScans := false
	for _, input := range work.Inputs {
		if input.MediaType != "application/pdf" {
			hasScans = true
		}
	}
	ocrEnabled := work.Snapshot.Settings.OCR.Enabled && (hasScans || work.Snapshot.ReOCR)
	selector, hasOCR := records.(OCRSelector)
	if ocrEnabled && (format != "pdf" || !hasOCR) {
		return nil, ErrUnsupported
	}
	if options.MaxInputBytes < 1 || options.MaxInputBytes > 1<<30 || options.MaxOutputBytes < 1 || options.MaxOutputBytes > 1<<30 || options.MaxPagePixels < 1 || options.MaxPages < 1 || len(work.Inputs) > options.MaxPages || options.JPEGQuality < 1 || options.JPEGQuality > 100 {
		return nil, documents.ErrInvalid
	}
	if len(work.Inputs) != len(work.Snapshot.Pages) || len(work.Inputs) == 0 || work.ReserveBytes < 1 {
		return nil, documents.ErrInvalid
	}
	if format != "pdf" && len(work.Inputs) != 1 {
		return nil, documents.ErrInvalid
	}
	if work.Job.DeadlineAtMS < 1 {
		return nil, documents.ErrInvalid
	}
	ctx, cancel := context.WithDeadline(ctx, time.UnixMilli(work.Job.DeadlineAtMS))
	defer cancel()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if options.MaxOutputBytes > work.ReserveBytes {
		options.MaxOutputBytes = work.ReserveBytes
	}
	inputs := make(map[string]artifactscope.Input, len(work.Inputs))
	for _, input := range work.Inputs {
		inputs[input.ID] = artifactscope.Input{RelativePath: input.RelativePath, Size: input.Size, SHA256: input.SHA256}
	}
	clean = false

	limits := artifactscope.Limits{MaxInputBytes: options.MaxInputBytes, MaxOutputBytes: options.MaxOutputBytes, MaxTotalOutputBytes: options.MaxOutputBytes, MaxOutputs: 1}
	var scope *artifactscope.Scope
	var budget *artifactscope.Budget
	if ocrEnabled || withThumbnails {
		budget, err = artifactscope.NewBudget(work.ReserveBytes)
		if err != nil {
			return nil, err
		}
		limits.MaxTotalOutputBytes = work.ReserveBytes
		limits.MaxOutputs = len(work.Inputs)
		scope, err = artifactscope.NewWithBudget(root, inputs, limits, budget)
	} else {
		scope, err = artifactscope.New(root, inputs, limits)
	}
	if err != nil {
		return nil, err
	}

	failedScope = scope
	scopes = append(scopes, scope)
	defer func() {
		if err != nil {

			uncertain = errors.Is(err, ocrplugin.ErrReconciliation)
			if uncertain {
				clean = false
				return
			}
			cleanupErr := closeScopes(scopes)
			clean = cleanupErr == nil
			err = errors.Join(err, cleanupErr)
		}
	}()
	pages := make([]pdfassemble.ScopedPage, len(work.Inputs))
	for i, input := range work.Inputs {
		page := work.Snapshot.Pages[i]
		if (page.SourceType != "photo" && page.SourceType != "pdf" && page.SourceType != "") || (page.SourceType == "pdf" && input.MediaType != "application/pdf") {
			return nil, documents.ErrInvalid
		}
		expected := page.OriginalArtifactID
		if work.Snapshot.ReOCR || page.SourceType == "photo" {
			if len(work.Snapshot.Sources) != len(work.Inputs) {
				return nil, documents.ErrInvalid
			}
			source := work.Snapshot.Sources[i]
			if len(work.Originals) != len(work.Inputs) || source.Original != work.Originals[i] || source.Original.ID != page.OriginalArtifactID || source.Effective != input || (page.SourceType == "photo" && (source.Effective.ID == source.Original.ID || source.Effective.MediaType != "image/png")) {
				return nil, documents.ErrInvalid
			}
			expected = source.Effective.ID
		}
		if expected != input.ID {
			return nil, documents.ErrInvalid
		}
		if input.MediaType == "application/pdf" {
			if format != "pdf" || page.SourceType != "pdf" {
				return nil, ErrUnsupported
			}
			pages[i] = pdfassemble.ScopedPage{ID: input.ID, MediaType: input.MediaType, Rotation: page.Rotation}
			continue
		}
		reader, e := scope.OpenInput(ctx, input.ID)
		if e != nil {
			return nil, e
		}
		cfg, format, readErr := image.DecodeConfig(io.LimitReader(reader, min(options.MaxInputBytes, 1<<20)))
		closeErr := reader.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if readErr != nil || closeErr != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width) > options.MaxPagePixels/int64(max(cfg.Height, 1)) || (format != "png" && format != "jpeg") || "image/"+format != input.MediaType {
			return nil, documents.ErrInvalid
		}
		size, color := page.Size, page.Color
		if page.SourceType == "photo" {
			size, color = "a4", "color"
		}
		pages[i] = pdfassemble.ScopedPage{ID: input.ID, MediaType: input.MediaType, Size: size, Color: color, Rotation: page.Rotation, Width: cfg.Width, Height: cfg.Height}
	}
	var reference, kind, media string
	var candidate *documents.OCRResultBundle
	if ocrEnabled {
		kind, media = artifactscope.PDF, "application/pdf"
		var final *artifactscope.Scope
		if work.Snapshot.ReOCR {
			if _, ok := records.(OCRResultRecords); !ok {
				return nil, ErrUnsupported
			}
			final, reference, candidate, err = prepareReOCR(ctx, records, selector, root, work, inputs, pages, scope, budget, options, func(s *artifactscope.Scope) { scopes = append(scopes, s) })
		} else {
			final, reference, err = prepareOCR(ctx, selector, root, work, inputs, pages, scope, budget, options)
		}
		if final != nil {
			scope = final
			failedScope = final
			scopes = append(scopes, final)
		}
	} else if format == "pdf" {
		kind, media = artifactscope.PDF, "application/pdf"
		reference, err = pdfassemble.GenerateIsolated(ctx, root, scope, assemblyRequest(work, inputs, pages, options), work.ReserveBytes)
	} else {
		kind, media = format, "image/"+format
		page := pages[0]
		reference, err = imageexport.GenerateScoped(ctx, scope, page.ID, imageexport.Page{Width: page.Width, Height: page.Height, Rotation: page.Rotation, Color: page.Color}, format, imageexport.Options{MaxInputBytes: options.MaxInputBytes, MaxOutputBytes: options.MaxOutputBytes, MaxPixels: options.MaxPagePixels, JPEGQuality: options.JPEGQuality})
	}
	if err != nil {
		return nil, err
	}
	artifacts, err := scope.Seal(ctx)
	if err != nil {
		return nil, err
	}
	if len(artifacts) != 1 || artifacts[0].ID != reference || artifacts[0].Kind != kind {
		return nil, documents.ErrInvalid
	}
	var thumbnailScope *artifactscope.Scope
	var derivatives []documents.ThumbnailRecord
	if withThumbnails && !work.Snapshot.ReOCR {
		missing := 0
		for _, input := range work.Inputs {
			if input.MediaType != "application/pdf" && !work.ThumbnailOriginalIDs[input.ID] {
				missing++
			}
		}
		if missing > 0 {
			policy := thumbRecords.ThumbnailOptions()
			if policy.MaxEdge < 1 || policy.MaxEdge > 512 || policy.MaxOutputBytes < 1 || policy.MaxOutputBytes > 1<<20 {
				return nil, documents.ErrInvalid
			}
			policy.MaxOutputBytes = min(policy.MaxOutputBytes, work.ReserveBytes)
			thumbnailScope, err = artifactscope.NewWithBudget(root, inputs, artifactscope.Limits{MaxInputBytes: options.MaxInputBytes, MaxOutputBytes: policy.MaxOutputBytes, MaxTotalOutputBytes: work.ReserveBytes, MaxOutputs: missing}, budget)
			if err != nil {
				return nil, err
			}
			scopes = append(scopes, thumbnailScope)
			originals := map[string]string{}
			for _, page := range pages {
				if page.MediaType == "application/pdf" || work.ThumbnailOriginalIDs[page.ID] {
					continue
				}
				policy.Input = captureimage.Options{MaxBytes: options.MaxInputBytes, MaxPixels: options.MaxPagePixels, Width: page.Width, Height: page.Height, Color: page.Color}
				reference, e := thumbnail.GenerateScoped(ctx, thumbnailScope, page.ID, policy)
				if e != nil {
					return nil, e
				}
				originals[reference] = page.ID
			}
			sealed, e := thumbnailScope.Seal(ctx)
			if e != nil {
				return nil, e
			}
			if len(sealed) != missing {
				return nil, documents.ErrInvalid
			}
			for _, item := range sealed {
				source, ok := originals[item.ID]
				if !ok || item.Kind != artifactscope.JPEG {
					return nil, documents.ErrInvalid
				}
				var width, height int
				for _, page := range pages {
					if page.ID == source {
						width, height = page.Width, page.Height
						break
					}
				}
				if width > policy.MaxEdge || height > policy.MaxEdge {
					if width >= height {
						height = max(1, int(int64(height)*int64(policy.MaxEdge)/int64(width)))
						width = policy.MaxEdge
					} else {
						width = max(1, int(int64(width)*int64(policy.MaxEdge)/int64(height)))
						height = policy.MaxEdge
					}
				}
				derivatives = append(derivatives, documents.ThumbnailRecord{OriginalID: source, Artifact: documents.ArtifactRecord{ID: item.ID, RelativePath: item.RelativePath, MediaType: "image/jpeg", Size: item.Size, SHA256: item.SHA256, PixelWidth: width, PixelHeight: height}})
			}
		}
	}
	a := artifacts[0]
	if candidate != nil {
		candidate.Output = resultArtifact(a, media, 0, 0)
		candidate.ID = a.ID
	}
	return &Prepared{candidate: candidate, stoppedAtMS: time.Now().UnixMilli(), records: records, scope: scope, extraScopes: otherScopes(scopes, scope), thumbnailScope: thumbnailScope, thumbnails: derivatives, owner: owner, job: job, runner: runner, revision: work.Job.Revision, artifact: documents.Export{ID: a.ID, RelativePath: a.RelativePath, MediaType: media, Size: a.Size, SHA256: a.SHA256}}, nil
}

// Publish conditionally commits the output. On uncertain commit it reads back
// using a separate bounded context. Unavailable read-back preserves the scope;
// callers must retain Prepared and retry Publish, never discard/restart it.
func (p *Prepared) Publish(ctx context.Context) (documents.Export, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.executionUncertain {
		return documents.Export{}, ErrPending
	}
	if p.discarded {
		return documents.Export{}, ErrClosed
	}
	if p.published {
		return p.artifact, p.releasePublished()
	}
	if !p.pending {
		var err error
		if p.candidate != nil {
			publisher, ok := p.records.(OCRResultRecords)
			if !ok {
				return documents.Export{}, ErrUnsupported
			}
			err = publisher.CompleteOCRResult(ctx, p.owner, p.job, p.runner, p.revision, *p.candidate)
		} else if len(p.thumbnails) > 0 {
			publisher, ok := p.records.(ThumbnailRecords)
			if !ok {
				return documents.Export{}, documents.ErrInvalid
			}
			err = publisher.PublishExportWithThumbnails(ctx, p.owner, p.job, p.runner, p.revision, p.artifact, p.thumbnails)
		} else {
			err = p.records.PublishExport(ctx, p.owner, p.job, p.runner, p.revision, p.artifact)
		}
		if err == nil {
			p.published = true
			return p.artifact, p.releasePublished()
		}
		p.publicationError = err
		if !errors.Is(err, documents.ErrCommitUncertain) {
			p.discarded = true
			return documents.Export{}, errors.Join(err, p.closeWork())
		}
		p.pending = true
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var recorded bool
	var err error
	if p.candidate != nil {
		recorded, err = p.records.(OCRResultRecords).OCRResultRecorded(recovery, p.owner, p.job, *p.candidate)
	} else {
		recorded, err = p.records.ExportRecorded(recovery, p.owner, p.job, p.artifact.ID)
	}
	if err != nil {
		return documents.Export{}, errors.Join(ErrPending, err)
	}
	p.pending = false
	if recorded {
		p.published = true
		return p.artifact, p.releasePublished()
	}
	p.discarded = true
	return documents.Export{}, errors.Join(p.publicationError, p.closeWork())
}

// Close discards only confirmed-unpublished output. Pending publication prevents
// deletion, including after cancellation. Cleanup failures may be retried.
func (p *Prepared) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending || p.executionUncertain {
		return ErrPending
	}
	if p.published {
		return p.releasePublished()
	}
	p.discarded = true
	return p.closeWork()
}

// StopRecords is a separate cleanup port, usable after owners are disabled.
type StopRecords interface {
	FinishStoppedRender(context.Context, string, string, documents.RenderStop, string) error
	RenderStoppedRecorded(context.Context, string, string, string) (bool, error)
}

// Finish discards confirmed-unpublished output and terminalizes this completed
// synchronous render. It refuses unresolved publication and published results.
// An uncertain cleanup commit is read back before any retry of the transition.
func (p *Prepared) Finish(ctx context.Context, outcome string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if outcome != "failed" && outcome != "interrupted" {
		return documents.ErrInvalid
	}
	if p.pending || p.executionUncertain {
		return ErrPending
	}
	if p.published {
		return ErrClosed
	}
	if p.finished {
		return nil
	}
	records, ok := p.records.(StopRecords)
	if !ok {
		return ErrUnsupported
	}
	if !p.cleanupPending {
		p.discarded = true
		if err := p.closeWork(); err != nil {
			return err
		}

		err := records.FinishStoppedRender(ctx, p.owner, p.job, documents.RenderStop{RunnerID: p.runner, ProcessStoppedAtMS: p.stoppedAtMS, WorkspaceCleanedAtMS: time.Now().UnixMilli()}, outcome)
		if err == nil {
			p.finished = true
			return nil
		}
		if !errors.Is(err, documents.ErrCommitUncertain) {
			return err
		}
		p.cleanupPending = true
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	recorded, err := records.RenderStoppedRecorded(recovery, p.owner, p.job, p.runner)
	if err != nil {
		return errors.Join(ErrPending, err)
	}
	p.cleanupPending = false
	if recorded {
		p.finished = true
		return nil
	}
	return documents.ErrCommitUncertain
}

func otherScopes(scopes []*artifactscope.Scope, primary *artifactscope.Scope) []*artifactscope.Scope {
	var extra []*artifactscope.Scope
	for _, scope := range scopes {
		if scope != primary {
			extra = append(extra, scope)
		}
	}
	return extra
}
func closeScopes(scopes []*artifactscope.Scope) error {
	var err error
	for i := len(scopes) - 1; i >= 0; i-- {
		if scopes[i] != nil {
			err = errors.Join(err, scopes[i].Close())
		}
	}
	return err
}
func (p *Prepared) closeWork() error {
	if p.executionUncertain {
		return ErrPending
	}
	return closeScopes(append(append([]*artifactscope.Scope(nil), p.extraScopes...), p.scope))
}

func (p *Prepared) releasePublished() error {
	if p.candidate != nil {
		var err error
		for _, s := range append(append([]*artifactscope.Scope(nil), p.extraScopes...), p.scope) {
			err = errors.Join(err, s.Release())
		}
		return err
	}
	err := p.scope.Release()
	if p.thumbnailScope != nil {
		err = errors.Join(err, p.thumbnailScope.Release())
	}
	return err
}
