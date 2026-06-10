package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"markupmarkdown/internal/models"
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

func New(uri, dbName string) (*Store, error) {
	if uri == "" {
		return nil, fmt.Errorf("MONGODB_URI is required")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	s := &Store{client: client, db: client.Database(dbName)}
	s.ensureIndexes(ctx)
	return s, nil
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func (s *Store) Documents() *mongo.Collection      { return s.db.Collection("documents") }
func (s *Store) Comments() *mongo.Collection       { return s.db.Collection("comments") }
func (s *Store) Users() *mongo.Collection          { return s.db.Collection("users") }
func (s *Store) Sessions() *mongo.Collection       { return s.db.Collection("sessions") }
func (s *Store) AuthStates() *mongo.Collection     { return s.db.Collection("auth_states") }
func (s *Store) UserSecrets() *mongo.Collection    { return s.db.Collection("user_secrets") }
func (s *Store) DocumentViews() *mongo.Collection  { return s.db.Collection("document_views") }
func (s *Store) Notifications() *mongo.Collection  { return s.db.Collection("notifications") }
func (s *Store) APITokens() *mongo.Collection      { return s.db.Collection("api_tokens") }
func (s *Store) TokenEvents() *mongo.Collection    { return s.db.Collection("token_events") }
func (s *Store) Indexes() *mongo.Collection        { return s.db.Collection("indexes") }
func (s *Store) HiddenItems() *mongo.Collection    { return s.db.Collection("hidden_items") }

func (s *Store) ensureIndexes(ctx context.Context) {
	_, _ = s.Documents().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "updated_at", Value: -1}}},
	})
	_, _ = s.Comments().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "document_id", Value: 1}, {Key: "created_at", Value: 1}}},
		{Keys: bson.D{{Key: "document_id", Value: 1}, {Key: "resolved", Value: 1}}},
	})
	_, _ = s.Users().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "github_id", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
	expires := int32(0)
	_, _ = s.Sessions().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(expires)},
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
	})
	tenMin := int32(600)
	_, _ = s.AuthStates().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "created_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(tenMin)},
	})
	_, _ = s.Documents().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "parent_id", Value: 1}}},
	})
	_, _ = s.DocumentViews().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "last_viewed_at", Value: -1}}},
		{Keys: bson.D{{Key: "document_id", Value: 1}}},
	})
	_, _ = s.Notifications().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "read_at", Value: 1}}},
	})
	_, _ = s.Users().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "login", Value: 1}}},
	})
	_, _ = s.APITokens().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "hash", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
	})
	tokenEventTTL := int32(30 * 24 * 3600) // 30 days
	_, _ = s.TokenEvents().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "token_id", Value: 1}, {Key: "at", Value: -1}}},
		{Keys: bson.D{{Key: "at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(tokenEventTTL)},
	})
	_, _ = s.Indexes().Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "created_by_id", Value: 1}, {Key: "updated_at", Value: -1}}},
		// Dedupe lookup: same creator pointing at the same source returns
		// the existing index instead of minting a duplicate.
		{Keys: bson.D{{Key: "created_by_id", Value: 1}, {Key: "kind", Value: 1}, {Key: "owner", Value: 1}, {Key: "repo", Value: 1}}},
	})
	_, _ = s.HiddenItems().Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Lookup by (user, kind) when filtering listDocuments / listMyIndexes.
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "kind", Value: 1}}},
		// Toggle / dedupe key — one row per (user, kind, item).
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "kind", Value: 1}, {Key: "item_id", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
}

// HideItem marks an item as hidden from the user's personal lists.
// Idempotent — re-calling for the same (user, kind, item) is a no-op.
// This is a soft, per-user dismissal: it doesn't soft-delete the
// underlying doc/index, only removes it from this user's listing.
func (s *Store) HideItem(ctx context.Context, userID, kind, itemID string) error {
	now := time.Now().UTC()
	_, err := s.HiddenItems().UpdateOne(ctx,
		bson.M{"user_id": userID, "kind": kind, "item_id": itemID},
		bson.M{"$setOnInsert": bson.M{"created_at": now}},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// UnhideItem clears a "Forget" marker. Used when the user undoes a
// Forget action (currently not surfaced in the UI but exposed for
// completeness + potential restore flow).
func (s *Store) UnhideItem(ctx context.Context, userID, kind, itemID string) error {
	_, err := s.HiddenItems().DeleteOne(ctx, bson.M{"user_id": userID, "kind": kind, "item_id": itemID})
	return err
}

// HiddenItemIDs returns the set of item IDs the user has Forgotten in
// the given kind ("doc" or "index"). Callers use it to filter their
// listing query — typically the same query that already pages through
// the underlying collection.
func (s *Store) HiddenItemIDs(ctx context.Context, userID, kind string) (map[string]bool, error) {
	cur, err := s.HiddenItems().Find(ctx, bson.M{"user_id": userID, "kind": kind})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	out := map[string]bool{}
	for cur.Next(ctx) {
		var row struct {
			ItemID string `bson:"item_id"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ItemID] = true
	}
	return out, nil
}

// InsertIndex stores a new markdown-index row.
func (s *Store) InsertIndex(ctx context.Context, i *models.Index) error {
	_, err := s.Indexes().InsertOne(ctx, i)
	return err
}

// GetIndex returns an index by id (not filtering deleted_at; callers
// decide whether they want soft-deleted rows back).
func (s *Store) GetIndex(ctx context.Context, id string) (*models.Index, error) {
	var idx models.Index
	if err := s.Indexes().FindOne(ctx, bson.M{"_id": id, "deleted_at": bson.M{"$exists": false}}).Decode(&idx); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &idx, nil
}

// ListIndexesForUser returns the live (non-deleted) indexes the user
// has created, newest first.
func (s *Store) ListIndexesForUser(ctx context.Context, userID string) ([]models.Index, error) {
	cur, err := s.Indexes().Find(ctx,
		bson.M{"created_by_id": userID, "deleted_at": bson.M{"$exists": false}},
		options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Index
	for cur.Next(ctx) {
		var i models.Index
		if err := cur.Decode(&i); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

// FindIndexBySource looks up a creator's existing index pointing at the
// given source so we can return it instead of minting a duplicate.
// repo is "" for user/org indexes.
func (s *Store) FindIndexBySource(ctx context.Context, userID string, kind models.IndexKind, owner, repo string) (*models.Index, error) {
	if userID == "" {
		return nil, nil
	}
	var idx models.Index
	if err := s.Indexes().FindOne(ctx, bson.M{
		"created_by_id": userID,
		"kind":          string(kind),
		"owner":         owner,
		"repo":          repo,
		"deleted_at":    bson.M{"$exists": false},
	}).Decode(&idx); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &idx, nil
}

// SoftDeleteIndex marks an index as deleted; the row sticks around for
// the same reason documents do (a future restore flow + an audit
// trail). Only the index's creator should call this — handler enforces.
func (s *Store) SoftDeleteIndex(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.Indexes().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"deleted_at": now}})
	return err
}

// UpdateIndexTitle renames an index. Cookie-session only — checked at
// the handler layer.
func (s *Store) UpdateIndexTitle(ctx context.Context, id, title string) error {
	_, err := s.Indexes().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"title": title, "updated_at": time.Now().UTC()},
	})
	return err
}

// UpdateIndexDefaultFilter pins (or clears, when filter == "") the
// default filename filter shown to share-link visitors. Owner-only
// — enforced at the handler.
func (s *Store) UpdateIndexDefaultFilter(ctx context.Context, id, filter string) error {
	now := time.Now().UTC()
	if filter == "" {
		_, err := s.Indexes().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
			"$set":   bson.M{"updated_at": now},
			"$unset": bson.M{"default_filter": ""},
		})
		return err
	}
	_, err := s.Indexes().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"default_filter": filter, "updated_at": now},
	})
	return err
}

// LogTokenEvent appends an entry to the per-token activity log.
func (s *Store) LogTokenEvent(ctx context.Context, e *models.TokenEvent) error {
	_, err := s.TokenEvents().InsertOne(ctx, e)
	return err
}

// ListTokenEvents returns the last `limit` events for a given token, newest first.
func (s *Store) ListTokenEvents(ctx context.Context, tokenID string, limit int) ([]models.TokenEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "at", Value: -1}}).
		SetLimit(int64(limit))
	cur, err := s.TokenEvents().Find(ctx, bson.M{"token_id": tokenID}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.TokenEvent
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// API tokens

func (s *Store) InsertAPIToken(ctx context.Context, t *models.APIToken) error {
	_, err := s.APITokens().InsertOne(ctx, t)
	return err
}

func (s *Store) GetAPITokenByHash(ctx context.Context, hash string) (*models.APIToken, error) {
	var t models.APIToken
	err := s.APITokens().FindOne(ctx, bson.M{
		"hash":       hash,
		"revoked_at": bson.M{"$exists": false},
	}).Decode(&t)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetAPITokenByID returns a single token scoped to the owning user. Filters
// out revoked tokens. Used by token edit + activity endpoints.
func (s *Store) GetAPITokenByID(ctx context.Context, userID, id string) (*models.APIToken, error) {
	var t models.APIToken
	err := s.APITokens().FindOne(ctx, bson.M{
		"_id":        id,
		"user_id":    userID,
		"revoked_at": bson.M{"$exists": false},
	}).Decode(&t)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListAPITokensForUser(ctx context.Context, userID string) ([]models.APIToken, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cur, err := s.APITokens().Find(ctx, bson.M{
		"user_id":    userID,
		"revoked_at": bson.M{"$exists": false},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.APIToken
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAPITokensByIDs returns a {id → token} map for the supplied set. Used
// by the comment-read paths to resolve agent identities at the current
// label, so renaming a token reflects everywhere it has commented.
func (s *Store) GetAPITokensByIDs(ctx context.Context, ids []string) (map[string]*models.APIToken, error) {
	if len(ids) == 0 {
		return map[string]*models.APIToken{}, nil
	}
	cur, err := s.APITokens().Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []models.APIToken
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]*models.APIToken, len(rows))
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out, nil
}

// DistinctDocIDsForToken returns every document where the token has authored
// a comment or reply. Used to fan out a comments-updated event on rename.
func (s *Store) DistinctDocIDsForToken(ctx context.Context, tokenID string) ([]string, error) {
	cur, err := s.Comments().Find(ctx, bson.M{
		"$or": []bson.M{
			{"token_id": tokenID},
			{"replies.token_id": tokenID},
		},
	}, options.Find().SetProjection(bson.M{"document_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		DocumentID string `bson:"document_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if _, ok := seen[r.DocumentID]; ok {
			continue
		}
		seen[r.DocumentID] = struct{}{}
		out = append(out, r.DocumentID)
	}
	return out, nil
}

func (s *Store) UpdateAPITokenLabel(ctx context.Context, userID, id, label string) error {
	_, err := s.APITokens().UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID, "revoked_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"label": label}},
	)
	return err
}

// UpdateAPITokenFields applies any subset of {label, scope}. Used by the
// inline edit + scope dropdown in the tokens modal.
func (s *Store) UpdateAPITokenFields(ctx context.Context, userID, id string, set bson.M) error {
	if len(set) == 0 {
		return nil
	}
	_, err := s.APITokens().UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID, "revoked_at": bson.M{"$exists": false}},
		bson.M{"$set": set},
	)
	return err
}

func (s *Store) RevokeAPIToken(ctx context.Context, userID, id string) error {
	_, err := s.APITokens().UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID},
		bson.M{"$set": bson.M{"revoked_at": time.Now().UTC()}},
	)
	return err
}

func (s *Store) TouchAPIToken(ctx context.Context, id string) {
	now := time.Now().UTC()
	_, _ = s.APITokens().UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"last_used_at": now}},
	)
}

// Notifications

func (s *Store) InsertNotification(ctx context.Context, n *models.Notification) error {
	_, err := s.Notifications().InsertOne(ctx, n)
	return err
}

func (s *Store) ListNotificationsForUser(ctx context.Context, userID string, limit int) ([]models.Notification, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(int64(limit))
	cur, err := s.Notifications().Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	var out []models.Notification
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	unread, err := s.Notifications().CountDocuments(ctx, bson.M{
		"user_id": userID,
		"read_at": bson.M{"$exists": false},
	})
	if err != nil {
		unread = 0
	}
	return out, unread, nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, userID string) error {
	_, err := s.Notifications().UpdateMany(ctx,
		bson.M{"user_id": userID, "read_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"read_at": time.Now().UTC()}},
	)
	return err
}

func (s *Store) MarkNotificationRead(ctx context.Context, userID, id string) error {
	_, err := s.Notifications().UpdateOne(ctx,
		bson.M{"_id": id, "user_id": userID},
		bson.M{"$set": bson.M{"read_at": time.Now().UTC()}},
	)
	return err
}

// MarkNotificationsReadForComment marks every unread notification for
// (user, comment) as read. Powers the auto-decrement of the bell badge
// as the user scrolls through new comments — viewing the comment
// counts as reading the notification, regardless of whether they
// arrived via the bell.
func (s *Store) MarkNotificationsReadForComment(ctx context.Context, userID, commentID string) (int64, error) {
	res, err := s.Notifications().UpdateMany(ctx,
		bson.M{
			"user_id":    userID,
			"comment_id": commentID,
			"read_at":    bson.M{"$exists": false},
		},
		bson.M{"$set": bson.M{"read_at": time.Now().UTC()}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// FindUsersByLogins returns user records for the supplied GitHub login set.
// Used by mention parsing to resolve @login → user.ID.
func (s *Store) FindUsersByLogins(ctx context.Context, logins []string) ([]models.User, error) {
	if len(logins) == 0 {
		return nil, nil
	}
	cur, err := s.Users().Find(ctx, bson.M{"login": bson.M{"$in": logins}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.User
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RecordDocumentView upserts the (doc, user) pair with the current timestamp.
// Cheap: a single Mongo upsert per page view. Used to scope the home-page
// list to docs the user has actually engaged with.
func (s *Store) RecordDocumentView(ctx context.Context, documentID, userID string) error {
	if documentID == "" || userID == "" {
		return nil
	}
	now := time.Now().UTC()
	id := documentID + ":" + userID
	_, err := s.DocumentViews().UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{
			"$set":         bson.M{"last_viewed_at": now},
			"$setOnInsert": bson.M{
				"_id":             id,
				"document_id":     documentID,
				"user_id":         userID,
				"first_viewed_at": now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// DocumentView is the per-(doc, user) read marker.
type DocumentView struct {
	ID            string    `bson:"_id"`
	DocumentID    string    `bson:"document_id"`
	UserID        string    `bson:"user_id"`
	FirstViewedAt time.Time `bson:"first_viewed_at"`
	LastViewedAt  time.Time `bson:"last_viewed_at"`
}

// GetDocumentView returns the existing view, or nil if never opened.
// Read this synchronously BEFORE enqueueing the bump — that way the API
// response reflects the prior state, which is what the unread filter
// keys off of.
func (s *Store) GetDocumentView(ctx context.Context, documentID, userID string) (*DocumentView, error) {
	id := documentID + ":" + userID
	var row DocumentView
	err := s.DocumentViews().FindOne(ctx, bson.M{"_id": id}).Decode(&row)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ViewersOfDocument returns the set of user IDs that have ever opened the
// given document. Used by listMentionCandidates to scope the @-mention
// autocomplete to people who plausibly know what doc the author is
// talking about — not the entire user base.
func (s *Store) ViewersOfDocument(ctx context.Context, documentID string) ([]string, error) {
	cur, err := s.DocumentViews().Find(ctx,
		bson.M{"document_id": documentID},
		options.Find().SetProjection(bson.M{"user_id": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		UserID string `bson:"user_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.UserID == "" {
			continue
		}
		if _, ok := seen[r.UserID]; ok {
			continue
		}
		seen[r.UserID] = struct{}{}
		out = append(out, r.UserID)
	}
	return out, nil
}

func (s *Store) viewedDocumentIDs(ctx context.Context, userID string) ([]string, error) {
	cur, err := s.DocumentViews().Find(ctx,
		bson.M{"user_id": userID},
		options.Find().SetProjection(bson.M{"document_id": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		DocumentID string `bson:"document_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.DocumentID)
	}
	return ids, nil
}

// User secrets

func (s *Store) GetUserSecrets(ctx context.Context, userID string) (*models.UserSecrets, error) {
	var us models.UserSecrets
	err := s.UserSecrets().FindOne(ctx, bson.M{"_id": userID}).Decode(&us)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &us, nil
}

func (s *Store) UpsertAnthropicKey(ctx context.Context, userID, ciphertext, hint string) error {
	now := time.Now().UTC()
	_, err := s.UserSecrets().UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{
			"$set": bson.M{
				"anthropic_key_ciphertext": ciphertext,
				"anthropic_key_hint":       hint,
				"anthropic_key_set_at":     now,
				"updated_at":               now,
			},
			"$setOnInsert": bson.M{
				"_id": userID,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

func (s *Store) DeleteAnthropicKey(ctx context.Context, userID string) error {
	_, err := s.UserSecrets().UpdateOne(ctx,
		bson.M{"_id": userID},
		bson.M{
			"$unset": bson.M{
				"anthropic_key_ciphertext": "",
				"anthropic_key_hint":       "",
				"anthropic_key_set_at":     "",
			},
			"$set": bson.M{"updated_at": time.Now().UTC()},
		},
	)
	return err
}

// Document revisions

// ListChildren returns non-deleted documents whose parent is the given
// doc, oldest first. Soft-deleted children stay hidden — callers that
// need to surface deleted nodes (Trash, audit) query directly.
func (s *Store) ListChildren(ctx context.Context, parentID string) ([]models.Document, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cur, err := s.Documents().Find(ctx, bson.M{
		"parent_id":  parentID,
		"deleted_at": bson.M{"$exists": false},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Document
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListChildrenRaw returns every child of parentID regardless of soft-
// delete state. Used when we need to walk the structural tree (e.g.,
// hard-purge a chain).
func (s *Store) ListChildrenRaw(ctx context.Context, parentID string) ([]models.Document, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cur, err := s.Documents().Find(ctx, bson.M{"parent_id": parentID}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Document
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RootDocument walks the parent chain up to the doc that has no parent
// (the original ingest). Returns the doc itself when it has no parent.
// Guards against cycles defensively. Used by the source-drift check so
// child revisions report drift relative to the original source, not
// against whichever GitHub state happens to match the AI-revised text.
//
// Walks via GetDocumentRaw so a soft-deleted ancestor doesn't sever
// the chain — alive descendants still belong to their original tree
// and should dedupe accordingly in listDocuments.
func (s *Store) RootDocument(ctx context.Context, id string) (*models.Document, error) {
	seen := map[string]bool{}
	current := id
	for !seen[current] {
		seen[current] = true
		d, err := s.GetDocumentRaw(ctx, current)
		if err != nil {
			return nil, err
		}
		if d == nil {
			return nil, nil
		}
		if d.ParentID == "" {
			return d, nil
		}
		current = d.ParentID
	}
	// Cycle — return whatever we last loaded so callers still get
	// something usable.
	return s.GetDocumentRaw(ctx, current)
}

// AncestorChain returns every ancestor of id starting at the root
// (inclusive) and ending at the doc just before id (exclusive of id
// itself). Useful for computing version indices ("v3 of 4") and for
// rendering breadcrumbs that show which earlier revision a doc was
// derived from. Walks via GetDocumentRaw so deleted ancestors still
// appear in the chain (the index would otherwise jump).
func (s *Store) AncestorChain(ctx context.Context, id string) ([]models.Document, error) {
	seen := map[string]bool{}
	var chain []models.Document
	current := id
	for !seen[current] {
		seen[current] = true
		d, err := s.GetDocumentRaw(ctx, current)
		if err != nil {
			return nil, err
		}
		if d == nil || d.ParentID == "" {
			break
		}
		parent, err := s.GetDocumentRaw(ctx, d.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			break
		}
		chain = append([]models.Document{*parent}, chain...)
		current = parent.ID
	}
	return chain, nil
}

// LatestDescendant walks the revision tree from docID via the most-
// recently-created child edge and returns the deepest ALIVE descendant
// it finds. Walks via ListChildrenRaw (including soft-deleted) so a
// deleted intermediate node doesn't sever the path to an alive
// descendant beyond it; only the returned doc is required to be alive.
//
// Returns nil when docID has no descendants at all, or when every
// descendant is soft-deleted. Guards against cycles defensively.
func (s *Store) LatestDescendant(ctx context.Context, docID string) (*models.Document, error) {
	current := docID
	var bestAlive *models.Document
	seen := map[string]bool{}
	for !seen[current] {
		seen[current] = true
		children, err := s.ListChildrenRaw(ctx, current)
		if err != nil {
			return bestAlive, err
		}
		if len(children) == 0 {
			break
		}
		next := children[0]
		for i := 1; i < len(children); i++ {
			if children[i].CreatedAt.After(next.CreatedAt) {
				next = children[i]
			}
		}
		if next.DeletedAt == nil {
			copied := next
			bestAlive = &copied
		}
		current = next.ID
	}
	return bestAlive, nil
}

// Users + Sessions

func (s *Store) UpsertUserByGitHubID(ctx context.Context, u *models.User) error {
	now := time.Now().UTC()
	u.UpdatedAt = now
	_, err := s.Users().UpdateOne(ctx,
		bson.M{"github_id": u.GitHubID},
		bson.M{
			"$set": bson.M{
				"login":        u.Login,
				"name":         u.Name,
				"email":        u.Email,
				"avatar_url":   u.AvatarURL,
				"access_token": u.AccessToken,
				"updated_at":   now,
			},
			"$setOnInsert": bson.M{
				"_id":        u.ID,
				"github_id":  u.GitHubID,
				"created_at": now,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return err
	}
	// Re-load to get authoritative state
	var loaded models.User
	if err := s.Users().FindOne(ctx, bson.M{"github_id": u.GitHubID}).Decode(&loaded); err == nil {
		*u = loaded
	}
	return nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*models.User, error) {
	var u models.User
	err := s.Users().FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) InsertSession(ctx context.Context, sess *models.Session) error {
	_, err := s.Sessions().InsertOne(ctx, sess)
	return err
}

func (s *Store) GetSession(ctx context.Context, id string) (*models.Session, error) {
	var sess models.Session
	err := s.Sessions().FindOne(ctx, bson.M{"_id": id}).Decode(&sess)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !sess.ExpiresAt.IsZero() && time.Now().After(sess.ExpiresAt) {
		return nil, nil
	}
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.Sessions().DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *Store) InsertAuthState(ctx context.Context, st *models.AuthState) error {
	_, err := s.AuthStates().InsertOne(ctx, st)
	return err
}

func (s *Store) ConsumeAuthState(ctx context.Context, id string) (*models.AuthState, error) {
	var st models.AuthState
	err := s.AuthStates().FindOneAndDelete(ctx, bson.M{"_id": id}).Decode(&st)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// Documents

func (s *Store) InsertDocument(ctx context.Context, d *models.Document) error {
	_, err := s.Documents().InsertOne(ctx, d)
	return err
}

func (s *Store) GetDocument(ctx context.Context, id string) (*models.Document, error) {
	var d models.Document
	err := s.Documents().FindOne(ctx, bson.M{
		"_id":        id,
		"deleted_at": bson.M{"$exists": false},
	}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDocumentRaw returns a doc by ID ignoring the soft-delete flag.
// Used by revision-chain walks (RootDocument / LatestDescendant) so a
// deleted ancestor or descendant doesn't sever the chain — the live
// nodes still need to know which tree they belong to.
func (s *Store) GetDocumentRaw(ctx context.Context, id string) (*models.Document, error) {
	var d models.Document
	err := s.Documents().FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDeletedDocument returns a soft-deleted doc by ID. Used by Restore.
func (s *Store) GetDeletedDocument(ctx context.Context, id string) (*models.Document, error) {
	var d models.Document
	err := s.Documents().FindOne(ctx, bson.M{
		"_id":        id,
		"deleted_at": bson.M{"$exists": true},
	}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ListDocuments(ctx context.Context) ([]models.Document, error) {
	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}})
	cur, err := s.Documents().Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Document
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDocumentsForUser returns documents the user has demonstrably engaged
// with: docs they viewed, created, AI-revised, or commented/replied on.
// Sorted newest-first by the doc's updated_at. The caller is responsible
// for filtering out private docs the user has lost GitHub access to.
func (s *Store) ListDocumentsForUser(ctx context.Context, userID string) ([]models.Document, error) {
	// Union of doc IDs from: comments authored by user, views, etc.
	idSet := map[string]struct{}{}

	// Comments authored by this user.
	cur, err := s.Comments().Find(ctx, bson.M{
		"$or": []bson.M{
			{"author_id": userID},
			{"replies.author_id": userID},
		},
	}, options.Find().SetProjection(bson.M{"document_id": 1}))
	if err == nil {
		var rows []struct {
			DocumentID string `bson:"document_id"`
		}
		if err := cur.All(ctx, &rows); err == nil {
			for _, r := range rows {
				idSet[r.DocumentID] = struct{}{}
			}
		}
		_ = cur.Close(ctx)
	}

	// Docs the user has viewed.
	if viewed, err := s.viewedDocumentIDs(ctx, userID); err == nil {
		for _, id := range viewed {
			idSet[id] = struct{}{}
		}
	}

	or := []bson.M{
		{"created_by_id": userID},
		{"revision_meta.generated_by_id": userID},
	}
	if len(idSet) > 0 {
		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		or = append(or, bson.M{"_id": bson.M{"$in": ids}})
	}

	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}})
	cur2, err := s.Documents().Find(ctx, bson.M{
		"$and": []bson.M{
			{"$or": or},
			{"deleted_at": bson.M{"$exists": false}},
		},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur2.Close(ctx)
	var out []models.Document
	if err := cur2.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListTrashForUser returns soft-deleted docs the user has touched.
func (s *Store) ListTrashForUser(ctx context.Context, userID string) ([]models.Document, error) {
	idSet := map[string]struct{}{}
	cur, err := s.Comments().Find(ctx, bson.M{
		"$or": []bson.M{
			{"author_id": userID},
			{"replies.author_id": userID},
		},
	}, options.Find().SetProjection(bson.M{"document_id": 1}))
	if err == nil {
		var rows []struct {
			DocumentID string `bson:"document_id"`
		}
		if err := cur.All(ctx, &rows); err == nil {
			for _, r := range rows {
				idSet[r.DocumentID] = struct{}{}
			}
		}
		_ = cur.Close(ctx)
	}
	if viewed, err := s.viewedDocumentIDs(ctx, userID); err == nil {
		for _, id := range viewed {
			idSet[id] = struct{}{}
		}
	}
	or := []bson.M{
		{"created_by_id": userID},
		{"revision_meta.generated_by_id": userID},
	}
	if len(idSet) > 0 {
		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		or = append(or, bson.M{"_id": bson.M{"$in": ids}})
	}

	opts := options.Find().SetSort(bson.D{{Key: "deleted_at", Value: -1}})
	cur2, err := s.Documents().Find(ctx, bson.M{
		"$and": []bson.M{
			{"$or": or},
			{"deleted_at": bson.M{"$exists": true}},
		},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur2.Close(ctx)
	var out []models.Document
	if err := cur2.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SoftDeleteDocument marks the doc deleted_at = now. Comments and views are
// retained so a Restore brings the doc back to the same state. A separate
// purge sweep handles the eventual hard delete.
func (s *Store) SoftDeleteDocument(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.Documents().UpdateOne(ctx,
		bson.M{"_id": id, "deleted_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"deleted_at": now, "updated_at": now}},
	)
	return err
}

// RestoreDocument clears the deleted_at marker.
func (s *Store) RestoreDocument(ctx context.Context, id string) error {
	_, err := s.Documents().UpdateOne(ctx,
		bson.M{"_id": id, "deleted_at": bson.M{"$exists": true}},
		bson.M{
			"$unset": bson.M{"deleted_at": ""},
			"$set":   bson.M{"updated_at": time.Now().UTC()},
		},
	)
	return err
}

// PurgeDocument is the hard-delete used by the background sweep once the
// retention window has expired.
func (s *Store) PurgeDocument(ctx context.Context, id string) error {
	if _, err := s.Documents().DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return err
	}
	if _, err := s.Comments().DeleteMany(ctx, bson.M{"document_id": id}); err != nil {
		return err
	}
	_, err := s.DocumentViews().DeleteMany(ctx, bson.M{"document_id": id})
	return err
}

// PurgeExpiredDeletes hard-deletes all soft-deleted docs older than cutoff.
// Returns the number purged.
func (s *Store) PurgeExpiredDeletes(ctx context.Context, cutoff time.Time) (int64, error) {
	cur, err := s.Documents().Find(ctx,
		bson.M{"deleted_at": bson.M{"$lt": cutoff}},
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	var rows []struct {
		ID string `bson:"_id"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return 0, err
	}
	var purged int64
	for _, r := range rows {
		if err := s.PurgeDocument(ctx, r.ID); err == nil {
			purged++
		}
	}
	return purged, nil
}

// FindLatestDocumentBySource returns the most recently updated, non-
// deleted document anchored to the same GitHub blob path
// (owner/repo/ref/path). Used by the human-readable URL system to
// dedupe: a viewer pasting the same github URL twice ends up on the
// SAME doc page so comments aggregate instead of fracturing across
// N parallel clones. Returns nil when no match exists.
func (s *Store) FindLatestDocumentBySource(ctx context.Context, owner, repo, ref, path string) (*models.Document, error) {
	if owner == "" || repo == "" || path == "" {
		return nil, nil
	}
	filter := bson.M{
		"github_owner": owner,
		"github_repo":  repo,
		"github_path":  path,
		"deleted_at":   bson.M{"$exists": false},
		"parent_id":    bson.M{"$exists": false}, // only chain roots — we walk to leaf via LatestDescendant
	}
	if ref != "" {
		filter["github_ref"] = ref
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "updated_at", Value: -1}})
	var doc models.Document
	if err := s.Documents().FindOne(ctx, filter, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (s *Store) UpdateDocumentTitle(ctx context.Context, id, title string) error {
	_, err := s.Documents().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"title": title, "updated_at": time.Now().UTC()},
	})
	return err
}

// SetDocumentSourceCheck stamps the result of a drift check. If
// latestSHA equals the stored SourceSHA we clear the drift fields;
// otherwise we record latestSHA + the drift timestamp so the frontend
// can render the "source updated on GitHub" banner.
//
// SourceDriftIgnoredSHA is preserved untouched here — the API layer is
// responsible for clearing it when latestSHA moves past the ignored
// value (a newer upstream commit unstucks the banner).
func (s *Store) SetDocumentSourceCheck(ctx context.Context, id, latestSHA string) error {
	now := time.Now().UTC()
	var doc models.Document
	if err := s.Documents().FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return err
	}
	set := bson.M{"source_checked_at": now}
	unset := bson.M{}
	if doc.SourceSHA == "" || latestSHA == doc.SourceSHA {
		// In sync (or no baseline to compare against).
		unset["source_latest_sha"] = ""
		unset["source_drifted_at"] = ""
		// The ignore-marker is also no longer relevant once we're back
		// in sync — drop it so the next genuine drift surfaces cleanly.
		unset["source_drift_ignored_sha"] = ""
	} else {
		set["source_latest_sha"] = latestSHA
		if doc.SourceDriftedAt == nil {
			set["source_drifted_at"] = now
		}
		// A new upstream SHA replaces any prior "ignore" choice — the
		// user agreed to ignore SHA-A, not SHA-B. Drop the marker so
		// the banner re-surfaces.
		if doc.SourceDriftIgnoredSHA != "" && doc.SourceDriftIgnoredSHA != latestSHA {
			unset["source_drift_ignored_sha"] = ""
		}
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	_, err := s.Documents().UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

// IgnoreDocumentSourceDrift records that the user dismissed the drift
// banner for sha — typically the doc's current SourceLatestSHA. The
// banner stays suppressed until upstream moves past sha, at which
// point SetDocumentSourceCheck clears the marker so the new drift
// shows.
func (s *Store) IgnoreDocumentSourceDrift(ctx context.Context, id, sha string) error {
	if sha == "" {
		return nil
	}
	_, err := s.Documents().UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"source_drift_ignored_sha": sha}},
	)
	return err
}

// UpdateDocumentSourceSHA stamps a new baseline SourceSHA on the doc
// — used after a successful direct-commit pushback to the doc's
// tracking branch, so the next drift check sees the freshly-committed
// SHA as "in sync" rather than reporting drift against the pre-push
// state.
func (s *Store) UpdateDocumentSourceSHA(ctx context.Context, id, sha string) error {
	if sha == "" {
		return nil
	}
	now := time.Now().UTC()
	_, err := s.Documents().UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{
			"$set": bson.M{
				"source_sha":        sha,
				"source_checked_at": now,
			},
			"$unset": bson.M{
				"source_latest_sha":        "",
				"source_drifted_at":        "",
				"source_drift_ignored_sha": "",
			},
		},
	)
	return err
}

// ReplaceDocumentSource updates the content + SHA after a successful
// sync (the user pulled in the latest GitHub version). Clears the
// drift fields and bumps updated_at so the doc list re-sorts.
func (s *Store) ReplaceDocumentSource(ctx context.Context, id, content, sha string) error {
	now := time.Now().UTC()
	_, err := s.Documents().UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{
			"$set": bson.M{
				"content":           content,
				"source_sha":        sha,
				"source_checked_at": now,
				"updated_at":        now,
			},
			"$unset": bson.M{
				"source_latest_sha": "",
				"source_drifted_at": "",
			},
		},
	)
	return err
}

// Comments

func (s *Store) InsertComment(ctx context.Context, c *models.Comment) error {
	_, err := s.Comments().InsertOne(ctx, c)
	return err
}

func (s *Store) GetComment(ctx context.Context, id string) (*models.Comment, error) {
	var c models.Comment
	err := s.Comments().FindOne(ctx, bson.M{"_id": id}).Decode(&c)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListComments(ctx context.Context, documentID string) ([]models.Comment, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cur, err := s.Comments().Find(ctx, bson.M{"document_id": documentID}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.Comment
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Replies == nil {
			out[i].Replies = []models.Reply{}
		}
	}
	return out, nil
}

func (s *Store) UpdateComment(ctx context.Context, id string, set bson.M) (*models.Comment, error) {
	set["updated_at"] = time.Now().UTC()
	_, err := s.Comments().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return nil, err
	}
	return s.GetComment(ctx, id)
}

func (s *Store) DeleteComment(ctx context.Context, id string) error {
	_, err := s.Comments().DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *Store) AppendReply(ctx context.Context, commentID string, r models.Reply) (*models.Comment, error) {
	_, err := s.Comments().UpdateOne(ctx,
		bson.M{"_id": commentID},
		bson.M{
			"$push": bson.M{"replies": r},
			"$set":  bson.M{"updated_at": time.Now().UTC()},
		},
	)
	if err != nil {
		return nil, err
	}
	return s.GetComment(ctx, commentID)
}

func (s *Store) UpdateReply(ctx context.Context, commentID, replyID, body string) (*models.Comment, error) {
	now := time.Now().UTC()
	_, err := s.Comments().UpdateOne(ctx,
		bson.M{"_id": commentID, "replies.id": replyID},
		bson.M{"$set": bson.M{
			"replies.$.body":       body,
			"replies.$.updated_at": now,
			"updated_at":           now,
		}},
	)
	if err != nil {
		return nil, err
	}
	return s.GetComment(ctx, commentID)
}

func (s *Store) DeleteReply(ctx context.Context, commentID, replyID string) (*models.Comment, error) {
	_, err := s.Comments().UpdateOne(ctx,
		bson.M{"_id": commentID},
		bson.M{
			"$pull": bson.M{"replies": bson.M{"id": replyID}},
			"$set":  bson.M{"updated_at": time.Now().UTC()},
		},
	)
	if err != nil {
		return nil, err
	}
	return s.GetComment(ctx, commentID)
}
