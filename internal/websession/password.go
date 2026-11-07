package websession

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/buildinfo"
)

func validLoginKind(kind string, version int64) bool {
	return kind == "oidc" && version == 0 || kind == "simple" && version > 0
}

// establishLogin is shared by identity providers. The database confirms both
// the source session and current principal/credential version before a new
// cookie can be committed. No provider receives or chooses the session token.
func (h *Handler) establishLogin(r *http.Request, id, kind string, version int64, admin bool) (*http.Request, error) {
	if !validID(id) || kind != h.mode || !validLoginKind(kind, version) {
		return nil, ErrSession
	}
	token := h.sessions.Token(r.Context())
	if token == "" {
		return nil, ErrSession
	}
	nonce, err := opaque("")
	if err != nil {
		return nil, err
	}
	grant := &loginGrant{source: hashedToken(token), nonce: nonce, principal: id, kind: kind, credentialVersion: version}
	r = r.WithContext(context.WithValue(r.Context(), grantKey{}, grant))
	if err := h.sessions.RenewToken(r.Context()); err != nil {
		return nil, err
	}
	h.sessions.Clear(r.Context())
	h.sessions.Put(r.Context(), "principal", id)
	h.sessions.Put(r.Context(), "can_administer", kind == "oidc" && admin)
	return r, nil
}

type loginLimiter struct {
	mu                  sync.Mutex
	tokens, rate, burst float64
	last                time.Time
}

func (l *loginLimiter) allow() (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.tokens = min(l.burst, l.tokens+max(0, now.Sub(l.last).Seconds())*l.rate)
	l.last = now
	if l.tokens < 1 {
		return false, max(1, int(math.Ceil((1-l.tokens)/l.rate)))
	}
	l.tokens--
	return true, 0
}

func decodePassword(r *http.Request) (string, string, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.URL.RawQuery != "" || len(r.Header.Values("Content-Type")) != 1 {
		return "", "", ErrInvalidRequest
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 8193))
	if err != nil || len(data) > 8192 || !utf8.Valid(data) {
		return "", "", ErrInvalidRequest
	}
	// Read keys explicitly: unknown/duplicate/case-variant/null fields are not
	// alternative spellings of a credential command.
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", "", ErrInvalidRequest
	}
	values := map[string]string{}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || name != "username" && name != "password" {
			return "", "", ErrInvalidRequest
		}
		if _, seen := values[name]; seen {
			return "", "", ErrInvalidRequest
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || string(raw) == "null" {
			return "", "", ErrInvalidRequest
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return "", "", ErrInvalidRequest
		}
		values[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(values) != 2 {
		return "", "", ErrInvalidRequest
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", "", ErrInvalidRequest
	}
	if len(values["username"]) > 256 || len(values["password"]) > 1024 {
		return "", "", ErrInvalidRequest
	}
	return values["username"], values["password"], nil
}

func (h *Handler) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if h.mode != "simple" {
		h.write(w, response{403, map[string]string{"code": "simple_disabled", "message": "Password sign-in disabled"}})
		return
	}
	if !validID(r.Header.Get("Idempotency-Key")) || len(r.Header.Values("Idempotency-Key")) != 1 {
		h.failure(w, ErrInvalidRequest)
		return
	}
	if allowed, retry := h.loginLimit.allow(); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		h.failure(w, authport.ErrLimit)
		return
	}
	username, password, err := decodePassword(r)
	if err != nil {
		h.failure(w, err)
		return
	}
	loaded, err := h.load(r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	if h.sessions.Token(r.Context()) == "" {
		h.failure(w, ErrSession)
		return
	}
	existing, _, err := h.principal(r.Context())
	if err != nil {
		h.failure(w, err)
		return
	}
	if existing != "" {
		h.failure(w, ErrConflict)
		return
	}
	principal, err := h.password.Authenticate(r.Context(), username, password)
	if err != nil {
		h.failure(w, err)
		return
	}
	r, err = h.establishLogin(r, principal.ID, "simple", principal.CredentialVersion, false)
	if err != nil {
		h.failure(w, err)
		return
	}
	h.finish(w, r, response{200, View{Version: buildinfo.Version, Authenticated: true, PrincipalKind: "simple", AuthenticateType: h.mode}})
}
