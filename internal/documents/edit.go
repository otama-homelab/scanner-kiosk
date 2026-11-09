package documents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"
)

var ErrRevision = errors.New("document revision changed")

// Edit retains the existing entry point while routing policy through Editor.
func (s *Store) Edit(ctx context.Context, owner string, request EditRequest, retention time.Duration) (Projection, error) {
	return (&Editor{records: s}).Edit(ctx, owner, request, retention)
}

// ApplyEdit is the SQLite atomic adapter; authorization is checked inside its
// transaction even though the command's shape was validated by the service.
func (s *Store) ApplyEdit(ctx context.Context, owner string, command EditCommand) (Projection, error) {
	request, retention, err := command.Request()
	if err != nil {
		return Projection{}, err
	}
	key, digest := command.Hashes()
	var out Projection
	err = s.withEnabled(ctx, owner, func(tx *sql.Tx) error {
		now := s.now().UnixMilli()
		if now < 0 {
			return ErrInvalid
		}
		var operation string
		var hash []byte
		var expiry int64
		err := tx.QueryRowContext(ctx, `SELECT operation,request_hash,expires_at_ms FROM request_receipts WHERE owner_id=? AND key_hash=?`, owner, key[:]).Scan(&operation, &hash, &expiry)
		if err == nil {
			if operation != request.Operation || !sameHash(hash, digest) {
				return ErrIdempotencyConflict
			}
			if expiry <= now {
				return ErrGone
			}
			var stored string
			err = tx.QueryRowContext(ctx, `SELECT e.projection_json FROM edit_results e JOIN documents d ON d.id=e.document_id AND d.owner_id=e.owner_id WHERE e.owner_id=? AND e.key_hash=? AND d.expires_at_ms>? AND d.state NOT IN ('expired','deleting')`, owner, key[:], now).Scan(&stored)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrGone
			}
			if err != nil {
				return ErrStorage
			}
			if err = json.Unmarshal([]byte(stored), &out); err != nil {
				return ErrStorage
			}
			if s.now().UnixMilli() >= out.ExpiresAt.UnixMilli() {
				return ErrGone
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrStorage
		}
		keyExpiry, err := s.freshRequestKey(ctx, tx, owner, request.IdempotencyKey, now)
		if err != nil {
			return err
		}
		var revision, documentExpiry int64
		var state string
		err = tx.QueryRowContext(ctx, `SELECT revision,state,expires_at_ms FROM documents WHERE id=? AND owner_id=? AND expires_at_ms>? AND state NOT IN ('expired','deleting')`, request.DocumentID, owner, now).Scan(&revision, &state, &documentExpiry)
		if err != nil {
			return readError(err)
		}
		if revision != request.Revision {
			return ErrRevision
		}
		if state != "review" && state != "ready" && state != "failed" {
			return ErrConflict
		}
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE document_id=? AND owner_id=? AND state IN ('queued','running','cancelling','reconciling')`, request.DocumentID, owner).Scan(&active); err != nil {
			return ErrStorage
		}
		if active != 0 {
			return ErrConflict
		}
		deadline, ok := addMillis(now, retention)
		if !ok || deadline < documentExpiry {
			return ErrInvalid
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,ordinal FROM pages WHERE document_id=? AND owner_id=? ORDER BY ordinal`, request.DocumentID, owner)
		if err != nil {
			return ErrStorage
		}
		pages := []string{}
		var maxOrdinal int64
		for rows.Next() {
			var p string
			var n int64
			if err = rows.Scan(&p, &n); err != nil {
				rows.Close()
				return ErrStorage
			}
			pages = append(pages, p)
			maxOrdinal = max(maxOrdinal, n)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrStorage
		}
		if len(pages) == 0 {
			return ErrConflict
		}
		if request.Operation == "reorder" {
			if len(pages) != len(request.PageIDs) {
				return ErrInvalid
			}
			present := map[string]bool{}
			for _, p := range pages {
				present[p] = true
			}
			for _, p := range request.PageIDs {
				if !present[p] {
					return ErrInvalid
				}
			}
			pages = request.PageIDs
		} else {
			found := false
			for _, p := range pages {
				if p == request.PageID {
					found = true
				}
			}
			if !found {
				return ErrUnavailable
			}
			if request.Operation == "rotate" {
				if _, err = tx.ExecContext(ctx, `UPDATE pages SET rotation=? WHERE id=? AND document_id=? AND owner_id=?`, request.Rotation, request.PageID, request.DocumentID, owner); err != nil {
					return ErrStorage
				}
			} else {
				if _, err = tx.ExecContext(ctx, `DELETE FROM pages WHERE id=? AND document_id=? AND owner_id=?`, request.PageID, request.DocumentID, owner); err != nil {
					return ErrStorage
				}
				remaining := []string{}
				for _, p := range pages {
					if p != request.PageID {
						remaining = append(remaining, p)
					}
				}
				pages = remaining
			}
		}
		if request.Operation != "rotate" {
			if maxOrdinal > math.MaxInt64-int64(len(pages))-1 {
				return ErrStorage
			}
			// Move each ordinal outside the old range, then install the new contiguous order.
			for i, p := range pages {
				if _, err = tx.ExecContext(ctx, `UPDATE pages SET ordinal=? WHERE id=? AND document_id=? AND owner_id=?`, maxOrdinal+1+int64(i), p, request.DocumentID, owner); err != nil {
					return ErrStorage
				}
			}
			for i, p := range pages {
				if _, err = tx.ExecContext(ctx, `UPDATE pages SET ordinal=? WHERE id=? AND document_id=? AND owner_id=?`, i, p, request.DocumentID, owner); err != nil {
					return ErrStorage
				}
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM published_outputs WHERE document_id=? AND owner_id=?`, request.DocumentID, owner); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `UPDATE documents SET revision=revision+1,state='review' WHERE id=? AND owner_id=?`, request.DocumentID, owner); err != nil {
			return ErrStorage
		}
		out, err = s.project(ctx, tx, owner, request.DocumentID)
		if err != nil {
			return err
		}
		stored, err := json.Marshal(out)
		if err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,expires_at_ms,key_expires_at_ms) VALUES(?,?,?,?,?,?)`, owner, key[:], request.Operation, digest[:], deadline, keyExpiry); err != nil {
			return ErrStorage
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO edit_results(owner_id,key_hash,document_id,projection_json) VALUES(?,?,?,?)`, owner, key[:], request.DocumentID, string(stored)); err != nil {
			return ErrStorage
		}
		if s.now().UnixMilli() >= documentExpiry {
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return Projection{}, err
	}
	return out, nil
}
