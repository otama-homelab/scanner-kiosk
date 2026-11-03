//go:build linux

package ocrresult_test

import (
	"context"
	"crypto/sha256"
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
