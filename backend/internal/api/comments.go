package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/render"
)

type createCommentRequest struct {
	Anchor models.Anchor `json:"anchor"`
	Body   string        `json:"body"`
	Author string        `json:"author"`
}

type patchCommentRequest struct {
	Body *string `json:"body,omitempty"`
}

type createReplyRequest struct {
	Body   string `json:"body"`
	Author string `json:"author"`
}

type resolveRequest struct {
	Author string `json:"author"`
}

const anonymous = "Anonymous"

// actorKindFor reads the auth source from the request. Any Bearer-token
// request is treated as agent-authored; cookie sessions are human.
func actorKindFor(r *http.Request) models.ActorKind {
	if _, ok := tokenInfoFromRequest(r); ok {
		return models.ActorAgent
	}
	return models.ActorHuman
}

// stampAgentWrite records the IDs we'll need to resolve display fields at
// read time. The Author field gets a snapshot (current token label) as a
// fallback for old clients and for raw Mongo readers — but the canonical
// rendering goes through resolveAgentIdentities at read time so renames
// flow through everywhere immediately.
func stampAgentWrite(c *models.Comment, tokenID, label string) {
	c.TokenID = tokenID
	c.Author = label
	c.AuthorAvatarURL = ""
}
func stampAgentWriteReply(r *models.Reply, tokenID, label string) {
	r.TokenID = tokenID
	r.Author = label
	r.AuthorAvatarURL = ""
}

// requireMineComment writes 403 and returns false unless the calling viewer
// is the human behind the comment — either the direct human author or the
// owner of the bot/token that wrote it. AuthorID on agent comments points
// at the token's owning user, so equality covers both.
//
// Used by edit/delete handlers. Doc access has already been checked; this
// is the per-row ownership layer on top.
func (a *API) requireMineComment(w http.ResponseWriter, r *http.Request, c *models.Comment) bool {
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	vid := a.viewerID(r)
	if vid == "" || vid != c.AuthorID {
		writeError(w, http.StatusForbidden, "you can only edit or delete comments you (or a bot you own) created")
		return false
	}
	return true
}

// requireMineReply mirrors requireMineComment for a reply nested in a parent
// comment.
func (a *API) requireMineReply(w http.ResponseWriter, r *http.Request, parent *models.Comment, replyID string) bool {
	if parent == nil {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	vid := a.viewerID(r)
	if vid == "" {
		writeError(w, http.StatusForbidden, "you can only edit or delete replies you (or a bot you own) created")
		return false
	}
	for i := range parent.Replies {
		if parent.Replies[i].ID == replyID {
			if parent.Replies[i].AuthorID != vid {
				writeError(w, http.StatusForbidden, "you can only edit or delete replies you (or a bot you own) created")
				return false
			}
			return true
		}
	}
	writeError(w, http.StatusNotFound, "reply not found")
	return false
}

// markMine stamps Comment.Mine / Reply.Mine based on viewerID. A comment is
// "mine" when the viewer is the human behind it — author for human-written
// content, token owner for agent content. AuthorID on agent comments points
// at the token's owning user (stamped in mcpapi.go's CreateComment), so the
// same equality check covers both cases.
func markMine(comments []models.Comment, viewerID string) {
	if viewerID == "" {
		return
	}
	for i := range comments {
		comments[i].Mine = comments[i].AuthorID == viewerID
		for j := range comments[i].Replies {
			comments[i].Replies[j].Mine = comments[i].Replies[j].AuthorID == viewerID
		}
	}
}

// viewerID returns the calling user's ID, or "" if anonymous.
func (a *API) viewerID(r *http.Request) string {
	if u := a.currentUser(r); u != nil {
		return u.ID
	}
	return ""
}

// resolveAgentIdentities overlays the display fields on agent-authored
// comments and replies from the current token + user records. Called by
// every read path that returns comments to the client.
func (a *API) resolveAgentIdentities(ctx context.Context, comments []models.Comment) {
	tokenIDs := map[string]struct{}{}
	userIDs := map[string]struct{}{}
	collect := func(actor models.ActorKind, tid, uid string) {
		if actor != models.ActorAgent {
			return
		}
		if tid != "" {
			tokenIDs[tid] = struct{}{}
		}
		if uid != "" {
			userIDs[uid] = struct{}{}
		}
	}
	for i := range comments {
		collect(comments[i].ActorKind, comments[i].TokenID, comments[i].AuthorID)
		for j := range comments[i].Replies {
			collect(comments[i].Replies[j].ActorKind, comments[i].Replies[j].TokenID, comments[i].Replies[j].AuthorID)
		}
	}
	tokens, _ := a.store.GetAPITokensByIDs(ctx, mapKeys(tokenIDs))
	users := map[string]*models.User{}
	for uid := range userIDs {
		if u, _ := a.store.GetUser(ctx, uid); u != nil {
			users[uid] = u
		}
	}

	overlay := func(actor models.ActorKind, tid, uid string, author, ownerName, ownerLogin *string) {
		if actor != models.ActorAgent {
			return
		}
		if tok := tokens[tid]; tok != nil {
			*author = tok.Label
		}
		if u := users[uid]; u != nil {
			*ownerName = preferName(u)
			*ownerLogin = u.Login
		}
	}
	for i := range comments {
		overlay(comments[i].ActorKind, comments[i].TokenID, comments[i].AuthorID,
			&comments[i].Author, &comments[i].OwnerName, &comments[i].OwnerLogin)
		comments[i].AuthorAvatarURL = ""
		for j := range comments[i].Replies {
			overlay(comments[i].Replies[j].ActorKind, comments[i].Replies[j].TokenID, comments[i].Replies[j].AuthorID,
				&comments[i].Replies[j].Author, &comments[i].Replies[j].OwnerName, &comments[i].Replies[j].OwnerLogin)
			comments[i].Replies[j].AuthorAvatarURL = ""
		}
	}
}

// decorate applies the standard read-time enrichment to a single comment
// destined for an HTTP response: agent display fields + Mine ownership flag.
// Use this everywhere a write handler returns a comment object.
func (a *API) decorate(r *http.Request, c *models.Comment) {
	a.resolveAgentIdentity(r.Context(), c)
	if c == nil {
		return
	}
	vid := a.viewerID(r)
	if vid == "" {
		return
	}
	c.Mine = c.AuthorID == vid
	for i := range c.Replies {
		c.Replies[i].Mine = c.Replies[i].AuthorID == vid
	}
}

// resolveAgentIdentity overlays one comment in place (used by paths that
// return a freshly created or updated single comment).
func (a *API) resolveAgentIdentity(ctx context.Context, c *models.Comment) {
	if c == nil {
		return
	}
	a.resolveAgentIdentities(ctx, []models.Comment{*c})
	// resolveAgentIdentities works on a copy of the slice element; redo
	// in-place by re-fetching token + user manually for the one item.
	if c.ActorKind == models.ActorAgent {
		if c.TokenID != "" {
			if tok, _ := a.store.GetAPITokensByIDs(ctx, []string{c.TokenID}); tok[c.TokenID] != nil {
				c.Author = tok[c.TokenID].Label
			}
		}
		if c.AuthorID != "" {
			if u, _ := a.store.GetUser(ctx, c.AuthorID); u != nil {
				c.OwnerName = preferName(u)
				c.OwnerLogin = u.Login
			}
		}
		c.AuthorAvatarURL = ""
	}
	for i := range c.Replies {
		if c.Replies[i].ActorKind != models.ActorAgent {
			continue
		}
		if c.Replies[i].TokenID != "" {
			if tok, _ := a.store.GetAPITokensByIDs(ctx, []string{c.Replies[i].TokenID}); tok[c.Replies[i].TokenID] != nil {
				c.Replies[i].Author = tok[c.Replies[i].TokenID].Label
			}
		}
		if c.Replies[i].AuthorID != "" {
			if u, _ := a.store.GetUser(ctx, c.Replies[i].AuthorID); u != nil {
				c.Replies[i].OwnerName = preferName(u)
				c.Replies[i].OwnerLogin = u.Login
			}
		}
		c.Replies[i].AuthorAvatarURL = ""
	}
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func preferName(u *models.User) string {
	if u == nil {
		return ""
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Login
}

func authorOr(a string) string {
	a = strings.TrimSpace(a)
	if a == "" {
		return anonymous
	}
	return a
}

// resolveAuthor returns the authoritative author label for a request: the
// authenticated GitHub user's display name takes precedence over any
// client-supplied string.
func (a *API) resolveAuthor(r *http.Request, supplied string) string {
	if u := a.currentUser(r); u != nil {
		if u.Name != "" {
			return u.Name
		}
		return u.Login
	}
	return authorOr(supplied)
}

func (a *API) listComments(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, docID); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	comments, err := a.store.ListComments(r.Context(), docID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if comments == nil {
		comments = []models.Comment{}
	}
	a.resolveAgentIdentities(r.Context(), comments)
	markMine(comments, a.viewerID(r))
	// Opt-in HTML rendering of bodies for agents / integrators that want
	// pre-rendered output. Default is markdown source (machine-readable).
	if r.URL.Query().Get("render") == "html" {
		for i := range comments {
			comments[i].BodyHTML = render.HTMLComment(comments[i].Body)
			for j := range comments[i].Replies {
				comments[i].Replies[j].BodyHTML = render.HTMLComment(comments[i].Replies[j].Body)
			}
		}
	}
	writeJSON(w, http.StatusOK, comments)
}

func (a *API) createComment(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	doc, accErr := a.checkDocAccess(r, docID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.enforceRate(w, r, a.rlComment, "Slow down — too many comments in a short window.") {
		return
	}
	capBody(w, r, maxBodyComment)

	var req createCommentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := ValidateCommentBody(req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Body = body
	if err := ValidateAnchor(req.Anchor); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().UTC()
	c := &models.Comment{
		ID:         uuid.NewString(),
		DocumentID: docID,
		Anchor:     req.Anchor,
		Author:     a.resolveAuthor(r, req.Author),
		Body:       req.Body,
		Resolved:   false,
		Replies:    []models.Reply{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if u := a.currentUser(r); u != nil {
		c.AuthorID = u.ID
		c.AuthorAvatarURL = u.AvatarURL
		c.ActorKind = actorKindFor(r)
		if info, ok := tokenInfoFromRequest(r); ok {
			stampAgentWrite(c, info.TokenID, info.Label)
			a.logTokenAction(r.Context(), info.TokenID, "comment.create", docID)
		}
	}
	if err := a.store.InsertComment(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.hub.Broadcast(docID, "comments-updated")
	a.fanOutCommentNotifications(fanOutInput{
		DocID:    docID,
		DocTitle: doc.Title,
		Body:     c.Body,
		Comment:  c,
		Actor:    a.currentUser(r),
	})
	a.decorate(r, c)
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) patchComment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	existing, _, accErr := a.checkCommentAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.requireMineComment(w, r, existing) {
		return
	}
	capBody(w, r, maxBodyComment)
	var req patchCommentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	set := bson.M{}
	if req.Body != nil {
		body, err := ValidateCommentBody(*req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		set["body"] = body
	}
	if len(set) == 0 {
		writeError(w, http.StatusBadRequest, "no changes")
		return
	}
	c, err := a.store.UpdateComment(r.Context(), id, set)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.decorate(r, c); writeJSON(w, http.StatusOK, c)
}

func (a *API) deleteComment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	existing, _, accErr := a.checkCommentAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	// Author-only delete. A token's scope no longer matters here — even an
	// admin-scope token can't delete a comment authored by a different user
	// (or by a bot owned by a different user). Cookie session + admin scope
	// still satisfy the path when the comment is theirs.
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.requireMineComment(w, r, existing) {
		return
	}
	if err := a.store.DeleteComment(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing != nil {
		a.hub.Broadcast(existing.DocumentID, "comments-updated")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) resolveComment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, _, accErr := a.checkCommentAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	var req resolveRequest
	_ = readJSON(r, &req)
	now := time.Now().UTC()
	c, err := a.store.UpdateComment(r.Context(), id, bson.M{
		"resolved":    true,
		"resolved_by": a.resolveAuthor(r, req.Author),
		"resolved_at": now,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.decorate(r, c); writeJSON(w, http.StatusOK, c)
}

func (a *API) reopenComment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, _, accErr := a.checkCommentAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	c, err := a.store.UpdateComment(r.Context(), id, bson.M{
		"resolved":    false,
		"resolved_by": "",
		"resolved_at": nil,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.decorate(r, c); writeJSON(w, http.StatusOK, c)
}

func (a *API) createReply(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	parentComment, doc, accErr := a.checkCommentAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.enforceRate(w, r, a.rlComment, "Slow down — too many replies in a short window.") {
		return
	}
	capBody(w, r, maxBodyComment)
	var req createReplyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := ValidateReplyBody(req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	reply := models.Reply{
		ID:        uuid.NewString(),
		Author:    a.resolveAuthor(r, req.Author),
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if u := a.currentUser(r); u != nil {
		reply.AuthorID = u.ID
		reply.AuthorAvatarURL = u.AvatarURL
		reply.ActorKind = actorKindFor(r)
		if info, ok := tokenInfoFromRequest(r); ok {
			stampAgentWriteReply(&reply, info.TokenID, info.Label)
			a.logTokenAction(r.Context(), info.TokenID, "reply.create", parentComment.DocumentID)
		}
	}
	c, appendErr := a.store.AppendReply(r.Context(), id, reply)
	if appendErr != nil {
		writeError(w, http.StatusInternalServerError, appendErr.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.fanOutCommentNotifications(fanOutInput{
		DocID:    c.DocumentID,
		DocTitle: doc.Title,
		Body:     body,
		Comment:  c,
		ReplyOf:  parentComment,
		Actor:    a.currentUser(r),
	})
	a.decorate(r, c)
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) updateReply(w http.ResponseWriter, r *http.Request) {
	commentID := mux.Vars(r)["id"]
	replyID := mux.Vars(r)["replyId"]
	parent, _, accErr := a.checkCommentAccess(r, commentID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.requireMineReply(w, r, parent, replyID) {
		return
	}
	capBody(w, r, maxBodyComment)
	var req createReplyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := ValidateReplyBody(req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := a.store.UpdateReply(r.Context(), commentID, replyID, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.decorate(r, c)
	writeJSON(w, http.StatusOK, c)
}

func (a *API) deleteReply(w http.ResponseWriter, r *http.Request) {
	commentID := mux.Vars(r)["id"]
	replyID := mux.Vars(r)["replyId"]
	parent, _, accErr := a.checkCommentAccess(r, commentID)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	if !a.enforceScope(w, r, models.TokenScopeWrite) {
		return
	}
	if !a.requireMineReply(w, r, parent, replyID) {
		return
	}
	c, err := a.store.DeleteReply(r.Context(), commentID, replyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.decorate(r, c); writeJSON(w, http.StatusOK, c)
}
