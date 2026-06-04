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
func (a *API) enforceScope(w http.ResponseWriter, r *http.Request, need models.TokenScope) bool {
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
