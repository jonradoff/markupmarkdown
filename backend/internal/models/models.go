package models

import "time"

type User struct {
	ID          string    `bson:"_id" json:"id"`
	GitHubID    int64     `bson:"github_id" json:"githubId"`
	Login       string    `bson:"login" json:"login"`
	Name        string    `bson:"name" json:"name"`
	Email       string    `bson:"email,omitempty" json:"email,omitempty"`
	AvatarURL   string    `bson:"avatar_url,omitempty" json:"avatarUrl,omitempty"`
	AccessToken string    `bson:"access_token" json:"-"`
	CreatedAt   time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `bson:"updated_at" json:"updatedAt"`
}

type Session struct {
	ID        string    `bson:"_id" json:"-"`
	UserID    string    `bson:"user_id" json:"-"`
	CreatedAt time.Time `bson:"created_at" json:"-"`
	ExpiresAt time.Time `bson:"expires_at" json:"-"`
}

type AuthState struct {
	ID           string    `bson:"_id" json:"-"`
	Redirect     string    `bson:"redirect,omitempty" json:"-"`
	CookieValue  string    `bson:"cookie_value" json:"-"`
	CreatedAt    time.Time `bson:"created_at" json:"-"`
}

type Document struct {
	ID        string    `bson:"_id" json:"id"`
	Title     string    `bson:"title" json:"title"`
	SourceURL string    `bson:"source_url,omitempty" json:"sourceUrl,omitempty"`
	Origin    string    `bson:"origin" json:"origin"` // "url" | "upload"
	Content   string    `bson:"content" json:"content"`
	// Private is true when the source could only be read with GitHub auth.
	// Readers of the cloned copy must also have GitHub access to {Owner, Repo}.
	Private    bool   `bson:"private" json:"private"`
	GitHubOwner string `bson:"github_owner,omitempty" json:"githubOwner,omitempty"`
	GitHubRepo  string `bson:"github_repo,omitempty" json:"githubRepo,omitempty"`
	GitHubRef   string `bson:"github_ref,omitempty" json:"githubRef,omitempty"`
	GitHubPath  string `bson:"github_path,omitempty" json:"githubPath,omitempty"`

	// Revision chain. A non-empty ParentID means this doc was created by
	// applying resolved comments from the parent via the AI revision feature.
	ParentID     string        `bson:"parent_id,omitempty" json:"parentId,omitempty"`
	RevisionMeta *RevisionMeta `bson:"revision_meta,omitempty" json:"revisionMeta,omitempty"`

	// CreatedByID is the authenticated user (GitHub login) who created this
	// document. Empty for documents created anonymously. Used by
	// ListDocumentsForUser to scope the home-page list to docs you worked on.
	CreatedByID string `bson:"created_by_id,omitempty" json:"-"`

	// DeletedAt is set when the doc enters soft-deleted state. The doc
	// stays in MongoDB for ~30 days so the user can restore it from the
	// Trash view; after that a background sweep will purge it.
	DeletedAt *time.Time `bson:"deleted_at,omitempty" json:"deletedAt,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt time.Time `bson:"updated_at" json:"updatedAt"`
}

// RevisionMeta records how an AI-generated revision was produced. This is
// purely informational — used to surface "AI-revised, applied N comments" on
// the doc page and for the version-history sidebar.
type RevisionMeta struct {
	Model             string    `bson:"model" json:"model"`
	AppliedCommentIDs []string  `bson:"applied_comment_ids" json:"appliedCommentIds"`
	TokensIn          int64     `bson:"tokens_in" json:"tokensIn"`
	TokensOut         int64     `bson:"tokens_out" json:"tokensOut"`
	GeneratedBy       string    `bson:"generated_by" json:"generatedBy"` // display name
	GeneratedByID     string    `bson:"generated_by_id,omitempty" json:"-"`
	GeneratedAt       time.Time `bson:"generated_at" json:"generatedAt"`
}

// UserSecrets holds per-user encrypted credentials. One document per user.
// Plaintext API keys never live in MongoDB.
type UserSecrets struct {
	UserID                 string    `bson:"_id" json:"-"`
	AnthropicKeyCiphertext string    `bson:"anthropic_key_ciphertext,omitempty" json:"-"`
	AnthropicKeyHint       string    `bson:"anthropic_key_hint,omitempty" json:"anthropicKeyHint,omitempty"`
	AnthropicKeySetAt      time.Time `bson:"anthropic_key_set_at,omitempty" json:"anthropicKeySetAt,omitempty"`
	UpdatedAt              time.Time `bson:"updated_at" json:"-"`
}

type Anchor struct {
	Start int    `bson:"start" json:"start"`
	End   int    `bson:"end" json:"end"`
	Exact string `bson:"exact" json:"exact"`
	Prefix string `bson:"prefix,omitempty" json:"prefix,omitempty"`
	Suffix string `bson:"suffix,omitempty" json:"suffix,omitempty"`
}

type Reply struct {
	ID              string    `bson:"id" json:"id"`
	Author          string    `bson:"author" json:"author"`
	AuthorID        string    `bson:"author_id,omitempty" json:"-"`
	AuthorAvatarURL string    `bson:"author_avatar_url,omitempty" json:"authorAvatarUrl,omitempty"`
	Body            string    `bson:"body" json:"body"`
	CreatedAt       time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt       time.Time `bson:"updated_at" json:"updatedAt"`
}

type Comment struct {
	ID              string `bson:"_id" json:"id"`
	DocumentID      string `bson:"document_id" json:"documentId"`
	Anchor          Anchor `bson:"anchor" json:"anchor"`
	Author          string `bson:"author" json:"author"`
	AuthorID        string `bson:"author_id,omitempty" json:"-"`
	AuthorAvatarURL string `bson:"author_avatar_url,omitempty" json:"authorAvatarUrl,omitempty"`
	Body            string `bson:"body" json:"body"`
	Resolved   bool      `bson:"resolved" json:"resolved"`
	ResolvedBy string    `bson:"resolved_by,omitempty" json:"resolvedBy,omitempty"`
	ResolvedAt *time.Time `bson:"resolved_at,omitempty" json:"resolvedAt,omitempty"`
	Replies    []Reply   `bson:"replies" json:"replies"`
	CreatedAt  time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt  time.Time `bson:"updated_at" json:"updatedAt"`
}
