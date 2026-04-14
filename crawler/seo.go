package crawler

import (
	"golang.org/x/net/html"
	"strings"
)

// extractSEO извлекает базовые SEO-метрики из распарсенного HTML-документа
func extractSEO(doc *html.Node) *SEO {
	seo := &SEO{}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					seo.Title = html.UnescapeString(strings.TrimSpace(n.FirstChild.Data))
					seo.HasTitle = seo.Title != ""
				}

			case "meta":
				// Ищем <meta name="description" content="...">
				var name, content string
				for _, attr := range n.Attr {
					switch attr.Key {
					case "name":
						name = strings.ToLower(attr.Val)
					case "content":
						content = attr.Val
					}
				}
				if name == "description" && content != "" {
					seo.Description = html.UnescapeString(strings.TrimSpace(content))
					seo.HasDescription = true
				}

			case "h1":
				if !seo.HasH1 {
					// Собираем текст из всех текстовых нод внутри h1
					var h1Text strings.Builder
					collectText(n, &h1Text)
					text := html.UnescapeString(strings.TrimSpace(h1Text.String()))
					if text != "" {
						seo.HasH1 = true
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return seo
}

// collectText рекурсивно собирает текстовое содержимое узла
func collectText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, sb)
	}
}
