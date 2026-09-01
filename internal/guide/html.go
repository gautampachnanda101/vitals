package guide

import (
	"fmt"
	"html"
	"strings"
)

// RenderHTML converts md into a complete, self-contained HTML page — no
// external stylesheet, font, or script, matching vitals' own no-phone-home
// stance even for its local docs server. Any browser renders it natively;
// nothing needs a Markdown viewer extension. Every H2/H3 gets an anchor ID
// derived from its own text, and a table of contents linking to them is
// inserted right after the page title.
func RenderHTML(md, title string) string {
	headings := extractHeadings(md)
	body := renderBodyHTML(md, headings)
	body = insertTOC(body, buildTOC(headings))

	return "<!doctype html>\n" + fmt.Sprintf(pageTemplate, html.EscapeString(title), body)
}

// heading is one H1/H2/H3 line, in document order.
type heading struct {
	Level int
	Text  string
	Slug  string
}

// extractHeadings scans md for headings outside fenced code blocks and
// assigns each a slug, de-duplicating collisions the same way GitHub does
// (a repeated slug gets -1, -2, ... appended).
func extractHeadings(md string) []heading {
	var out []heading
	seen := map[string]int{}
	inFence := false

	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		var level int
		var text string
		switch {
		case strings.HasPrefix(t, "### "):
			level, text = 3, strings.TrimPrefix(t, "### ")
		case strings.HasPrefix(t, "## "):
			level, text = 2, strings.TrimPrefix(t, "## ")
		case strings.HasPrefix(t, "# "):
			level, text = 1, strings.TrimPrefix(t, "# ")
		default:
			continue
		}

		slug := slugify(text)
		if n, dup := seen[slug]; dup {
			seen[slug] = n + 1
			slug = fmt.Sprintf("%s-%d", slug, n+1)
		} else {
			seen[slug] = 0
		}
		out = append(out, heading{Level: level, Text: text, Slug: slug})
	}
	return out
}

// slugify turns heading text into a GitHub-style anchor: lowercase,
// alphanumerics kept as-is, runs of anything else collapsed to a single
// hyphen, and no leading or trailing hyphen.
func slugify(s string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
		default:
			pendingHyphen = true
		}
	}
	return b.String()
}

// renderBodyHTML renders md's body, attaching each heading's precomputed
// slug (in the same document order extractHeadings produced) as its id.
func renderBodyHTML(md string, headings []heading) string {
	var body strings.Builder
	inFence := false
	inList := false
	var para []string
	hi := 0
	nextSlug := func() string {
		if hi < len(headings) {
			s := headings[hi].Slug
			hi++
			return s
		}
		return ""
	}

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		body.WriteString("<p>" + renderInlineHTML(strings.Join(para, " ")) + "</p>\n")
		para = nil
	}
	closeList := func() {
		if inList {
			body.WriteString("</ul>\n")
			inList = false
		}
	}

	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimRight(line, " ")

		if strings.HasPrefix(strings.TrimSpace(trimmed), "```") {
			if !inFence {
				flushPara()
				closeList()
				body.WriteString("<pre><code>")
			} else {
				body.WriteString("</code></pre>\n")
			}
			inFence = !inFence
			continue
		}
		if inFence {
			body.WriteString(html.EscapeString(line) + "\n")
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "### "):
			flushPara()
			closeList()
			fmt.Fprintf(&body, "<h3 id=\"%s\">%s</h3>\n", nextSlug(), renderInlineHTML(strings.TrimPrefix(trimmed, "### ")))
		case strings.HasPrefix(trimmed, "## "):
			flushPara()
			closeList()
			fmt.Fprintf(&body, "<h2 id=\"%s\">%s</h2>\n", nextSlug(), renderInlineHTML(strings.TrimPrefix(trimmed, "## ")))
		case strings.HasPrefix(trimmed, "# "):
			flushPara()
			closeList()
			fmt.Fprintf(&body, "<h1 id=\"%s\">%s</h1>\n", nextSlug(), renderInlineHTML(strings.TrimPrefix(trimmed, "# ")))
		case strings.HasPrefix(trimmed, "- "):
			flushPara()
			if !inList {
				body.WriteString("<ul>\n")
				inList = true
			}
			fmt.Fprintf(&body, "<li>%s</li>\n", renderInlineHTML(strings.TrimPrefix(trimmed, "- ")))
		case trimmed == "":
			flushPara()
			closeList()
		default:
			para = append(para, trimmed)
		}
	}
	flushPara()
	closeList()
	return body.String()
}

// buildTOC renders a nested table of contents from every H2 (top level) and
// H3 (nested under the H2 before it) heading. The page's own H1 title is
// excluded — a TOC entry pointing at the page you're already on says nothing.
func buildTOC(headings []heading) string {
	var b strings.Builder
	b.WriteString(`<nav class="toc"><h2>Contents</h2><ul>`)
	b.WriteByte('\n')

	openLI := false
	openSubUL := false
	for _, h := range headings {
		switch h.Level {
		case 2:
			if openSubUL {
				b.WriteString("</ul>")
				openSubUL = false
			}
			if openLI {
				b.WriteString("</li>\n")
			}
			fmt.Fprintf(&b, `<li><a href="#%s">%s</a>`, h.Slug, html.EscapeString(h.Text))
			openLI = true
		case 3:
			if !openSubUL {
				b.WriteString("<ul>")
				openSubUL = true
			}
			fmt.Fprintf(&b, `<li><a href="#%s">%s</a></li>`, h.Slug, html.EscapeString(h.Text))
		}
	}
	if openSubUL {
		b.WriteString("</ul>")
	}
	if openLI {
		b.WriteString("</li>\n")
	}
	b.WriteString("</ul></nav>\n")
	return b.String()
}

// insertTOC places toc right after the page's H1, or at the very start if
// there is no H1 to anchor it to.
func insertTOC(body, toc string) string {
	const marker = "</h1>\n"
	if idx := strings.Index(body, marker); idx >= 0 {
		at := idx + len(marker)
		return body[:at] + toc + body[at:]
	}
	return toc + body
}

// renderInlineHTML escapes text for safe HTML output, then reintroduces the
// three inline constructs vitals' own docs use as real tags. Escaping runs
// first so the tags inserted afterward are the only markup in the result.
func renderInlineHTML(s string) string {
	s = html.EscapeString(s)
	s = reLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = reCode.ReplaceAllString(s, `<code>$1</code>`)
	s = reBold.ReplaceAllString(s, `<strong>$1</strong>`)
	return s
}

// pageTemplate is a minimal, self-contained page: system fonts only, a
// prefers-color-scheme dark variant, no external resources of any kind.
const pageTemplate = `<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  :root { color-scheme: light dark; }
  body {
    max-width: 46rem; margin: 2.5rem auto; padding: 0 1.5rem;
    font: 16px/1.6 -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    color: #1b1f23; background: #ffffff;
  }
  h1, h2, h3 { line-height: 1.25; }
  h1 { border-bottom: 2px solid #2b5d53; padding-bottom: .3rem; }
  h2 { border-bottom: 1px solid #d9d7d0; padding-bottom: .2rem; margin-top: 2.2rem; }
  code { font: 0.9em ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; background: #f0efe9; padding: .1em .35em; border-radius: 3px; }
  pre { background: #f0efe9; padding: .9rem 1rem; border-radius: 6px; overflow-x: auto; }
  pre code { background: none; padding: 0; }
  a { color: #2b5d53; }
  ul { padding-left: 1.4rem; }
  li { margin: .25rem 0; }
  nav.toc { background: #f7f6f2; border: 1px solid #e3e1db; border-radius: 6px; padding: .9rem 1.3rem; margin: 1.5rem 0 2rem; }
  nav.toc h2 { margin: 0 0 .5rem; border: none; padding: 0; font-size: 1rem; text-transform: uppercase; letter-spacing: .04em; color: #6b6f76; }
  nav.toc ul { margin: 0; }
  nav.toc > ul { padding-left: 1.1rem; }
  nav.toc li { margin: .15rem 0; }
  @media (prefers-color-scheme: dark) {
    body { color: #e9eaea; background: #14171a; }
    code, pre { background: #20252a; }
    h2 { border-bottom-color: #2a2e33; }
    a { color: #6fbfa8; }
    nav.toc { background: #1b1f23; border-color: #2a2e33; }
    nav.toc h2 { color: #9aa0a6; }
  }
</style>
</head>
<body>
%s</body>
</html>
`
