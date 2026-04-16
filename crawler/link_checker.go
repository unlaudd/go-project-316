// Package crawler provides link validation functionality.
package crawler

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// checkLink validates a single URL with retry logic.
func checkLink(ctx context.Context, client *http.Client, limiter *rate.Limiter, url string) *BrokenLink {
	for attempt := 0; attempt <= 2; attempt++ {
		// Пауза перед повторной попыткой (кроме первой)
		if attempt > 0 {
			if err := waitShort(ctx); err != nil {
				return &BrokenLink{URL: url, Error: err.Error()}
			}
		}

		// Пробуем проверить ссылку
		result := tryHeadRequest(ctx, client, limiter, url)
		
		// Если ссылка рабочая — возвращаем nil
		if result == nil {
			return nil
		}
		
		// Если ошибка не временная — возвращаем сразу
		if !isRetryable(result) {
			return result
		}
		// Иначе продолжаем цикл для повторной попытки
	}
	
	// Все попытки исчерпаны
	return &BrokenLink{URL: url, Error: "max retries exceeded"}
}

// tryHeadRequest делает один HEAD-запрос и возвращает результат
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

	// 2xx и 3xx — ссылка рабочая
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	
	// 4xx и 5xx — потенциально битая
	return &BrokenLink{URL: url, StatusCode: resp.StatusCode}
}

// isRetryable определяет, стоит ли повторять запрос
func isRetryable(broken *BrokenLink) bool {
	if broken == nil {
		return false
	}
	// Сетевая ошибка (StatusCode=0, Error!= "")
	if broken.StatusCode == 0 && broken.Error != "" {
		return true
	}
	// Rate limit или серверная ошибка
	return broken.StatusCode == 429 || broken.StatusCode >= 500
}

// waitShort ждёт 100ms или отмену контекста
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
