//go:build linux

package ocrresult_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"scanner-kiosk/internal/artifactscope"
	"scanner-kiosk/internal/ocrresult"
	"scanner-kiosk/pkg/ocrplugin"
)

func TestBindingUsesSealedScopeAndRejectsForeignOutput(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxTotalOutputBytes: 2048, MaxOutputs: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	payloads := []string{"日本語", "English"}
	refs := make([]string, 2)
	for i, payload := range payloads {
		writer, err := scope.CreateOutput(ctx, ocrplugin.Text)
		if err != nil {
			t.Fatal(err)
		}
		refs[i] = writer.Reference()
		if _, err = writer.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	sealed, err := scope.Seal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request := ocrplugin.Request{JobID: "job", InputRevision: 1, ModelID: "model", ModelVersion: "version", Output: ocrplugin.Text, Pages: []ocrplugin.InputPage{{ID: "first", ArtifactID: "original-first", Width: 100, Height: 200}, {ID: "second", ArtifactID: "original-second", Width: 200, Height: 100}}}
	result := ocrplugin.Result{ModelVersion: "version", Pages: []ocrplugin.PageResult{{PageID: "second", ArtifactID: refs[1], Kind: ocrplugin.Text}, {PageID: "first", ArtifactID: refs[0], Kind: ocrplugin.Text}}}
	total := int64(len(payloads[0]) + len(payloads[1]))
	pages, err := ocrresult.BindSealed(request, result, sealed, total)
	if err != nil {
		t.Fatal(err)
	}
	for i, page := range pages {
		if page.Input != request.Pages[i] || page.Artifact.ID != refs[i] || page.Artifact.SHA256 != sha256.Sum256([]byte(payloads[i])) || page.Artifact.RelativePath == "" {
			t.Fatalf("page %d lost coordinator identity/metadata", i)
		}
	}
	// Provider references from another scope must not acquire this scope's metadata.
	foreign, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxTotalOutputBytes: 1024, MaxOutputs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	writer, err := foreign.CreateOutput(ctx, ocrplugin.Text)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte("foreign")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	result.Pages[0].ArtifactID = writer.Reference()
	if output, err := ocrresult.BindSealed(request, result, sealed, total); !errors.Is(err, ocrplugin.ErrResult) || output != nil {
		t.Fatalf("foreign reference accepted: %v", err)
	}
	if output, err := ocrresult.BindSealed(request, ocrplugin.Result{ModelVersion: "version", Pages: []ocrplugin.PageResult{{PageID: "first", ArtifactID: refs[0], Kind: ocrplugin.Text}, {PageID: "second", ArtifactID: refs[1], Kind: ocrplugin.Text}}}, sealed, total-1); !errors.Is(err, ocrplugin.ErrResultLimit) || output != nil {
		t.Fatalf("over-budget scope accepted: %v", err)
	}
	sealed[0].RelativePath = "changed"
	request.Pages[0].ID = "changed"
	if pages[0].Input.ID != "first" || pages[0].Artifact.RelativePath == "changed" {
		t.Fatal("binding aliases mutable caller records")
	}
}

func TestPositionedExactCoverage(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	scope, err := artifactscope.New(root, nil, artifactscope.Limits{MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxTotalOutputBytes: 2048, MaxOutputs: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	request := ocrplugin.Request{JobID: "job", InputRevision: 7, ModelID: "installed", ModelVersion: "exact-version", Output: ocrplugin.PositionedText, Pages: []ocrplugin.InputPage{{ID: "first", ArtifactID: "original-first", Width: 80, Height: 100}, {ID: "second", ArtifactID: "original-second", Width: 90, Height: 200}}}
	result := ocrplugin.Result{ModelVersion: request.ModelVersion}
	var total int64
	for _, input := range request.Pages {
		writer, err := scope.CreateOutput(ctx, ocrplugin.PositionedText)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(ocrplugin.PositionedPage{PageID: input.ID, Width: input.Width, Height: input.Height, Spans: []ocrplugin.Span{{Text: "日本語", Box: ocrplugin.Box{X: 1, Y: 2, Width: 3, Height: 4}}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		total += int64(len(payload))
		result.Pages = append(result.Pages, ocrplugin.PageResult{PageID: input.ID, ArtifactID: writer.Reference(), Kind: ocrplugin.PositionedText})
	}
	sealed, err := scope.Seal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result.Pages[0], result.Pages[1] = result.Pages[1], result.Pages[0]
	pages, err := ocrresult.BindSealed(request, result, sealed, total)
	if err != nil || len(pages) != 2 || pages[0].Input.ID != "first" || pages[1].Input.ID != "second" || pages[0].Artifact.ID != result.Pages[1].ArtifactID {
		t.Fatalf("sealed results did not follow immutable request order: %v", err)
	}
	for _, name := range []string{"missing-page", "foreign-artifact", "duplicate-page", "duplicate-artifact", "extra-artifact", "reused-input", "wrong-kind", "stale-version", "aggregate-bytes"} {
		t.Run(name, func(t *testing.T) {
			changed := result
			changed.Pages = append([]ocrplugin.PageResult(nil), result.Pages...)
			artifacts := append([]artifactscope.Artifact(nil), sealed...)
			budget := total
			switch name {
			case "missing-page":
				changed.Pages = changed.Pages[:1]
			case "foreign-artifact":
				changed.Pages[0].ArtifactID = "foreign-output"
			case "duplicate-page":
				changed.Pages[1].PageID = changed.Pages[0].PageID
			case "duplicate-artifact":
				changed.Pages[1].ArtifactID = changed.Pages[0].ArtifactID
			case "extra-artifact":
				artifacts = append(artifacts, artifacts[0])
			case "reused-input":
				artifacts[0].ID = request.Pages[0].ArtifactID
			case "wrong-kind":
				changed.Pages[0].Kind = ocrplugin.Text
			case "stale-version":
				changed.ModelVersion = "another-version"
			case "aggregate-bytes":
				budget--
			}
			if bound, err := ocrresult.BindSealed(request, changed, artifacts, budget); err == nil || bound != nil {
				t.Fatal("invalid positioned coverage accepted")
			}
		})
	}
}
