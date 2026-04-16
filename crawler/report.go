// Package crawler defines the data structures for the crawl report.
// All structs are designed for JSON serialization with consistent, required fields.
package crawler

import "time"

// SEO holds basic SEO metadata extracted from a page.
// All fields are always present in the JSON output, even if empty/false.
type SEO struct {
	// HasTitle reports whether a non-empty <title> tag was found.
	HasTitle bool `json:"has_title"`
	// Title contains the extracted title text (empty if not found).
	Title string `json:"title"`
	// HasDescription reports whether a non-empty meta description was found.
	HasDescription bool `json:"has_description"`
	// Description contains the extracted meta description text (empty if not found).
	Description string `json:"description"`
	// HasH1 reports whether a non-empty <h1> tag was found.
	HasH1 bool `json:"has_h1"`
}

// BrokenLink represents a link that failed to load or returned an error status.
// All fields are always present in the JSON output.
type BrokenLink struct {
	// URL is the absolute URL of the broken link.
	URL string `json:"url"`
	// StatusCode is the HTTP status code received (0 for network errors).
	StatusCode int `json:"status_code,omitempty"`
	// Error contains a description of the failure (empty if only status code is available).
	Error      string `json:"error,omitempty"`
}

// Asset represents an external resource referenced by a page (image, script, or stylesheet).
// All fields are always present in the JSON output.
type Asset struct {
	// URL is the absolute URL of the asset.
	URL string `json:"url"`
	// Type categorizes the asset: "image", "script", "style", or "other".
	Type string `json:"type"`
	// StatusCode is the HTTP status code received when fetching the asset.
	StatusCode int `json:"status_code,omitempty"`
	// SizeBytes is the size of the asset in bytes (0 if unknown).
	SizeBytes int64 `json:"size_bytes,omitempty"`
	// Error contains a description of any fetch error (empty on success).
	Error      string `json:"error,omitempty"`
}

// PageReport contains the analysis results for a single crawled page.
// All fields are always present in the JSON output, even if empty or zero-valued.
type PageReport struct {
	// URL is the absolute URL of the page.
	URL string `json:"url"`
	// Depth is the crawl depth at which this page was discovered (0 for root).
	Depth int `json:"depth"`
	// HTTPStatus is the HTTP status code received for the page request.
	HTTPStatus int `json:"http_status"`
	// Status indicates the outcome: "ok", "error", or "skipped".
	Status string `json:"status"`
	// Error contains a description if Status is "error" or "skipped" (empty otherwise).
	Error string `json:"error,omitempty"`
	// BrokenLinks lists all links on the page that failed to load.
	BrokenLinks []BrokenLink `json:"broken_links"`
	// DiscoveredAt is the timestamp when the page was first crawled.
	DiscoveredAt time.Time `json:"discovered_at"`
	// SEO contains extracted SEO metadata (nil if none found).
	SEO          SEO                `json:"seo"`
	// Assets lists all external resources referenced by the page.
	Assets []Asset `json:"assets"`
}

// Report is the top-level JSON output of the crawl operation.
// All fields are always present in the JSON output.
type Report struct {
	// RootURL is the starting URL of the crawl.
	RootURL string `json:"root_url"`
	// Depth is the maximum crawl depth that was configured.
	Depth int `json:"depth"`
	// GeneratedAt is the timestamp when the report was generated.
	GeneratedAt time.Time `json:"generated_at"`
	// Pages contains the analysis results for each crawled page, sorted by depth then URL.
	Pages []PageReport `json:"pages"`
}
