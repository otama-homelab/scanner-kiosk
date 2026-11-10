//go:build linux

package renderjob

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"scanner-kiosk/internal/artifactread"
	"strings"
	"unicode/utf8"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/ocrresult"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdftextlayer"
	"scanner-kiosk/pkg/ocrplugin"
)

// OCRSelector supplies configured providers through the common versioned port.
// Render jobs pin provider/model/version/settings; no fallback is selected here.
type OCRSelector interface {
	SelectOCR(context.Context, string, ocrplugin.Request) (ocrplugin.Provider, error)
}

// OCRTextLayerFonts supplies the same operator-approved font as the configured
// positioned renderer. Missing glyphs/configuration fail closed.
type OCRTextLayerFonts interface {
	OCRTextLayerFont(context.Context) ([]byte, error)
}

func resultArtifact(a artifactscope.Artifact, media string, width, height int) documents.ArtifactRecord {
	return documents.ArtifactRecord{ID: a.ID, RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256, MediaType: media, PixelWidth: width, PixelHeight: height}
}

func prepareReOCR(ctx context.Context, records Records, selector OCRSelector, root string, work documents.RenderWork, inputs map[string]artifactscope.Input, pages []pdfassemble.ScopedPage, providerScope *artifactscope.Scope, budget *artifactscope.Budget, options pdfassemble.Options, track func(*artifactscope.Scope)) (*artifactscope.Scope, string, *documents.OCRResultBundle, error) {
	settings := work.Snapshot.Settings.OCR
	recognitionPages := append([]pdfassemble.ScopedPage(nil), pages...)
	rasterReplies := make(map[int]rasterReply)
	rasterScope := providerScope
	for i, p := range pages {
		if p.MediaType != "application/pdf" {
			continue
		}
		data, reply, e := rasterPDF(ctx, records, root, rasterScope, p.ID, options, budget)
		if e != nil {
			return nil, "", nil, e
		}
		media := reply.Mapping.MediaBox
		width, height := media[2]-media[0], media[3]-media[1]
		if reply.Mapping.SourceRotation%180 != 0 {
			width, height = height, width
		}
		if width != work.Snapshot.Pages[i].PDFWidthPoints || height != work.Snapshot.Pages[i].PDFHeightPoints {
			return nil, "", nil, documents.ErrInvalid
		}
		w, e := rasterScope.CreateArtifact(ctx, artifactscope.PNG)
		if e != nil {
			return nil, "", nil, e
		}
		if _, e = w.Write(data); e != nil {
			return nil, "", nil, e
		}
		if e = w.Close(); e != nil {
			return nil, "", nil, e
		}
		recognitionPages[i].ID = w.Reference()
		recognitionPages[i].MediaType = "image/png"
		recognitionPages[i].Width = reply.Width
		recognitionPages[i].Height = reply.Height
		rasterReplies[i] = reply
	}
	var rasterArtifacts []artifactscope.Artifact
	var err error
	if len(rasterReplies) > 0 {
		rasterArtifacts, err = rasterScope.Seal(ctx)
		if err != nil {
			return nil, "", nil, err
		}
	}
	recognitionInputs := make(map[string]artifactscope.Input, len(inputs)+len(rasterArtifacts))
	for id, a := range inputs {
		recognitionInputs[id] = a
	}
	for _, a := range rasterArtifacts {
		recognitionInputs[a.ID] = artifactscope.Input{RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256}
	}
	if len(rasterArtifacts) > 0 {
		providerScope, err = artifactscope.NewWithBudget(root, recognitionInputs, artifactscope.Limits{MaxInputBytes: options.MaxInputBytes, MaxOutputBytes: options.MaxOutputBytes, MaxTotalOutputBytes: work.ReserveBytes, MaxOutputs: len(pages)}, budget)
		if err != nil {
			return nil, "", nil, err
		}
		track(providerScope)
	}
	req := ocrplugin.Request{JobID: work.Job.ID, InputRevision: work.Job.Revision, ModelID: settings.ModelID, ModelVersion: settings.ModelVersion, Languages: append([]string(nil), settings.Languages...), QualityPreset: settings.QualityPreset, Output: work.Snapshot.OCROutput}
	for i, p := range recognitionPages {
		dpi := ocrDensity(work.Snapshot.Pages[i])
		if _, ok := rasterReplies[i]; ok {
			dpi = 300
		}
		req.Pages = append(req.Pages, ocrplugin.InputPage{ID: work.Snapshot.Pages[i].ID, ArtifactID: p.ID, Width: p.Width, Height: p.Height, DPI: dpi})
	}
	provider, err := selector.SelectOCR(ctx, settings.ProviderID, req)
	if err != nil {
		return nil, "", nil, err
	}
	invocation := req
	invocation.Pages = append([]ocrplugin.InputPage(nil), req.Pages...)
	invocation.Languages = append([]string(nil), req.Languages...)
	result, err := provider.Run(ctx, invocation, providerScope)
	if err != nil {
		return nil, "", nil, err
	}
	sealed, err := providerScope.Seal(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	bound, err := ocrresult.BindSealed(req, result, sealed, work.ReserveBytes)
	if err != nil {
		return nil, "", nil, err
	}
	combined := map[string]artifactscope.Input{}
	for id, a := range recognitionInputs {
		combined[id] = a
	}
	for _, b := range bound {
		a := b.Artifact
		combined[a.ID] = artifactscope.Input{RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256}
	}
	var derived *artifactscope.Scope
	read := func(id string, limit int64) ([]byte, error) {
		meta, ok := combined[id]
		if !ok {
			return nil, documents.ErrInvalid
		}
		readerRoot, e := artifactread.OpenRoot(root, max(options.MaxInputBytes, options.MaxOutputBytes))
		if e != nil {
			return nil, e
		}
		defer readerRoot.Close()
		r, e := readerRoot.Open(ctx, artifactread.Input{RelativePath: meta.RelativePath, Size: meta.Size, SHA256: meta.SHA256})
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(io.LimitReader(r, limit+1))
		ce := r.Close()
		if e != nil || ce != nil {
			return nil, errors.Join(e, ce)
		}
		if int64(len(data)) > limit {
			return nil, artifactscope.ErrLimit
		}
		return data, nil
	}
	write := func(kind string, data []byte) (string, error) {
		if derived == nil {
			var e error
			derived, e = artifactscope.NewWithBudget(root, combined, artifactscope.Limits{MaxInputBytes: max(options.MaxInputBytes, options.MaxOutputBytes), MaxOutputBytes: options.MaxOutputBytes, MaxTotalOutputBytes: work.ReserveBytes, MaxOutputs: len(pages) * 3}, budget)
			if e != nil {
				return "", e
			}
			track(derived)
		}
		var w ocrplugin.ArtifactWriter
		var e error
		if kind == string(ocrplugin.Text) {
			w, e = derived.CreateOutput(ctx, ocrplugin.Text)
		} else {
			w, e = derived.CreateArtifact(ctx, kind)
		}
		if e != nil {
			return "", e
		}
		if _, e = w.Write(data); e != nil {
			return "", e
		}
		if e = w.Close(); e != nil {
			return "", e
		}
		return w.Reference(), nil
	}
	bundle := &documents.OCRResultBundle{JobID: work.Job.ID, InputRevision: work.Job.Revision, Settings: work.Snapshot.Settings}
	type refs struct{ text, layer, preview string }
	references := make([]refs, len(pages))
	for i, b := range bound {
		source := work.Snapshot.Sources[i]
		p := work.Snapshot.Pages[i]
		page := documents.OCRResultPage{PageID: p.ID, OriginalArtifactID: source.Original.ID, EffectiveArtifactID: source.Effective.ID, OriginalSHA256: source.Original.SHA256, EffectiveSHA256: source.Effective.SHA256, CorrectionID: source.CorrectionID, TransformJSON: source.TransformJSON, SourceType: "scan", Rotation: p.Rotation, PixelWidth: b.Input.Width, PixelHeight: b.Input.Height, PDFTextOrigin: "recognition"}
		if p.SourceType == "photo" {
			page.SourceType = "photo"
		}
		if raster, ok := rasterReplies[i]; ok {
			page.SourceType = "pdf"
			page.PDFWidthPoints = p.PDFWidthPoints
			page.PDFHeightPoints = p.PDFHeightPoints
			page.PDFTextOrigin = raster.Origin
			mapping := raster.Mapping
			page.PDFRaster = &mapping
			for _, a := range rasterArtifacts {
				if a.ID == recognitionPages[i].ID {
					preview := resultArtifact(a, "image/png", page.PixelWidth, page.PixelHeight)
					page.Preview = &preview
				}
			}
		}
		data, e := read(b.Artifact.ID, min(options.MaxOutputBytes, 64<<20))
		if e != nil {
			return nil, "", nil, e
		}
		var transcript []byte
		switch req.Output {
		case ocrplugin.PositionedText:
			var positioned ocrplugin.PositionedPage
			decoder := json.NewDecoder(strings.NewReader(string(data)))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&positioned) != nil || decoder.Decode(new(any)) != io.EOF || len(positioned.Spans) > 10000 || ocrplugin.ValidatePositionedPage(positioned, b.Input) != nil {
				return nil, "", nil, documents.ErrInvalid
			}
			var lines []string
			for _, s := range positioned.Spans {
				lines = append(lines, s.Text)
			}
			transcript = []byte(strings.Join(lines, "\n"))
			fontPort, ok := records.(OCRTextLayerFonts)
			if !ok {
				return nil, "", nil, ErrUnsupported
			}
			font, e := fontPort.OCRTextLayerFont(ctx)
			if e != nil {
				return nil, "", nil, e
			}
			var layer []byte
			if page.PDFRaster != nil {
				box := page.PDFRaster.CropBox
				layer, e = pdftextlayer.RenderPositionedPDF(ctx, positioned, b.Input, font, min(options.MaxOutputBytes, budget.Remaining()), box[2]-box[0], box[3]-box[1])
			} else {
				layer, e = pdftextlayer.RenderPositioned(ctx, positioned, b.Input, font, min(options.MaxOutputBytes, budget.Remaining()))
			}
			if e != nil {
				return nil, "", nil, e
			}
			references[i].layer, e = write(artifactscope.PDF, layer)
			if e != nil {
				return nil, "", nil, e
			}
			a := resultArtifact(b.Artifact, "application/json", page.PixelWidth, page.PixelHeight)
			page.Positions = &a
		case ocrplugin.Text:
			if !utf8.Valid(data) {
				return nil, "", nil, documents.ErrInvalid
			}
			transcript = data
		case ocrplugin.PDFTextLayer:
			page.TranscriptUnavailable = true
			a := resultArtifact(b.Artifact, "application/pdf", 0, 0)
			page.Layer = &a
		default:
			return nil, "", nil, ErrUnsupported
		}
		if req.Output == ocrplugin.Text {
			page.Text = resultArtifact(b.Artifact, "text/plain", 0, 0)
		} else {
			references[i].text, err = write(string(ocrplugin.Text), transcript)
			if err != nil {
				return nil, "", nil, err
			}
		}
		if page.Preview != nil {
			bundle.Pages = append(bundle.Pages, page)
			continue
		}
		preview, e := read(recognitionPages[i].ID, options.MaxInputBytes)
		if e != nil {
			return nil, "", nil, e
		}
		kind := artifactscope.PNG
		if recognitionPages[i].MediaType == "image/jpeg" {
			kind = artifactscope.JPEG
		}
		references[i].preview, e = write(kind, preview)
		if e != nil {
			return nil, "", nil, e
		}
		bundle.Pages = append(bundle.Pages, page)
	}
	var outputs []artifactscope.Artifact
	if derived != nil {
		outputs, err = derived.Seal(ctx)
		if err != nil {
			return nil, "", nil, err
		}
	}
	byID := map[string]artifactscope.Artifact{}
	for _, a := range outputs {
		byID[a.ID] = a
		combined[a.ID] = artifactscope.Input{RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256}
	}
	request := assemblyRequest(work, inputs, pages, options)
	for i, ref := range references {
		p := &bundle.Pages[i]
		if ref.text != "" {
			p.Text = resultArtifact(byID[ref.text], "text/plain", 0, 0)
		}
		if ref.preview != "" {
			preview := resultArtifact(byID[ref.preview], recognitionPages[i].MediaType, p.PixelWidth, p.PixelHeight)
			p.Preview = &preview
		}
		if ref.layer != "" {
			a := resultArtifact(byID[ref.layer], "application/pdf", 0, 0)
			p.Layer = &a
		}
		if p.Layer != nil {
			a := p.Layer
			request.OCR = true
			request.Pages[i].Layer = &artifactread.Input{RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256}
		}
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, budget.Remaining())
	if options.MaxOutputBytes < 1 {
		return nil, "", nil, artifactscope.ErrLimit
	}
	request.Options = options
	final, err := artifactscope.NewWithBudget(root, combined, artifactscope.Limits{MaxInputBytes: options.MaxInputBytes, MaxOutputBytes: options.MaxOutputBytes, MaxTotalOutputBytes: work.ReserveBytes, MaxOutputs: 1}, budget)
	if err != nil {
		return nil, "", nil, err
	}
	reference, err := pdfassemble.GenerateIsolated(ctx, root, final, request, budget.Remaining())
	return final, reference, bundle, err
}

func prepareOCR(ctx context.Context, selector OCRSelector, root string, work documents.RenderWork, inputs map[string]artifactscope.Input, pages []pdfassemble.ScopedPage, ocrScope *artifactscope.Scope, budget *artifactscope.Budget, options pdfassemble.Options) (final *artifactscope.Scope, reference string, err error) {
	settings := work.Snapshot.Settings.OCR
	req := ocrplugin.Request{JobID: work.Job.ID, InputRevision: work.Job.Revision, ModelID: settings.ModelID, ModelVersion: settings.ModelVersion, Languages: append([]string(nil), settings.Languages...), QualityPreset: settings.QualityPreset, Output: ocrplugin.PDFTextLayer}
	for i, page := range pages {
		if page.MediaType == "application/pdf" {
			continue
		}
		req.Pages = append(req.Pages, ocrplugin.InputPage{ID: work.Snapshot.Pages[i].ID, ArtifactID: page.ID, Width: page.Width, Height: page.Height, DPI: ocrDensity(work.Snapshot.Pages[i])})
	}
	provider, err := selector.SelectOCR(ctx, settings.ProviderID, req)
	if err != nil {
		return nil, "", err
	}
	invocation := req
	invocation.Pages = append([]ocrplugin.InputPage(nil), req.Pages...)
	invocation.Languages = append([]string(nil), req.Languages...)
	result, err := provider.Run(ctx, invocation, ocrScope)
	if err != nil {
		return nil, "", err
	}
	sealed, err := ocrScope.Seal(ctx)
	if err != nil {
		return nil, "", err
	}
	bound, err := ocrresult.BindSealed(req, result, sealed, work.ReserveBytes)
	if err != nil {
		return nil, "", err
	}
	combined := make(map[string]artifactscope.Input, len(inputs)+len(bound))
	for id, input := range inputs {
		combined[id] = input
	}
	for _, page := range bound {
		a := page.Artifact
		combined[a.ID] = artifactscope.Input{RelativePath: a.RelativePath, Size: a.Size, SHA256: a.SHA256}
	}
	options.MaxOutputBytes = min(options.MaxOutputBytes, budget.Remaining())
	if options.MaxOutputBytes < 1 {
		return nil, "", artifactscope.ErrLimit
	}
	final, err = artifactscope.NewWithBudget(root, combined, artifactscope.Limits{MaxInputBytes: options.MaxInputBytes, MaxOutputBytes: options.MaxOutputBytes, MaxTotalOutputBytes: work.ReserveBytes, MaxOutputs: 1}, budget)
	if err != nil {
		if errors.Is(err, artifactscope.ErrStorage) {
			err = errors.Join(ocrplugin.ErrReconciliation, err)
		}
		return nil, "", err
	}
	request := assemblyRequest(work, inputs, pages, options)
	request.OCR = true
	byPage := map[string]artifactscope.Artifact{}
	for _, layer := range bound {
		byPage[layer.Input.ID] = layer.Artifact
	}
	for i, page := range request.Pages {
		if layer, ok := byPage[page.ID]; ok {
			request.Pages[i].Layer = &artifactread.Input{RelativePath: layer.RelativePath, Size: layer.Size, SHA256: layer.SHA256}
		}
	}
	reference, err = pdfassemble.GenerateIsolated(ctx, root, final, request, budget.Remaining())
	if err != nil {
		return final, "", err
	}
	if err = ocrScope.Close(); err != nil {
		return final, "", err
	}
	return final, reference, nil
}

// Camera sources have no measured capture DPI. This is only the existing OCR
// port's required density hint; it is never stored as page/capture provenance.
func ocrDensity(p documents.CapturedPage) int {
	if p.SourceType == "photo" {
		return 300
	}
	return p.DPI
}
