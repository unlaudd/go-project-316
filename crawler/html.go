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
// Links are normalized and deduplicated to prevent checking the same resource twice.
func extractLinks(baseURL string, doc *html.Node) []string {
	var links []string
	base, err := url.Parse(baseURL)
	if err != nil {
		return links
	}

	seen := make(map[string]struct{})

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					// 🆕 Вызываем вынесенную функцию обработки ссылки
					if link := processLink(attr.Val, base, seen); link != "" {
						links = append(links, link)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links
}

// processLink validates, normalizes, and deduplicates a single link URL.
// Returns the normalized URL if valid and not seen before, or empty string otherwise.
func processLink(href string, base *url.URL, seen map[string]struct{}) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}

	linkURL, err := url.Parse(href)
	if err != nil {
		return ""
	}

	// Ignore unsupported schemes
	if !supportedSchemes[linkURL.Scheme] && linkURL.Scheme != "" {
		return ""
	}

	// Resolve relative URLs
	if linkURL.Scheme == "" {
		linkURL = base.ResolveReference(linkURL)
	}

	if !linkURL.IsAbs() || !supportedSchemes[linkURL.Scheme] {
		return ""
	}

	// Normalize URL: remove fragment, strip trailing slash
	linkURL.Fragment = ""
	linkURL.Path = strings.TrimSuffix(linkURL.Path, "/")
	if linkURL.Path == "" {
		linkURL.Path = "/"
	}
	norm := linkURL.String()

	// Deduplicate
	if _, exists := seen[norm]; !exists {
		seen[norm] = struct{}{}
		return norm
	}
	return ""
}
