// Package render provides server-side markdown helpers:
//
//   - HTMLComment: renders a comment body to sanitized HTML (used by the
//     ?render=html query and MCP tools)
//   - PlainText: extracts the readable text content of a markdown document
//     (used to anchor agent-supplied comments by text-substring)
package render

import (
	"bytes"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var (
	mdComment = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	sanitizer = func() *bluemonday.Policy {
		// Start from the UGC policy (allows a sensible default set of
		// inline + block elements) and then lock down a couple of edges.
		p := bluemonday.UGCPolicy()
		p.AllowAttrs("class").OnElements("code", "pre", "span")
		// Force rel/target on external links.
		p.RequireParseableURLs(true)
		p.RequireNoFollowOnLinks(true)
		p.AddTargetBlankToFullyQualifiedLinks(true)
		return p
	}()
)

// HTMLComment renders a markdown comment body to sanitized HTML. Newlines
// in the source are preserved as <br> via GFM.
func HTMLComment(body string) string {
	if body == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := mdComment.Convert([]byte(body), &buf); err != nil {
		return ""
	}
	return sanitizer.Sanitize(buf.String())
}

// PlainText returns a best-effort plain-text rendering of markdown source.
// Used by the MCP add_comment tool to anchor agent-supplied comments by
// text substring rather than character offsets.
//
// We deliberately mimic the textContent the browser sees from the
// react-markdown render: block elements emit their text in document order,
// hard breaks produce a newline, code blocks contribute their content.
func PlainText(source string) string {
	src := []byte(source)
	root := mdComment.Parser().Parse(text.NewReader(src))
	var sb strings.Builder
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			// Block-level close: introduce a newline so paragraphs separate.
			switch n.(type) {
			case *ast.Paragraph, *ast.Heading, *ast.ListItem,
				*ast.FencedCodeBlock, *ast.CodeBlock, *ast.Blockquote,
				*ast.ThematicBreak, *ast.HTMLBlock:
				sb.WriteByte('\n')
			}
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Text:
			sb.Write(v.Segment.Value(src))
			if v.HardLineBreak() {
				sb.WriteByte('\n')
			} else if v.SoftLineBreak() {
				sb.WriteByte(' ')
			}
		case *ast.String:
			sb.Write(v.Value)
		case *ast.CodeSpan:
			// Children are Text nodes; let walker emit them.
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			l := n.Lines().Len()
			for i := 0; i < l; i++ {
				line := n.Lines().At(i)
				sb.Write(line.Value(src))
			}
			return ast.WalkSkipChildren, nil
		case *ast.AutoLink:
			sb.Write(v.URL(src))
		case *ast.Image:
			// images contribute their alt text
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

// FindOccurrence returns the start and end byte offsets of the nth (1-based)
// occurrence of `needle` in `haystack`. Returns (-1, -1) if not found.
func FindOccurrence(haystack, needle string, n int) (int, int) {
	if needle == "" || n < 1 {
		return -1, -1
	}
	idx := 0
	for i := 0; i < n; i++ {
		j := strings.Index(haystack[idx:], needle)
		if j < 0 {
			return -1, -1
		}
		idx += j
		if i == n-1 {
			return idx, idx + len(needle)
		}
		idx += len(needle)
	}
	return -1, -1
}

// CountOccurrences is FindOccurrence's safety net — used by MCP to error
// helpfully when an agent supplies an ambiguous substring.
func CountOccurrences(haystack, needle string) int {
	if needle == "" {
		return 0
	}
	return strings.Count(haystack, needle)
}
