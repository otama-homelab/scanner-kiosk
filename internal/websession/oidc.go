package websession

import (
	"net/http"
	"net/url"
	"scanner-kiosk/internal/authport"
	"strings"
	"time"
)

func (h *Handler) oidcAvailable(w http.ResponseWriter) bool {
	if h.mode != "oidc" {
		h.write(w, response{403, map[string]string{"code": "oidc_disabled", "message": "OIDC access disabled"}})
		return false
	}
	if h.oidc == nil {
		h.failure(w, authport.ErrUnavailable)
		return false
	}
	return true
}
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if !h.persist(w, r) {
		return
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusSeeOther)
}
func (h *Handler) oidcStart(w http.ResponseWriter, r *http.Request) {
	if !h.oidcAvailable(w) {
		return
	}
	loaded, err := h.mutation(w, r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	if h.sessions.Token(r.Context()) == "" {
		h.failure(w, ErrSession)
		return
	}
	principal, _, err := h.principal(r.Context())
	if err != nil {
		h.failure(w, err)
		return
	}
	if principal != "" {
		h.failure(w, ErrConflict)
		return
	}
	target, err := h.oidc.Start(r.Context(), h.sessions.Token(r.Context()), r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.failure(w, err)
		return
	}
	h.finish(w, r, response{200, map[string]string{"authorization_url": target}})
}
func (h *Handler) oidcCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !h.oidcAvailable(w) {
		return
	}
	if len(r.URL.RawQuery) > 8192 {
		h.failure(w, ErrInvalidRequest)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		h.failure(w, ErrInvalidRequest)
		return
	}
	for _, name := range []string{"state", "code", "error", "iss"} {
		if len(query[name]) > 1 {
			h.failure(w, ErrInvalidRequest)
			return
		}
	}
	state, code, providerError := query.Get("state"), query.Get("code"), query.Get("error")
	if len(state) != 43 || !validID(state) || (code == "") == (providerError == "") || len(code) > 4096 || strings.ContainsAny(code, "\r\n\x00") {
		h.failure(w, ErrInvalidRequest)
		return
	}
	if issuer := query.Get("iss"); issuer != "" && issuer != h.oidc.Issuer() {
		h.failure(w, ErrInvalidRequest)
		return
	}
	if providerError != "" {
		switch providerError {
		case "access_denied", "login_required", "interaction_required", "temporarily_unavailable", "server_error", "invalid_request", "unauthorized_client", "unsupported_response_type", "invalid_scope", "consent_required", "account_selection_required":
		default:
			h.failure(w, ErrInvalidRequest)
			return
		}
	}
	loaded, err := h.load(r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	token := h.sessions.Token(r.Context())
	if token == "" {
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
	if providerError != "" {
		if err = h.oidc.Reject(r.Context(), token, state); err != nil {
			h.failure(w, err)
			return
		}
		h.redirect(w, r, "/?auth=cancelled")
		return
	}
	principal, err := h.oidc.Finish(r.Context(), token, state, code)
	if err != nil {
		h.failure(w, err)
		return
	}
	r, err = h.establishLogin(r, principal.ID, "oidc", 0, principal.CanAdminister)
	if err != nil {
		h.failure(w, err)
		return
	}
	h.redirect(w, r, "/")
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if h.mode != "oidc" && h.mode != "simple" {
		h.write(w, response{403, map[string]string{"code": "logout_disabled", "message": "Account sign-out disabled"}})
		return
	}
	loaded, err := h.mutation(w, r)
	if err != nil {
		h.failure(w, err)
		return
	}
	r = loaded
	// The old source Cookie remains a revocation capability for its one login
	// successor, including when logout races a callback's final session commit.
	if h.sessions.Token(r.Context()) == "" {
		cookie, err := r.Cookie(CookieName)
		if err != nil {
			h.failure(w, ErrSession)
			return
		}
		known, err := h.records.KnownSession(r.Context(), hashedToken(cookie.Value))
		if err != nil {
			h.failure(w, err)
			return
		}
		if known {
			if err = (&sessionStore{records: h.records, retention: h.sessions.Lifetime + 5*time.Minute}).DeleteCtx(r.Context(), hashedToken(cookie.Value)); err != nil {
				h.failure(w, err)
				return
			}
		}
		// Unknown/expired Cookies after recovery have no server-side authority, but
		// explicit same-origin logout must still let the browser clear them.
		h.sessions.WriteSessionCookie(r.Context(), w, "", time.Time{})
		h.write(w, response{204, nil})
		return
	}
	if err = h.sessions.Destroy(r.Context()); err != nil {
		h.failure(w, err)
		return
	}
	h.finish(w, r, response{204, nil})
}
