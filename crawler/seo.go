// Package crawler provides SEO metadata extraction from HTML documents.
package crawler

import (
	"strings"

	"golang.org/x/net/html"
)

// extractSEO parses an HTML document and extracts basic SEO metadata.
// It looks for <title>, <meta name="description">, and <h1> tags.
func extractSEO(doc *html.Node) *SEO {
	seo := &SEO{}

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
			extractTitle(n, seo)
		case "meta":
			extractMetaDescription(n, seo)
		case "h1":
			extractH1(n, seo)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return seo
}

// extractTitle extracts text content from a <title> element.
func extractTitle(n *html.Node, seo *SEO) {
	if n.FirstChild == nil || n.FirstChild.Type != html.TextNode {
		return
	}
	text := html.UnescapeString(strings.TrimSpace(n.FirstChild.Data))
	if text != "" {
		seo.Title = text
		seo.HasTitle = true
	}
}

// extractMetaDescription extracts content from <meta name="description">.
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
func extractH1(n *html.Node, seo *SEO) {
	if seo.HasH1 {
		return // уже нашли, можно не искать дальше
	}
	var h1Text strings.Builder
	collectText(n, &h1Text)
	text := html.UnescapeString(strings.TrimSpace(h1Text.String()))
	if text != "" {
		seo.HasH1 = true
	}
}

// collectText recursively accumulates text content from an HTML node and its descendants.
func collectText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, sb)
	}
}
