package documents

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"scanner-kiosk/internal/storage"
)

type fixtureSnapshot struct {
	state, documents []byte
}

// Domain tests need independent stores, not repeated cold-migration coverage.
var emptyFixtureSchemas = sync.OnceValues(func() (fixtureSnapshot, error) {
	root, err := os.MkdirTemp("", "scanner-document-schema-")
	if err != nil {
		return fixtureSnapshot{}, err
	}
	defer os.RemoveAll(root)
	prepare := func(kind storage.Kind) ([]byte, error) {
		path := filepath.Join(root, string(kind)+".sqlite3")
		db, err := storage.Open(path, kind)
		if err != nil {
			return nil, err
		}
		var busy, frames, checkpointed int
		err = db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed)
		closeErr := db.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return nil, err
		}
		if busy != 0 {
			return nil, errors.New("fixture schema checkpoint did not finish")
		}
		return os.ReadFile(path)
	}
	state, err := prepare(storage.State)
	if err != nil {
		return fixtureSnapshot{}, err
	}
	documents, err := prepare(storage.Documents)
	return fixtureSnapshot{state: state, documents: documents}, err
})

func seedFixtureSchemas(t *testing.T, statePath, documentsPath string) {
	t.Helper()
	seed, err := emptyFixtureSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{statePath: seed.state, documentsPath: seed.documents} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFixtureSchemasRemainPrivateAndIndependent(t *testing.T) {
	first, second := setup(t), setup(t)
	must(t, first.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	must(t, first.docs, "UPDATE documents SET state='review' WHERE id=?", docA)
	if scalar(t, second.state, "SELECT count(*) FROM principals WHERE id=? AND disabled_at_ms IS NULL", alice) != 1 || scalar(t, second.docs, "SELECT count(*) FROM documents WHERE id=? AND state='processing'", docA) != 1 {
		t.Fatal("fixture mutations leaked into a separate store")
	}
	for _, f := range []*fixture{first, second} {
		if scalar(t, f.state, "PRAGMA foreign_keys") != 1 || scalar(t, f.docs, "PRAGMA foreign_keys") != 1 {
			t.Fatal("fixture bypassed storage connection constraints")
		}
		for _, path := range []string{f.statePath, f.docsPath} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("fixture file is not private", path, err)
			}
		}
	}
}
