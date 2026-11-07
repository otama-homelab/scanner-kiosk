package websession

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"scanner-kiosk/internal/buildinfo"
	"scanner-kiosk/internal/documents"
	"strings"
	"sync"
	"testing"
	"time"

	"scanner-kiosk/internal/storage"
)

const publicURL = "https://scanner.example.test"

func TestSessionViewReportsRunningVersion(t *testing.T) {
	h, _, _ := fixture(t)
	check := func(w *httptest.ResponseRecorder) {
		t.Helper()
		view(t, w)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["version"] != buildinfo.Version {
			t.Fatalf("session version %v differs from running version %q", body["version"], buildinfo.Version)
		}
	}
	first := request(t, h, "GET", "/api/session", nil, "")
	check(first)
	check(request(t, h, "GET", "/api/session", &http.Cookie{Name: CookieName, Value: strings.Repeat("a", 43)}, ""))
	guest := request(t, h, "POST", "/auth/guest", cookie(t, first), "request_version_0001")
	check(guest)
	check(request(t, h, "GET", "/api/session", cookie(t, guest), ""))
}

func options() Options {
	return Options{PublicURL: publicURL, AuthenticateType: "guest", Lifetime: time.Hour, IdleTimeout: 30 * time.Minute, ReceiptLifetime: 72 * time.Hour}
}
func fixture(t *testing.T) (*Handler, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.sqlite3")
	db, err := storage.Open(path, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	handler, err := New(db, options())
	if err != nil {
		t.Fatal(err)
	}
	return handler, db, path
}
func request(t *testing.T, h http.Handler, method, path string, cookie *http.Cookie, key string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, publicURL+path, nil)
	if method == "POST" {
		r.Header.Set("Origin", publicURL)
		r.Header.Set("Idempotency-Key", key)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func cookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected session cookie, got %d: %s", len(cookies), w.Body.String())
	}
	c := cookies[0]
	if c.Name != CookieName || !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Domain != "" || c.Path != "/" {
		t.Fatal("unsafe cookie attributes")
	}
	return c
}
func view(t *testing.T, w *httptest.ResponseRecorder) View {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	var result View
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" || len(w.Header().Values("Cache-Control")) != 1 {
		t.Fatal("cacheable session")
	}
	return result
}
func scalar(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var value int
	if err := db.QueryRow(q, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestGuestLifecycleIsolationRenewalAndEnd(t *testing.T) {
	h, db, _ := fixture(t)
	first := request(t, h, "GET", "/api/session", nil, "")
	if result := view(t, first); result.Authenticated || result.PrincipalKind != "none" || result.AuthenticateType != "guest" {
		t.Fatal("bootstrap created guest")
	}
	anonymous := cookie(t, first)
	if scalar(t, db, "SELECT count(*) FROM principals") != 0 {
		t.Fatal("safe GET created principal")
	}
	a := request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001")
	if result := view(t, a); !result.Authenticated || result.PrincipalKind != "guest" || result.CanAdminister {
		t.Fatal("wrong guest view")
	}
	guestA := cookie(t, a)
	if guestA.Value == anonymous.Value {
		t.Fatal("guest login did not rotate token")
	}
	if result := view(t, request(t, h, "GET", "/api/session", anonymous, "")); result.Authenticated {
		t.Fatal("old token still authenticated")
	}
	b := request(t, h, "GET", "/api/session", nil, "")
	guestB := cookie(t, request(t, h, "POST", "/auth/guest", cookie(t, b), "request_guest_0002"))
	if scalar(t, db, "SELECT count(*) FROM principals WHERE kind='guest'") != 2 {
		t.Fatal("browsers shared guest principal")
	}
	retry := request(t, h, "POST", "/auth/guest", guestA, "request_guest_0001")
	if !view(t, retry).Authenticated || scalar(t, db, "SELECT count(*) FROM principals") != 2 {
		t.Fatal("retry created another principal")
	}
	if response := request(t, h, "POST", "/auth/guest", guestA, "request_changed_0001"); response.Code != 409 {
		t.Fatal("changed creation intent accepted", response.Code)
	}
	end := request(t, h, "POST", "/auth/guest/end", guestA, "request_end_000001")
	if end.Code != 202 || cookie(t, end).MaxAge != -1 {
		t.Fatal("guest end not accepted", end.Code)
	}
	if scalar(t, db, "SELECT count(*) FROM principals WHERE disabled_at_ms IS NOT NULL") != 1 || scalar(t, db, "SELECT count(*) FROM principal_cleanup_requests WHERE reason='guest_end'") != 1 {
		t.Fatal("guest end missing disable/cleanup intent")
	}
	if view(t, request(t, h, "GET", "/api/session", guestA, "")).Authenticated {
		t.Fatal("ended guest recovered")
	}
	if !view(t, request(t, h, "GET", "/api/session", guestB, "")).Authenticated {
		t.Fatal("ending guest affected another browser")
	}
	if response := request(t, h, "POST", "/auth/guest", guestA, "request_guest_0001"); response.Code != 401 {
		t.Fatal("stale POST recreated guest", response.Code)
	}
	if scalar(t, db, "SELECT count(*) FROM principals") != 2 {
		t.Fatal("end/replay created new guest")
	}
	digest := sha256.Sum256([]byte(guestA.Value))
	hashed := base64.RawURLEncoding.EncodeToString(digest[:])
	if scalar(t, db, "SELECT count(*) FROM session_revocations WHERE token=?", hashed) != 1 || scalar(t, db, "SELECT count(*) FROM sessions WHERE token=?", guestA.Value) != 0 {
		t.Fatal("raw token stored or revocation absent")
	}
}
func TestSessionPersistsAcrossDatabaseReopen(t *testing.T) {
	h, db, path := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	authenticated := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(path, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	h, err = New(reopened, options())
	if err != nil {
		t.Fatal(err)
	}
	if !view(t, request(t, h, "GET", "/api/session", authenticated, "")).Authenticated {
		t.Fatal("normal restart lost session")
	}
}
func TestSessionOriginModeAndInputRejection(t *testing.T) {
	for _, mode := range []string{"missing-origin", "foreign-origin", "scheme", "fetch-cross-site", "host", "duplicate-origin", "missing-key", "body", "no-bootstrap", "oidc"} {
		t.Run(mode, func(t *testing.T) {
			h, db, _ := fixture(t)
			r := httptest.NewRequest("POST", publicURL+"/auth/guest", nil)
			r.Header.Set("Origin", publicURL)
			r.Header.Set("Idempotency-Key", "request_guest_0001")
			if mode != "no-bootstrap" {
				r.AddCookie(cookie(t, request(t, h, "GET", "/api/session", nil, "")))
			}
			expected := 403
			switch mode {
			case "missing-origin":
				r.Header.Del("Origin")
			case "foreign-origin":
				r.Header.Set("Origin", "https://other.example.test")
			case "scheme":
				r.Header.Set("Origin", "http://scanner.example.test")
			case "fetch-cross-site":
				r.Header.Set("Sec-Fetch-Site", "cross-site")
			case "host":
				r.Host = "other.example.test"
			case "duplicate-origin":
				r.Header.Add("Origin", publicURL)
			case "missing-key":
				r.Header.Del("Idempotency-Key")
				expected = 400
			case "body":
				r.Body = io.NopCloser(strings.NewReader("{}"))
				expected = 400
			case "no-bootstrap":
				expected = 401
			case "oidc":
				o := options()
				o.AuthenticateType = "oidc"
				var err error
				h, err = New(db, o)
				if err != nil {
					t.Fatal(err)
				}
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != expected || scalar(t, db, "SELECT count(*) FROM principals") != 0 || len(w.Result().Cookies()) != 0 {
				t.Fatalf("invalid request mutated auth state: %s %d %s", mode, w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal("non-JSON error", err)
			}
		})
	}
}
func TestConcurrentGuestCreationOneBrowserOnePrincipal(t *testing.T) {
	h, db, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001")
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.Code != 200 && result.Code != 401 {
			t.Fatalf("concurrent create failed unexpectedly: %d %s", result.Code, result.Body.String())
		}
	}
	if scalar(t, db, "SELECT count(*) FROM principals") != 1 || scalar(t, db, "SELECT count(*) FROM guest_starts") != 1 {
		t.Fatal("concurrent creation duplicated guest")
	}
}
func TestRevocationPreventsStaleConcurrentSessionCommit(t *testing.T) {
	h, db, path := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	ctx, err := h.sessions.Load(context.Background(), guest.Value)
	if err != nil {
		t.Fatal(err)
	}
	end := request(t, h, "POST", "/auth/guest/end", guest, "request_end_000001")
	if end.Code != 202 {
		t.Fatal(end.Code)
	}
	h.sessions.Put(ctx, "stale_request", "must not resurrect")
	if _, _, err = h.sessions.Commit(ctx); err == nil {
		t.Fatal("stale concurrent request resurrected revoked token")
	}
	if scalar(t, db, "SELECT count(*) FROM sessions") != 0 {
		t.Fatal("revoked session recreated")
	}
	other, err := storage.Open(path, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	digest := sha256.Sum256([]byte(guest.Value))
	token := base64.RawURLEncoding.EncodeToString(digest[:])
	if _, err = other.Exec("INSERT INTO sessions(token,data,expiry) VALUES(?,x'00',julianday('now','+1 hour'))", token); err == nil {
		t.Fatal("other connection bypassed revocation")
	}
}
func TestDisabledExpiredAndTamperedSessionsDoNotAuthenticate(t *testing.T) {
	for _, mode := range []string{"disabled", "expired", "tampered", "duplicate-cookie"} {
		t.Run(mode, func(t *testing.T) {
			h, db, _ := fixture(t)
			anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
			guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
			switch mode {
			case "disabled":
				if _, err := db.Exec("UPDATE principals SET disabled_at_ms=?", time.Now().UnixMilli()); err != nil {
					t.Fatal(err)
				}
			case "expired":
				if _, err := db.Exec("DROP TRIGGER session_update_not_revoked; UPDATE sessions SET expiry=0"); err != nil {
					t.Fatal(err)
				}
			case "tampered":
				guest.Value = "forged_session_value_0001"
			}
			r := httptest.NewRequest("GET", publicURL+"/api/session", nil)
			r.AddCookie(guest)
			if mode == "duplicate-cookie" {
				r.AddCookie(&http.Cookie{Name: CookieName, Value: ""})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if mode == "duplicate-cookie" {
				if w.Code != 401 {
					t.Fatal("ambiguous cookies accepted")
				}
			} else if view(t, w).Authenticated {
				t.Fatal("unavailable session authenticated")
			}
		})
	}
}

func TestRealTLSGuestHTTPFlow(t *testing.T) {
	_, db, _ := fixture(t)
	var handler http.Handler
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	server.StartTLS()
	defer server.Close()
	o := options()
	o.PublicURL = server.URL
	var err error
	handler, err = New(db, o)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	client.Timeout = 5 * time.Second
	result, err := client.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, result.Body)
	result.Body.Close()
	r, err := http.NewRequest("POST", server.URL+"/auth/guest", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", server.URL)
	r.Header.Set("Idempotency-Key", "request_tls_guest_01")
	result, err = client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	var current View
	if err = json.NewDecoder(result.Body).Decode(&current); err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != 200 || !current.Authenticated || current.PrincipalKind != "guest" {
		t.Fatal("TLS cookie flow did not authenticate", result.StatusCode)
	}
	result, err = client.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if err = json.NewDecoder(result.Body).Decode(&current); err != nil || !current.Authenticated {
		t.Fatal("TLS cookie was not retained", err)
	}
}
func TestCommitFailureDoesNotSendSuccessPayload(t *testing.T) {
	h, _, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	ctx, err := h.sessions.Load(context.Background(), guest.Value)
	if err != nil {
		t.Fatal(err)
	}
	if result := request(t, h, "POST", "/auth/guest/end", guest, "request_end_000001"); result.Code != 202 {
		t.Fatal(result.Code)
	}
	h.sessions.Put(ctx, "stale_request", "must not resurrect")
	r := httptest.NewRequest("GET", publicURL+"/api/session", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.finish(w, r, response{200, View{Authenticated: true, PrincipalKind: "guest", AuthenticateType: "guest"}})
	if w.Code != 503 || len(w.Result().Cookies()) != 0 || strings.Contains(w.Body.String(), "authenticated") {
		t.Fatal("commit failure leaked success response", w.Code, w.Body.String())
	}
}
func TestGuestEndRevokesDocumentAccessAcrossStores(t *testing.T) {
	h, state, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	var principal string
	if err := state.QueryRow("SELECT principal_id FROM guest_starts").Scan(&principal); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "documents.sqlite3"), storage.Documents)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO owners(id) VALUES(?)", principal); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	document := "document_guest_0001"
	if _, err = db.Exec("INSERT INTO documents(id,owner_id,purpose,state,created_at_ms,expires_at_ms) VALUES(?,?,'document','review',?,?)", document, principal, now, now+3600000); err != nil {
		t.Fatal(err)
	}
	records, err := documents.New(state, db, time.Now, func(context.Context) (int64, error) { return 1 << 30, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = records.Get(context.Background(), principal, document); err != nil {
		t.Fatal(err)
	}
	if result := request(t, h, "POST", "/auth/guest/end", guest, "request_end_000001"); result.Code != 202 {
		t.Fatal(result.Code)
	}
	if _, err = records.Get(context.Background(), principal, document); err == nil {
		t.Fatal("ended guest still accessed stored document")
	}
	if scalar(t, db, "SELECT count(*) FROM documents") != 1 {
		t.Fatal("guest end pretended immediate file cleanup")
	}
}

func TestStaleBootstrapDoesNotOverwriteNewSessionCookie(t *testing.T) {
	h, db, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	stale := request(t, h, "GET", "/api/session", anonymous, "")
	if view(t, stale).Authenticated || len(stale.Result().Cookies()) != 0 {
		t.Fatal("stale request replaced current browser cookie")
	}
	if !view(t, request(t, h, "GET", "/api/session", guest, "")).Authenticated {
		t.Fatal("new session lost authority")
	}
	if result := request(t, h, "POST", "/auth/guest/end", guest, "request_end_000001"); result.Code != 202 {
		t.Fatal(result.Code)
	}
	stale = request(t, h, "GET", "/api/session", guest, "")
	if view(t, stale).Authenticated || len(stale.Result().Cookies()) != 0 {
		t.Fatal("ended session was automatically replaced")
	}
	clear := request(t, h, "POST", "/auth/guest/end", guest, "request_end_000001")
	if clear.Code != 202 || cookie(t, clear).MaxAge != -1 {
		t.Fatal("explicit stale-session end did not clear cookie")
	}
	if scalar(t, db, "SELECT count(*) FROM principals") != 1 || scalar(t, db, "SELECT count(*) FROM principal_cleanup_requests") != 1 {
		t.Fatal("repeated end created work or a new guest")
	}
}

func TestOIDCModeNeverAcceptsGuestSession(t *testing.T) {
	h, db, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	configured := options()
	configured.AuthenticateType = "oidc"
	oidc, err := New(db, configured)
	if err != nil {
		t.Fatal(err)
	}
	result := request(t, oidc, "GET", "/api/session", guest, "")
	if current := view(t, result); current.Authenticated || current.PrincipalKind != "none" || current.AuthenticateType != "oidc" || len(result.Result().Cookies()) != 0 {
		t.Fatal("OIDC mode fell back to guest authority")
	}
	if response := request(t, oidc, "POST", "/auth/guest", guest, "request_guest_0001"); response.Code != 403 {
		t.Fatal("OIDC mode enabled guest creation")
	}
}

func TestNoneUsesIsolatedGuestLifecycle(t *testing.T) {
	_, db, _ := fixture(t)
	o := options()
	o.AuthenticateType = "none"
	h, err := New(db, o)
	if err != nil {
		t.Fatal(err)
	}
	a := request(t, h, "GET", "/api/session", nil, "")
	if v := view(t, a); v.Authenticated || v.AuthenticateType != "none" {
		t.Fatal(v)
	}
	first := request(t, h, "POST", "/auth/guest", cookie(t, a), "request_none_first")
	if v := view(t, first); !v.Authenticated || v.PrincipalKind != "guest" || v.AuthenticateType != "none" || v.CanAdminister {
		t.Fatal(v)
	}
	firstCookie := cookie(t, first)
	b := request(t, h, "GET", "/api/session", nil, "")
	second := cookie(t, request(t, h, "POST", "/auth/guest", cookie(t, b), "request_none_second"))
	if scalar(t, db, "SELECT count(*) FROM principals WHERE kind='guest'") != 2 {
		t.Fatal("shared anonymous owner")
	}
	if !view(t, request(t, h, "GET", "/api/session", firstCookie, "")).Authenticated {
		t.Fatal("reload lost session")
	}
	if w := request(t, h, "POST", "/auth/guest/end", firstCookie, "request_none_end"); w.Code != 202 {
		t.Fatal(w.Code)
	}
	if !view(t, request(t, h, "GET", "/api/session", second, "")).Authenticated {
		t.Fatal("end affected other browser")
	}
	if view(t, request(t, h, "GET", "/api/session", firstCookie, "")).Authenticated {
		t.Fatal("ended session recovered")
	}
}
