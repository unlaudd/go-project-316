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
	Assets        []Asset      `json:"assets,omitempty"`
}

// Asset описывает внешний ресурс страницы (изображение, скрипт, стиль)
type Asset struct {
	URL        string `json:"url"`
	Type       string `json:"type"`          // "image", "script", "style", "other"
	StatusCode int    `json:"status_code"`   // HTTP-статус или 0 при сетевой ошибке
	SizeBytes  int64  `json:"size_bytes"`    // Размер в байтах (0 если не определён)
	Error      string `json:"error,omitempty"`
}

// Report — итоговый JSON-отчёт
type Report struct {
	RootURL     string        `json:"root_url"`
	Depth       int           `json:"depth"`
	GeneratedAt time.Time     `json:"generated_at"`
	Pages       []PageReport  `json:"pages"`
}
