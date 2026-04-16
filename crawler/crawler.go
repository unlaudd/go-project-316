// Package crawler implements a concurrent web crawler with configurable depth,
// rate limiting, retry logic, and asset analysis.
package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/time/rate"
)

// crawlTask represents a single URL to be crawled with its current depth.
type crawlTask struct {
	url   string
	depth int
}

// crawlEngine holds the state for a crawling session.
type crawlEngine struct {
	opts       Options
	client     *http.Client
	startHost  string
	mu         sync.Mutex
	visited    map[string]bool
	queue      chan crawlTask
	results    []PageReport
	resultMu   sync.Mutex
	limiter    *rate.Limiter
	assetCache *assetCache
}

// Analyze is the main entry point for crawling a website.
// It returns a JSON-encoded report or an error if the crawl fails to start.
// The context can be used to cancel the operation gracefully.
func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("URL is required")
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	if client.Timeout == 0 {
		client.Timeout = opts.Timeout
	}

	startURL, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid start URL: %w", err)
	}

	engine := &crawlEngine{
		opts:       opts,
		client:     client,
		startHost:  startURL.Hostname(),
		visited:    make(map[string]bool),
		queue:      make(chan crawlTask, 100),
		limiter:    createLimiter(opts),
		assetCache: newAssetCache(),
	}

	results, err := engine.run(ctx)
	if err != nil {
		return nil, err
	}

	return formatReport(opts.URL, opts.Depth, results, opts.IndentJSON)
}

// createLimiter returns a rate limiter configured from Options.
// RPS takes precedence over Delay; if neither is set, returns nil (no limiting).
func createLimiter(opts Options) *rate.Limiter {
	var limit rate.Limit
	var burst int

	if opts.RPS > 0 {
		limit = rate.Limit(opts.RPS)
		// Allow a small burst to accommodate concurrent workers without starving them.
		burst = opts.Concurrency
		if burst < 2 {
			burst = 2
		}
		return rate.NewLimiter(limit, burst)
	}

	if opts.Delay > 0 {
		limit = rate.Limit(1.0 / opts.Delay.Seconds())
		return rate.NewLimiter(limit, 1)
	}

	return nil
}

// run orchestrates the concurrent crawling process.
// It spawns worker goroutines, seeds the queue with the start URL,
// and collects results once all tasks are complete.
func (e *crawlEngine) run(ctx context.Context) ([]PageReport, error) {
	var taskWg sync.WaitGroup
	var workerWg sync.WaitGroup

	for i := 0; i < e.opts.Concurrency; i++ {
		workerWg.Add(1)
		go e.worker(ctx, &taskWg, &workerWg)
	}

	normalizedStart := normalizeURL(e.opts.URL)
	e.mu.Lock()
	e.visited[normalizedStart] = true
	e.mu.Unlock()

	taskWg.Add(1)
	e.queue <- crawlTask{url: e.opts.URL, depth: 0}

	go func() {
		taskWg.Wait()
		close(e.queue)
	}()

	workerWg.Wait()

	e.resultMu.Lock()
	results := e.results
	e.resultMu.Unlock()
	return results, nil
}

// worker processes crawl tasks from the queue.
// It respects rate limiting, context cancellation, and domain restrictions.
func (e *crawlEngine) worker(ctx context.Context, taskWg *sync.WaitGroup, workerWg *sync.WaitGroup) {
	defer workerWg.Done()

	for task := range e.queue {
		if e.limiter != nil {
			if err := e.limiter.Wait(ctx); err != nil {
				taskWg.Done()
				continue
			}
		}
		if ctx.Err() != nil {
			taskWg.Done()
			continue
		}

		page, links := crawlPage(ctx, e.client, e.limiter, e.assetCache, task.url, task.depth, e.opts)

		e.resultMu.Lock()
		e.results = append(e.results, page)
		e.resultMu.Unlock()

		if task.depth+1 < e.opts.Depth {
			for _, link := range links {
				parsed, pErr := url.Parse(link)
				if pErr != nil || parsed.Hostname() != e.startHost {
					continue
				}
				normalized := normalizeURL(link)
				e.mu.Lock()
				if !e.visited[normalized] {
					e.visited[normalized] = true
					e.mu.Unlock()
					taskWg.Add(1)
					e.queue <- crawlTask{url: link, depth: task.depth + 1}
				} else {
					e.mu.Unlock()
				}
			}
		}
		taskWg.Done()
	}
}

// normalizeURL removes fragments and trailing slashes for consistent comparison.
func normalizeURL(raw string) string {
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

// formatReport builds the final JSON report, optionally with indentation.
func formatReport(rootURL string, depth int, results []PageReport, indentJSON bool) ([]byte, error) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Depth != results[j].Depth {
			return results[i].Depth < results[j].Depth
		}
		return results[i].URL < results[j].URL
	})

	report := Report{
		RootURL:     rootURL,
		Depth:       depth,
		GeneratedAt: time.Now().UTC(),
		Pages:       results,
	}

	indent := ""
	if indentJSON {
		indent = "  "
	}
	return json.MarshalIndent(report, "", indent)
}

// crawlPage fetches a single page and extracts its content, links, SEO data, and assets.
func crawlPage(ctx context.Context, client *http.Client, limiter *rate.Limiter, assetCache *assetCache, url string, depth int, opts Options) (PageReport, []string) {
	report := PageReport{
		URL:   url,
		Depth: depth,
		SEO:   &SEO{},
	}
	
	if ctx.Err() != nil {
		report.Status = "skipped"
		report.Error = ctx.Err().Error()
		return report, nil
	}

	resp, err := fetchWithRetries(ctx, client, url, opts)
	if err != nil {
		report.Status = "error"
		report.Error = err.Error()
		return report, nil
	}
	defer func() { _ = resp.Body.Close() }()

	report.HTTPStatus = resp.StatusCode
	report.Status = "ok"
	
    report.BrokenLinks = []BrokenLink{}
	report.Assets = []Asset{}

	links := analyzePageContent(ctx, client, limiter, assetCache, resp.Body, opts.URL, &report, opts)
	seenBroken := make(map[string]bool)
	uniqueBroken := []BrokenLink{}
	for _, bl := range report.BrokenLinks {
		if !seenBroken[bl.URL] {
			seenBroken[bl.URL] = true
			uniqueBroken = append(uniqueBroken, bl)
		}
	}
	report.BrokenLinks = uniqueBroken

	report.DiscoveredAt = time.Now().UTC()
	return report, links
}

// fetchWithRetries performs an HTTP request with retry logic for temporary failures.
// It respects context cancellation and applies exponential backoff between attempts.
func fetchWithRetries(ctx context.Context, client *http.Client, url string, opts Options) (*http.Response, error) {
	retryDelay := opts.Delay
	if retryDelay == 0 {
		retryDelay = 200 * time.Millisecond
	}

	for attempt := 0; attempt <= opts.Retries; attempt++ {
		resp, err := doRequest(ctx, client, url, opts)

		if shouldAcceptResponse(err, resp) {
			return resp, err
		}

		if attempt == opts.Retries {
			return handleFinalAttempt(resp, err, opts)
		}

		closeResponseBody(resp)

		if waitErr := waitForRetry(ctx, retryDelay, attempt); waitErr != nil {
			return nil, waitErr
		}
	}
	return nil, fmt.Errorf("unexpected end of retry loop")
}

// doRequest performs a single HTTP GET request with the configured user agent.
func doRequest(ctx context.Context, client *http.Client, url string, opts Options) (*http.Response, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	ua := opts.UserAgent
	if ua == "" {
		ua = "hexlet-go-crawler/1.0"
	}
	req.Header.Set("User-Agent", ua)

	return client.Do(req)
}

// shouldAcceptResponse reports whether a response should be accepted without retry.
// Network errors and temporary status codes (429, 5xx) trigger retries; others do not.
func shouldAcceptResponse(err error, resp *http.Response) bool {
	if err != nil {
		return false
	}
	return !isTemporaryStatus(resp.StatusCode)
}

// handleFinalAttempt decides the outcome when retry attempts are exhausted.
// If retries were configured and the status is temporary, returns an error;
// otherwise returns the response as-is.
func handleFinalAttempt(resp *http.Response, err error, opts Options) (*http.Response, error) {
	if resp != nil {
		if opts.Retries > 0 && isTemporaryStatus(resp.StatusCode) {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("temporary status %d after %d retries", resp.StatusCode, opts.Retries)
		}
		return resp, nil
	}
	return nil, err
}

// closeResponseBody safely closes the response body if present.
func closeResponseBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// waitForRetry blocks for a backoff duration or until context is cancelled.
func waitForRetry(ctx context.Context, baseDelay time.Duration, attempt int) error {
	delay := baseDelay * time.Duration(attempt+1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// analyzePageContent parses HTML and extracts SEO metadata, assets, and broken links.
func analyzePageContent(
	ctx context.Context,
	client *http.Client,
	limiter *rate.Limiter,
	assetCache *assetCache,
	body io.Reader,
	baseURL string,
	report *PageReport,
	opts Options,
) []string {
	doc, err := html.Parse(body)
	if err != nil {
		return nil
	}

	seo := extractSEO(doc)
	if seo.HasTitle || seo.HasDescription || seo.HasH1 {
		report.SEO = seo
	}

	assets := extractAssets(baseURL, doc)
	for _, a := range assets {
		if ctx.Err() != nil {
			break
		}
		asset := assetCache.getOrCreate(a.url, func() *Asset {
			return checkAsset(ctx, client, limiter, a.url, a.tag, a.attrs)
		})
		report.Assets = append(report.Assets, *asset)
	}

	links := extractLinks(baseURL, doc)
	for _, link := range links {
		if ctx.Err() != nil {
			break
		}
		if broken := checkLink(ctx, client, limiter, link); broken != nil {
			report.BrokenLinks = append(report.BrokenLinks, *broken)
		}
	}
	return links
}

// isTemporaryStatus reports whether an HTTP status code indicates a temporary failure.
func isTemporaryStatus(code int) bool {
	return code == http.StatusTooManyRequests || (code >= 500 && code < 600)
}
