import { forwardRef, memo, useCallback } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import rehypeSlug from "rehype-slug";
import { makeUrlTransform } from "../utils/baseUrl";

interface Props {
  content: string;
  baseUrl?: string;
  /** The doc's canonical GitHub source URL (if any). Used to detect
   * "same-document" hyperlinks written as fully-qualified URLs —
   * `[Section](https://github.com/owner/repo/blob/ref/file.md#anchor)`
   * is the same physical file as the doc the user is currently
   * reading, so the click should scroll within the page rather than
   * navigating away to github.com. */
  sourceUrl?: string;
}

// Extend the default sanitize schema to allow common HTML tags people put in
// READMEs: <img>, <picture>, <details>/<summary>, plus the width/height/align
// attributes those tags typically use. `id` on headings is allow-listed so
// rehype-slug's generated ids survive sanitization — that's what makes
// in-document anchor links ([Section](#section)) jump to the right place.
const schema = {
  ...defaultSchema,
  tagNames: [
    ...(defaultSchema.tagNames ?? []),
    "picture",
    "source",
    "details",
    "summary",
    "kbd",
    "sub",
    "sup",
  ],
  attributes: {
    ...defaultSchema.attributes,
    h1: [...((defaultSchema.attributes?.h1 as unknown[]) ?? []), "id"],
    h2: [...((defaultSchema.attributes?.h2 as unknown[]) ?? []), "id"],
    h3: [...((defaultSchema.attributes?.h3 as unknown[]) ?? []), "id"],
    h4: [...((defaultSchema.attributes?.h4 as unknown[]) ?? []), "id"],
    h5: [...((defaultSchema.attributes?.h5 as unknown[]) ?? []), "id"],
    h6: [...((defaultSchema.attributes?.h6 as unknown[]) ?? []), "id"],
    img: [
      ...((defaultSchema.attributes?.img as unknown[]) ?? []),
      "width",
      "height",
      "align",
      "srcset",
    ],
    source: ["srcset", "media", "type"],
    "*": [
      ...((defaultSchema.attributes?.["*"] as unknown[]) ?? []),
      "align",
    ],
  },
};

const MarkdownRender = memo(
  forwardRef<HTMLDivElement, Props>(({ content, baseUrl, sourceUrl }, ref) => {
    const urlTransform = makeUrlTransform(baseUrl);
    // Intercept clicks on in-document anchor links so they scroll
    // within the page instead of triggering a full reload. Three URL
    // shapes count as "same document":
    //   1. `#section-name` — bare fragment (toc / readme convention)
    //   2. `https://github.com/owner/repo/blob/ref/file.md#anchor` —
    //      fully-qualified GitHub URL pointing at the source the doc
    //      was cloned from. Common in markdown authored on GitHub.
    //   3. `https://mumd.metavert.io/owner/repo/blob/ref/file.md#anchor`
    //      — the same doc but expressed as our human URL.
    // Native browser behaviour would change window.location and snap-
    // scroll to the target — but with a sticky header in the layout,
    // the heading lands hidden behind it. We smooth-scroll into view
    // and let CSS `scroll-margin-top` (set on mm-prose headings in
    // index.css) keep the heading clear of the toolbar. Off-document
    // links fall through to default.
    const onClick = useCallback(
      (e: React.MouseEvent<HTMLDivElement>) => {
        const anchor = (e.target as HTMLElement).closest("a");
        if (!anchor) return;
        const href = anchor.getAttribute("href") ?? "";
        if (!href) return;

        let id = "";
        if (href.startsWith("#") && href.length > 1) {
          id = decodeURIComponent(href.slice(1));
        } else {
          // Try to interpret the href as a fully-qualified URL and
          // detect whether it points at the same doc we're rendering.
          // Resolve against the page URL so relative paths like
          // `./WINGMAN_PRD.md#section` work too.
          let linkURL: URL;
          try {
            linkURL = new URL(href, window.location.href);
          } catch {
            return;
          }
          if (!linkURL.hash || linkURL.hash.length < 2) return;
          if (!isSameDoc(linkURL, sourceUrl)) return;
          id = decodeURIComponent(linkURL.hash.slice(1));
        }

        const root = e.currentTarget;
        // CSS.escape isn't perfect for ids that begin with a digit, but
        // getElementById sidesteps that entirely and is scoped to the
        // document — fine because rehype-slug makes ids unique per
        // heading and our docs only have one MarkdownRender at a time.
        const target = document.getElementById(id);
        if (!target || !root.contains(target)) return;
        e.preventDefault();
        target.scrollIntoView({ behavior: "smooth", block: "start" });
        // Update the URL hash so the back button works without re-navigating
        // through React Router.
        if (window.history && window.history.replaceState) {
          window.history.replaceState(null, "", `#${id}`);
        }
      },
      [sourceUrl],
    );
    return (
      <div ref={ref} className="mm-prose" onClick={onClick}>
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={[rehypeSlug, rehypeRaw, [rehypeSanitize, schema]]}
          urlTransform={urlTransform}
        >
          {content}
        </ReactMarkdown>
      </div>
    );
  })
);

// isSameDoc returns true when `linkURL` points at the same physical
// file as the doc currently being rendered. Three matching shapes:
//   • points at this exact page (mumd.metavert.io/<same path>)
//   • points at the doc's GitHub source URL (sourceUrl, github.com/…)
//   • points at the raw.githubusercontent.com derivation of the source
// We strip the leading slash and lowercase the host before comparing —
// GitHub URLs are case-insensitive on the owner/repo segments.
function isSameDoc(linkURL: URL, sourceUrl?: string): boolean {
  const linkHost = linkURL.host.toLowerCase();
  const linkPath = linkURL.pathname.replace(/\/+$/, "");
  if (
    linkHost === window.location.host.toLowerCase() &&
    linkPath === window.location.pathname.replace(/\/+$/, "")
  ) {
    return true;
  }
  if (!sourceUrl) return false;
  try {
    const src = new URL(sourceUrl);
    const srcHost = src.host.toLowerCase();
    const srcPath = src.pathname.replace(/\/+$/, "");
    if (linkHost === srcHost && linkPath.toLowerCase() === srcPath.toLowerCase()) {
      return true;
    }
    // Also match the raw.githubusercontent.com form, which is what
    // urlTransform may have produced for relative links inside the doc.
    const m = sourceUrl.match(
      /^https:\/\/github\.com\/([^/]+)\/([^/]+)\/blob\/([^/]+)\/(.+)$/,
    );
    if (m && linkHost === "raw.githubusercontent.com") {
      const rawPath = `/${m[1]}/${m[2]}/${m[3]}/${m[4]}`.toLowerCase();
      if (linkPath.toLowerCase() === rawPath) return true;
    }
  } catch {
    /* malformed sourceUrl — fall through to "not same doc" */
  }
  return false;
}

MarkdownRender.displayName = "MarkdownRender";
export default MarkdownRender;
