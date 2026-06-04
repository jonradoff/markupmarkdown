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

	// SourceSHA is the GitHub blob SHA of the source file at ingest time.
	// When non-empty and a fresh check returns a different SHA, the doc has
	// drifted from upstream — we surface a banner and offer "Open latest"
	// which creates a new revision with re-anchored comments.
	SourceSHA       string     `bson:"source_sha,omitempty" json:"sourceSha,omitempty"`
	SourceCheckedAt *time.Time `bson:"source_checked_at,omitempty" json:"sourceCheckedAt,omitempty"`
	// SourceLatestSHA + SourceDriftedAt are populated when the cached check
	// detects upstream drift. Cleared once the user opens a revision built
	// from the latest content (or dismisses the banner).
	SourceLatestSHA string     `bson:"source_latest_sha,omitempty" json:"sourceLatestSha,omitempty"`
	SourceDriftedAt *time.Time `bson:"source_drifted_at,omitempty" json:"sourceDriftedAt,omitempty"`

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
// NotificationKind enumerates what the user is being told about.
type NotificationKind string

const (
	NotifyMention NotificationKind = "mention"
	NotifyReply   NotificationKind = "reply"
)

// Notification is an in-app pulled-by-the-bell-icon record.
type Notification struct {
	ID             string           `bson:"_id" json:"id"`
	UserID         string           `bson:"user_id" json:"-"`
	Kind           NotificationKind `bson:"kind" json:"kind"`
	DocumentID     string           `bson:"document_id" json:"documentId"`
	DocumentTitle  string           `bson:"document_title" json:"documentTitle"`
	CommentID      string           `bson:"comment_id" json:"commentId"`
	ActorID        string           `bson:"actor_id" json:"-"`
	ActorName      string           `bson:"actor_name" json:"actorName"`
	ActorAvatarURL string           `bson:"actor_avatar_url,omitempty" json:"actorAvatarUrl,omitempty"`
	Preview        string           `bson:"preview" json:"preview"`
	CreatedAt      time.Time        `bson:"created_at" json:"createdAt"`
	ReadAt         *time.Time       `bson:"read_at,omitempty" json:"readAt,omitempty"`
}

// TokenScope is the privilege level a Personal Access Token grants.
//
// The hierarchy is admin > write > read:
//   - read:  list docs, read comments, list mention candidates,
//            list notifications. Cannot write anything.
//   - write: read + add comments / replies, resolve threads, run
//            revise_with_ai in preview mode. Cannot delete documents
//            or accept AI revisions.
//   - admin: write + delete documents, accept AI revisions (which
//            creates a new child document), edit other authored fields.
type TokenScope string

const (
	TokenScopeRead  TokenScope = "read"
	TokenScopeWrite TokenScope = "write"
	TokenScopeAdmin TokenScope = "admin"
)

// AllowsScope returns true if `have` is at least as privileged as `need`.
func (s TokenScope) AllowsScope(need TokenScope) bool {
	rank := map[TokenScope]int{TokenScopeRead: 1, TokenScopeWrite: 2, TokenScopeAdmin: 3}
	return rank[s] >= rank[need]
}

// APIToken is a per-user Personal Access Token. Used to authenticate REST
// and MCP calls from agents (or scripts that can't carry the session
// cookie). Content created via a token is always treated as agent-authored
// — the token's Label is the agent's identity in the UI; the token owner
// is shown on hover as the accountable human.
//
// We store only SHA-256(token), never the plaintext.
type APIToken struct {
	ID         string     `bson:"_id" json:"id"`
	UserID     string     `bson:"user_id" json:"-"`
	Hash       string     `bson:"hash" json:"-"`
	Prefix     string     `bson:"prefix" json:"prefix"` // first 12 chars of token (e.g. "mmk_a3f7c2…")
	Label      string     `bson:"label" json:"label"`
	Scope      TokenScope `bson:"scope,omitempty" json:"scope"`
	CreatedAt  time.Time  `bson:"created_at" json:"createdAt"`
	ExpiresAt  *time.Time `bson:"expires_at,omitempty" json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `bson:"last_used_at,omitempty" json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `bson:"revoked_at,omitempty" json:"-"`
}

// TokenEvent is one entry in a per-token activity log. We sample
// (~once/minute per token per action) so the collection stays small.
type TokenEvent struct {
	ID         string    `bson:"_id" json:"id"`
	TokenID    string    `bson:"token_id" json:"-"`
	UserID     string    `bson:"user_id" json:"-"`
	Action     string    `bson:"action" json:"action"`
	DocumentID string    `bson:"document_id,omitempty" json:"documentId,omitempty"`
	At         time.Time `bson:"at" json:"at"`
}

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

// ActorKind distinguishes human-authored from agent-authored content. We
// stamp it at write time from the auth source (Bearer token marked is_agent
// vs cookie session). UI uses this to surface a bot badge so humans can
// instantly tell who's who in a thread.
type ActorKind string

const (
	ActorHuman ActorKind = "human"
	ActorAgent ActorKind = "agent"
)

type Reply struct {
	ID              string    `bson:"id" json:"id"`
	Author          string    `bson:"author" json:"author"`
	AuthorID        string    `bson:"author_id,omitempty" json:"-"`
	AuthorAvatarURL string    `bson:"author_avatar_url,omitempty" json:"authorAvatarUrl,omitempty"`
	ActorKind       ActorKind `bson:"actor_kind,omitempty" json:"actorKind,omitempty"`
	// TokenID identifies the API token used to create agent content. The
	// display fields (Author, OwnerName, OwnerLogin) are RESOLVED at read
	// time from the current token + owner records, so renaming a token
	// updates everywhere it has commented.
	TokenID    string `bson:"token_id,omitempty" json:"-"`
	OwnerName  string `bson:"-" json:"ownerName,omitempty"`
	OwnerLogin string `bson:"-" json:"ownerLogin,omitempty"`
	Body            string    `bson:"body" json:"body"`
	BodyHTML        string    `bson:"-" json:"bodyHtml,omitempty"`
	// Mine is computed at read time: true when the viewer is the human
	// behind this reply — either as the direct author or as the owner of
	// the bot/token that wrote it. Drives the edit/delete affordances in
	// the UI; never persisted.
	Mine            bool      `bson:"-" json:"mine,omitempty"`
	CreatedAt       time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt       time.Time `bson:"updated_at" json:"updatedAt"`
}

type Comment struct {
	ID              string    `bson:"_id" json:"id"`
	DocumentID      string    `bson:"document_id" json:"documentId"`
	Anchor          Anchor    `bson:"anchor" json:"anchor"`
	Author          string    `bson:"author" json:"author"`
	AuthorID        string    `bson:"author_id,omitempty" json:"-"`
	AuthorAvatarURL string    `bson:"author_avatar_url,omitempty" json:"authorAvatarUrl,omitempty"`
	ActorKind       ActorKind `bson:"actor_kind,omitempty" json:"actorKind,omitempty"`
	TokenID         string    `bson:"token_id,omitempty" json:"-"`
	OwnerName       string    `bson:"-" json:"ownerName,omitempty"`
	OwnerLogin      string    `bson:"-" json:"ownerLogin,omitempty"`
	Body            string    `bson:"body" json:"body"`
	BodyHTML        string    `bson:"-" json:"bodyHtml,omitempty"` // populated only when render=html requested
	Resolved   bool      `bson:"resolved" json:"resolved"`
	ResolvedBy string    `bson:"resolved_by,omitempty" json:"resolvedBy,omitempty"`
	ResolvedAt *time.Time `bson:"resolved_at,omitempty" json:"resolvedAt,omitempty"`
	Replies    []Reply   `bson:"replies" json:"replies"`
	// Orphan is true when the source document changed and we could not
	// unambiguously re-anchor this comment in the new content (zero
	// matches, multiple matches, or the user/agent created it as a
	// doc-level comment with no text anchor). Orphan comments render in
	// a dedicated section at the bottom of the doc and offer a manual
	// re-anchor flow. Stored so the orphan state survives reloads even
	// without a new SHA check.
	Orphan        bool   `bson:"orphan,omitempty" json:"orphan,omitempty"`
	// OriginalExact preserves the quoted text from before the re-anchor
	// attempt failed. The current Anchor.Exact still reflects the last
	// successful match; OriginalExact is what we render in the orphan
	// card's "previously highlighted" blockquote so reviewers know what
	// the comment was about.
	OriginalExact string `bson:"original_exact,omitempty" json:"originalExact,omitempty"`
	// Mine is computed at read time. See Reply.Mine for semantics.
	Mine       bool      `bson:"-" json:"mine,omitempty"`
	CreatedAt  time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt  time.Time `bson:"updated_at" json:"updatedAt"`
}
