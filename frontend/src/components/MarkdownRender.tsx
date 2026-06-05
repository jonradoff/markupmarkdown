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
  forwardRef<HTMLDivElement, Props>(({ content, baseUrl }, ref) => {
    const urlTransform = makeUrlTransform(baseUrl);
    // Intercept clicks on in-document anchor links (e.g. a TOC entry
    // pointing at `#section-name`). Native browser behaviour would
    // change window.location and snap-scroll to the target — but with
    // a sticky header in the layout, the heading lands hidden behind
    // it. We smooth-scroll into view and let CSS `scroll-margin-top`
    // (set on mm-prose headings in index.css) keep the heading clear
    // of the toolbar. Off-document links fall through to default.
    const onClick = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
      const anchor = (e.target as HTMLElement).closest("a");
      if (!anchor) return;
      const href = anchor.getAttribute("href") ?? "";
      if (!href.startsWith("#") || href.length < 2) return;
      const id = decodeURIComponent(href.slice(1));
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
    }, []);
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

MarkdownRender.displayName = "MarkdownRender";
export default MarkdownRender;
