//go:build linux

package workerruntime

import (
	"path/filepath"
	"testing"

	"scanner-kiosk/internal/storage"
)

func TestDeliveryTracerAPI9Lifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "documents.sqlite3")
	db, err := storage.Open(path, storage.Documents)
	if err != nil { t.Fatal(err) }
	db.Close()
	db, err = storage.Open(path, storage.Documents)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	var count int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='deliveries'").Scan(&count); err != nil { t.Fatal(err) }
	if count != 1 { t.Fatal("durable delivery ledger must exist independently of normal jobs before sending") }
}
