package store

import (
	"context"
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

func (s *Store) Documents() *mongo.Collection    { return s.db.Collection("documents") }
func (s *Store) Comments() *mongo.Collection     { return s.db.Collection("comments") }
func (s *Store) Users() *mongo.Collection        { return s.db.Collection("users") }
func (s *Store) Sessions() *mongo.Collection     { return s.db.Collection("sessions") }
func (s *Store) AuthStates() *mongo.Collection   { return s.db.Collection("auth_states") }
func (s *Store) UserSecrets() *mongo.Collection  { return s.db.Collection("user_secrets") }

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

// ListChildren returns documents whose parent is the given doc, oldest first.
func (s *Store) ListChildren(ctx context.Context, parentID string) ([]models.Document, error) {
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
	err := s.Documents().FindOne(ctx, bson.M{"_id": id}).Decode(&d)
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

func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	if _, err := s.Documents().DeleteOne(ctx, bson.M{"_id": id}); err != nil {
		return err
	}
	_, err := s.Comments().DeleteMany(ctx, bson.M{"document_id": id})
	return err
}

func (s *Store) UpdateDocumentTitle(ctx context.Context, id, title string) error {
	_, err := s.Documents().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"title": title, "updated_at": time.Now().UTC()},
	})
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
