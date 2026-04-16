// Package crawler provides HTML parsing utilities for link extraction.
package crawler

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// supportedSchemes lists the URL schemes that the crawler processes.
// Only HTTP and HTTPS links are followed; other schemes are ignored.
var supportedSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

// extractLinks parses an HTML document and returns a list of absolute HTTP(S) links.
// Links are resolved relative to baseURL when necessary. Fragments, empty hrefs,
// and unsupported schemes are filtered out.
func extractLinks(baseURL string, doc *html.Node) []string {
	var links []string
	base, err := url.Parse(baseURL)
	if err != nil {
		return links
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					href := strings.TrimSpace(attr.Val)
					if href == "" {
						continue
					}
					// Skip fragment-only links (e.g., "#section") as they don't navigate away.
					if strings.HasPrefix(href, "#") {
						continue
					}

					linkURL, err := url.Parse(href)
					if err != nil {
						continue
					}

					// Ignore links with unsupported schemes (mailto:, ftp:, etc.).
					if !supportedSchemes[linkURL.Scheme] && linkURL.Scheme != "" {
						continue
					}

					// Resolve relative URLs against the base URL.
					if linkURL.Scheme == "" {
						linkURL = base.ResolveReference(linkURL)
					}

					// Only include absolute HTTP(S) links.
					if linkURL.IsAbs() && supportedSchemes[linkURL.Scheme] {
						links = append(links, linkURL.String())
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
	return links
}
