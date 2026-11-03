package websession

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestApplicationAuthorizationUsesSessionIdentity(t *testing.T) {
	h, db, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	var owner string
	if err := db.QueryRow("SELECT id FROM principals WHERE kind='guest'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	called := 0
	application := h.RequirePrincipal(func(w http.ResponseWriter, r *http.Request, p Principal) {
		called++
		if p.ID != owner || p.Kind != "guest" || p.CanAdminister {
			t.Fatal("wrong application identity", p)
		}
		w.WriteHeader(204)
	})
	for _, c := range []*http.Cookie{nil, anonymous} {
		w := request(t, application, "GET", "/api/documents", c, "")
		if w.Code != 401 || len(w.Result().Cookies()) != 0 || called != 0 {
			t.Fatal("unauthenticated route created identity", w.Code)
		}
	}
	r := httptest.NewRequest("GET", publicURL+"/api/documents?owner=another-principal", nil)
	r.AddCookie(guest)
	r.Header.Set("X-Principal-ID", "another-principal")
	r.Header.Set("X-Forwarded-User", "another-principal")
	w := httptest.NewRecorder()
	application.ServeHTTP(w, r)
	if w.Code != 204 || called != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authenticated route failed", w.Code)
	}
	request(t, h, "POST", "/auth/guest/end", guest, "request_end_00001")
	w = request(t, application, "GET", "/api/documents", guest, "")
	if w.Code != 401 || called != 1 {
		t.Fatal("ended principal reached application", w.Code)
	}
}

func TestApplicationAuthorizationEnforcesOriginAndHost(t *testing.T) {
	h, _, _ := fixture(t)
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	called := 0
	application := h.RequirePrincipal(func(w http.ResponseWriter, r *http.Request, p Principal) { called++; w.WriteHeader(204) })
	for _, test := range []struct{ name, method, host, origin, site string }{
		{"wrong-host", "GET", "attacker.example.test", "", ""},
		{"wrong-origin", "GET", "scanner.example.test", "https://attacker.example.test", ""},
		{"cross-site", "GET", "scanner.example.test", "", "cross-site"},
		{"missing-origin", "POST", "scanner.example.test", "", ""},
		{"wrong-mutation-origin", "POST", "scanner.example.test", "https://attacker.example.test", "same-origin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, publicURL+"/api/documents", nil)
			r.Host = test.host
			r.AddCookie(guest)
			if test.origin != "" {
				r.Header.Set("Origin", test.origin)
			}
			if test.site != "" {
				r.Header.Set("Sec-Fetch-Site", test.site)
			}
			w := httptest.NewRecorder()
			application.ServeHTTP(w, r)
			if w.Code != 403 || called != 0 {
				t.Fatal("untrusted application request accepted", w.Code)
			}
		})
	}
	w := request(t, application, "POST", "/api/documents", guest, "")
	if w.Code != 204 || called != 1 {
		t.Fatal("same-origin mutation rejected", w.Code)
	}
}

func TestAuthorizationRecheckRequiresOriginalActiveSession(t *testing.T) {
	h, db, _ := fixture(t)
	if err := h.CheckAuthorization(context.Background()); !errors.Is(err, ErrSession) {
		t.Fatal("missing authorization context accepted", err)
	}
	anonymous := cookie(t, request(t, h, "GET", "/api/session", nil, ""))
	guest := cookie(t, request(t, h, "POST", "/auth/guest", anonymous, "request_guest_0001"))
	var authority context.Context
	application := h.RequirePrincipal(func(w http.ResponseWriter, r *http.Request, p Principal) {
		authority = r.Context()
		if err := h.CheckAuthorization(authority); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(204)
	})
	if w := request(t, application, "GET", "/api/documents", guest, ""); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := (&sessionStore{records: &SQLiteRecords{db: db}, retention: time.Hour}).DeleteCtx(context.Background(), hashedToken(guest.Value)); err != nil {
		t.Fatal(err)
	}
	if err := h.CheckAuthorization(authority); !errors.Is(err, ErrSession) {
		t.Fatal("revoked session retained read authority", err)
	}
	if scalar(t, db, "SELECT count(*) FROM principals WHERE disabled_at_ms IS NULL") != 1 {
		t.Fatal("session revocation disabled principal")
	}
}
