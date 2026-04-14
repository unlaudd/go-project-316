package crawler

import "time"

// SEO содержит базовые метаданные страницы
type SEO struct {
	HasTitle       bool   `json:"has_title"`
	Title          string `json:"title,omitempty"`
	HasDescription bool   `json:"has_description"`
	Description    string `json:"description,omitempty"`
	HasH1          bool   `json:"has_h1"`
}

// BrokenLink описывает ссылку, которая не работает
type BrokenLink struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}

// PageReport содержит информацию об одной странице
type PageReport struct {
	URL           string        `json:"url"`
	Depth         int           `json:"depth"`
	HTTPStatus    int           `json:"http_status"`
	Status        string        `json:"status"` // "ok", "error", "skipped"
	Error         string        `json:"error,omitempty"`
	BrokenLinks   []BrokenLink  `json:"broken_links,omitempty"`
	DiscoveredAt  time.Time     `json:"discovered_at,omitempty"`
	SEO           *SEO         `json:"seo,omitempty"`
}

// Report — итоговый JSON-отчёт
type Report struct {
	RootURL     string        `json:"root_url"`
	Depth       int           `json:"depth"`
	GeneratedAt time.Time     `json:"generated_at"`
	Pages       []PageReport  `json:"pages"`
}
