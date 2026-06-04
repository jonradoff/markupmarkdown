package api

import (
	"sync"
	"time"

	"markupmarkdown/internal/models"
	"markupmarkdown/internal/render"
)

// plainTextCache memoizes the goldmark plain-text extraction per
// (docID, updatedAt). Agents that fire many add_comment calls against the
// same doc all hit the cache instead of re-parsing the markdown each time.
//
// Bounded by lastEvict cleanup — entries older than 30 minutes since last
// access are dropped on the next miss. Acceptable to occasionally re-parse
// after a long idle; the goal is to clip the obvious hot path.
type plainTextCacheEntry struct {
	updatedAt time.Time
	plain     string
	touched   time.Time
}

var plainTextCache = struct {
	sync.RWMutex
	m         map[string]*plainTextCacheEntry
	lastEvict time.Time
}{m: map[string]*plainTextCacheEntry{}}

const plainTextCacheIdleTTL = 30 * time.Minute

func plainTextFor(doc *models.Document) string {
	if doc == nil {
		return ""
	}
	now := time.Now()

	plainTextCache.RLock()
	if e := plainTextCache.m[doc.ID]; e != nil && e.updatedAt.Equal(doc.UpdatedAt) {
		e.touched = now
		plain := e.plain
		plainTextCache.RUnlock()
		return plain
	}
	plainTextCache.RUnlock()

	plain := render.PlainText(doc.Content)

	plainTextCache.Lock()
	plainTextCache.m[doc.ID] = &plainTextCacheEntry{
		updatedAt: doc.UpdatedAt,
		plain:     plain,
		touched:   now,
	}
	if now.Sub(plainTextCache.lastEvict) > plainTextCacheIdleTTL {
		for k, v := range plainTextCache.m {
			if now.Sub(v.touched) > plainTextCacheIdleTTL {
				delete(plainTextCache.m, k)
			}
		}
		plainTextCache.lastEvict = now
	}
	plainTextCache.Unlock()
	return plain
}
