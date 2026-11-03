package websession

import (
	"context"
	"net/http"
)

// Principal is derived exclusively from the authenticated server session and
// enabled principal record. Application services must still check ownership and
// enabled state at their own transaction boundary.
type Principal struct {
	ID, Kind      string
	CanAdminister bool
}
type authorizationKey struct{}
type authorization struct {
	hash      string
	principal Principal
}

// CheckAuthorization revalidates the exact session which admitted a long-lived
// operation, without refreshing it or holding a transaction across network I/O.
func (h *Handler) CheckAuthorization(ctx context.Context) error {
	grant, ok := ctx.Value(authorizationKey{}).(authorization)
	if !ok || grant.hash == "" {
		return ErrSession
	}
	return h.records.AuthorizeSession(ctx, grant.hash, grant.principal.ID, grant.principal.Kind)
}

// RequirePrincipal authenticates an application route without creating a guest
// or anonymous session. It never invokes next if session persistence fails.
// Application mutations enforce their own body, idempotency and revision rules.
func (h *Handler) RequirePrincipal(next func(http.ResponseWriter, *http.Request, Principal)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.guard(w, r, false) {
			return
		}
		loaded, err := h.load(r)
		if err != nil {
			h.failure(w, err)
			return
		}
		r = loaded
		id, kind, err := h.principal(r.Context())
		if err != nil {
			h.failure(w, err)
			return
		}
		if id == "" {
			h.failure(w, ErrSession)
			return
		}
		principal := Principal{ID: id, Kind: kind, CanAdminister: kind == "oidc" && h.sessions.GetBool(r.Context(), "can_administer")}
		if !h.persist(w, r) {
			return
		}
		grant := authorization{hash: hashedToken(h.sessions.Token(r.Context())), principal: principal}
		r = r.WithContext(context.WithValue(r.Context(), authorizationKey{}, grant))
		next(w, r, principal)
	})
}
