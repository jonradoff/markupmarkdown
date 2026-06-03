import { forwardRef, memo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import { makeUrlTransform } from "../utils/baseUrl";

interface Props {
  content: string;
  baseUrl?: string;
}

// Extend the default sanitize schema to allow common HTML tags people put in
// READMEs: <img>, <picture>, <details>/<summary>, plus the width/height/align
// attributes those tags typically use.
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
    return (
      <div ref={ref} className="mm-prose">
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={[rehypeRaw, [rehypeSanitize, schema]]}
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
