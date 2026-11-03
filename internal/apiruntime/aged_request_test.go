package apiruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/documents"
)

func TestAgedScanHTTPReplayAndPurgedClockFence(t *testing.T) {
	f := scanHTTPFixture(t)
	f.ready(t, 0)
	key := func(at int64, n int) string { return fmt.Sprintf("rq1_%013d_%032x", at, n) }
	now := time.Now().UnixMilli()
	expired := scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, key(now-(25*time.Hour).Milliseconds(), 1), "", nil)
	if expired.Code != 410 || !strings.Contains(expired.Body.String(), "gone") {
		t.Fatal("expired native intent reached readiness", expired.Code, expired.Body.String())
	}
	fresh := key(now, 2)
	first := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, fresh, "", nil))
	again := scanHTTPJob(t, scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, fresh, "", nil))
	if again.ID != first.ID {
		t.Fatal("native intent replayed")
	}
	future := time.Now().Add(73 * time.Hour)
	records, err := documents.New(f.app.state, f.app.docs, func() time.Time { return future }, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	if n, err := records.PurgeExpiredRequestReceipts(context.Background(), 100); err != nil || n != 1 {
		t.Fatal("receipt retirement", n, err)
	}
	// The serving clock is now behind the purge clock. It must fail closed even
	// though the original timestamp would otherwise look fresh again.
	w := scanHTTPCall(f.app, f.cookie, "/api/documents", scanBody, fresh, "", nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "storage_unavailable") {
		t.Fatal("retired key bypassed clock fence", w.Code, w.Body.String())
	}
	var jobs, reservations int
	if err = f.app.docs.QueryRow("SELECT count(*) FROM jobs").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err = f.app.docs.QueryRow("SELECT count(*) FROM reservations").Scan(&reservations); err != nil || jobs != 1 || reservations != 1 {
		t.Fatal("retirement recreated/released original work", jobs, reservations, err)
	}
}
