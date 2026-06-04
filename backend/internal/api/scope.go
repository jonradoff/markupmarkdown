package api

import (
	"net/http"

	"markupmarkdown/internal/models"
)

// effectiveScope returns the privilege level of the request.
//
//   - Cookie session  → admin (the human owns the account)
//   - API token       → the token's stored scope (defaults to write for
//                       pre-scope tokens; see userFromToken)
//   - Unauthenticated → "" (callers should have rejected before this point)
//
// This is the single source of truth for "what can this request do?". REST
// handlers call enforceScope; the MCP server reads it off authIdentity.
func effectiveScope(r *http.Request) models.TokenScope {
	info, ok := tokenInfoFromRequest(r)
	if ok {
		return info.Scope
	}
	// No bearer token in the chain → must be a cookie session, which is
	// the human owner with full privileges.
	return models.TokenScopeAdmin
}

// enforceScope writes 403 and returns false if the caller's scope is below
// the required level. Use at the top of any write/admin handler that's
// callable via the token-auth surface.
//
// IMPORTANT: this calls currentUser first so that tokenInfo is stashed on
// the context BEFORE effectiveScope reads it. Without that, a public-doc
// path that hasn't yet authenticated would treat a Bearer-token request as
// a cookie session and silently grant admin. That bug is what this call
// closes.
func (a *API) enforceScope(w http.ResponseWriter, r *http.Request, need models.TokenScope) bool {
	_ = a.currentUser(r) // side effect: stashes tokenInfo for Bearer requests
	have := effectiveScope(r)
	if have == "" {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return false
	}
	if !have.AllowsScope(need) {
		writeError(w, http.StatusForbidden, "this token's scope ("+string(have)+") cannot perform "+string(need)+" actions")
		return false
	}
	return true
}
