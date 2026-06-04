package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"

	"markupmarkdown/internal/models"
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
	writeJSON(w, http.StatusOK, comments)
}

func (a *API) createComment(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, docID); accErr != nil {
		a.writeAccessError(w, r, accErr)
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
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	if len(req.Body) > maxCommentBodyLen {
		writeError(w, http.StatusBadRequest, "comment body too long")
		return
	}
	if req.Anchor.End <= req.Anchor.Start {
		writeError(w, http.StatusBadRequest, "invalid anchor range")
		return
	}
	if strings.TrimSpace(req.Anchor.Exact) == "" {
		writeError(w, http.StatusBadRequest, "anchor.exact is required")
		return
	}
	if len(req.Anchor.Exact) > maxAnchorExactLen {
		writeError(w, http.StatusBadRequest, "anchor.exact too long")
		return
	}

	now := time.Now().UTC()
	c := &models.Comment{
		ID:         uuid.NewString(),
		DocumentID: docID,
		Anchor:     req.Anchor,
		Author:     a.resolveAuthor(r, req.Author),
		Body:       strings.TrimSpace(req.Body),
		Resolved:   false,
		Replies:    []models.Reply{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if u := a.currentUser(r); u != nil {
		c.AuthorID = u.ID
		c.AuthorAvatarURL = u.AvatarURL
	}
	if err := a.store.InsertComment(r.Context(), c); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.hub.Broadcast(docID, "comments-updated")
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) patchComment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, _, accErr := a.checkCommentAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
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
		body := strings.TrimSpace(*req.Body)
		if body == "" {
			writeError(w, http.StatusBadRequest, "body cannot be empty")
			return
		}
		if len(body) > maxCommentBodyLen {
			writeError(w, http.StatusBadRequest, "comment body too long")
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
	writeJSON(w, http.StatusOK, c)
}

func (a *API) deleteComment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	existing, _, accErr := a.checkCommentAccess(r, id)
	if accErr != nil {
		a.writeAccessError(w, r, accErr)
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
	writeJSON(w, http.StatusOK, c)
}

func (a *API) reopenComment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, _, accErr := a.checkCommentAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
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
	writeJSON(w, http.StatusOK, c)
}

func (a *API) createReply(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if _, _, accErr := a.checkCommentAccess(r, id); accErr != nil {
		a.writeAccessError(w, r, accErr)
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
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	if len(body) > maxReplyBodyLen {
		writeError(w, http.StatusBadRequest, "reply body too long")
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
	}
	c, err := a.store.AppendReply(r.Context(), id, reply)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	writeJSON(w, http.StatusCreated, c)
}

func (a *API) updateReply(w http.ResponseWriter, r *http.Request) {
	commentID := mux.Vars(r)["id"]
	replyID := mux.Vars(r)["replyId"]
	if _, _, accErr := a.checkCommentAccess(r, commentID); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	capBody(w, r, maxBodyComment)
	var req createReplyRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	if len(body) > maxReplyBodyLen {
		writeError(w, http.StatusBadRequest, "reply body too long")
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
	writeJSON(w, http.StatusOK, c)
}

func (a *API) deleteReply(w http.ResponseWriter, r *http.Request) {
	commentID := mux.Vars(r)["id"]
	replyID := mux.Vars(r)["replyId"]
	if _, _, accErr := a.checkCommentAccess(r, commentID); accErr != nil {
		a.writeAccessError(w, r, accErr)
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
	writeJSON(w, http.StatusOK, c)
}
