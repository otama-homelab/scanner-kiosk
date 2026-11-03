package oidcprovider_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/oidcattempt"
	"scanner-kiosk/internal/oidcidentity"
	"scanner-kiosk/internal/oidcprovider"
	"scanner-kiosk/internal/storage"
	"scanner-kiosk/internal/websession"
)

const clientID = "fixture-scanner"
const secret = "synthetic-client-secret"
const redirect = "https://scanner.example.test/auth/oidc/callback"

func browser(t *testing.T) (*sql.DB, string, oidcattempt.Material, oidcattempt.Material) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "state.db"), storage.State)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h, err := websession.New(db, websession.Options{PublicURL: "https://scanner.example.test", AuthenticateType: "oidc", Lifetime: time.Hour, IdleTimeout: 30 * time.Minute, ReceiptLifetime: 72 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://scanner.example.test/api/session", nil))
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal("browser bootstrap failed")
	}
	token := w.Result().Cookies()[0].Value
	attempts, err := oidcattempt.New(db, oidcattempt.Options{Lifetime: 5 * time.Minute, MaxPending: 2, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := attempts.Begin(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := attempts.Consume(context.Background(), initial.State(), token)
	if err != nil {
		t.Fatal(err)
	}
	return db, token, initial, consumed
}

type idp struct {
	server       *httptest.Server
	options      oidcprovider.Options
	tokens, jwks atomic.Int32
	ready        chan struct{}
}

func newIDP(t *testing.T, key *rsa.PrivateKey, material oidcattempt.Material, mode, auth string) *idp {
	t.Helper()
	fixture := &idp{ready: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		if mode == "discovery-large" {
			fmt.Fprint(w, strings.Repeat("x", 70000))
			return
		}
		if mode == "discovery-redirect" {
			http.Redirect(w, r, "/forbidden", 302)
			return
		}
		issuer := fixture.server.URL
		if mode == "discovery-issuer" {
			issuer = "https://wrong.example.test"
		}
		tokenURL := fixture.server.URL + "/token"
		if mode == "insecure-token" {
			tokenURL = "http://127.0.0.1/token"
		}
		authURL := fixture.server.URL + "/authorize"
		if mode == "insecure-auth" {
			authURL = "http://127.0.0.1/authorize"
		}
		algs := []string{"RS256"}
		if mode == "unsafe-algorithm" {
			algs = []string{"none", "HS256"}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": authURL, "token_endpoint": tokenURL, "jwks_uri": fixture.server.URL + "/jwks", "id_token_signing_alg_values_supported": algs})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		fixture.jwks.Add(1)
		if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials reached JWKS")
		}
		if mode == "jwks-redirect" {
			http.Redirect(w, r, "/forbidden", 302)
			return
		}
		if mode == "jwks-large" {
			fmt.Fprint(w, strings.Repeat("x", 70000))
			return
		}
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "fixture", Use: "sig", Algorithm: "RS256"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		fixture.tokens.Add(1)
		if r.Method != "POST" {
			t.Error("token request not POST")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("code") != "fixture-code" || r.Form.Get("code_verifier") != material.Verifier() || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != redirect {
			t.Error("code/PKCE/redirect mismatch")
		}
		user, password, basic := r.BasicAuth()
		switch auth {
		case "client_secret_basic":
			if !basic || user != clientID || password != secret || r.Form.Get("client_secret") != "" {
				t.Error("basic client auth mismatch")
			}
		case "client_secret_post":
			if basic || r.Form.Get("client_id") != clientID || r.Form.Get("client_secret") != secret {
				t.Error("post client auth mismatch")
			}
		case "none":
			if basic || r.Form.Get("client_id") != clientID || r.Form.Get("client_secret") != "" {
				t.Error("public client sent secret")
			}
		}
		if mode == "token-stall" {
			close(fixture.ready)
			<-r.Context().Done()
			return
		}
		if mode == "token-redirect" {
			http.Redirect(w, r, "/forbidden", 302)
			return
		}
		if mode == "token-error" {
			w.WriteHeader(503)
			fmt.Fprint(w, "synthetic sensitive error")
			return
		}
		if mode == "token-large" {
			fmt.Fprint(w, strings.Repeat("x", 70000))
			return
		}
		claims := map[string]any{"iss": fixture.server.URL, "sub": "User-123", "aud": clientID, "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": material.Nonce(), "groups": []string{"scanner-admins"}, "email": "shared@example.test"}
		signing := key
		switch mode {
		case "issuer":
			claims["iss"] = "https://wrong.example.test"
		case "audience":
			claims["aud"] = "other-client"
		case "expired":
			claims["exp"] = time.Now().Add(-time.Minute).Unix()
		case "future":
			claims["iat"] = time.Now().Add(5 * time.Minute).Unix()
		case "nonce":
			claims["nonce"] = "wrong"
			claims["nonce_supported"] = false
		case "missing-nonce":
			delete(claims, "nonce")
		case "subject":
			claims["sub"] = ""
		case "unicode-subject":
			claims["sub"] = "非ASCII"
		case "azp":
			claims["azp"] = "other-client"
		case "multi-aud":
			claims["aud"] = []string{clientID, "other-client"}
		case "valid-multi-aud":
			claims["aud"] = []string{clientID, "other-client"}
			claims["azp"] = clientID
		case "access-hash":
			claims["at_hash"] = "wrong"
		case "group-string":
			claims["groups"] = "scanner-admins"
		case "group-other":
			claims["groups"] = []string{"other-admins"}
		case "signature":
			var err error
			signing, err = rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signing}, (&jose.SignerOptions{}).WithHeader("kid", "fixture"))
		if err != nil {
			t.Error(err)
			return
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			t.Error(err)
			return
		}
		object, err := signer.Sign(payload)
		if err != nil {
			t.Error(err)
			return
		}
		raw, err := object.CompactSerialize()
		if err != nil {
			t.Error(err)
			return
		}
		response := map[string]any{"access_token": "synthetic-access", "token_type": "Bearer", "id_token": raw}
		if mode == "missing-token" {
			delete(response, "id_token")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	mux.HandleFunc("/forbidden", func(w http.ResponseWriter, r *http.Request) { t.Error("OIDC followed a redirect") })
	fixture.server = httptest.NewTLSServer(mux)
	t.Cleanup(fixture.server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(fixture.server.Certificate())
	fixture.options = oidcprovider.Options{Issuer: fixture.server.URL, ClientID: clientID, ClientAuth: auth, ClientSecret: config.Secret(secret), RedirectURL: redirect, Scopes: []string{"openid", "profile"}, AdminGroups: []string{"scanner-admins"}, Timeout: 10 * time.Second, MaxResponseBytes: 65536, Roots: roots}
	if auth == "none" {
		fixture.options.ClientSecret = ""
	}
	return fixture
}
func key(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func TestProviderTLSExchangeAndIdentityBinding(t *testing.T) {
	signing := key(t)
	for _, auth := range []string{"client_secret_basic", "client_secret_post", "none"} {
		t.Run(auth, func(t *testing.T) {
			db, token, initial, material := browser(t)
			fixture := newIDP(t, signing, material, "valid", auth)
			if auth == "none" {
				fixture.options.ClientSecret = config.Secret(secret)
			}
			provider, err := oidcprovider.New(context.Background(), fixture.options)
			if err != nil {
				t.Fatal(err)
			}
			target, err := provider.AuthorizationURL(initial)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(target)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			if q.Get("state") != initial.State() || q.Get("nonce") != initial.Nonce() || q.Get("code_challenge") != initial.Challenge() || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != redirect || strings.Contains(target, secret) {
				t.Fatal("authorization URL lost binding or exposed secret")
			}
			if _, err = provider.Exchange(context.Background(), "fixture-code", initial); err == nil || fixture.tokens.Load() != 0 {
				t.Fatal("unconsumed state allowed token exchange")
			}
			verified, err := provider.Exchange(context.Background(), "fixture-code", material)
			if err != nil || verified.Issuer() != fixture.server.URL || verified.Subject() != "User-123" || !verified.CanAdminister() {
				t.Fatal("verified identity mismatch", err)
			}
			mapping, err := oidcidentity.New(db)
			if err != nil {
				t.Fatal(err)
			}
			principal, err := mapping.Resolve(context.Background(), verified, token)
			if err != nil || principal.ID == "" || !principal.CanAdminister {
				t.Fatal("identity mapping failed", err)
			}
			again, err := mapping.Resolve(context.Background(), verified, token)
			if err != nil || again.ID != principal.ID {
				t.Fatal("repeat login remapped identity", err)
			}
			if fixture.tokens.Load() != 1 || fixture.jwks.Load() != 1 {
				t.Fatal("token exchange retried or JWKS not checked")
			}
			if _, err = db.Exec("UPDATE principals SET disabled_at_ms=? WHERE id=?", time.Now().UnixMilli(), principal.ID); err != nil {
				t.Fatal(err)
			}
			if result, err := mapping.Resolve(context.Background(), verified, token); err == nil || result.ID != "" {
				t.Fatal("disabled identity reactivated")
			}
		})
	}
}
func TestProviderRejectsInvalidTokensAndResponses(t *testing.T) {
	signing := key(t)
	for _, mode := range []string{"signature", "issuer", "audience", "expired", "future", "nonce", "missing-nonce", "subject", "unicode-subject", "azp", "multi-aud", "access-hash", "missing-token", "token-redirect", "token-error", "token-large", "jwks-redirect", "jwks-large", "valid-multi-aud", "group-string", "group-other"} {
		t.Run(mode, func(t *testing.T) {
			_, _, _, material := browser(t)
			fixture := newIDP(t, signing, material, mode, "client_secret_basic")
			provider, err := oidcprovider.New(context.Background(), fixture.options)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := provider.Exchange(context.Background(), "fixture-code", material)
			valid := mode == "valid-multi-aud" || mode == "group-string" || mode == "group-other"
			if valid {
				if err != nil || identity.Subject() == "" {
					t.Fatal("valid ordinary identity rejected", err)
				}
				if mode != "valid-multi-aud" && identity.CanAdminister() {
					t.Fatal("malformed/other group granted admin")
				}
			} else if err == nil || identity.Issuer() != "" || identity.Subject() != "" {
				t.Fatal("invalid provider response granted identity", mode, err)
			}
			if fixture.tokens.Load() != 1 {
				t.Fatal("token endpoint retried")
			}
			if err != nil && (strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "synthetic sensitive")) {
				t.Fatal("protocol error disclosed secrets")
			}
		})
	}
}
func TestProviderRejectsUnsafeDiscovery(t *testing.T) {
	signing := key(t)
	for _, mode := range []string{"discovery-issuer", "insecure-token", "insecure-auth", "unsafe-algorithm", "untrusted-tls", "discovery-large", "discovery-redirect"} {
		t.Run(mode, func(t *testing.T) {
			_, _, _, material := browser(t)
			fixture := newIDP(t, signing, material, mode, "client_secret_basic")
			if mode == "untrusted-tls" {
				fixture.options.Roots = nil
			}
			if provider, err := oidcprovider.New(context.Background(), fixture.options); err == nil || provider != nil {
				t.Fatal("unsafe discovery accepted")
			}
			if fixture.tokens.Load() != 0 {
				t.Fatal("discovery sent token credentials")
			}
		})
	}
}

func TestIdentityIssuerSeparationAndPostExchangeRevocation(t *testing.T) {
	db, browserToken, _, material := browser(t)
	signing := key(t)
	mapping, err := oidcidentity.New(db)
	if err != nil {
		t.Fatal(err)
	}
	var identities []oidcprovider.Verified
	var ids []string
	for range 2 {
		fixture := newIDP(t, signing, material, "valid", "client_secret_basic")
		provider, err := oidcprovider.New(context.Background(), fixture.options)
		if err != nil {
			t.Fatal(err)
		}
		identity, err := provider.Exchange(context.Background(), "fixture-code", material)
		if err != nil {
			t.Fatal(err)
		}
		principal, err := mapping.Resolve(context.Background(), identity, browserToken)
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, identity)
		ids = append(ids, principal.ID)
	}
	if ids[0] == ids[1] {
		t.Fatal("same subject/email from different issuers merged")
	}
	hash := sha256.Sum256([]byte(browserToken))
	token := base64.RawURLEncoding.EncodeToString(hash[:])
	now := time.Now()
	if _, err = db.Exec("INSERT INTO session_revocations(token,revoked_at_ms,retain_until_ms) VALUES(?,?,?)", token, now.UnixMilli(), now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if result, err := mapping.Resolve(context.Background(), identities[0], browserToken); err == nil || result.ID != "" {
		t.Fatal("browser revoked during exchange still granted identity")
	}
}
func TestConcurrentIdentityMappingCreatesOnePrincipal(t *testing.T) {
	db, browserToken, _, material := browser(t)
	fixture := newIDP(t, key(t), material, "valid", "client_secret_basic")
	provider, err := oidcprovider.New(context.Background(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.Exchange(context.Background(), "fixture-code", material)
	if err != nil {
		t.Fatal(err)
	}
	var path string
	rows, err := db.Query("PRAGMA database_list")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var seq int
		var name, file string
		if err = rows.Scan(&seq, &name, &file); err != nil {
			t.Fatal(err)
		}
		if name == "main" {
			path = file
		}
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := storage.Open(path, storage.State)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	first, err := oidcidentity.New(db)
	if err != nil {
		t.Fatal(err)
	}
	second, err := oidcidentity.New(other)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		principal oidcidentity.Principal
		err       error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, mapping := range []*oidcidentity.Store{first, second} {
		wg.Add(1)
		go func(store *oidcidentity.Store) {
			defer wg.Done()
			principal, err := store.Resolve(context.Background(), identity, browserToken)
			results <- result{principal, err}
		}(mapping)
	}
	wg.Wait()
	close(results)
	id := ""
	for result := range results {
		if result.err != nil || result.principal.ID == "" {
			t.Fatal("mapping failed", result.err)
		}
		if id != "" && id != result.principal.ID {
			t.Fatal("concurrent login duplicated identity")
		}
		id = result.principal.ID
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM oidc_identities").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate identity records", err)
	}
	if result, err := first.Resolve(context.Background(), oidcprovider.Verified{}, browserToken); err == nil || result.ID != "" {
		t.Fatal("unverified zero identity accepted")
	}
}

func TestProviderCancellationDuringTokenExchange(t *testing.T) {
	_, _, _, material := browser(t)
	fixture := newIDP(t, key(t), material, "token-stall", "client_secret_basic")
	provider, err := oidcprovider.New(context.Background(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		identity, err := provider.Exchange(ctx, "fixture-code", material)
		if identity.Subject() != "" {
			done <- fmt.Errorf("cancelled exchange returned identity")
			return
		}
		done <- err
	}()
	select {
	case <-fixture.ready:
	case err := <-done:
		t.Fatalf("token exchange ended before readiness: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("token endpoint never became ready")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("token exchange did not cancel")
	}
	if fixture.tokens.Load() != 1 || fixture.jwks.Load() != 0 {
		t.Fatal("cancelled exchange retried or verified unrelated token")
	}
}
