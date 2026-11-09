package documenthttp

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"scanner-kiosk/internal/documents"
)

func TestDeliveryStrictJSONAndProjection(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"task_id":null}`, `{"TASK_ID":"id"}`, `{"task_id":"one","task_id":"two"}`, `{"url":"https://private.invalid/"}`, `{} {}`, `{"task_id":"` + strings.Repeat("a", 4096) + `"}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, ok := deliveryJSON(w, r, map[string]bool{"task_id": true}); ok || w.Code < 400 {
			t.Fatal("invalid recovery JSON accepted", body, w.Code)
		}
	}
	d := documents.Delivery{ID: "delivery_public_http01", OwnerID: "private-owner", DocumentID: "document_public_http01", DestinationID: "private-destination", ConfigFingerprint: "private-fingerprint", Filename: "private-filename", TaskID: "private-task", State: "unknown", AttemptedAtMS: 1}
	d.Artifact.RelativePath = "private-path"
	raw, err := json.Marshal(projectDelivery(d))
	if err != nil || strings.Contains(string(raw), "private-") || !strings.Contains(string(raw), `"destination":"paperless"`) || !strings.Contains(string(raw), `"remote_processing_may_continue":true`) {
		t.Fatal("unsafe projection", string(raw), err)
	}
}
