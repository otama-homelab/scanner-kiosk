package documents

import (
	"context"
	"database/sql"
	"math"
)

func validScanPlacement(before, replacement string) bool {
	return (before == "" || id(before)) && (replacement == "" || id(replacement)) && (before == "" || replacement == "")
}

// Called only inside successful scan publication; a failed transaction retains
// the replacement target and the complete previous page order.
func applyScanPlacement(ctx context.Context, tx *sql.Tx, document, owner string, snapshot ScanSnapshot, appendOrdinal int) (int, error) {
	target := snapshot.InsertBeforePageID
	if snapshot.ReplacePageID != "" {
		target = snapshot.ReplacePageID
	}
	if target == "" {
		return appendOrdinal, nil
	}
	var ordinal int
	if err := tx.QueryRowContext(ctx, "SELECT ordinal FROM pages WHERE id=? AND document_id=? AND owner_id=?", target, document, owner).Scan(&ordinal); err != nil {
		return 0, readError(err)
	}
	if snapshot.ReplacePageID != "" {
		if _, err := tx.ExecContext(ctx, "DELETE FROM pages WHERE id=? AND document_id=? AND owner_id=?", target, document, owner); err != nil {
			return 0, ErrStorage
		}
		return ordinal, nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,ordinal FROM pages WHERE document_id=? AND owner_id=? AND ordinal>=? ORDER BY ordinal DESC", document, owner, ordinal)
	if err != nil {
		return 0, ErrStorage
	}
	type position struct {
		id      string
		ordinal int64
	}
	var positions []position
	for rows.Next() {
		var p position
		if rows.Scan(&p.id, &p.ordinal) != nil || p.ordinal == math.MaxInt64 {
			rows.Close()
			return 0, ErrStorage
		}
		positions = append(positions, p)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return 0, ErrStorage
	}
	// Descending updates avoid transient collisions with the unique ordinal key.
	for _, p := range positions {
		if _, err = tx.ExecContext(ctx, "UPDATE pages SET ordinal=? WHERE id=? AND document_id=? AND owner_id=?", p.ordinal+1, p.id, document, owner); err != nil {
			return 0, ErrStorage
		}
	}
	return ordinal, nil
}
