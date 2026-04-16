// Package crawler provides SEO metadata extraction from HTML documents.
package crawler

import (
	"strings"

	"golang.org/x/net/html"
)

// extractSEO parses an HTML document and extracts basic SEO metadata.
// It looks for <title>, <meta name="description">, and <h1> tags.
// Returns an SEO struct with populated fields; boolean flags indicate presence.
func extractSEO(doc *html.Node) *SEO {
	seo := &SEO{}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				// Extract text content from <title> tag.
				if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					seo.Title = html.UnescapeString(strings.TrimSpace(n.FirstChild.Data))
					seo.HasTitle = seo.Title != ""
				}

			case "meta":
				// Look for <meta name="description" content="...">.
				var name, content string
				for _, attr := range n.Attr {
					switch attr.Key {
					case "name":
						name = strings.ToLower(attr.Val)
					case "content":
						content = attr.Val
					}
				}
				if name == "description" && content != "" {
					seo.Description = html.UnescapeString(strings.TrimSpace(content))
					seo.HasDescription = true
				}

			case "h1":
				// Record presence of a non-empty <h1> tag.
				// We only need to know if at least one exists, so skip after first.
				if !seo.HasH1 {
					var h1Text strings.Builder
					collectText(n, &h1Text)
					text := html.UnescapeString(strings.TrimSpace(h1Text.String()))
					if text != "" {
						seo.HasH1 = true
					}
				}
			}
		}
		// Recurse into child nodes.
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return seo
}

// collectText recursively accumulates text content from an HTML node and its descendants.
// It appends raw text node data to the provided strings.Builder.
func collectText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, sb)
	}
}
