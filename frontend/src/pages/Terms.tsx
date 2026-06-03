import { Link } from "react-router-dom";

export default function TermsPage() {
  return (
    <div className="max-w-3xl mx-auto px-6 py-10 mm-prose">
      <div className="text-xs text-muted mb-2">
        <Link to="/" className="hover:text-accent">← Home</Link>
      </div>
      <h1>Terms of Service</h1>
      <p className="text-xs text-muted">Last updated: 2026-06-03</p>

      <h2>1. What this service is</h2>
      <p>
        markupmarkdown (the "Service") is a free, lightweight web app for
        commenting on markdown files in a collaborative way. The Service is
        operated by <a href="https://metavert.io" target="_blank" rel="noreferrer">Metavert LLC</a>{" "}
        ("we", "us"). The source code is published under the MIT License; you
        are free to self-host your own copy.
      </p>

      <h2>2. Your content</h2>
      <p>
        You retain all rights to the markdown documents and comments you
        upload, paste, or generate via the Service. You grant us only the
        rights necessary to operate the Service for you — storing your
        documents and comments in our database so they can be displayed back
        to you and the people you share links with.
      </p>
      <p>
        You agree not to use the Service to upload, store, or share content
        that is unlawful, infringes others' rights, contains malware, or is
        intended to harass or harm others. We may remove content or suspend
        accounts that violate these rules, at our sole discretion.
      </p>

      <h2>3. GitHub and private repositories</h2>
      <p>
        If you sign in with GitHub and import a private repository file, the
        Service stores a copy of that file in our database and gates access
        to it behind real-time verification against the GitHub API — only
        users who currently have GitHub-side access to the source repository
        can view the cloned copy. We do not share the content of private
        documents with third parties.
      </p>

      <h2>4. AI revision and your Anthropic API key</h2>
      <p>
        The "Revise with AI" feature is optional and uses your own Anthropic
        API key. When you save a key, we encrypt it at rest using AES-256-GCM
        with a master key held only as a server-side environment variable.
        When you request a revision, we decrypt your key in memory for the
        duration of that request and use it to make a single Anthropic API
        call on your behalf. The document content, resolved comment threads,
        and revision output are transmitted to Anthropic under your account.
        Anthropic's terms apply to that usage. You may delete your stored
        API key at any time.
      </p>

      <h2>5. No warranty</h2>
      <p>
        The Service is provided "as is", without warranty of any kind,
        express or implied. We do not guarantee uninterrupted availability,
        data durability, or that the Service will meet your specific
        requirements. You are responsible for keeping your own backups of
        important documents.
      </p>

      <h2>6. Limitation of liability</h2>
      <p>
        To the maximum extent permitted by law, in no event shall Metavert
        LLC be liable for any indirect, incidental, special, consequential,
        or punitive damages, or any loss of profits or revenues, whether
        incurred directly or indirectly, or any loss of data, use, goodwill,
        or other intangible losses, resulting from your access to or use of
        (or inability to access or use) the Service.
      </p>

      <h2>7. Changes</h2>
      <p>
        We may update these Terms from time to time. Material changes will
        be reflected by updating the "Last updated" date above. Your
        continued use of the Service after a change constitutes acceptance
        of the revised Terms.
      </p>

      <h2>8. Contact</h2>
      <p>
        Questions about these Terms? Reach us at{" "}
        <a href="https://metavert.io" target="_blank" rel="noreferrer">metavert.io</a>.
      </p>
    </div>
  );
}
