package documents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// Count actual driver executions, including prepared-statement fallback. This
// does not inspect implementation text or change production query execution.
type queryConnector struct {
	base    driver.Driver
	dsn     string
	queries *atomic.Int64
}

func (c queryConnector) Driver() driver.Driver { return c.base }
func (c queryConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.base.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &queryConn{Conn: conn, queries: c.queries}, nil
}

type queryConn struct {
	driver.Conn
	queries *atomic.Int64
}

func (c *queryConn) Prepare(q string) (driver.Stmt, error) {
	s, e := c.Conn.Prepare(q)
	if e != nil {
		return nil, e
	}
	return &queryStmt{Stmt: s, queries: c.queries}, nil
}
func (c *queryConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *queryConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if v, ok := c.Conn.(driver.ExecerContext); ok {
		return v.ExecContext(ctx, q, args)
	}
	return nil, driver.ErrSkip
}
func (c *queryConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if v, ok := c.Conn.(driver.QueryerContext); ok {
		rows, e := v.QueryContext(ctx, q, args)
		if !errors.Is(e, driver.ErrSkip) {
			c.queries.Add(1)
		}
		return rows, e
	}
	return nil, driver.ErrSkip
}

type queryStmt struct {
	driver.Stmt
	queries *atomic.Int64
}

func (s *queryStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.queries.Add(1)
	return s.Stmt.Query(args)
}
func (s *queryStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if v, ok := s.Stmt.(driver.StmtQueryContext); ok {
		rows, e := v.QueryContext(ctx, args)
		if !errors.Is(e, driver.ErrSkip) {
			s.queries.Add(1)
		}
		return rows, e
	}
	return nil, driver.ErrSkip
}
func countedDB(t *testing.T, db *sql.DB, path string, count *atomic.Int64) *sql.DB {
	t.Helper()
	address := url.URL{Scheme: "file", Path: path}
	query := url.Values{"_txlock": {"immediate"}, "_pragma": {"foreign_keys(1)", "busy_timeout(5000)"}}
	address.RawQuery = query.Encode()
	result := sql.OpenDB(queryConnector{base: db.Driver(), dsn: address.String(), queries: count})
	result.SetMaxOpenConns(1)
	t.Cleanup(func() { result.Close() })
	return result
}
func TestHistoryQueryCountIndependentOfDocumentsAndPages(t *testing.T) {
	f := setup(t)
	ids := []string{docA}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("document_batch_%04d", i)
		ids = append(ids, id)
		must(t, f.docs, `INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',1000,10000)`, id, alice)
	}
	for i, id := range ids {
		for page := 0; page < 2; page++ {
			artifact := fmt.Sprintf("original_batch_%04d_%d", i, page)
			pid := fmt.Sprintf("page_batch_%04d_%d", i, page)
			must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'original','ready',?,'image/png',20,zeroblob(32),1,1000)`, artifact, id, alice, artifact+".png")
			must(t, f.docs, `INSERT INTO pages(id,document_id,owner_id,ordinal,original_artifact_id,rotation,capture_size,capture_color,capture_dpi) VALUES(?,?,?,?,?,90,'a4','gray',300)`, pid, id, alice, page, artifact)
		}
		output := fmt.Sprintf("output_batch_%04d", i)
		must(t, f.docs, `INSERT INTO artifacts(id,document_id,owner_id,kind,state,relative_path,media_type,byte_size,sha256,input_revision,created_at_ms) VALUES(?,?,?,'output','ready',?,'application/pdf',30,zeroblob(32),1,1000)`, output, id, alice, output+".pdf")
		must(t, f.docs, `INSERT INTO published_outputs(document_id,owner_id,revision,artifact_id) VALUES(?,?,1,?)`, id, alice, output)
	}
	var stateQueries, documentQueries atomic.Int64
	store, err := New(countedDB(t, f.state, f.statePath, &stateQueries), countedDB(t, f.docs, f.docsPath, &documentQueries), func() time.Time { return time.UnixMilli(f.now) }, fixtureSpace)
	if err != nil {
		t.Fatal(err)
	}
	assertCounts := func(state, docs int64) {
		t.Helper()
		if stateQueries.Load() != state || documentQueries.Load() != docs {
			t.Fatalf("queries state=%d documents=%d; want %d/%d", stateQueries.Load(), documentQueries.Load(), state, docs)
		}
	}
	reset := func() { stateQueries.Store(0); documentQueries.Store(0) }
	ctx := context.Background()
	for _, limit := range []int{1, 20, 100} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			reset()
			values, next, err := store.ReadHistory(ctx, alice, limit, nil)
			if err != nil || len(values) != limit || next == nil {
				t.Fatal("history pagination", len(values), next, err)
			}
			assertCounts(1, 3)
			for _, p := range values {
				if p.ID == docB || p.PageCount != 2 || len(p.Pages) != 2 || p.Output == nil || p.Output.Format != "pdf" || p.ReadyRevision == nil || *p.ReadyRevision != 1 {
					t.Fatal("batch projection lost content or owner isolation")
				}
			}
			if limit == 100 {
				reset()
				tail, last, err := store.ReadHistory(ctx, alice, limit, next)
				if err != nil || len(tail) != 1 || last != nil {
					t.Fatal("last page", len(tail), err)
				}
				assertCounts(1, 3)
			}
		})
	}
	reset()
	p, err := store.ReadDocument(ctx, alice, docA)
	if err != nil || p.PageCount != 2 {
		t.Fatal(err)
	}
	assertCounts(1, 3)
	reset()
	if _, err = store.ReadDocument(ctx, bob, docA); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-owner detail", err)
	}
	assertCounts(1, 1)
	reset()
	values, _, err := store.ReadHistory(ctx, alice, 100, &Position{ExpiresAtMS: 10000, ID: "zzzzzzzzzzzzzzzz"})
	if err != nil || len(values) != 0 {
		t.Fatal("empty history", err)
	}
	assertCounts(1, 1)
	must(t, f.state, "UPDATE principals SET disabled_at_ms=2000 WHERE id=?", alice)
	reset()
	if _, _, err = store.ReadHistory(ctx, alice, 100, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("disabled owner", err)
	}
	assertCounts(1, 0)
}
