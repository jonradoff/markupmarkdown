package api

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"markupmarkdown/internal/models"
)

// GitHub login pattern: alphanumeric + hyphens, 1–39 chars, cannot start/end
// with hyphen. We're lenient on those edge rules — the DB lookup is the
// final authority.
var mentionPattern = regexp.MustCompile(`(?:^|[^\w])@([a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38}))`)

// extractMentions returns the unique set of GitHub-style logins referenced
// in `body`, lowercased for case-insensitive matching.
func extractMentions(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mentionPattern.FindAllStringSubmatch(body, -1) {
		login := strings.ToLower(strings.TrimRight(m[1], "-"))
		if login == "" || seen[login] {
			continue
		}
		seen[login] = true
		out = append(out, login)
	}
	return out
}

// previewSnippet returns a short, single-line excerpt of body suitable for
// the notification list.
func previewSnippet(body string) string {
	s := strings.ReplaceAll(body, "\n", " ")
	s = strings.TrimSpace(s)
	const max = 140
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, fetchErrorResponse{
			Error: "Sign in to see notifications.",
			Kind:  "sign_in_required",
		})
		return
	}
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	notes, unread, err := a.store.ListNotificationsForUser(r.Context(), user.ID, limit)
	if err != nil {
		internalError(w, "store.list_notifications", err)
		return
	}
	if notes == nil {
		notes = []models.Notification{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"unread":        unread,
		"notifications": notes,
	})
}

func (a *API) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	if err := a.store.MarkAllNotificationsRead(r.Context(), user.ID); err != nil {
		internalError(w, "store.mark_all_read", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return
	}
	id := mux.Vars(r)["id"]
	if err := a.store.MarkNotificationRead(r.Context(), user.ID, id); err != nil {
		internalError(w, "store.mark_read", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listMentionCandidates returns the union of GitHub-signed-in users who've
// commented or replied on this doc. Powers the autocomplete in the comment
// composer.
func (a *API) listMentionCandidates(w http.ResponseWriter, r *http.Request) {
	docID := mux.Vars(r)["id"]
	if _, accErr := a.checkDocAccess(r, docID); accErr != nil {
		a.writeAccessError(w, r, accErr)
		return
	}
	comments, err := a.store.ListComments(r.Context(), docID)
	if err != nil {
		internalError(w, "store.list_comments_for_mentions", err)
		return
	}
	userIDs := map[string]struct{}{}
	for _, c := range comments {
		if c.AuthorID != "" {
			userIDs[c.AuthorID] = struct{}{}
		}
		for _, rep := range c.Replies {
			if rep.AuthorID != "" {
				userIDs[rep.AuthorID] = struct{}{}
			}
		}
	}
	// Always include the requester so they show up first in the menu.
	if u := a.currentUser(r); u != nil {
		userIDs[u.ID] = struct{}{}
	}
	type candidate struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatarUrl,omitempty"`
	}
	out := make([]candidate, 0, len(userIDs))
	for id := range userIDs {
		u, _ := a.store.GetUser(r.Context(), id)
		if u == nil || u.Login == "" {
			continue
		}
		out = append(out, candidate{Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL})
	}
	writeJSON(w, http.StatusOK, out)
}

// fanOutCommentNotifications generates mention + reply notifications for a
// new comment or reply. Called from createComment / createReply on a
// best-effort background goroutine — never blocks the response.
type fanOutInput struct {
	DocID    string
	DocTitle string
	Body     string
	Comment  *models.Comment
	// If non-nil, the new event is a reply to an existing comment.
	ReplyOf *models.Comment
	Actor   *models.User
}

func (a *API) fanOutCommentNotifications(in fanOutInput) {
	if in.Actor == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		preview := previewSnippet(in.Body)
		threadCommentID := in.Comment.ID
		if in.ReplyOf != nil {
			threadCommentID = in.ReplyOf.ID
		}

		recipients := map[string]models.NotificationKind{} // userID → kind

		// Mentions take priority over generic "reply" notifications.
		logins := extractMentions(in.Body)
		if len(logins) > 0 {
			users, err := a.store.FindUsersByLogins(ctx, logins)
			if err == nil {
				for _, u := range users {
					if u.ID == in.Actor.ID {
						continue // don't notify yourself for self-mentions
					}
					recipients[u.ID] = models.NotifyMention
				}
			}
		}

		// Thread participants get a "reply" notification, but only when this
		// event IS a reply, and only if they aren't already getting a mention.
		if in.ReplyOf != nil {
			participants := map[string]bool{}
			if in.ReplyOf.AuthorID != "" {
				participants[in.ReplyOf.AuthorID] = true
			}
			for _, r := range in.ReplyOf.Replies {
				if r.AuthorID != "" {
					participants[r.AuthorID] = true
				}
			}
			delete(participants, in.Actor.ID)
			for uid := range participants {
				if _, hasMention := recipients[uid]; hasMention {
					continue
				}
				recipients[uid] = models.NotifyReply
			}
		}

		now := time.Now().UTC()
		for uid, kind := range recipients {
			n := &models.Notification{
				ID:             uuid.NewString(),
				UserID:         uid,
				Kind:           kind,
				DocumentID:     in.DocID,
				DocumentTitle:  in.DocTitle,
				CommentID:      threadCommentID,
				ActorID:        in.Actor.ID,
				ActorName:      preferName(in.Actor),
				ActorAvatarURL: in.Actor.AvatarURL,
				Preview:        preview,
				CreatedAt:      now,
			}
			_ = a.store.InsertNotification(ctx, n)
		}
	}()
}

func preferName(u *models.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Login
}
