package crawler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"strconv"

	"golang.org/x/net/html"
	"golang.org/x/time/rate"
)

// Константы для goconst
const (
	// HTML теги
	tagImg   = "img"
	tagScript = "script"
	tagLink  = "link"
	
	// HTML атрибуты
	attrSrc  = "src"
	attrHref = "href"
	attrRel  = "rel"
	attrRelStylesheet = "stylesheet"
	
	// Типы ассетов
	typeImage = "image"
	typeScript = "script"
	typeStyle = "style"
	typeOther = "other"
	
	// Схемы URL
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// assetCache кэширует результаты проверки ассетов (глобально для всего обхода)
type assetCache struct {
	mu      sync.Mutex
	cache   map[string]*Asset
	loading map[string]chan struct{}
}

func newAssetCache() *assetCache {
	return &assetCache{
		cache:   make(map[string]*Asset),
		loading: make(map[string]chan struct{}),
	}
}

// detectAssetType определяет тип ассета по тегу и атрибутам
func detectAssetType(tagName string, attrs map[string]string) string {
	switch tagName {
	case tagImg:
		return typeImage
	case tagScript:
		return typeScript
	case tagLink:
		if strings.ToLower(attrs[attrRel]) == attrRelStylesheet {
			return typeStyle
		}
	}
	return typeOther
}

// Вспомогательная: проверяет, является ли тег ассетом
func isAssetTag(tagName string) bool {
	return tagName == tagImg || tagName == tagScript || tagName == tagLink
}

// Вспомогательная: извлекает src/href из атрибутов в зависимости от тега
func getAssetSrc(tagName string, attrs map[string]string) string {
	switch tagName {
	case tagImg, tagScript:
		return attrs[attrSrc]
	case tagLink:
		if strings.ToLower(attrs[attrRel]) == attrRelStylesheet {
			return attrs[attrHref]
		}
	}
	return ""
}

// Вспомогательная: проверяет и резолвит URL ассета
func resolveAssetURL(src, baseURL string) (string, bool) {
	if src == "" || strings.HasPrefix(src, "#") || strings.HasPrefix(src, "data:") {
		return "", false
	}
	
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", false
	}
	
	parsed, err := url.Parse(src)
	if err != nil {
		return "", false
	}
	
	if parsed.Scheme == "" {
		parsed = base.ResolveReference(parsed)
	}
	
	isValid := parsed.IsAbs() && (parsed.Scheme == schemeHTTP || parsed.Scheme == schemeHTTPS)
	if !isValid {
		return "", false
	}
	
	// Нормализуем перед возвратом
	return normalizeURLForCache(parsed.String()), true
}

// assetInfo хранит данные об извлечённом ассете
type assetInfo struct {
	url   string
	tag   string
	attrs map[string]string
}

// extractAssets извлекает список ассетов из HTML-документа
func extractAssets(baseURL string, doc *html.Node) []assetInfo {
	var assets []assetInfo
	
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		if !isAssetTag(n.Data) {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		attrs := make(map[string]string)
		for _, a := range n.Attr {
			attrs[strings.ToLower(a.Key)] = a.Val
		}

		src := getAssetSrc(n.Data, attrs)
		if resolvedURL, ok := resolveAssetURL(src, baseURL); ok {
			assets = append(assets, assetInfo{
				url:   resolvedURL,
				tag:   n.Data,
				attrs: attrs,
			})
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return assets
}

// Вспомогательная: создаёт и выполняет единичный запрос к ассету
func doAssetRequest(ctx context.Context, client *http.Client, rawURL, method, ua string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	return client.Do(req)
}

// Вспомогательная: определяет размер ассета из заголовка или тела ответа
func determineAssetSize(resp *http.Response) (int64, string) {
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if size, err := strconv.ParseInt(cl, 10, 64); err == nil {
			return size, ""
		}
	}
	
	if resp.StatusCode < 400 {
		n, err := io.Copy(io.Discard, resp.Body)
		if err != nil {
			return 0, "failed to read body: " + err.Error()
		}
		return n, ""
	}
	
	return 0, ""
}

// checkAsset проверяет доступность и метаданные ассета (оптимизированная версия)
func checkAsset(ctx context.Context, client *http.Client, limiter *rate.Limiter, rawURL string, tag string, attrs map[string]string) *Asset {
	asset := &Asset{
		URL:  rawURL,
		Type: detectAssetType(tag, attrs),
	}

	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			asset.Error = err.Error()
			return asset
		}
	}

	const userAgent = "hexlet-go-crawler/1.0"
	
	// 1. Пробуем HEAD для экономии трафика
	resp, err := doAssetRequest(ctx, client, rawURL, http.MethodHead, userAgent)
	
	// 2. Фоллбэк на GET, если HEAD не поддерживается (405) или произошла сетевая ошибка
	// Обратите внимание: мы НЕ фоллбечимся на 404/500, так как это валидный ответ
	shouldFallback := err != nil || (resp != nil && resp.StatusCode == http.StatusMethodNotAllowed)
	
	if shouldFallback {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		resp, err = doAssetRequest(ctx, client, rawURL, http.MethodGet, userAgent)
	}

	if err != nil {
		asset.Error = err.Error()
		return asset
	}
	defer func() { _ = resp.Body.Close() }()

	asset.StatusCode = resp.StatusCode
	asset.SizeBytes, asset.Error = determineAssetSize(resp)
	
	if asset.StatusCode >= 400 && asset.Error == "" {
		asset.Error = http.StatusText(asset.StatusCode)
	}

	return asset
}

func (c *assetCache) getOrCreate(url string, fetcher func() *Asset) *Asset {
	c.mu.Lock()
	
	// Если уже в кэше — возвращаем
	if asset, ok := c.cache[url]; ok {
		c.mu.Unlock()
		return asset
	}
	
	// Если уже загружается — ждём завершения
	if waitCh, ok := c.loading[url]; ok {
		c.mu.Unlock()
		<-waitCh // Блокируемся, пока загрузка не завершится
		// После ожидания берём результат из кэша
		c.mu.Lock()
		asset := c.cache[url]
		c.mu.Unlock()
		return asset
	}
	
	// Начинаем загрузку: создаём канал и регистрируем его
	waitCh := make(chan struct{})
	c.loading[url] = waitCh
	c.mu.Unlock()
	
	// Загружаем ассет (вне критической секции, чтобы не блокировать другие URL)
	asset := fetcher()
	
	// Сохраняем результат и уведомляем ожидающих
	c.mu.Lock()
	delete(c.loading, url)
	c.cache[url] = asset
	c.mu.Unlock()
	close(waitCh) // Разблокируем всех ожидающих
	
	return asset
}

// normalizeURLForCache приводит URL к единому виду для кэширования
func normalizeURLForCache(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}
