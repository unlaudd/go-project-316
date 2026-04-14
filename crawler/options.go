package crawler

import (
	"net/http"
	"time"
)

// Options содержит параметры запуска краулера
type Options struct {
	URL          string
	Depth        int
	Retries      int
	Delay        time.Duration
	Timeout      time.Duration
	RPS          float64
	UserAgent    string
	Concurrency  int
	IndentJSON   bool
	HTTPClient   *http.Client
}

// DefaultOptions возвращает настройки по умолчанию
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
