package crawler

import "time"

// PageReport содержит информацию об одной странице
type PageReport struct {
	URL        string `json:"url"`
	Depth      int    `json:"depth"`
	HTTPStatus int    `json:"http_status"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// Report — итоговый JSON-отчёт
type Report struct {
	RootURL    string        `json:"root_url"`
	Depth      int           `json:"depth"`
	GeneratedAt time.Time    `json:"generated_at"`
	Pages      []PageReport  `json:"pages"`
}