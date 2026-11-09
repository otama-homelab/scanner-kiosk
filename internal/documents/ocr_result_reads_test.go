package documents

import (
 "context"
 "encoding/json"
 "testing"
)

func TestReOCRResultReads(t *testing.T) {
 f, b := completedOCRCandidate(t)
 p, err := f.store.ReadDocument(context.Background(), alice, docA)
 if err != nil { t.Fatal(err) }
 raw, err := json.Marshal(p)
 if err != nil { t.Fatal(err) }
 var fields map[string]json.RawMessage
 if err := json.Unmarshal(raw, &fields); err != nil { t.Fatal(err) }
 if fields["ocr_results"] == nil { t.Fatal("candidate result identity unavailable in document projection", b.ID) }
}
