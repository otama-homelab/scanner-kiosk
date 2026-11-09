//go:build linux

package workerruntime

import (
	"context"
	"scanner-kiosk/internal/artifactread"
	"scanner-kiosk/internal/documents"
	"scanner-kiosk/internal/paperless"
)

// ProcessDelivery performs one bounded step. A later tick can only GET after an attempt.
func ProcessDelivery(ctx context.Context, store *documents.Store, root *artifactread.Root, client *paperless.Client, owner, deliveryID string) error {
	d, err := store.ReadDelivery(ctx, owner, deliveryID)
	if err != nil {
		return err
	}
	if d.DestinationID != client.DestinationID() || d.ConfigFingerprint != client.ConfigFingerprint() {
		return documents.ErrConflict
	}
	if d.State == "queued" {
		file, err := root.Open(ctx, artifactread.Input{RelativePath: d.Artifact.RelativePath, Size: d.Artifact.Size, SHA256: d.Artifact.SHA256})
		if err != nil {
			return err
		}
		defer file.Close()
		d, err = store.ClaimDelivery(ctx, owner, deliveryID)
		if err != nil {
			return err
		}
		taskID, err := client.Upload(ctx, d.Filename, d.Artifact.Size, file)
		if err != nil {
			if markErr := store.MarkDeliveryUnknown(ctx, owner, deliveryID); markErr != nil {
				return markErr
			}
			return err
		}
		return store.AcceptDelivery(ctx, owner, deliveryID, taskID)
	}
	if d.State == "attempted" {
		return store.MarkDeliveryUnknown(ctx, owner, deliveryID)
	}
	if (d.State != "accepted" && d.State != "unknown") || d.TaskID == "" {
		return nil
	}
	task, err := client.Task(ctx, d.TaskID, d.Filename)
	if err != nil {
		if markErr := store.MarkDeliveryUnknown(ctx, owner, deliveryID); markErr != nil {
			return markErr
		}
		return err
	}
	if task.Status != "SUCCESS" {
		return nil
	}
	if err = client.Metadata(ctx, *task.RelatedDocument, d.Artifact.SHA256); err != nil {
		if markErr := store.MarkDeliveryUnknown(ctx, owner, deliveryID); markErr != nil {
			return markErr
		}
		return err
	}
	return store.ConfirmDelivery(ctx, owner, deliveryID, d.TaskID, *task.RelatedDocument, d.Artifact.SHA256)
}
