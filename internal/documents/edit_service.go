package documents

import (
	"context"
	"crypto/sha256"
	"math"
	"slices"
	"time"
)

type EditRequest struct {
	DocumentID     string
	Revision       int64
	IdempotencyKey string
	Operation      string
	PageID         string
	Rotation       int
	PageIDs        []string
}

// EditRecords commits a validated edit as one atomic command. Implementations
// must recheck enabled owner, expiry, revision, active work and receipt identity;
// no SQL transaction or independently writable document records cross this port.
type EditRecords interface {
	ApplyEdit(context.Context, string, EditCommand) (Projection, error)
}

type Editor struct{ records EditRecords }

func NewEditor(records EditRecords) (*Editor, error) {
	if records == nil {
		return nil, ErrInvalid
	}
	return &Editor{records: records}, nil
}

// Edit changes metadata only; the backend preserves originals, creation-based
// TTL and historical replay results even after subsequent edits.
func (e *Editor) Edit(ctx context.Context, owner string, request EditRequest, retention time.Duration) (Projection, error) {
	command, err := prepareEdit(request, retention)
	if err != nil {
		return Projection{}, err
	}
	return e.records.ApplyEdit(ctx, owner, command)
}

// EditCommand binds an immutable validated request to the existing v1 receipt
// hashes. Zero commands are invalid; adapters read detached values via Request.
type EditCommand struct {
	request     EditRequest
	retention   time.Duration
	key, digest [32]byte
	valid       bool
}

func (c EditCommand) Request() (EditRequest, time.Duration, error) {
	if !c.valid {
		return EditRequest{}, 0, ErrInvalid
	}
	request := c.request
	request.PageIDs = slices.Clone(request.PageIDs)
	return request, c.retention, nil
}
func (c EditCommand) Hashes() ([32]byte, [32]byte) { return c.key, c.digest }

func prepareEdit(request EditRequest, retention time.Duration) (EditCommand, error) {
	if !id(request.DocumentID) || !id(request.IdempotencyKey) || request.Revision < 1 || request.Revision == math.MaxInt64 || retention < time.Second || retention > 365*24*time.Hour {
		return EditCommand{}, ErrInvalid
	}
	switch request.Operation {
	case "rotate":
		if !id(request.PageID) || request.PageIDs != nil || (request.Rotation != 0 && request.Rotation != 90 && request.Rotation != 180 && request.Rotation != 270) {
			return EditCommand{}, ErrInvalid
		}
	case "delete_page":
		if !id(request.PageID) || request.PageIDs != nil || request.Rotation != 0 {
			return EditCommand{}, ErrInvalid
		}
	case "reorder":
		if request.PageID != "" || request.Rotation != 0 || len(request.PageIDs) < 1 || len(request.PageIDs) > 1000 {
			return EditCommand{}, ErrInvalid
		}
		seen := map[string]bool{}
		for _, p := range request.PageIDs {
			if !id(p) || seen[p] {
				return EditCommand{}, ErrInvalid
			}
			seen[p] = true
		}
	default:
		return EditCommand{}, ErrInvalid
	}
	request.PageIDs = slices.Clone(request.PageIDs)
	// Explicit v1 wire shape avoids dependence on Go field-name refactors.
	canonical, err := canonicalCommand(struct {
		Version   int      `json:"version"`
		Operation string   `json:"operation"`
		Document  string   `json:"document"`
		Revision  int64    `json:"revision"`
		Page      string   `json:"page"`
		Rotation  int      `json:"rotation"`
		Pages     []string `json:"pages"`
	}{1, request.Operation, request.DocumentID, request.Revision, request.PageID, request.Rotation, request.PageIDs})
	if err != nil {
		return EditCommand{}, ErrInvalid
	}
	key := sha256.Sum256([]byte(request.IdempotencyKey))
	digest := sha256.Sum256(canonical)
	return EditCommand{request: request, retention: retention, key: key, digest: digest, valid: true}, nil
}
