// Package crawler provides link validation functionality.
package crawler

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// checkLink validates a single URL with retry logic.
// It attempts a HEAD request first, retrying up to 2 times for temporary errors
// (network failures, 429 Too Many Requests, or 5xx server errors).
// Returns nil if the link is accessible, or a BrokenLink descriptor if it fails.
func checkLink(ctx context.Context, client *http.Client, limiter *rate.Limiter, url string) *BrokenLink {
	for attempt := 0; attempt <= 2; attempt++ {
		// Wait before retry (skip on first attempt)
		if attempt > 0 {
			if err := waitShort(ctx); err != nil {
				return &BrokenLink{URL: url, Error: err.Error()}
			}
		}

		// Attempt a single HEAD request
		result := tryHeadRequest(ctx, client, limiter, url)

		// Link is accessible
		if result == nil {
			return nil
		}

		// Return immediately if error is not retryable
		if !isRetryable(result) {
			return result
		}
		// Otherwise, continue loop for another retry attempt
	}

	// All retry attempts exhausted
	return &BrokenLink{URL: url, Error: "max retries exceeded"}
}

// tryHeadRequest performs a single HEAD request to validate a URL.
// It respects the provided rate limiter and context for cancellation.
// Returns nil if the response status is 2xx or 3xx, or a BrokenLink otherwise.
func tryHeadRequest(ctx context.Context, client *http.Client, limiter *rate.Limiter, url string) *BrokenLink {
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return &BrokenLink{URL: url, Error: err.Error()}
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return &BrokenLink{URL: url, Error: err.Error()}
	}
	req.Header.Set("User-Agent", "hexlet-go-crawler/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return &BrokenLink{URL: url, Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	// 2xx and 3xx statuses indicate an accessible link
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}

	// 4xx and 5xx statuses indicate a broken or unavailable link
	return &BrokenLink{URL: url, StatusCode: resp.StatusCode}
}

// isRetryable reports whether a BrokenLink error should trigger a retry.
// Network errors (StatusCode=0 with non-empty Error), 429 Too Many Requests,
// and 5xx server errors are considered retryable.
func isRetryable(broken *BrokenLink) bool {
	if broken == nil {
		return false
	}
	// Network-level error
	if broken.StatusCode == 0 && broken.Error != "" {
		return true
	}
	// Rate limit or server error
	return broken.StatusCode == 429 || broken.StatusCode >= 500
}

// waitShort blocks for 100ms or until context is cancelled.
// Returns ctx.Err() if cancelled, or nil on successful wait.
func waitShort(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
