package crawler

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// supportedSchemes перечисляет схемы, которые мы проверяем
var supportedSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

// extractLinks извлекает все абсолютные HTTP(S)-ссылки из HTML-документа
func extractLinks(baseURL string, doc *html.Node) []string {
	var links []string
	base, err := url.Parse(baseURL)
	if err != nil {
		return links
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					href := strings.TrimSpace(attr.Val)
					if href == "" {
						continue
					}
					// Игнорируем якоря и фрагменты той же страницы
					if strings.HasPrefix(href, "#") {
						continue
					}
					// Пытаемся распарсить и резолвить ссылку
					linkURL, err := url.Parse(href)
					if err != nil {
						continue
					}
					// Пропускаем неподдерживаемые схемы
					if !supportedSchemes[linkURL.Scheme] && linkURL.Scheme != "" {
						continue
					}
					// Если схема не указана — резолвим относительно базового URL
					if linkURL.Scheme == "" {
						linkURL = base.ResolveReference(linkURL)
					}
					// Добавляем только валидные абсолютные ссылки
					if linkURL.IsAbs() && supportedSchemes[linkURL.Scheme] {
						links = append(links, linkURL.String())
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return links
}
