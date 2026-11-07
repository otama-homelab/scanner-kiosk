package websession

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/buildinfo"
	"scanner-kiosk/internal/simpleauth"
)

const simplePassword = "Synthetic browser passphrase 2026!"
const nextPassword = "Another browser passphrase 2027!"

type passwordFunc func(context.Context, string, string) (authport.PasswordResult, error)

func (f passwordFunc) Authenticate(ctx context.Context, name, password string) (authport.PasswordResult, error) {
	return f(ctx, name, password)
}

func simpleFixture(t *testing.T) (*Handler, *simpleauth.Service, *SQLiteRecords) {
	t.Helper()
	_, db, _ := fixture(t)
	credentials, err := simpleauth.NewSQLiteRecords(db, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	password, err := simpleauth.New(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := password.Create(context.Background(), "alice", simplePassword); err != nil {
		t.Fatal(err)
	}
	if _, err := password.Create(context.Background(), "bob", simplePassword); err != nil {
		t.Fatal(err)
	}
	options := options()
	options.AuthenticateType = "simple"
	options.Password = password
	options.LoginBurst = 20
	options.LoginRequestsPerMinute = 600
	backend, err := NewSQLiteRecords(db)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewWithRecords(backend, options)
	if err != nil {
		t.Fatal(err)
	}
	return h, password, backend
}

func simpleRequest(t *testing.T, h *Handler, browser *http.Cookie, name, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": name, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", publicURL+"/auth/simple/login", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", publicURL)
	r.Header.Set("Idempotency-Key", "request_simple_login_01")
	if browser != nil {
		r.AddCookie(browser)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func simpleLogin(t *testing.T, h *Handler, name string) *http.Cookie {
	t.Helper()
	browser := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	result := simpleRequest(t, h, browser, name, simplePassword)
	if value := view(t, result); !value.Authenticated || value.PrincipalKind != "simple" || value.AuthenticateType != "simple" || value.CanAdminister || value.Version != buildinfo.Version {
		t.Fatal("wrong simple session", value)
	}
	current := cookie(t, result)
	if current.Value == browser.Value {
		t.Fatal("login did not rotate cookie")
	}
	if view(t, request(t, h, "GET", "/api/session", browser, "")).Authenticated {
		t.Fatal("source cookie retained login")
	}
	return current
}

func TestSimpleHTTPLoginIsolationResetAndLogout(t *testing.T) {
	h, password, backend := simpleFixture(t)
	alice := simpleLogin(t, h, "Alice")
	secondAlice := simpleLogin(t, h, "alice")
	bob := simpleLogin(t, h, "bob")
	var authority context.Context
	var principal Principal
	protected := h.RequirePrincipal(func(w http.ResponseWriter, r *http.Request, p Principal) {
		authority = r.Context()
		principal = p
		w.WriteHeader(204)
	})
	if result := request(t, protected, "GET", "/api/documents", alice, ""); result.Code != 204 {
		t.Fatal(result.Code)
	}
	firstID := principal.ID
	if err := h.CheckAuthorization(authority); err != nil {
		t.Fatal(err)
	}
	if result := request(t, h, "POST", "/auth/logout", alice, "request_simple_logout"); result.Code != 204 {
		t.Fatal(result.Code)
	}
	if err := h.CheckAuthorization(authority); !errors.Is(err, ErrSession) {
		t.Fatal("logout preserved stream access", err)
	}
	if !view(t, request(t, h, "GET", "/api/session", secondAlice, "")).Authenticated {
		t.Fatal("logout disabled account/other browser")
	}
	if result := request(t, protected, "GET", "/api/documents", bob, ""); result.Code != 204 || principal.ID == firstID {
		t.Fatal("different names shared owner")
	}
	if _, err := password.SetPassword(context.Background(), "alice", nextPassword); err != nil {
		t.Fatal(err)
	}
	if view(t, request(t, h, "GET", "/api/session", secondAlice, "")).Authenticated {
		t.Fatal("reset cookie remained authenticated")
	}
	if result := request(t, protected, "GET", "/api/documents", secondAlice, ""); result.Code != 401 {
		t.Fatal("reset cookie regained access", result.Code)
	}
	if !view(t, request(t, h, "GET", "/api/session", bob, "")).Authenticated {
		t.Fatal("reset affected another user")
	}
	if err := (&sessionStore{records: backend, retention: time.Hour}).CommitCtx(context.Background(), hashedToken(secondAlice.Value), []byte("stale"), time.Now().Add(time.Hour)); err == nil {
		t.Fatal("stale session refreshed after reset")
	}
	if result := request(t, h, "POST", "/auth/logout", secondAlice, "request_reset_logout"); result.Code != 204 {
		t.Fatal("reset cookie could not sign out", result.Code)
	}
}

type loginHookRecords struct {
	Records
	before    func(LoginGrantRecord)
	uncertain bool
}

func (h loginHookRecords) CommitLogin(ctx context.Context, record SessionData, grant LoginGrantRecord) error {
	if h.before != nil {
		h.before(grant)
	}
	err := h.Records.CommitLogin(ctx, record, grant)
	if err == nil && h.uncertain {
		return authport.ErrUnavailable
	}
	return err
}

func TestSimpleHTTPRejectsCredentialOrLogoutRacesAtSessionCommit(t *testing.T) {
	for _, mode := range []string{"reset-before-rotation", "reset-before-commit", "disable-before-commit", "logout-before-commit", "uncertain-commit"} {
		t.Run(mode, func(t *testing.T) {
			h, password, backend := simpleFixture(t)
			ctx := context.Background()
			if mode == "reset-before-rotation" {
				h.password = passwordFunc(func(ctx context.Context, name, secret string) (authport.PasswordResult, error) {
					result, err := password.Authenticate(ctx, name, secret)
					if err != nil {
						return result, err
					}
					_, err = password.SetPassword(ctx, name, nextPassword)
					return result, err
				})
			} else {
				wrapped := loginHookRecords{Records: backend, uncertain: mode == "uncertain-commit"}
				wrapped.before = func(grant LoginGrantRecord) {
					switch mode {
					case "reset-before-commit":
						if _, err := password.SetPassword(ctx, "alice", nextPassword); err != nil {
							t.Fatal(err)
						}
					case "disable-before-commit":
						if _, err := backend.db.Exec("UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), grant.PrincipalID); err != nil {
							t.Fatal(err)
						}
					case "logout-before-commit":
						if err := backend.RevokeSession(ctx, grant.SourceHash, time.Hour); err != nil {
							t.Fatal(err)
						}
					}
				}
				h.records = wrapped
				h.sessions.Store = &sessionStore{records: wrapped, retention: time.Hour}
			}
			browser := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
			result := simpleRequest(t, h, browser, "alice", simplePassword)
			status := 401
			if mode == "uncertain-commit" {
				status = 503
			}
			if result.Code != status || len(result.Result().Cookies()) != 0 || strings.Contains(result.Body.String(), simplePassword) {
				t.Fatal("failed grant issued success/cookie", mode, result.Code, result.Body.String())
			}
			if mode == "uncertain-commit" {
				if scalar(t, backend.db, "SELECT count(*) FROM simple_session_credentials") != 1 {
					t.Fatal("fixture did not commit login")
				}
				if result := request(t, h, "POST", "/auth/logout", browser, "request_uncertain_logout"); result.Code != 204 {
					t.Fatal("source could not revoke hidden successor", result.Code)
				}
			}
			if scalar(t, backend.db, "SELECT count(*) FROM simple_session_credentials") != 0 {
				t.Fatal("rejected/revoked login kept authority")
			}
		})
	}
}

func TestSimpleHTTPStrictInputModeAndAttemptLimits(t *testing.T) {
	h, _, _ := simpleFixture(t)
	browser := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	for _, body := range []string{`{}`, `[]`, `{"username":"alice","password":null}`, `{"Username":"alice","password":"secret"}`, `{"username":"alice","username":"bob","password":"secret"}`, `{"username":"alice","password":"secret","admin":true}`, `{"username":"alice","password":"secret"} {}`, `{"username":"alice","password":"` + strings.Repeat("x", 1025) + `"}`, strings.Repeat(" ", 8193), "\xff"} {
		r := httptest.NewRequest("POST", publicURL+"/auth/simple/login", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", publicURL)
		r.Header.Set("Idempotency-Key", "request_simple_strict")
		r.AddCookie(browser)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || len(w.Result().Cookies()) != 0 {
			t.Fatal("invalid credential command accepted", w.Code)
		}
	}
	for _, name := range []string{"alice", "missing"} {
		result := simpleRequest(t, h, browser, name, "incorrect password")
		if result.Code != 401 || len(result.Result().Cookies()) != 0 {
			t.Fatal("wrong credentials accepted", name, result.Code)
		}
	}
	if result := request(t, h, "POST", "/auth/guest", browser, "request_guest_forbidden"); result.Code != 403 {
		t.Fatal("simple mode enabled guest")
	}
	if result := request(t, h, "POST", "/auth/oidc/start", browser, "request_oidc_forbidden"); result.Code != 403 {
		t.Fatal("simple mode enabled OIDC")
	}
	h.loginLimit = &loginLimiter{tokens: 1, burst: 1, rate: 1.0 / 60, last: time.Now()}
	if result := simpleRequest(t, h, browser, "alice", "incorrect password"); result.Code != 401 {
		t.Fatal(result.Code)
	}
	if result := simpleRequest(t, h, browser, "missing", "incorrect password"); result.Code != 429 || result.Header().Get("Retry-After") == "" || len(result.Result().Cookies()) != 0 {
		t.Fatal("global login rate bypassed", result.Code)
	}
	guest, _, _ := fixture(t)
	if result := simpleRequest(t, guest, nil, "alice", simplePassword); result.Code != 403 {
		t.Fatal("guest mode enabled password login", result.Code)
	}
}
