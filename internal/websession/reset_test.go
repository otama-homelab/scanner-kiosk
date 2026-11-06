package websession

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaleBootstrapRequestsCookieResetWithoutReplacingIt(t *testing.T) {
	h, _, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	stale := request(t, h, "GET", "/api/session", anonymous, "")
	var body struct {
		ResetRequired bool `json:"reset_required"`
	}
	if err := json.Unmarshal(stale.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if stale.Code != 200 || !body.ResetRequired || len(stale.Result().Cookies()) != 0 {
		t.Fatal("stale bootstrap must request recovery without overwriting a newer cookie", stale.Body.String())
	}
	if !view(t, request(t, h, "GET", "/api/session", guest, "")).Authenticated {
		t.Fatal("successor session lost authority")
	}
}

func TestAnonymousResetKeepsOriginBodyKeyAndRateGuards(t *testing.T) {
	h, _, _ := fixture(t)
	for _, test := range []struct {
		name, origin, key, body string
		status                  int
	}{
		{"foreign origin", "https://other.test", "request_reset_0001", "", 403},
		{"missing origin", "", "request_reset_0001", "", 403},
		{"missing key", publicURL, "", "", 400},
		{"unexpected body", publicURL, "request_reset_0001", "x", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", publicURL+"/auth/session/reset", strings.NewReader(test.body))
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Idempotency-Key", test.key)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.status || len(w.Result().Cookies()) != 0 {
				t.Fatal("rejected reset changed a cookie", w.Code)
			}
		})
	}
	for range 5 {
		if w := request(t, h, "POST", "/auth/session/reset", nil, "request_reset_0001"); w.Code != 202 {
			t.Fatal(w.Code)
		}
	}
	w := request(t, h, "POST", "/auth/session/reset", nil, "request_reset_0001")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || len(w.Result().Cookies()) != 0 {
		t.Fatal("reset is not rate limited", w.Code)
	}
}

func TestAnonymousResetClearsOnlyTheCookieWithoutRevokingASuccessor(t *testing.T) {
	h, db, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	for _, stale := range []*http.Cookie{anonymous, {Name: CookieName, Value: strings.Repeat("a", 43)}} {
		clear := request(t, h, "POST", "/auth/session/reset", stale, "request_reset_0001")
		if clear.Code != 202 || cookie(t, clear).MaxAge != -1 {
			t.Fatal("stale cookie was not cleared", clear.Code, clear.Body.String())
		}
	}
	if !view(t, request(t, h, "GET", "/api/session", guest, "")).Authenticated {
		t.Fatal("automatic reset must not revoke the old cookie's login successor")
	}
	valid := request(t, h, "POST", "/auth/session/reset", guest, "request_reset_0002")
	if valid.Code != 409 || len(valid.Result().Cookies()) != 0 {
		t.Fatal("a valid login must not be cleared", valid.Code)
	}
	if scalar(t, db, "SELECT count(*) FROM principal_cleanup_requests") != 0 {
		t.Fatal("cookie recovery scheduled document cleanup")
	}
	db.Close()
	failed := request(t, h, "POST", "/auth/session/reset", guest, "request_reset_0003")
	if failed.Code != 503 || len(failed.Result().Cookies()) != 0 {
		t.Fatal("storage failure must not clear the cookie", failed.Code)
	}
}
