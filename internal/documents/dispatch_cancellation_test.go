package documents

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

type dispatchErrorConnector struct {
	queryConnector
	fault  error
	cancel context.CancelFunc
	hits   *atomic.Int64
}

func (c dispatchErrorConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.queryConnector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &dispatchErrorConn{queryConn: conn.(*queryConn), fault: c.fault, cancel: c.cancel, hits: c.hits}, nil
}

type dispatchErrorConn struct {
	*queryConn
	fault  error
	cancel context.CancelFunc
	hits   *atomic.Int64
}

func (c *dispatchErrorConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	c.hits.Add(1)
	c.cancel()
	return nil, c.fault
}

func TestDispatchPreservesExplicitCancellationButNotCombinedFailure(t *testing.T) {
	for _, kind := range []string{"render", "scan"} {
		t.Run(kind, func(t *testing.T) {
			for _, mode := range []string{"cancel", "deadline", "combined", "storage"} {
				t.Run(mode, func(t *testing.T) {
					f := setup(t)
					before := scalar(t, f.docs, "SELECT count(*) FROM reservations")
					queued := scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE state='queued' AND runner_id IS NULL AND started_at_ms IS NULL")
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					fault, expected := error(context.Canceled), error(context.Canceled)
					switch mode {
					case "deadline":
						fault, expected = context.DeadlineExceeded, context.DeadlineExceeded
					case "combined":
						fault, expected = errors.Join(errors.New("independent failure"), context.Canceled), ErrStorage
					case "storage":
						fault, expected = errors.New("independent failure"), ErrStorage
					}
					var queries, hits atomic.Int64
					db := sql.OpenDB(dispatchErrorConnector{queryConnector: queryConnector{base: &sqlite.Driver{}, dsn: f.docsPath + "?_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", queries: &queries}, fault: fault, cancel: cancel, hits: &hits})
					defer db.Close()
					db.SetMaxOpenConns(1)
					store, err := New(f.state, db, f.store.now, fixtureSpace)
					if err != nil {
						t.Fatal(err)
					}
					commits := 0
					store.commit = func(tx *sql.Tx) error { commits++; return tx.Commit() }
					if kind == "render" {
						_, err = store.ClaimNextRender(ctx, runner)
					} else {
						_, err = store.ClaimNextScan(ctx, runner)
					}
					if err != expected || hits.Load() != 1 || commits != 0 {
						t.Fatalf("precommit result=%v expected=%v driver calls=%d commits=%d", err, expected, hits.Load(), commits)
					}
					if scalar(t, f.docs, "SELECT count(*) FROM reservations") != before || scalar(t, f.docs, "SELECT count(*) FROM jobs WHERE state='queued' AND runner_id IS NULL AND started_at_ms IS NULL") != queued {
						t.Fatal("failed dispatch mutated work/reservations")
					}
				})
			}
		})
	}
}
