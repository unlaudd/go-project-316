// Package crawler provides link validation functionality.
package crawler

import (
	"context"
	"net/http"

	"golang.org/x/time/rate"
)

// checkLink validates a single URL and returns a BrokenLink descriptor if it fails.
// It respects the provided rate limiter and context for cancellation.
// Returns nil if the link is accessible (status < 400), or a BrokenLink with error details otherwise.
func checkLink(ctx context.Context, client *http.Client, limiter *rate.Limiter, url string) *BrokenLink {
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return &BrokenLink{URL: url, Error: err.Error()}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &BrokenLink{URL: url, Error: err.Error()}
	}
	req.Header.Set("User-Agent", "hexlet-go-crawler/1.0")

	resp, err := client.Do(req)
	if err != nil {
		// Network-level errors (timeout, connection refused, etc.) are reported as broken.
		return &BrokenLink{URL: url, Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	// HTTP status codes >= 400 indicate a broken or unavailable resource.
	if resp.StatusCode >= 400 {
		return &BrokenLink{URL: url, StatusCode: resp.StatusCode}
	}

	return nil
}
