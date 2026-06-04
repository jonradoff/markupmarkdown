package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/ai"
	"markupmarkdown/internal/mcpserver"
	"markupmarkdown/internal/models"
	"markupmarkdown/internal/render"
)

// This file implements the mcpserver.API interface against the running
// api.API. It exists so the MCP server can stay in its own package without
// pulling in the full handler graph.

func (a *API) UserFromBearer(ctx context.Context, tok string) (*models.User, string, string, error) {
	if !strings.HasPrefix(tok, "mmk_") || len(tok) < 32 {
		return nil, "", "", nil
	}
	rec, err := a.store.GetAPITokenByHash(ctx, HashToken(tok))
	if err != nil {
		return nil, "", "", err
	}
	if rec == nil {
		return nil, "", "", nil
	}
	u, err := a.store.GetUser(ctx, rec.UserID)
	if err != nil {
		return nil, "", "", err
	}
	if u == nil {
		return nil, "", "", nil
	}
	go a.store.TouchAPIToken(contextDetached(), rec.ID)
	return u, rec.ID, rec.Label, nil
}

func (a *API) DocAccess(ctx context.Context, userID, docID, accessToken string) (*models.Document, error) {
	doc, err := a.store.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errors.New("document not found")
	}
	if !doc.Private {
		return doc, nil
	}
	ok, err := repoAccessCache.check(ctx, userID, accessToken, doc.GitHubOwner, doc.GitHubRepo)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no current GitHub access to %s/%s", doc.GitHubOwner, doc.GitHubRepo)
	}
	return doc, nil
}

func (a *API) ListDocumentsForUser(ctx context.Context, userID string, includeTrash bool) ([]models.Document, error) {
	if includeTrash {
		live, err := a.store.ListDocumentsForUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		trash, err := a.store.ListTrashForUser(ctx, userID)
		if err != nil {
			return live, nil
		}
		return append(live, trash...), nil
	}
	return a.store.ListDocumentsForUser(ctx, userID)
}

func (a *API) ListComments(ctx context.Context, docID string) ([]models.Comment, error) {
	return a.store.ListComments(ctx, docID)
}

// CreateComment is used by the MCP path. Bearer auth implies agent, so
// every call here stamps the bot identity from tokenID + agentLabel; the
// label is recomputed at read time from the current token record so
// renaming the token reflects everywhere.
func (a *API) CreateComment(ctx context.Context, userID, docID, body, quoted string, occurrence int, tokenID, agentLabel string) (*models.Comment, error) {
	if occurrence < 1 {
		occurrence = 1
	}
	doc, err := a.store.GetDocument(ctx, docID)
	if err != nil || doc == nil {
		return nil, errors.New("document not found")
	}

	// Anchor the agent's comment by text-substring. We extract the plain
	// text once and locate the Nth occurrence — frontend's anchor utility
	// recomputes offsets at render time from the same logic.
	plain := render.PlainText(doc.Content)
	matches := render.CountOccurrences(plain, quoted)
	if matches == 0 {
		return nil, fmt.Errorf("`quoted_text` not found in document — copy a verbatim span")
	}
	if matches > 1 && occurrence > matches {
		return nil, fmt.Errorf("`quoted_text` appears %d times; occurrence=%d is out of range", matches, occurrence)
	}
	start, end := render.FindOccurrence(plain, quoted, occurrence)
	if start < 0 {
		return nil, fmt.Errorf("internal: failed to resolve occurrence %d of %d", occurrence, matches)
	}

	now := time.Now().UTC()
	u, _ := a.store.GetUser(ctx, userID)
	c := &models.Comment{
		ID:         uuid.NewString(),
		DocumentID: docID,
		Anchor:     models.Anchor{Start: start, End: end, Exact: quoted},
		AuthorID:   userID,
		Body:       strings.TrimSpace(body),
		Replies:    []models.Reply{},
		ActorKind:  models.ActorAgent,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	stampAgentWrite(c, tokenID, agentLabel)
	if err := a.store.InsertComment(ctx, c); err != nil {
		return nil, err
	}
	a.resolveAgentIdentity(ctx, c)
	a.hub.Broadcast(docID, "comments-updated")
	a.fanOutCommentNotifications(fanOutInput{
		DocID: docID, DocTitle: doc.Title, Body: c.Body, Comment: c, Actor: u,
	})
	return c, nil
}

func (a *API) ReplyToComment(ctx context.Context, userID, commentID, body, tokenID, agentLabel string) (*models.Comment, error) {
	parent, err := a.store.GetComment(ctx, commentID)
	if err != nil || parent == nil {
		return nil, errors.New("comment not found")
	}
	doc, err := a.store.GetDocument(ctx, parent.DocumentID)
	if err != nil || doc == nil {
		return nil, errors.New("document not found")
	}
	u, _ := a.store.GetUser(ctx, userID)
	now := time.Now().UTC()
	reply := models.Reply{
		ID:        uuid.NewString(),
		AuthorID:  userID,
		Body:      strings.TrimSpace(body),
		ActorKind: models.ActorAgent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	stampAgentWriteReply(&reply, tokenID, agentLabel)
	_ = u
	c, err := a.store.AppendReply(ctx, commentID, reply)
	if err != nil {
		return nil, err
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	a.fanOutCommentNotifications(fanOutInput{
		DocID: c.DocumentID, DocTitle: doc.Title, Body: reply.Body,
		Comment: c, ReplyOf: parent, Actor: u,
	})
	a.resolveAgentIdentity(ctx, c)
	return c, nil
}

func (a *API) ResolveComment(ctx context.Context, userID, id string, reopen bool) (*models.Comment, error) {
	u, _ := a.store.GetUser(ctx, userID)
	name := preferName(u)
	var update bson_M
	now := time.Now().UTC()
	if reopen {
		update = bson_M{"resolved": false, "resolved_by": "", "resolved_at": nil}
	} else {
		update = bson_M{"resolved": true, "resolved_by": name, "resolved_at": now}
	}
	c, err := a.store.UpdateComment(ctx, id, update)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("comment not found")
	}
	a.hub.Broadcast(c.DocumentID, "comments-updated")
	return c, nil
}

// bson_M is a tiny alias to avoid pulling bson.M into mcpapi.go for this
// one update.
type bson_M = map[string]any

func (a *API) ReviseWithAI(ctx context.Context, userID, docID string, commentIDs []string, accept bool) (*mcpserver.RevisionOutput, error) {
	doc, err := a.store.GetDocument(ctx, docID)
	if err != nil || doc == nil {
		return nil, errors.New("document not found")
	}
	apiKey, err := a.decryptedAnthropicKey(ctx, userID)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, errors.New("no Anthropic API key on file for this user — add one at the markupmarkdown UI before calling this tool")
	}

	allComments, err := a.store.ListComments(ctx, docID)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range commentIDs {
		want[id] = true
	}
	filterByIDs := len(want) > 0
	var resolved []models.Comment
	for _, c := range allComments {
		if !c.Resolved {
			continue
		}
		if filterByIDs && !want[c.ID] {
			continue
		}
		resolved = append(resolved, c)
	}
	if len(resolved) == 0 {
		return nil, errors.New("no resolved comments to apply (resolve at least one first)")
	}

	rev := make([]ai.ResolvedComment, 0, len(resolved))
	applied := make([]string, 0, len(resolved))
	for _, c := range resolved {
		applied = append(applied, c.ID)
		rc := ai.ResolvedComment{
			Quoted: c.Anchor.Exact, Author: c.Author, Body: c.Body, ResolvedBy: c.ResolvedBy,
		}
		for _, rep := range c.Replies {
			rc.Replies = append(rc.Replies, ai.ResolvedReply{Author: rep.Author, Body: rep.Body})
		}
		rev = append(rev, rc)
	}

	result, err := ai.Revise(ctx, apiKey, doc.Title, doc.Content, rev, nil)
	if err != nil {
		return nil, err
	}
	out := &mcpserver.RevisionOutput{
		OriginalContent: doc.Content,
		RevisedContent:  result.Content,
		Model:           result.Model,
		TokensIn:        result.TokensIn,
		TokensOut:       result.TokensOut,
		AppliedIDs:      applied,
	}

	if accept {
		u, _ := a.store.GetUser(ctx, userID)
		now := time.Now().UTC()
		newDoc := &models.Document{
			ID:           uuid.NewString(),
			Title:        doc.Title,
			Origin:       doc.Origin,
			SourceURL:    doc.SourceURL,
			Content:      strings.TrimRight(result.Content, "\n") + "\n",
			Private:      doc.Private,
			GitHubOwner:  doc.GitHubOwner,
			GitHubRepo:   doc.GitHubRepo,
			GitHubRef:    doc.GitHubRef,
			GitHubPath:   doc.GitHubPath,
			ParentID:     doc.ID,
			CreatedByID:  userID,
			RevisionMeta: &models.RevisionMeta{
				Model:             result.Model,
				AppliedCommentIDs: applied,
				TokensIn:          result.TokensIn,
				TokensOut:         result.TokensOut,
				GeneratedBy:       preferName(u),
				GeneratedByID:     userID,
				GeneratedAt:       now,
			},
			CreatedAt: now, UpdatedAt: now,
		}
		if err := a.store.InsertDocument(ctx, newDoc); err != nil {
			return nil, err
		}
		out.NewDocID = newDoc.ID
	}
	return out, nil
}
