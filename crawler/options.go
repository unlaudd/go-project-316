// Package crawler provides configuration types for the web crawler.
package crawler

import (
	"net/http"
	"time"
)

// Options holds configuration parameters for a crawl operation.
// All fields are optional unless otherwise noted; zero values trigger defaults.
type Options struct {
	// URL is the root URL to start crawling from (required).
	URL string

	// Depth limits how many levels of links to follow from the root URL.
	// A depth of 1 crawls only the root page; 2 includes links from the root, etc.
	Depth int

	// Retries specifies how many additional attempts to make for failed requests.
	// Total attempts = 1 (initial) + Retries. Applies only to temporary errors.
	Retries int

	// Delay sets a fixed pause between requests. Ignored if RPS is set.
	Delay time.Duration

	// Timeout is the per-request HTTP timeout. Zero means no timeout (not recommended).
	Timeout time.Duration

	// RPS limits requests per second. Takes precedence over Delay if both are set.
	// Zero means no rate limiting.
	RPS float64

	// UserAgent sets the HTTP User-Agent header. Defaults to "hexlet-go-crawler/1.0" if empty.
	UserAgent string

	// Concurrency controls the number of worker goroutines for parallel crawling.
	Concurrency int

	// IndentJSON enables pretty-printing of the output JSON report.
	IndentJSON bool

	// HTTPClient allows injecting a custom HTTP client. If nil, a default client is created.
	// Note: If provided, its Timeout field is used unless Options.Timeout is non-zero.
	HTTPClient *http.Client
}

// DefaultOptions returns a new Options struct with sensible defaults.
// Callers should modify the returned copy as needed before passing to Analyze.
func DefaultOptions() Options {
	return Options{
		Depth:       10,
		Retries:     1,
		Delay:       0,
		Timeout:     15 * time.Second,
		RPS:         0,
		Concurrency: 4,
		IndentJSON:  false,
		HTTPClient:  &http.Client{},
	}
}
