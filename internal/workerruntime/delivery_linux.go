//go:build linux

package workerruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
		var nonce [24]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return documents.ErrStorage
		}
		runner := "delivery_runner_" + hex.EncodeToString(nonce[:])
		d, err = store.ClaimDeliveryAttempt(ctx, owner, deliveryID, runner)
		if err != nil {
			if errors.Is(err, documents.ErrCommitUncertain) {
				// A readback describes the known attempt; it never permits a POST.
				if known, readErr := store.LoadDeliveryAttempt(ctx, owner, deliveryID, runner); readErr == nil && known.State == "attempted" {
					return errors.Join(err, store.MarkDeliveryUnknown(ctx, owner, deliveryID))
				}
			}
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
	reserved, err := store.ReserveDeliveryPoll(ctx, owner, deliveryID)
	if err != nil || !reserved {
		return err
	}
	task, err := client.Task(ctx, d.TaskID, d.Filename)
	if err != nil {
		if markErr := store.MarkDeliveryUnknown(ctx, owner, deliveryID); markErr != nil {
			return markErr
		}
		return err
	}
	if task.Status == "FAILURE" || task.Status == "REVOKED" {
		return store.FailDelivery(ctx, owner, deliveryID, d.TaskID)
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

// RecoverDeliveryTask performs exact GET-only owner recovery. The adapter checks
// UUID and opaque filename before any remote identity is persisted locally.
func RecoverDeliveryTask(ctx context.Context, store *documents.Store, client *paperless.Client, owner, deliveryID, taskID string) error {
	if !paperless.ValidTaskID(taskID) {
		return documents.ErrInvalid
	}
	d, err := store.ReadDelivery(ctx, owner, deliveryID)
	if err != nil {
		return err
	}
	if d.DestinationID != client.DestinationID() || d.ConfigFingerprint != client.ConfigFingerprint() ||
		(d.State != "attempted" && d.State != "unknown" && d.State != "accepted") || (d.TaskID != "" && d.TaskID != taskID) {
		return documents.ErrConflict
	}
	if _, err = client.Task(ctx, taskID, d.Filename); err != nil {
		return errors.Join(err, store.MarkDeliveryUnknown(ctx, owner, deliveryID))
	}
	return store.AcceptDelivery(ctx, owner, deliveryID, taskID)
}
