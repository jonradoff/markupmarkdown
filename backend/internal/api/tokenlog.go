package api

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"markupmarkdown/internal/models"
)

// Per-token activity log. We sample to ~one event per minute per
// (token, action) so the token_events collection stays small even for
// busy agents that fire dozens of comment.create per second. The Mongo
// collection has a 30-day TTL set in store.ensureIndexes.

// sampler memoizes the last-sampled time per (tokenID,action) in process so
// we don't have to round-trip Mongo for every write. Process restart resets
// the sampler, which is fine — it just means up to one extra event per
// action per token at startup.
var sampler = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

const tokenSampleWindow = time.Minute

func (a *API) logTokenAction(parent context.Context, tokenID, action, docID string) {
	if tokenID == "" || action == "" {
		return
	}
	key := tokenID + "|" + action
	now := time.Now().UTC()

	sampler.Lock()
	last, ok := sampler.m[key]
	if ok && now.Sub(last) < tokenSampleWindow {
		sampler.Unlock()
		return
	}
	sampler.m[key] = now
	sampler.Unlock()

	// Resolve user ID via the token record so the activity log has its
	// per-user index path. Detached context so we never block the caller.
	go func() {
		ctx := contextDetached()
		_ = parent // not propagated — we don't want request cancel to abort logging
		tokMap, err := a.store.GetAPITokensByIDs(ctx, []string{tokenID})
		if err != nil || tokMap[tokenID] == nil {
			return
		}
		ev := &models.TokenEvent{
			ID:         uuid.NewString(),
			TokenID:    tokenID,
			UserID:     tokMap[tokenID].UserID,
			Action:     action,
			DocumentID: docID,
			At:         now,
		}
		_ = a.store.LogTokenEvent(ctx, ev)
	}()
}
