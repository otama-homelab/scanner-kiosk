package websession

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const oidcPrincipal = "principal_oidc_0001"

func loginFixture(t *testing.T) (*Handler, *http.Cookie) {
	t.Helper()
	_, db, _ := fixture(t)
	o := options()
	o.AuthenticateType = "oidc"
	h, err := New(db, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO principals(id,kind,created_at_ms) VALUES(?,'oidc',?)", oidcPrincipal, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return h, cookie(t, request(t, h, "GET", "/api/session", nil, ""))
}
func grantRequest(t *testing.T, h *Handler, browser *http.Cookie) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", publicURL+"/auth/oidc/callback", nil)
	r.AddCookie(browser)
	loaded, err := h.load(r)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := opaque("")
	if err != nil {
		t.Fatal(err)
	}
	grant := &loginGrant{source: hashedToken(browser.Value), nonce: nonce, principal: oidcPrincipal, kind: "oidc"}
	return loaded.WithContext(context.WithValue(loaded.Context(), grantKey{}, grant))
}
func putIdentity(h *Handler, r *http.Request) {
	h.sessions.Clear(r.Context())
	h.sessions.Put(r.Context(), "principal", oidcPrincipal)
	h.sessions.Put(r.Context(), "can_administer", true)
}
func TestLogoutCancelsGrantBeforeSessionCommit(t *testing.T) {
	h, browser := loginFixture(t)
	r := grantRequest(t, h, browser)
	if err := h.sessions.RenewToken(r.Context()); err != nil {
		t.Fatal(err)
	}
	putIdentity(h, r)
	out := request(t, h, "POST", "/auth/logout", browser, "request_logout_001")
	if out.Code != 204 {
		t.Fatal(out.Code, out.Body.String())
	}
	w := httptest.NewRecorder()
	h.redirect(w, r, "/")
	if w.Code != 401 || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" || scalar(t, testSQLiteDB(h), "SELECT count(*) FROM sessions") != 0 {
		t.Fatal("cancelled grant issued session", w.Code, w.Body.String())
	}
	if scalar(t, testSQLiteDB(h), "SELECT count(*) FROM session_login_grants WHERE cancelled_at_ms IS NOT NULL") != 1 {
		t.Fatal("grant cancellation missing")
	}
}
func TestOldCookieLogoutRevokesCommittedLoginSuccessor(t *testing.T) {
	h, browser := loginFixture(t)
	r := grantRequest(t, h, browser)
	if err := h.sessions.RenewToken(r.Context()); err != nil {
		t.Fatal(err)
	}
	putIdentity(h, r)
	raw, _, err := h.sessions.Commit(r.Context())
	if err != nil {
		t.Fatal(err)
	}
	if out := request(t, h, "POST", "/auth/logout", browser, "request_logout_001"); out.Code != 204 {
		t.Fatal(out.Code)
	}
	result := request(t, h, "GET", "/api/session", &http.Cookie{Name: CookieName, Value: raw}, "")
	if view(t, result).Authenticated || scalar(t, testSQLiteDB(h), "SELECT count(*) FROM sessions") != 0 {
		t.Fatal("late login Cookie resurrected session")
	}
	if scalar(t, testSQLiteDB(h), "SELECT count(*) FROM principals WHERE disabled_at_ms IS NOT NULL") != 0 {
		t.Fatal("browser logout disabled owner")
	}
}
func TestDisabledPrincipalCannotCommitLoginGrant(t *testing.T) {
	h, browser := loginFixture(t)
	r := grantRequest(t, h, browser)
	if err := h.sessions.RenewToken(r.Context()); err != nil {
		t.Fatal(err)
	}
	putIdentity(h, r)
	if _, err := testSQLiteDB(h).Exec("UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), oidcPrincipal); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.redirect(w, r, "/")
	if w.Code != 401 || len(w.Result().Cookies()) != 0 || scalar(t, testSQLiteDB(h), "SELECT count(*) FROM sessions") != 0 {
		t.Fatal("disabled identity committed Cookie")
	}
}
func TestOneSourceCookieCanGrantOnlyOneLogin(t *testing.T) {
	h, browser := loginFixture(t)
	requests := []*http.Request{grantRequest(t, h, browser), grantRequest(t, h, browser)}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, r := range requests {
		wg.Add(1)
		go func(r *http.Request) { defer wg.Done(); results <- h.sessions.RenewToken(r.Context()) }(r)
	}
	wg.Wait()
	close(results)
	successes, failures := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 || scalar(t, testSQLiteDB(h), "SELECT count(*) FROM session_login_grants") != 1 {
		t.Fatal("one source granted concurrent logins")
	}
}

func TestOIDCExplicitLogoutClearsUnknownCookieAfterRecovery(t *testing.T) {
	h, browser := loginFixture(t)
	if _, err := testSQLiteDB(h).Exec("DELETE FROM sessions; DELETE FROM session_revocations"); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "GET", "/api/session", browser, "")
	if view(t, w).Authenticated || len(w.Result().Cookies()) != 0 {
		t.Fatal("unknown Cookie recovered authority")
	}
	w = request(t, h, "POST", "/auth/logout", browser, "request_reset_00001")
	if w.Code != 204 || cookie(t, w).MaxAge != -1 {
		t.Fatal("explicit reset did not clear unknown Cookie", w.Code)
	}
	if scalar(t, testSQLiteDB(h), "SELECT count(*) FROM principals") != 1 {
		t.Fatal("reset created principal")
	}
}

func testSQLiteDB(h *Handler) *sql.DB { return h.records.(*SQLiteRecords).db }
