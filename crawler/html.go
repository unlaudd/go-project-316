// Package crawler provides HTML parsing utilities for link extraction.
package crawler

import (
	"net/url"
	"strings"
)

// supportedSchemes lists the URL schemes that the crawler processes.
// Only HTTP and HTTPS links are followed; other schemes are ignored.
var supportedSchemes = map[string]bool{
	"http":  true,
	"https": true,
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
