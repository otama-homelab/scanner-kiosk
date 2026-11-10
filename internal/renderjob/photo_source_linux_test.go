//go:build linux

package renderjob

import (
	"context"
	"testing"

	"scanner-kiosk/internal/documents"
)

type sourceOverride struct {
	*faultRecords
	work documents.RenderWork
}

func (r sourceOverride) LoadRenderWork(context.Context, string, string, string) (documents.RenderWork, error) {
	return r.work, nil
}

func TestPhotoEffectiveSourceStrictLegacyGuards(t *testing.T) {
	for _, mode := range []string{"scan-original", "scan-effective", "pdf-masquerade", "photo-without-lineage", "photo-raw-fallback", "photo-wrong-original", "photo-wrong-hash"} {
		t.Run(mode, func(t *testing.T) {
			records, _, root, job, options := fixture(t)
			work, err := records.LoadRenderWork(context.Background(), owner, job, runner)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "scan-effective":
				work.Inputs[0].ID = "other_original_0001"
			case "pdf-masquerade":
				work.Snapshot.Pages[0].SourceType = "pdf"
			case "photo-without-lineage":
				work.Snapshot.Pages[0].SourceType = "photo"
			case "photo-raw-fallback", "photo-wrong-original", "photo-wrong-hash":
				work.Snapshot.Pages[0].SourceType = "photo"
				source := documents.RenderSource{Original: work.Inputs[0], Effective: work.Inputs[0]}
				if mode != "photo-raw-fallback" {
					source.Original.ID = "immutable_raw_00001"
					work.Snapshot.Pages[0].OriginalArtifactID = source.Original.ID
				}
				if mode == "photo-wrong-original" {
					source.Original.ID = "foreign_raw_000001"
				}
				if mode == "photo-wrong-hash" {
					source.Effective.SHA256[0] ^= 1
				}
				work.Snapshot.Sources = []documents.RenderSource{source}
			}
			prepared, err := Prepare(context.Background(), sourceOverride{records, work}, root, owner, job, runner, options)
			if mode == "scan-original" {
				if err != nil {
					t.Fatal("legacy scan rejected", err)
				}
				prepared.Close()
				return
			}
			if err == nil {
				prepared.Close()
				t.Fatal("mismatched source accepted", mode)
			}
		})
	}
}
