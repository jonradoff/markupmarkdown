import { Link } from "react-router-dom";

export default function PrivacyPage() {
  return (
    <div className="max-w-3xl mx-auto px-6 py-10 mm-prose">
      <div className="text-xs text-muted mb-2">
        <Link to="/" className="hover:text-accent">← Home</Link>
      </div>
      <h1>Privacy Policy</h1>
      <p className="text-xs text-muted">Last updated: 2026-06-03</p>

      <p>
        markupmarkdown is operated by{" "}
        <a href="https://metavert.io" target="_blank" rel="noreferrer">Metavert LLC</a>{" "}
        ("we", "us"). This policy explains what data we collect and what we
        do with it. We've tried to write it for technical readers, in plain
        language.
      </p>

      <h2>What we store</h2>
      <ul>
        <li>
          <strong>Documents you create</strong> — the markdown content, the
          source URL (if any), and metadata (title, created/updated
          timestamps, whether it's a private GitHub source).
        </li>
        <li>
          <strong>Comments and replies</strong> — text, author name, anchor
          information (which span of the document the comment refers to),
          timestamps, resolved-by attribution.
        </li>
        <li>
          <strong>GitHub identity</strong> (only if you sign in with GitHub)
          — your GitHub user ID, login, display name, email, avatar URL, and
          an OAuth access token used to verify your access to private repos
          and to fetch private files on your behalf. The OAuth token is
          stored in our database but never returned to the client.
        </li>
        <li>
          <strong>Anthropic API key</strong> (only if you save one for the
          AI revision feature) — encrypted at rest using AES-256-GCM with a
          random nonce per encryption. The 32-byte master key is held only
          as a server-side environment variable, never in the database.
          Decrypted only in memory for the duration of a revision request.
          You can delete it at any time.
        </li>
        <li>
          <strong>Sessions</strong> — a randomly generated session ID stored
          in an HttpOnly cookie, plus a MongoDB record mapping the ID to
          your user. Expires after 30 days of inactivity.
        </li>
      </ul>

      <h2>What we don't do</h2>
      <ul>
        <li>
          No third-party analytics, no tracking pixels, no advertising
          cookies. The only cookie set by the Service is a session ID.
        </li>
        <li>
          No telemetry or behavioral tracking sent to external services.
        </li>
        <li>
          No sale or sharing of personal data with third parties for their
          own purposes.
        </li>
        <li>
          No logging of document or comment content beyond what is needed to
          serve and store it. Standard server access logs (request method,
          URL path, status, response time, IP address) are kept for
          operational purposes for up to 30 days.
        </li>
      </ul>

      <h2>Third parties we use</h2>
      <ul>
        <li>
          <strong>MongoDB Atlas</strong> — our database host. All
          documents, comments, and user records live here.
        </li>
        <li>
          <strong>Fly.io</strong> — our hosting provider. They handle
          request routing and operate the machines the Service runs on.
        </li>
        <li>
          <strong>GitHub</strong> — only when you sign in or when we fetch
          a markdown file from github.com on your behalf.
        </li>
        <li>
          <strong>Anthropic</strong> — only when you use the "Revise with
          AI" feature. The document content, resolved comment threads, and
          revision output are sent to Anthropic under <em>your</em> API key.
          Their privacy practices apply to that usage.
        </li>
      </ul>

      <h2>Sharing and access</h2>
      <p>
        Documents created from a public source (a public GitHub URL or a
        local upload) are accessible to anyone with the document's URL.
        Documents cloned from a private source are gated — only users who
        currently have GitHub-side read access to the original repository
        can view the document or its comments.
      </p>

      <h2>Deletion</h2>
      <p>
        You can delete any document you've opened via the Delete button on
        the document page; this removes the document and all its comments
        from the database. To delete your stored Anthropic API key, use the
        Anthropic API key entry in your avatar menu. To delete your GitHub
        session, sign out from the avatar menu. For complete account
        deletion (your user record, all sessions, all stored secrets),
        contact us via <a href="https://metavert.io" target="_blank" rel="noreferrer">metavert.io</a>.
      </p>

      <h2>Cookies</h2>
      <p>
        We set one cookie: <code>mm_session</code>, HttpOnly, Lax SameSite,
        Secure in production. It holds a session ID only. No third-party
        cookies are set by the Service.
      </p>

      <h2>Changes</h2>
      <p>
        We may update this Privacy Policy from time to time. Material
        changes will be reflected by updating the "Last updated" date above.
      </p>

      <h2>Contact</h2>
      <p>
        Questions about this policy or requests for data export or deletion?
        Reach us at{" "}
        <a href="https://metavert.io" target="_blank" rel="noreferrer">metavert.io</a>.
      </p>
    </div>
  );
}
