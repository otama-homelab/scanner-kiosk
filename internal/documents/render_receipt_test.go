package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestRenderReceiptV1Compatibility(t *testing.T) {
	f := setup(t)
	request := admissionRequest()
	key := sha256.Sum256([]byte(request.IdempotencyKey))
	// Digest from the original v1 command framing and canonical JSON, before the
	// HTTP admission refactor. Existing receipts must survive this code change.
	digest, err := hex.DecodeString("5fbb4d09a3abed25258cf0d36fef8cf18f7d0bab27563e77655e9522e0de9a6f")
	if err != nil {
		t.Fatal(err)
	}
	must(t, f.docs, `INSERT INTO request_receipts(owner_id,key_hash,operation,request_hash,result_job_id,expires_at_ms) VALUES(?,?,'render',?,?,3600000)`, alice, key[:], digest, jobA)
	result, found, err := f.store.FindRender(context.Background(), alice, request)
	if err != nil || !found || !result.Replay || result.JobID != jobA {
		t.Fatal("v1 receipt not reconciled", result, found, err)
	}
	result, err = f.store.AdmitRender(context.Background(), alice, request, admissionLimits())
	if err != nil || !result.Replay || result.JobID != jobA {
		t.Fatal("v1 admission replay changed", result, err)
	}
}
