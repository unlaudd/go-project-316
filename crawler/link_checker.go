package crawler

import (
	"context"
	"net/http"
	"golang.org/x/time/rate"
)

// checkLink проверяет доступность ссылки и возвращает информацию, если она "битая"
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
		return &BrokenLink{URL: url, Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return &BrokenLink{URL: url, StatusCode: resp.StatusCode}
	}
	return nil
}
