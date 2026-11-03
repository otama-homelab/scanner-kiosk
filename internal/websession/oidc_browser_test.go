package websession_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
	"scanner-kiosk/internal/oidcattempt"
	"scanner-kiosk/internal/oidcflow"
	"scanner-kiosk/internal/oidcidentity"
	"scanner-kiosk/internal/oidcprovider"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
)

type codeData struct{ nonce, challenge string }
type oidcFixture struct {
	db       *sql.DB
	app, idp *httptest.Server
	client   *http.Client
	tokens   atomic.Int32
	now      atomic.Int64
}

func newOIDC(t *testing.T) *oidcFixture {
	t.Helper()
	f := &oidcFixture{}
	db, err := storage.Open(filepath.Join(t.TempDir(), "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	f.db = db
	t.Cleanup(func() { db.Close() })
	var handler http.Handler
	f.app = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(f.app.Close)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	codes := map[string]codeData{}
	next := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"issuer": f.idp.URL, "authorization_endpoint": f.idp.URL + "/authorize", "token_endpoint": f.idp.URL + "/token", "jwks_uri": f.idp.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != "scanner" || q.Get("redirect_uri") != f.app.URL+"/auth/oidc/callback" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 {
			t.Error("invalid authorization request")
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		next++
		code := fmt.Sprintf("fixture-code-%d", next)
		codes[code] = codeData{q.Get("nonce"), q.Get("code_challenge")}
		mu.Unlock()
		target := f.app.URL + "/auth/oidc/callback?" + url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {f.idp.URL}}.Encode()
		w.Header().Set("Location", target)
		w.WriteHeader(303)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "fixture", Use: "sig", Algorithm: "RS256"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokens.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		mu.Lock()
		code, ok := codes[r.Form.Get("code")]
		if ok {
			delete(codes, r.Form.Get("code"))
		}
		mu.Unlock()
		if !ok || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != code.challenge || r.Form.Get("client_id") != "scanner" || r.Form.Get("redirect_uri") != f.app.URL+"/auth/oidc/callback" || r.Form.Get("client_secret") != "" {
			w.WriteHeader(400)
			return
		}
		claims := map[string]any{"iss": f.idp.URL, "sub": "scanner-user", "aud": "scanner", "nonce": code.nonce, "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "groups": []string{"scanner-admins"}}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "fixture"))
		if err != nil {
			t.Error(err)
			return
		}
		data, err := json.Marshal(claims)
		if err != nil {
			t.Error(err)
			return
		}
		object, err := signer.Sign(data)
		if err != nil {
			t.Error(err)
			return
		}
		raw, err := object.CompactSerialize()
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-access", "token_type": "Bearer", "id_token": raw})
	})
	f.idp = httptest.NewTLSServer(mux)
	t.Cleanup(f.idp.Close)
	roots := x509.NewCertPool()
	roots.AddCert(f.idp.Certificate())
	provider, err := oidcprovider.New(context.Background(), oidcprovider.Options{Issuer: f.idp.URL, ClientID: "scanner", ClientAuth: "none", RedirectURL: f.app.URL + "/auth/oidc/callback", Scopes: []string{"openid", "profile"}, AdminGroups: []string{"scanner-admins"}, Timeout: 10 * time.Second, MaxResponseBytes: 65536, Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	f.now.Store(time.Now().UnixMilli())
	attempts, err := oidcattempt.New(db, oidcattempt.Options{Lifetime: 5 * time.Minute, MaxPending: 4, Now: func() time.Time { return time.UnixMilli(f.now.Load()) }})
	if err != nil {
		t.Fatal(err)
	}
	identities, err := oidcidentity.New(db)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := oidcflow.New(attempts, provider, identities)
	if err != nil {
		t.Fatal(err)
	}
	handler, err = websession.New(db, websession.Options{PublicURL: f.app.URL, AuthenticateType: "oidc", Lifetime: time.Hour, IdleTimeout: 30 * time.Minute, ReceiptLifetime: 72 * time.Hour, OIDC: flow})
	if err != nil {
		t.Fatal(err)
	}
	f.client = f.browser(t)
	return f
}
func (f *oidcFixture) browser(t *testing.T) *http.Client {
	t.Helper()
	client := *f.app.Client()
	client.Jar, _ = cookiejar.New(nil)
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
func send(t *testing.T, client *http.Client, request *http.Request) *http.Response {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
func (f *oidcFixture) get(t *testing.T, client *http.Client, path string) *http.Response {
	t.Helper()
	r, err := http.NewRequest("GET", f.app.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return send(t, client, r)
}
func (f *oidcFixture) post(t *testing.T, client *http.Client, path, key string) *http.Response {
	t.Helper()
	r, err := http.NewRequest("POST", f.app.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", f.app.URL)
	r.Header.Set("Idempotency-Key", key)
	return send(t, client, r)
}
func closeBody(r *http.Response) { io.Copy(io.Discard, r.Body); r.Body.Close() }
func requireStatus(t *testing.T, r *http.Response, status int) {
	t.Helper()
	if r.StatusCode != status {
		data, _ := io.ReadAll(r.Body)
		r.Body.Close()
		t.Fatalf("expected %d got %d: %s", status, r.StatusCode, data)
	}
}
func (f *oidcFixture) start(t *testing.T, client *http.Client, key string) string {
	t.Helper()
	r := f.post(t, client, "/auth/oidc/start", key)
	requireStatus(t, r, 200)
	var body struct {
		URL string `json:"authorization_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if r.Header.Get("Location") != "" {
		t.Fatal("fetch start unexpectedly redirects")
	}
	location := body.URL
	closeBody(r)
	if !strings.HasPrefix(location, f.idp.URL+"/authorize?") {
		t.Fatal("unregistered authorization target")
	}
	return location
}
func (f *oidcFixture) authorize(t *testing.T, target string) string {
	t.Helper()
	client := f.idp.Client()
	client.Timeout = 10 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, err := http.NewRequest("GET", target, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := send(t, client, r)
	requireStatus(t, response, 303)
	location := response.Header.Get("Location")
	closeBody(response)
	return location
}
func (f *oidcFixture) callback(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()
	r, err := http.NewRequest("GET", target, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	return send(t, client, r)
}
func TestOIDCBrowserTLSLoginReplayAndLogout(t *testing.T) {
	f := newOIDC(t)
	r := f.get(t, f.client, "/api/session")
	requireStatus(t, r, 200)
	closeBody(r)
	appURL, _ := url.Parse(f.app.URL)
	before := f.client.Jar.Cookies(appURL)[0].Value
	target := f.start(t, f.client, "request_oidc_00001")
	if retry := f.start(t, f.client, "request_oidc_00001"); retry != target {
		t.Fatal("start retry changed state/nonce/PKCE")
	}
	callback := f.authorize(t, target)
	r = f.callback(t, f.client, callback)
	requireStatus(t, r, 303)
	if r.Header.Get("Location") != "/" || r.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("unsafe callback redirect")
	}
	closeBody(r)
	after := f.client.Jar.Cookies(appURL)[0].Value
	if after == before {
		t.Fatal("login did not renew Cookie")
	}
	r = f.get(t, f.client, "/api/session")
	requireStatus(t, r, 200)
	var current websession.View
	if err := json.NewDecoder(r.Body).Decode(&current); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if !current.Authenticated || current.PrincipalKind != "oidc" || !current.CanAdminister {
		t.Fatal("login not reflected in session")
	}
	r = f.callback(t, f.client, callback)
	if r.StatusCode < 400 {
		t.Fatal("callback replay accepted")
	}
	closeBody(r)
	if f.tokens.Load() != 1 {
		t.Fatal("callback replay reached token exchange")
	}
	r = f.post(t, f.client, "/auth/logout", "request_logout_0001")
	requireStatus(t, r, 204)
	closeBody(r)
	r = f.get(t, f.client, "/api/session")
	requireStatus(t, r, 200)
	if err := json.NewDecoder(r.Body).Decode(&current); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if current.Authenticated {
		t.Fatal("logout retained identity")
	}
	var disabled int
	if err := f.db.QueryRow("SELECT count(*) FROM principals WHERE kind='oidc' AND disabled_at_ms IS NOT NULL").Scan(&disabled); err != nil || disabled != 0 {
		t.Fatal("OIDC logout disabled owner/jobs", err)
	}
}
func TestOIDCBrowserWrongBindingAndProviderError(t *testing.T) {
	f := newOIDC(t)
	r := f.get(t, f.client, "/api/session")
	closeBody(r)
	target := f.start(t, f.client, "request_oidc_00001")
	callback := f.authorize(t, target)
	other := f.browser(t)
	r = f.get(t, other, "/api/session")
	closeBody(r)
	r = f.callback(t, other, callback)
	requireStatus(t, r, 401)
	closeBody(r)
	if f.tokens.Load() != 0 {
		t.Fatal("foreign browser reached token exchange")
	}
	u, _ := url.Parse(target)
	state := u.Query().Get("state")
	r = f.callback(t, f.client, f.app.URL+"/auth/oidc/callback?"+url.Values{"state": {state}, "error": {"access_denied"}, "error_description": {"sensitive provider text"}, "error_uri": {"https://evil.example.test/"}}.Encode())
	requireStatus(t, r, 303)
	if r.Header.Get("Location") != "/?auth=cancelled" {
		t.Fatal("provider error reflected into redirect")
	}
	closeBody(r)
	r = f.callback(t, f.client, callback)
	requireStatus(t, r, 401)
	closeBody(r)
	if f.tokens.Load() != 0 {
		t.Fatal("cancelled state reached token exchange")
	}
}
func TestOIDCStartExpiryAndCallbackInput(t *testing.T) {
	f := newOIDC(t)
	r := f.get(t, f.client, "/api/session")
	closeBody(r)
	target := f.start(t, f.client, "request_oidc_00001")
	u, _ := url.Parse(target)
	state := u.Query().Get("state")
	for _, query := range []string{"state=" + state + "&state=" + state + "&code=x", "state=" + state + "&code=x&error=access_denied", "state=" + state + "&code=x&iss=https%3A%2F%2Fwrong.example.test", "state=" + state + "&error=unknown_error"} {
		r = f.callback(t, f.client, f.app.URL+"/auth/oidc/callback?"+query)
		requireStatus(t, r, 400)
		closeBody(r)
	}
	if f.tokens.Load() != 0 {
		t.Fatal("invalid callback reached provider")
	}
	f.now.Add((6 * time.Minute).Milliseconds())
	r = f.post(t, f.client, "/auth/oidc/start", "request_oidc_00001")
	requireStatus(t, r, 410)
	closeBody(r)
	var count int
	if err := f.db.QueryRow("SELECT count(*) FROM auth_attempts").Scan(&count); err != nil || count != 1 {
		t.Fatal("expired start was recreated", err)
	}
}
