package websession

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// A deliberately limited non-SQL fixture proves HTTP/session injection. It is
// not a production backend or evidence of cross-database transaction semantics.
type memorySessionFixture struct {
	mu         sync.Mutex
	sessions   map[string]SessionData
	revoked    map[string]bool
	principals map[string]bool
	starts     map[[32]byte]GuestStartRecord
	failCommit bool
}

func (m *memorySessionFixture) FindSession(_ context.Context, token string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.sessions[token]
	if !ok || m.revoked[token] || !r.Expiry.After(time.Now()) {
		return nil, false, nil
	}
	return append([]byte(nil), r.Data...), true, nil
}
func (m *memorySessionFixture) CommitSession(_ context.Context, record SessionData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCommit {
		return errors.New("injected backend unavailable")
	}
	if m.revoked[record.TokenHash] {
		return ErrSession
	}
	record.Data = append([]byte(nil), record.Data...)
	m.sessions[record.TokenHash] = record
	return nil
}
func (m *memorySessionFixture) RotateLogin(context.Context, LoginGrantRecord, time.Duration) error {
	return ErrSession
}
func (m *memorySessionFixture) CommitLogin(context.Context, SessionData, LoginGrantRecord) error {
	return ErrSession
}
func (m *memorySessionFixture) RevokeSession(_ context.Context, token string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revoked[token] = true
	delete(m.sessions, token)
	return nil
}
func (m *memorySessionFixture) KnownSession(_ context.Context, token string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[token]
	return ok || m.revoked[token], nil
}
func (m *memorySessionFixture) PrincipalKind(_ context.Context, principal string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.principals[principal] {
		return "", ErrSession
	}
	return "guest", nil
}
func (m *memorySessionFixture) CreateGuest(_ context.Context, record GuestStartRecord) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.starts[record.BindingHash]; ok {
		if !m.principals[existing.ProposedPrincipalID] {
			return "", ErrGone
		}
		if existing.KeyHash != record.KeyHash {
			return "", ErrConflict
		}
		return existing.ProposedPrincipalID, nil
	}
	m.starts[record.BindingHash] = record
	m.principals[record.ProposedPrincipalID] = true
	return record.ProposedPrincipalID, nil
}
func (m *memorySessionFixture) DisableGuest(_ context.Context, principal string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.principals[principal] {
		return ErrSession
	}
	m.principals[principal] = false
	return nil
}
func (m *memorySessionFixture) AuthorizeSession(ctx context.Context, token, principal, kind string) error {
	_, found, err := m.FindSession(ctx, token)
	if err != nil {
		return err
	}
	_, principalErr := m.PrincipalKind(ctx, principal)
	if !found || principalErr != nil || kind != "guest" {
		return ErrSession
	}
	return nil
}

func TestSessionHandlerWithNonSQLRecords(t *testing.T) {
	records := &memorySessionFixture{sessions: map[string]SessionData{}, revoked: map[string]bool{}, principals: map[string]bool{}, starts: map[[32]byte]GuestStartRecord{}}
	h, err := NewWithRecords(records, options())
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := request(t, h, "GET", "/api/session", nil, "")
	if view(t, bootstrap).Authenticated || len(records.principals) != 0 {
		t.Fatal("GET created identity")
	}
	anonymous := cookie(t, bootstrap)
	login := request(t, h, "POST", "/auth/guest", anonymous, "request_records_0001")
	if !view(t, login).Authenticated {
		t.Fatal("injected backend failed login")
	}
	current := cookie(t, login)
	if current.Value == anonymous.Value {
		t.Fatal("token was not rotated")
	}
	if view(t, request(t, h, "GET", "/api/session", anonymous, "")).Authenticated {
		t.Fatal("old cookie restored identity")
	}
	var authority context.Context
	protected := h.RequirePrincipal(func(w http.ResponseWriter, r *http.Request, p Principal) {
		if p.Kind != "guest" || p.ID == "" || p.CanAdminister {
			t.Error("wrong backend principal")
		}
		authority = r.Context()
		w.WriteHeader(204)
	})
	if result := request(t, protected, "GET", "/api/documents", current, ""); result.Code != 204 || authority == nil {
		t.Fatal("authorization did not use backend")
	}
	if err := h.CheckAuthorization(authority); err != nil {
		t.Fatal(err)
	}
	if result := request(t, h, "POST", "/auth/guest/end", current, "request_records_end_01"); result.Code != 202 {
		t.Fatal("end failed", result.Code)
	}
	if err := h.CheckAuthorization(authority); !errors.Is(err, ErrSession) {
		t.Fatal("ended session kept stream authority", err)
	}
	if result := request(t, protected, "GET", "/api/documents", current, ""); result.Code != 401 {
		t.Fatal("ended cookie regained authority", result.Code)
	}
	if err := records.CommitSession(context.Background(), SessionData{TokenHash: hashedToken(current.Value), Data: []byte("stale"), Expiry: time.Now().Add(time.Hour)}); !errors.Is(err, ErrSession) {
		t.Fatal("stale backend write resurrected session", err)
	}
}

func TestSessionRecordFailureDoesNotIssueCookie(t *testing.T) {
	if _, err := NewWithRecords(nil, options()); !errors.Is(err, ErrSession) {
		t.Fatal("nil backend accepted")
	}
	records := &memorySessionFixture{failCommit: true}
	h, err := NewWithRecords(records, options())
	if err != nil {
		t.Fatal(err)
	}
	result := request(t, h, "GET", "/api/session", nil, "")
	if result.Code != 503 || len(result.Result().Cookies()) != 0 {
		t.Fatal("failed commit issued session", result.Code)
	}
}

// Keep a concrete-type-free wrapper in OIDC HTTP tests as well: accidental
// assertions to SQLiteRecords/sessionStore must not be a backend requirement.
type wrappedRecords struct{ Records }

func TestOIDCLogoutThroughWrappedRecords(t *testing.T) {
	h, browser := loginFixture(t)
	wrapped, err := NewWithRecords(wrappedRecords{h.records}, Options{PublicURL: publicURL, AuthenticateType: "oidc", Lifetime: time.Hour, IdleTimeout: 30 * time.Minute, ReceiptLifetime: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	r := grantRequest(t, wrapped, browser)
	if err := wrapped.sessions.RenewToken(r.Context()); err != nil {
		t.Fatal(err)
	}
	putIdentity(wrapped, r)
	raw, _, err := wrapped.sessions.Commit(r.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result := request(t, wrapped, "POST", "/auth/logout", browser, "request_wrapped_logout"); result.Code != 204 {
		t.Fatal("old source logout failed", result.Code)
	}
	if view(t, request(t, wrapped, "GET", "/api/session", &http.Cookie{Name: CookieName, Value: raw}, "")).Authenticated {
		t.Fatal("successor survived backend revocation")
	}
	if result := request(t, wrapped, "POST", "/auth/logout", browser, "request_wrapped_again_01"); result.Code != 204 {
		t.Fatal("known revoked cookie logout failed", result.Code)
	}
}
