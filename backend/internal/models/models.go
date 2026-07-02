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
	// SourceKind discriminates between the kinds of upstream source a
	// doc can be cloned from. Newer than Origin; Origin is kept around
	// because cmd/migrate-private/main.go still reads it. New code
	// should switch on SourceKind. Values: "github_blob", "gist",
	// "url", "upload".
	SourceKind string `bson:"source_kind,omitempty" json:"sourceKind,omitempty"`
	Content    string `bson:"content" json:"content"`
	// Private is true when the source could only be read with GitHub auth.
	// Readers of the cloned copy must also have GitHub access to {Owner, Repo}.
	Private    bool   `bson:"private" json:"private"`
	GitHubOwner string `bson:"github_owner,omitempty" json:"githubOwner,omitempty"`
	GitHubRepo  string `bson:"github_repo,omitempty" json:"githubRepo,omitempty"`
	GitHubRef   string `bson:"github_ref,omitempty" json:"githubRef,omitempty"`
	GitHubPath  string `bson:"github_path,omitempty" json:"githubPath,omitempty"`

	// Gist fields, populated when SourceKind == "gist". GistCommit mirrors
	// SourceSHA's role for github blobs — both are upstream-content
	// fingerprints used by the drift-detection machinery. GistFilename
	// is which file inside the gist this doc was cloned from (the first
	// file at ingest time); GistFileCount is stored so the "this gist
	// has N more files" UI affordance doesn't re-hit the gist API on
	// every render.
	GistOwner     string `bson:"gist_owner,omitempty" json:"gistOwner,omitempty"`
	GistID        string `bson:"gist_id,omitempty" json:"gistId,omitempty"`
	GistCommit    string `bson:"gist_commit,omitempty" json:"gistCommit,omitempty"`
	GistFilename  string `bson:"gist_filename,omitempty" json:"gistFilename,omitempty"`
	GistFileCount int    `bson:"gist_file_count,omitempty" json:"gistFileCount,omitempty"`

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
	// SourceDriftIgnoredSHA records an upstream blob SHA the user
	// explicitly dismissed via the drift banner's "Ignore" button. The
	// banner stays suppressed for that SHA — and only that SHA. If a
	// *newer* upstream SHA shows up, the banner returns so the user
	// gets a chance to act on the latest change.
	SourceDriftIgnoredSHA string `bson:"source_drift_ignored_sha,omitempty" json:"sourceDriftIgnoredSha,omitempty"`

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

// SourceKind values. Use these instead of bare strings so the compiler
// catches typos. Origin is the legacy field still populated alongside.
const (
	SourceKindGitHubBlob = "github_blob"
	SourceKindGist       = "gist"
	SourceKindURL        = "url"
	SourceKindUpload     = "upload"
)

// IndexKind discriminates between the three GitHub URL shapes a
// markdown-index can target: a single repo (list every .md file in
// the tree), a user profile (top-level .md files across each repo
// the viewer can see), or an org (same, scoped to org-owned repos).
type IndexKind string

const (
	IndexKindRepo IndexKind = "repo"
	IndexKindUser IndexKind = "user"
	IndexKindOrg  IndexKind = "org"
)

// Index is a shareable listing of markdown files anchored to a GitHub
// resource. The CONTENT (file listing) is computed on view using the
// viewer's GitHub token — different viewers may see different items if
// their repo access differs, which is the desired behavior. We store
// only the source identity + minimal metadata; items are never frozen.
type Index struct {
	ID        string    `bson:"_id" json:"id"`
	Kind      IndexKind `bson:"kind" json:"kind"`
	// Owner is the GitHub login (user or org). Always set.
	Owner string `bson:"owner" json:"owner"`
	// Repo is set when Kind=="repo"; empty for user/org indexes.
	Repo string `bson:"repo,omitempty" json:"repo,omitempty"`
	// Title is the human-readable label shown in lists. Defaults to a
	// derived form (e.g., "anthropics/claude-code" or "anthropics") at
	// create-time; the user can rename it later.
	Title string `bson:"title" json:"title"`
	// SourceURL is the canonical GitHub URL the index was created from.
	SourceURL string `bson:"source_url" json:"sourceUrl"`
	// Private is true when the underlying GitHub resource required
	// authenticated access at create-time. Read handlers re-verify
	// access on every view (same model as Document.Private).
	Private bool `bson:"private" json:"private"`
	// DefaultFilter is the case-insensitive filename-filter substring
	// the creator has pinned as the default view for share-link
	// visitors. Empty = no pinned filter (visitors see "All"). A
	// visitor with their own per-(browser, index) tab choice in
	// localStorage overrides this; only first-time visitors land on
	// the pinned filter.
	DefaultFilter string `bson:"default_filter,omitempty" json:"defaultFilter,omitempty"`
	// CreatedByID is the GitHub login of the creator. Empty for
	// anonymously-created indexes (only possible for public targets).
	// Exposed in JSON so the frontend can show "Pin" / "Delete" only
	// to the creator and let everyone else see who curated the list.
	CreatedByID string     `bson:"created_by_id,omitempty" json:"createdById,omitempty"`
	DeletedAt   *time.Time `bson:"deleted_at,omitempty" json:"deletedAt,omitempty"`
	CreatedAt   time.Time  `bson:"created_at" json:"createdAt"`
	UpdatedAt   time.Time  `bson:"updated_at" json:"updatedAt"`
}

// RevisionMeta records how an AI-generated revision was produced. This is
// purely informational — used to surface "AI-revised, applied N comments" on
// the doc page and for the version-history sidebar.
//
// AncestorSourceSHA and AncestorContent capture the state of the parent
// doc at the moment the revision was generated. They are the "common
// ancestor" used by Merge to reconcile this revision with later upstream
// changes — a 3-way merge needs (ancestor, ours, theirs) where ours is
// the current child content, theirs is the new upstream content, and
// ancestor is the source content this revision was originally based on.
// Without ancestor_content, a child whose upstream changes is locked
// out of clean merging and falls back to the older replace-in-place
// behaviour.
type RevisionMeta struct {
	Model             string    `bson:"model" json:"model"`
	AppliedCommentIDs []string  `bson:"applied_comment_ids" json:"appliedCommentIds"`
	TokensIn          int64     `bson:"tokens_in" json:"tokensIn"`
	TokensOut         int64     `bson:"tokens_out" json:"tokensOut"`
	GeneratedBy       string    `bson:"generated_by" json:"generatedBy"` // display name
	GeneratedByID     string    `bson:"generated_by_id,omitempty" json:"-"`
	GeneratedAt       time.Time `bson:"generated_at" json:"generatedAt"`
	// ActorKind distinguishes a human-authored revision (manual edit
	// from the web UI, or Revise with AI from a cookie session) from
	// an agent-authored revision (any write through an MCP / REST
	// Bearer token). Mirrors Comment.ActorKind so the frontend can
	// surface the same bot badge on revisions that it already shows
	// on comments.
	ActorKind ActorKind `bson:"actor_kind,omitempty" json:"actorKind,omitempty"`
	// TokenID identifies the API token the agent revision was written
	// under. The display name (GeneratedBy) is also written so
	// non-token readers still see a sensible label.
	TokenID string `bson:"token_id,omitempty" json:"-"`
	// AncestorSourceSHA is the source_sha of the parent at the moment the
	// revision was generated. Empty for revisions created before the
	// merge engine existed.
	AncestorSourceSHA string `bson:"ancestor_source_sha,omitempty" json:"ancestorSourceSha,omitempty"`
	// AncestorContent is the verbatim parent content the AI revision was
	// based on. Used as the "common ancestor" in 3-way merge. Excluded
	// from JSON to avoid doubling the size of every doc response.
	AncestorContent string `bson:"ancestor_content,omitempty" json:"-"`
	// AcceptedAt is stamped when a human explicitly accepts an
	// agent-authored revision. Only populated when ActorKind == agent.
	// The pushback flow (pushback.go) refuses to push an agent-authored
	// revision to GitHub until it's accepted, so this is the audit
	// trail for that gate.
	AcceptedAt   *time.Time `bson:"accepted_at,omitempty" json:"acceptedAt,omitempty"`
	AcceptedByID string     `bson:"accepted_by_id,omitempty" json:"-"`
	AcceptedBy   string     `bson:"accepted_by,omitempty" json:"acceptedBy,omitempty"`
}

// ReviewState is the discrete coordination vocabulary a reviewer picks.
// Mirrors GitHub PR review states. Absence of a review is equivalent to
// no state at all (not "commented"); the "commented" state is an
// explicit signal from a reviewer who wants to record participation
// without approving or blocking. `changes_requested` blocks the
// pushback flow (see pushback.go) until dismissed by the reviewer or
// force-overridden.
type ReviewState string

const (
	ReviewStateApproved         ReviewState = "approved"
	ReviewStateChangesRequested ReviewState = "changes_requested"
	ReviewStateCommented        ReviewState = "commented"
)

// Review is a per-doc-per-user review record. Exactly one Review per
// (document_id, user_id) pair — the ID is deterministic (docID +
// ":" + userID) so upserts stay atomic without a compound index.
// Reviews live on the specific doc revision they were set on; new
// child revisions start with a fresh (empty) review surface, same as
// GitHub PRs dismiss stale reviews when new commits land.
//
// The display fields (Author, OwnerName, OwnerLogin, AuthorAvatarURL)
// are `bson:"-"` because they're resolved at read time from the
// current token + user records, so renames propagate everywhere. Same
// pattern as Comment.
type Review struct {
	ID         string      `bson:"_id" json:"-"`
	DocumentID string      `bson:"document_id" json:"documentId"`
	UserID     string      `bson:"user_id" json:"-"`
	State      ReviewState `bson:"state" json:"state"`
	Note       string      `bson:"note,omitempty" json:"note,omitempty"`

	// TokenID is populated for reviews written via a Bearer token
	// (MCP or REST). Empty for cookie-session reviews.
	TokenID         string    `bson:"token_id,omitempty" json:"-"`
	ActorKind       ActorKind `bson:"actor_kind,omitempty" json:"actorKind,omitempty"`
	Author          string    `bson:"-" json:"author,omitempty"`
	AuthorAvatarURL string    `bson:"-" json:"authorAvatarUrl,omitempty"`
	OwnerName       string    `bson:"-" json:"ownerName,omitempty"`
	OwnerLogin      string    `bson:"-" json:"ownerLogin,omitempty"`
	Mine            bool      `bson:"-" json:"mine,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt time.Time `bson:"updated_at" json:"updatedAt"`
}

// ReviewSummary is the aggregate view exposed on document responses so
// the SPA can render "3 approved / 1 requested changes" without
// listing individual reviewers.
type ReviewSummary struct {
	Approved         int `json:"approved"`
	ChangesRequested int `json:"changesRequested"`
	Commented        int `json:"commented"`
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
	// Suggestion is an optional structured edit proposal — the comment
	// says "replace the anchored text with THIS." The frontend renders
	// a one-click Apply button when present (empirically the highest-
	// actionability review artifact — see Brown & Parnin ESEC/FSE '20).
	// Applying creates a manual revision and resolves the comment.
	// Doc-level comments (Anchor.Exact == "") can't carry suggestions
	// because there's nothing to replace.
	Suggestion *Suggestion `bson:"suggestion,omitempty" json:"suggestion,omitempty"`
	// Mine is computed at read time. See Reply.Mine for semantics.
	Mine       bool      `bson:"-" json:"mine,omitempty"`
	CreatedAt  time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt  time.Time `bson:"updated_at" json:"updatedAt"`
}

// Suggestion is a structured edit proposal on an anchored comment.
// Replacement is the text that should replace the comment's Anchor.Exact
// span in the source markdown. AppliedAt / AppliedByID / AppliedBy are
// stamped when a reviewer clicks Apply, so subsequent viewers can see
// the suggestion was already used.
type Suggestion struct {
	Replacement   string     `bson:"replacement" json:"replacement"`
	AppliedAt     *time.Time `bson:"applied_at,omitempty" json:"appliedAt,omitempty"`
	AppliedByID   string     `bson:"applied_by_id,omitempty" json:"-"`
	AppliedBy     string     `bson:"applied_by,omitempty" json:"appliedBy,omitempty"`
	AppliedDocID  string     `bson:"applied_doc_id,omitempty" json:"appliedDocId,omitempty"`
}
