package documents

import (
	"context"
	"crypto/sha256"
	"time"
)

type DeleteRequest struct {
	DocumentID     string
	Revision       int64
	IdempotencyKey string
}

// LifecycleRecords records revocation intents atomically. Each adapter rechecks
// owner, expiry, revision/job state and receipts; neither command proves physical
// stop or releases original files, device leases or reservations.
type LifecycleRecords interface {
	ApplyDelete(context.Context, string, DeleteCommand) (JobProjection, error)
	ApplyCancel(context.Context, string, CancelCommand) (JobProjection, error)
}

type Lifecycle struct{ records LifecycleRecords }

func NewLifecycle(records LifecycleRecords) (*Lifecycle, error) {
	if records == nil {
		return nil, ErrInvalid
	}
	return &Lifecycle{records: records}, nil
}

// DeleteCommand preserves the validated v1 delete intent and receipt hashes.
// Its zero value is invalid; request and hash accessors return detached values.
type DeleteCommand struct {
	request     DeleteRequest
	retention   time.Duration
	key, digest [32]byte
	valid       bool
}

func (c DeleteCommand) Request() (DeleteRequest, time.Duration, error) {
	if !c.valid {
		return DeleteRequest{}, 0, ErrInvalid
	}
	return c.request, c.retention, nil
}
func (c DeleteCommand) Hashes() ([32]byte, [32]byte) { return c.key, c.digest }

// CancelCommand preserves the established cancel-v1 receipt identity.
type CancelCommand struct {
	jobID, key      string
	retention       time.Duration
	keyHash, digest [32]byte
	valid           bool
}

func (c CancelCommand) Request() (string, string, time.Duration, error) {
	if !c.valid {
		return "", "", 0, ErrInvalid
	}
	return c.jobID, c.key, c.retention, nil
}
func (c CancelCommand) Hashes() ([32]byte, [32]byte) { return c.keyHash, c.digest }

func (l *Lifecycle) RequestDelete(ctx context.Context, owner string, request DeleteRequest, retention time.Duration) (JobProjection, error) {
	if !id(request.DocumentID) || request.Revision < 1 || !id(request.IdempotencyKey) || retention < time.Second || retention > 365*24*time.Hour {
		return JobProjection{}, ErrInvalid
	}
	canonical, err := canonicalCommand(struct {
		Version  int    `json:"version"`
		Document string `json:"document"`
		Revision int64  `json:"revision"`
	}{1, request.DocumentID, request.Revision})
	if err != nil {
		return JobProjection{}, ErrInvalid
	}
	digest := sha256.Sum256(canonical)
	key := sha256.Sum256([]byte(request.IdempotencyKey))
	return l.records.ApplyDelete(ctx, owner, DeleteCommand{request: request, retention: retention, key: key, digest: digest, valid: true})
}

func (l *Lifecycle) CancelJob(ctx context.Context, owner, jobID, key string, retention time.Duration) (JobProjection, error) {
	if !id(jobID) || !id(key) || retention < time.Second || retention > 365*24*time.Hour {
		return JobProjection{}, ErrInvalid
	}
	command := CancelCommand{jobID: jobID, key: key, retention: retention, keyHash: sha256.Sum256([]byte(key)), digest: sha256.Sum256([]byte("cancel-v1\x00" + jobID)), valid: true}
	return l.records.ApplyCancel(ctx, owner, command)
}
