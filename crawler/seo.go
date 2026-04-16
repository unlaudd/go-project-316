// Package crawler provides SEO metadata extraction from HTML documents.
package crawler

import (
	"strings"

	"golang.org/x/net/html"
)

// extractSEO parses an HTML document and extracts basic SEO metadata.
// It looks for <title>, <meta name="description">, and <h1> tags.
// Returns an SEO value with extracted data (empty fields if not found).
func extractSEO(doc *html.Node) SEO {
	seo := SEO{}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		switch n.Data {
		case "title":
			extractTitle(n, &seo)
		case "meta":
			extractMetaDescription(n, &seo)
		case "h1":
			extractH1(n, &seo)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return seo
}

// extractTitle extracts text content from a <title> element.
// It sets HasTitle and Title fields in the provided SEO struct if a non-empty title is found.
// The function returns early if HasTitle is already true to avoid overwriting.
func extractTitle(n *html.Node, seo *SEO) {
	if seo.HasTitle {
		return
	}
	if n.FirstChild == nil || n.FirstChild.Type != html.TextNode {
		return
	}
	text := html.UnescapeString(strings.TrimSpace(n.FirstChild.Data))
	if text != "" {
		seo.Title = text
		seo.HasTitle = true
	}
}

// extractMetaDescription extracts content from a <meta name="description"> tag.
// It sets HasDescription and Description fields in the provided SEO struct if found.
// The name attribute is compared case-insensitively.
func extractMetaDescription(n *html.Node, seo *SEO) {
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
}

// extractH1 checks for the presence of a non-empty <h1> element.
// It sets HasH1 to true if at least one <h1> tag with non-empty text content is found.
func extractH1(n *html.Node, seo *SEO) {
	if seo.HasH1 {
		return
	}
	var h1Text strings.Builder
	collectText(n, &h1Text)
	text := html.UnescapeString(strings.TrimSpace(h1Text.String()))
	if text != "" {
		seo.HasH1 = true
	}
}

// collectText recursively accumulates text content from an HTML node and its descendants.
// It appends raw text node data to the provided strings.Builder without unescaping.
func collectText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, sb)
	}
}
