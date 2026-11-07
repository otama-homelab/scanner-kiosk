// Package websession supplies HTTP session bootstrap and explicit guest lifecycle.
// OIDC authentication, document endpoints and serving the app remain separate.
package websession

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/alexedwards/scs/v2"
	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/buildinfo"
)

var (
	ErrInvalidRequest = errors.New("invalid session request")
	ErrSession        = errors.New("session unavailable")
	ErrConflict       = errors.New("session operation conflicts")
	ErrGone           = errors.New("session operation expired")
)

const CookieName = "__Host-scanner"

type Options struct {
	PublicURL, AuthenticateType            string
	OIDC                                   authport.OIDC
	Password                               authport.Password
	LoginRequestsPerMinute, LoginBurst     int64
	Lifetime, IdleTimeout, ReceiptLifetime time.Duration
}
type Handler struct {
	records         Records
	sessions        *scs.SessionManager
	origin, mode    string
	receiptLifetime time.Duration
	routes          http.Handler
	oidc            authport.OIDC
	password        authport.Password
	loginLimit      *loginLimiter
}
type View struct {
	Version          string `json:"version"`
	Authenticated    bool   `json:"authenticated"`
	PrincipalKind    string `json:"principal_kind"`
	AuthenticateType string `json:"authenticate_type"`
	CanAdminister    bool   `json:"can_administer"`
	ResetRequired    bool   `json:"reset_required,omitempty"`
}

type response struct {
	status int
	body   any
}

func NewWithRecords(records Records, options Options) (*Handler, error) {
	u, err := url.Parse(options.PublicURL)
	if records == nil || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (options.AuthenticateType != "guest" && options.AuthenticateType != "oidc" && options.AuthenticateType != "simple" && options.AuthenticateType != "none") || (options.AuthenticateType == "simple" && options.Password == nil) || options.Lifetime < time.Second || options.Lifetime > 365*24*time.Hour || options.IdleTimeout < time.Second || options.IdleTimeout > options.Lifetime || options.ReceiptLifetime < time.Second || options.ReceiptLifetime > 365*24*time.Hour {
		return nil, ErrSession
	}
	if options.LoginRequestsPerMinute == 0 {
		options.LoginRequestsPerMinute = 10
	}
	if options.LoginBurst == 0 {
		options.LoginBurst = 5
	}
	if options.LoginRequestsPerMinute < 1 || options.LoginRequestsPerMinute > 600 || options.LoginBurst < 1 || options.LoginBurst > 20 {
		return nil, ErrSession
	}
	h := &Handler{records: records, origin: u.Scheme + "://" + u.Host, mode: options.AuthenticateType, oidc: options.OIDC, password: options.Password, loginLimit: &loginLimiter{tokens: float64(options.LoginBurst), burst: float64(options.LoginBurst), rate: float64(options.LoginRequestsPerMinute) / 60, last: time.Now()}, receiptLifetime: options.ReceiptLifetime}
	manager := scs.New()
	manager.Store = &sessionStore{records: records, retention: options.Lifetime + 5*time.Minute}
	manager.HashTokenInStore = true
	manager.Lifetime = options.Lifetime
	manager.IdleTimeout = options.IdleTimeout
	manager.Cookie.Name = CookieName
	manager.Cookie.Path = "/"
	manager.Cookie.Domain = ""
	manager.Cookie.Secure = true
	manager.Cookie.HttpOnly = true
	manager.Cookie.SameSite = http.SameSiteLaxMode
	h.sessions = manager
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session", h.bootstrap)
	mux.HandleFunc("POST /auth/guest", h.guest)
	mux.HandleFunc("POST /auth/guest/end", h.endGuest)
	mux.HandleFunc("POST /auth/oidc/start", h.oidcStart)
	mux.HandleFunc("POST /auth/simple/login", h.passwordLogin)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("POST /auth/session/reset", h.resetAnonymous)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.write(w, response{404, map[string]string{"code": "not_found", "message": "Not found"}})
	})
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.write(w, response{403, map[string]string{"code": "origin_denied", "message": "Origin denied"}})
	}))
	h.routes = protection.Handler(mux)
	return h, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.guard(w, r, true) {
		return
	}
	h.routes.ServeHTTP(w, r)
}

// Auth JSON responses are committed before writing any response bytes. Session
// errors cannot append a success payload or issue a successful login cookie.
func (h *Handler) load(r *http.Request) (*http.Request, error) {
	token := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == CookieName {
			count++
			if count > 1 {
				return nil, ErrSession
			}
			token = cookie.Value
		}
	}
	if token != "" && !validID(token) {
		return nil, ErrSession
	}
	ctx, err := h.sessions.Load(r.Context(), token)
	if err != nil {
		return nil, err
	}
	return r.WithContext(ctx), nil
}
func (h *Handler) finish(w http.ResponseWriter, r *http.Request, result response) {
	if h.persist(w, r) {
		h.write(w, result)
	}
}
func (h *Handler) persist(w http.ResponseWriter, r *http.Request) bool {
	switch h.sessions.Status(r.Context()) {
	case scs.Modified:
		token, expiry, err := h.sessions.Commit(r.Context())
		if err != nil {
			h.failure(w, err)
			return false
		}
		h.sessions.WriteSessionCookie(r.Context(), w, token, expiry)
	case scs.Destroyed:
		h.sessions.WriteSessionCookie(r.Context(), w, "", time.Time{})
	}
	// SCS adds no-cache="Set-Cookie". Our API policy is the stricter no-store;
	// keep one directive rather than duplicate headers with ambiguous consumers.
	w.Header().Set("Cache-Control", "no-store")
	return true
}
func (h *Handler) write(w http.ResponseWriter, result response) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(result.status)
	if result.body != nil {
		_ = json.NewEncoder(w).Encode(result.body)
	}
}
func (h *Handler) failure(w http.ResponseWriter, err error) {
	status, code, message := 503, "session_unavailable", "Session unavailable"
	switch {
	case errors.Is(err, ErrInvalidRequest):
		status, code, message = 400, "invalid_request", "Invalid request"
	case errors.Is(err, ErrSession):
		status, code, message = 401, "session_required", "Session required"
	case errors.Is(err, authport.ErrDenied):
		status, code, message = 401, "authentication_rejected", "Authentication rejected"
	case errors.Is(err, authport.ErrUnavailable):
		status, code, message = 503, "authentication_unavailable", "Sign-in unavailable"
	case errors.Is(err, ErrConflict) || errors.Is(err, authport.ErrConflict):
		status, code, message = 409, "conflict", "Operation conflicts with current session"
	case errors.Is(err, ErrGone) || errors.Is(err, authport.ErrGone):
		status, code, message = 410, "gone", "Operation expired"
	case errors.Is(err, authport.ErrLimit):
		if w.Header().Get("Retry-After") == "" {
			w.Header().Set("Retry-After", "1")
		}
		status, code, message = 429, "limit_exceeded", "Too many sign-in attempts"
	}
	h.write(w, response{status, map[string]string{"code": code, "message": message}})
}
func (h *Handler) principal(ctx context.Context) (string, string, error) {
	principal := h.sessions.GetString(ctx, "principal")
	if principal == "" {
		return "", "none", nil
	}
	kind, err := h.records.PrincipalKind(ctx, principal)
	if errors.Is(err, ErrSession) || err == nil && kind != h.mode && !(h.mode == "none" && kind == "guest") {
		return "", "none", ErrSession
	}
	if err != nil {
		return "", "none", err
	}
	if kind == "simple" {
		if err := h.records.AuthorizeSession(ctx, hashedToken(h.sessions.Token(ctx)), principal, kind); err != nil {
			return "", "none", err
		}
	}
	return principal, kind, nil
}
func (h *Handler) bootstrap(w http.ResponseWriter, r *http.Request) {
	loaded, err := h.load(r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	// A late request with a rotated/expired cookie must not overwrite a newer
	// browser session with a newly issued anonymous cookie.
	if _, cookieErr := r.Cookie(CookieName); cookieErr == nil && h.sessions.Token(r.Context()) == "" {
		h.write(w, response{200, View{Version: buildinfo.Version, PrincipalKind: "none", AuthenticateType: h.mode, ResetRequired: true}})
		return
	}
	principal, kind, err := h.principal(r.Context())
	if errors.Is(err, ErrSession) {
		h.write(w, response{200, View{Version: buildinfo.Version, PrincipalKind: "none", AuthenticateType: h.mode, ResetRequired: true}})
		return
	} else if err != nil {
		h.failure(w, err)
		return
	}
	if principal == "" && h.sessions.Status(r.Context()) != scs.Destroyed && h.sessions.GetString(r.Context(), "browser_binding") == "" {
		binding, err := opaque("browser_")
		if err != nil {
			h.failure(w, err)
			return
		}
		h.sessions.Put(r.Context(), "browser_binding", binding)
	}
	h.finish(w, r, response{200, View{Version: buildinfo.Version, Authenticated: principal != "", PrincipalKind: kind, AuthenticateType: h.mode, CanAdminister: kind == "oidc" && h.sessions.GetBool(r.Context(), "can_administer")}})
}
func (h *Handler) mutation(w http.ResponseWriter, r *http.Request) (*http.Request, error) {
	if !validID(r.Header.Get("Idempotency-Key")) || len(r.Header.Values("Idempotency-Key")) != 1 {
		return nil, ErrInvalidRequest
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(data) != 0 {
		return nil, ErrInvalidRequest
	}
	return h.load(r)
}

// Unlike logout, stale-cookie recovery must not revoke a newer login successor.
func (h *Handler) resetAnonymous(w http.ResponseWriter, r *http.Request) {
	loaded, err := h.mutation(w, r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	principal, _, err := h.principal(r.Context())
	if err != nil && !errors.Is(err, ErrSession) {
		h.failure(w, err)
		return
	}
	if err == nil && principal != "" {
		h.failure(w, ErrConflict)
		return
	}
	if allowed, retry := h.loginLimit.allow(); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		h.failure(w, authport.ErrLimit)
		return
	}
	h.sessions.WriteSessionCookie(r.Context(), w, "", time.Time{})
	h.write(w, response{202, nil})
}

func (h *Handler) guest(w http.ResponseWriter, r *http.Request) {
	if h.mode != "guest" && h.mode != "none" {
		h.write(w, response{403, map[string]string{"code": "guest_disabled", "message": "Guest access disabled"}})
		return
	}
	loaded, err := h.mutation(w, r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	binding := h.sessions.GetString(r.Context(), "browser_binding")
	if binding == "" {
		h.failure(w, ErrSession)
		return
	}
	principal, err := h.createGuest(r.Context(), binding, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.failure(w, err)
		return
	}
	existing := h.sessions.GetString(r.Context(), "principal")
	if existing != "" && existing != principal {
		h.failure(w, ErrConflict)
		return
	}
	if existing == "" {
		if err = h.sessions.RenewToken(r.Context()); err != nil {
			h.failure(w, err)
			return
		}
		h.sessions.Put(r.Context(), "principal", principal)
	}
	h.finish(w, r, response{200, View{Version: buildinfo.Version, Authenticated: true, PrincipalKind: "guest", AuthenticateType: h.mode}})
}
func (h *Handler) endGuest(w http.ResponseWriter, r *http.Request) {
	if h.mode != "guest" && h.mode != "none" {
		h.write(w, response{403, map[string]string{"code": "guest_disabled", "message": "Guest access disabled"}})
		return
	}
	loaded, err := h.mutation(w, r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	principal, kind, err := h.principal(r.Context())
	if errors.Is(err, ErrSession) || err == nil && principal == "" {
		if _, cookieErr := r.Cookie(CookieName); cookieErr != nil {
			h.failure(w, ErrSession)
			return
		}
		if h.sessions.Token(r.Context()) != "" {
			if destroyErr := h.sessions.Destroy(r.Context()); destroyErr != nil {
				h.failure(w, destroyErr)
				return
			}
			h.finish(w, r, response{202, nil})
		} else {
			h.sessions.WriteSessionCookie(r.Context(), w, "", time.Time{})
			h.write(w, response{202, nil})
		}
		return
	}
	if err != nil {
		h.failure(w, err)
		return
	}
	if kind != "guest" {
		h.failure(w, ErrSession)
		return
	}
	if err = h.disableGuest(r.Context(), principal); err != nil {
		h.failure(w, err)
		return
	}
	if err = h.sessions.Destroy(r.Context()); err != nil {
		h.failure(w, err)
		return
	}
	h.finish(w, r, response{202, nil})
}

// guard centralizes public-origin and response protections for auth and application routes.
func (h *Handler) guard(w http.ResponseWriter, r *http.Request, callback bool) bool {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Vary", "Cookie")
	w.Header().Set("Content-Type", "application/json")
	origin, err := url.Parse(h.origin)
	if err != nil || r.Host != origin.Host {
		h.write(w, response{403, map[string]string{"code": "origin_denied", "message": "Origin denied"}})
		return false
	}
	// Only the fixed query callback accepts an IdP's cross-site navigation.
	if callback && r.Method == "GET" && r.URL.Path == "/auth/oidc/callback" {
		h.oidcCallback(w, r)
		return false
	}
	if r.Header.Get("Origin") != "" && r.Header.Get("Origin") != h.origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		h.write(w, response{403, map[string]string{"code": "origin_denied", "message": "Origin denied"}})
		return false
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != h.origin {
			h.write(w, response{403, map[string]string{"code": "origin_denied", "message": "Origin denied"}})
			return false
		}
	}
	return true
}
