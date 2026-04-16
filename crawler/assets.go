// Package crawler provides asset extraction and validation functionality.
package crawler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"
	"golang.org/x/time/rate"
)

// HTML tag constants for asset detection.
const (
	tagImg    = "img"
	tagScript = "script"
	tagLink   = "link"
)

// HTML attribute constants.
const (
	attrSrc           = "src"
	attrHref          = "href"
	attrRel           = "rel"
	attrRelStylesheet = "stylesheet"
)

// Asset type constants.
const (
	typeImage  = "image"
	typeScript = "script"
	typeStyle  = "style"
	typeOther  = "other"
)

// URL scheme constants.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// assetCache provides thread-safe caching for asset check results.
// It implements a single-flight pattern to prevent duplicate requests for the same URL.
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

// detectAssetType determines the asset type based on HTML tag and attributes.
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

// isAssetTag reports whether the given tag name represents a loadable asset.
func isAssetTag(tagName string) bool {
	return tagName == tagImg || tagName == tagScript || tagName == tagLink
}

// getAssetSrc extracts the source URL from tag attributes.
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

// resolveAssetURL validates and normalizes a relative or absolute asset URL.
// Returns the normalized absolute URL and true if valid, or empty string and false otherwise.
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

	return normalizeURLForCache(parsed.String()), true
}

// assetInfo holds intermediate data for an extracted asset.
type assetInfo struct {
	url   string
	tag   string
	attrs map[string]string
}

// extractAssets parses an HTML document and returns a list of loadable assets.
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

// doAssetRequest performs a single HTTP request with the given method and user agent.
func doAssetRequest(ctx context.Context, client *http.Client, rawURL, method, ua string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	return client.Do(req)
}

// determineAssetSize extracts the content size from response headers or body.
// Returns the size in bytes and an error message if reading the body fails.
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

// checkAsset fetches an asset and populates its metadata.
// It attempts a HEAD request first for efficiency, falling back to GET if needed.
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

	resp, err := doAssetRequest(ctx, client, rawURL, http.MethodHead, userAgent)

	// Fallback to GET if HEAD is not supported or a network error occurred.
	// Note: HTTP error codes like 404 or 500 are valid responses and do not trigger fallback.
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

// getOrCreate returns a cached asset or fetches it using the provided fetcher.
// It guarantees that fetcher is called at most once per URL, even under concurrent access.
func (c *assetCache) getOrCreate(url string, fetcher func() *Asset) *Asset {
	c.mu.Lock()

	if asset, ok := c.cache[url]; ok {
		c.mu.Unlock()
		return asset
	}

	if waitCh, ok := c.loading[url]; ok {
		c.mu.Unlock()
		<-waitCh
		c.mu.Lock()
		asset := c.cache[url]
		c.mu.Unlock()
		return asset
	}

	waitCh := make(chan struct{})
	c.loading[url] = waitCh
	c.mu.Unlock()

	asset := fetcher()

	c.mu.Lock()
	delete(c.loading, url)
	c.cache[url] = asset
	c.mu.Unlock()
	close(waitCh)

	return asset
}

// normalizeURLForCache normalizes a URL for consistent cache key generation.
// It removes fragments and trailing slashes to treat equivalent URLs as identical.
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
