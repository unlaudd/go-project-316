// Package crawler provides tests for the web crawling functionality.
package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockTransport implements http.RoundTripper to simulate network-level errors.
type mockTransport struct {
	err error
}

func (m *mockTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, m.err
}

// TestAnalyze_HTTPLogic verifies that Analyze correctly handles various HTTP responses.
// It tests successful responses, client/server errors, network failures, and timeouts.
func TestAnalyze_HTTPLogic(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.Handler
		mockErr        error
		retries        int
		wantHTTPStatus int
		wantStatus     string
		wantErr        bool
	}{
		{
			name: "200 OK success",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("<html>test</html>"))
			}),
			wantHTTPStatus: http.StatusOK,
			wantStatus:     "ok",
		},
		{
			name: "404 Not Found",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}),
			wantHTTPStatus: http.StatusNotFound,
			wantStatus:     "ok",
		},
		{
			name: "500 Internal Server Error",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}),
			wantHTTPStatus: http.StatusInternalServerError,
			wantStatus:     "ok",
		},
		{
			name:           "network error",
			mockErr:        errors.New("connection refused"),
			retries:        1,
			wantStatus:     "error",
			wantHTTPStatus: 0,
		},
		{
			name: "request timeout",
			serverHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			}),
			wantHTTPStatus: 0,
			wantStatus:     "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			opts := DefaultOptions()
			opts.Depth = 1
			opts.Retries = tt.retries
			opts.Timeout = 100 * time.Millisecond

			var client *http.Client
			if tt.mockErr != nil {
				client = &http.Client{Transport: &mockTransport{err: tt.mockErr}}
				opts.URL = "http://mock.local"
			} else {
				server := httptest.NewServer(tt.serverHandler)
				t.Cleanup(server.Close)
				client = server.Client()
				client.Timeout = opts.Timeout
				opts.URL = server.URL
			}

			opts.HTTPClient = client

			result, err := Analyze(ctx, opts)
			if tt.wantErr && err == nil {
				t.Fatal("expected error from Analyze, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error from Analyze: %v", err)
			}

			var report Report
			if err := json.Unmarshal(result, &report); err != nil {
				t.Fatalf("failed to unmarshal report: %v", err)
			}

			if len(report.Pages) != 1 {
				t.Fatalf("expected 1 page, got %d", len(report.Pages))
			}

			page := report.Pages[0]
			if page.HTTPStatus != tt.wantHTTPStatus {
				t.Errorf("HTTP status = %d, want %d", page.HTTPStatus, tt.wantHTTPStatus)
			}
			if page.Status != tt.wantStatus {
				t.Errorf("page status = %q, want %q", page.Status, tt.wantStatus)
			}
		})
	}
}

// TestAnalyze_BrokenLinks verifies that broken internal links are detected and reported.
func TestAnalyze_BrokenLinks(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
			<html>
				<body>
					<a href="/ok">Working</a>
					<a href="/missing">Broken</a>
					<a href="mailto:test@example.com">Email</a>
					<a href="#anchor">Anchor</a>
				</body>
			</html>
		`))
	})

	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1
	opts.IndentJSON = true
	opts.HTTPClient = &http.Client{Timeout: 5 * time.Second}

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(report.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(report.Pages))
	}

	page := report.Pages[0]
    
	if len(page.BrokenLinks) != 1 {
		t.Errorf("expected 1 broken link, got %d: %+v", len(page.BrokenLinks), page.BrokenLinks)
	}

	if len(page.BrokenLinks) > 0 {
		broken := page.BrokenLinks[0]
		if !strings.HasSuffix(broken.URL, "/missing") {
			t.Errorf("broken link URL = %q, want suffix /missing", broken.URL)
		}
		if broken.StatusCode != http.StatusNotFound {
			t.Errorf("broken link status = %d, want %d", broken.StatusCode, http.StatusNotFound)
		}
	}
}

// TestAnalyze_BrokenLinks_NetworkError verifies that links to unreachable domains are reported as broken.
func TestAnalyze_BrokenLinks_NetworkError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<a href="https://nonexistent.invalid/path">Dead</a>`))
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1
	opts.HTTPClient = &http.Client{Timeout: 1 * time.Second}

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(report.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(report.Pages))
	}

	page := report.Pages[0]
	if len(page.BrokenLinks) != 1 {
		t.Errorf("expected 1 broken link (network error), got %d", len(page.BrokenLinks))
	}

	if len(page.BrokenLinks) > 0 {
		broken := page.BrokenLinks[0]
		if broken.Error == "" {
			t.Error("expected error message for network failure")
		}
		if broken.StatusCode != 0 {
			t.Error("status_code should be 0 for network errors")
		}
	}
}

// TestAnalyze_IgnoresUnsupportedSchemes verifies that non-HTTP(S) links are ignored.
func TestAnalyze_IgnoresUnsupportedSchemes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
			<a href="mailto:test@example.com">Email</a>
			<a href="tel:+1234567890">Phone</a>
			<a href="javascript:void(0)">JS</a>
			<a href="ftp://example.com/file">FTP</a>
			<a href="#section">Anchor</a>
			<a href="">Empty</a>
		`))
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(report.Pages[0].BrokenLinks) != 0 {
		t.Errorf("expected no broken links (all unsupported), got: %+v", report.Pages[0].BrokenLinks)
	}
}

// TestAnalyze_SEO_Metrics verifies that SEO metadata is correctly extracted from pages.
func TestAnalyze_SEO_Metrics(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		wantHasTitle bool
		wantTitle    string
		wantHasDesc  bool
		wantDesc     string
		wantHasH1    bool
	}{
		{
			name: "all SEO tags present",
			html: `
				<html>
				<head>
					<title>My &amp; Awesome Site</title>
					<meta name="description" content="Welcome to our &lt;site&gt;">
				</head>
				<body>
					<h1>Main Heading</h1>
				</body>
				</html>`,
			wantHasTitle: true,
			wantTitle:    "My & Awesome Site",
			wantHasDesc:  true,
			wantDesc:     "Welcome to our <site>",
			wantHasH1:    true,
		},
		{
			name:         "no SEO tags",
			html:         `<html><body><p>Just content</p></body></html>`,
			wantHasTitle: false,
			wantTitle:    "",
			wantHasDesc:  false,
			wantDesc:     "",
			wantHasH1:    false,
		},
		{
			name: "empty title and description",
			html: `
				<html>
				<head>
					<title>   </title>
					<meta name="description" content="">
				</head>
				<body><h1>   </h1></body>
				</html>`,
			wantHasTitle: false,
			wantTitle:    "",
			wantHasDesc:  false,
			wantDesc:     "",
			wantHasH1:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(tt.html))
			})

			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)

			ctx := context.Background()
			opts := DefaultOptions()
			opts.URL = server.URL
			opts.Depth = 1
			opts.IndentJSON = true

			result, err := Analyze(ctx, opts)
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}

			var report Report
			if err := json.Unmarshal(result, &report); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}

			if len(report.Pages) != 1 {
				t.Fatalf("expected 1 page, got %d", len(report.Pages))
			}

			page := report.Pages[0]
			seo := page.SEO
			if seo.HasTitle != tt.wantHasTitle {
				t.Errorf("HasTitle = %v, want %v", seo.HasTitle, tt.wantHasTitle)
			}
			if seo.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", seo.Title, tt.wantTitle)
			}
			if seo.HasDescription != tt.wantHasDesc {
				t.Errorf("HasDescription = %v, want %v", seo.HasDescription, tt.wantHasDesc)
			}
			if seo.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", seo.Description, tt.wantDesc)
			}
			if seo.HasH1 != tt.wantHasH1 {
				t.Errorf("HasH1 = %v, want %v", seo.HasH1, tt.wantHasH1)
			}
		})
	}
}

// TestAnalyze_DepthLimit verifies that the crawl depth limit is respected.
func TestAnalyze_DepthLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<a href="/page1">Link</a>`))
	})
	mux.HandleFunc("/page1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`Page 1`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}

	if len(report.Pages) != 1 {
		t.Errorf("expected 1 page (depth=1), got %d", len(report.Pages))
	}

	pageURL := report.Pages[0].URL
	if !strings.HasSuffix(pageURL, "/") && pageURL != server.URL {
		_ = pageURL
	}
}

// TestAnalyze_DomainRestriction verifies that only links within the start domain are crawled.
func TestAnalyze_DomainRestriction(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
			<a href="/internal">Internal</a>
			<a href="https://external.example.com/page">External</a>
		`))
	})
	mux.HandleFunc("/internal", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Internal"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 2

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}

	if len(report.Pages) != 2 {
		t.Errorf("expected 2 pages (internal only), got %d", len(report.Pages))
	}
	for _, p := range report.Pages {
		if p.URL == "https://external.example.com/page" {
			t.Error("external URL should not be in pages")
		}
	}
}

// TestAnalyze_Deduplication verifies that duplicate URLs are crawled only once.
func TestAnalyze_Deduplication(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`
			<a href="/target">Dup 1</a>
			<a href="/target">Dup 2</a>
		`))
	})
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Target"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 2

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}

	targetCount := 0
	for _, p := range report.Pages {
		if p.URL == server.URL+"/target" {
			targetCount++
		}
	}
	if targetCount != 1 {
		t.Errorf("expected 1 entry for /target (deduplicated), got %d", targetCount)
	}
}

// TestAnalyze_ContextCancellation verifies that Analyze returns valid JSON when context is cancelled.
func TestAnalyze_ContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<a href="/page1">Link</a>`))
	})
	mux.HandleFunc("/page1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Page 1"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 2
	opts.Concurrency = 2

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Logf("Analyze returned error on cancel: %v", err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if report.RootURL != server.URL {
		t.Errorf("RootURL = %q, want %q", report.RootURL, server.URL)
	}
}

// TestAnalyze_RateLimiting verifies that the global rate limiter enforces RPS limits.
func TestAnalyze_RateLimiting(t *testing.T) {
	var (
		reqMu    sync.Mutex
		reqTimes []time.Time
		linkIdx  int
	)

	handler := func(w http.ResponseWriter, r *http.Request) {
		reqMu.Lock()
		reqTimes = append(reqTimes, time.Now())
		linkIdx++
		reqMu.Unlock()

		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<a href="/p%d">next</a>`, linkIdx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/p", handler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 50
	opts.Concurrency = 1
	opts.RPS = 5

	_, err := Analyze(ctx, opts)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatal(err)
	}

	reqMu.Lock()
	count := len(reqTimes)
	reqMu.Unlock()

	if count > 8 {
		t.Errorf("expected <= 8 requests in 1s at 5 RPS, got %d (rate limit not enforced)", count)
	}
	if count < 2 {
		t.Errorf("too few requests executed: %d", count)
	}
}

// TestAnalyze_Retries_SuccessAfterFailures verifies that retries succeed after temporary failures.
func TestAnalyze_Retries_SuccessAfterFailures(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		if reqCount <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "OK")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Retries = 2
	opts.Delay = 10 * time.Millisecond

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}

	if report.Pages[0].HTTPStatus != http.StatusOK {
		t.Errorf("expected 200 after retries, got %d", report.Pages[0].HTTPStatus)
	}
	if report.Pages[0].Status != "ok" {
		t.Errorf("expected status ok, got %q", report.Pages[0].Status)
	}
	if reqCount != 3 {
		t.Errorf("expected 3 requests, got %d", reqCount)
	}
}

// TestAnalyze_Retries_Exhausted verifies that exhausted retries result in an error status.
func TestAnalyze_Retries_Exhausted(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.WriteHeader(http.StatusTooManyRequests)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Retries = 2
	opts.Delay = 10 * time.Millisecond

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}

	if report.Pages[0].Status != "error" {
		t.Errorf("expected status error after exhausted retries, got %q", report.Pages[0].Status)
	}
	if !strings.Contains(report.Pages[0].Error, "429") {
		t.Errorf("error should mention status 429, got: %s", report.Pages[0].Error)
	}
	if reqCount != 3 {
		t.Errorf("expected exactly 3 requests, got %d", reqCount)
	}
}

// TestAnalyze_Retries_ContextCancel verifies that context cancellation stops retries promptly.
func TestAnalyze_Retries_ContextCancel(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Retries = 5
	opts.Delay = 200 * time.Millisecond

	_, _ = Analyze(ctx, opts)

	if reqCount > 2 {
		t.Errorf("too many requests (%d) despite short context timeout", reqCount)
	}
}

// TestAnalyze_Assets_Deduplication verifies that each unique asset is fetched only once.
func TestAnalyze_Assets_Deduplication(t *testing.T) {
	var reqMu sync.Mutex
	reqCount := make(map[string]int)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqMu.Lock()
		reqCount[r.URL.Path]++
		reqMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<img src="/asset.png"><a href="/page2">next</a>`)
	})

	mux.HandleFunc("/page2", func(w http.ResponseWriter, r *http.Request) {
		reqMu.Lock()
		reqCount[r.URL.Path]++
		reqMu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<img src="/asset.png">`)
	})

	mux.HandleFunc("/asset.png", func(w http.ResponseWriter, r *http.Request) {
		reqMu.Lock()
		reqCount[r.URL.Path]++
		reqMu.Unlock()
		w.Header().Set("Content-Length", "1234")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake image data"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 2
	opts.Concurrency = 1

	_, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	reqMu.Lock()
	assetReqs := reqCount["/asset.png"]
	reqMu.Unlock()

	if assetReqs != 1 {
		t.Errorf("expected 1 request to /asset.png (deduplicated), got %d", assetReqs)
	}
}

// TestAnalyze_Assets_MissingContentLength verifies asset size is read from body when Content-Length is absent.
func TestAnalyze_Assets_MissingContentLength(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<script src="/asset.js"></script>`)
	})

	mux.HandleFunc("/asset.js", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", 500)))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}

	if len(report.Pages[0].Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(report.Pages[0].Assets))
	}

	asset := report.Pages[0].Assets[0]
	if asset.SizeBytes != 500 {
		t.Errorf("expected size 500, got %d", asset.SizeBytes)
	}
	if asset.Error != "" {
		t.Errorf("expected no error, got %q", asset.Error)
	}
}

// TestAnalyze_Assets_ErrorResponse verifies that asset errors are reported with status code and message.
func TestAnalyze_Assets_ErrorResponse(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<link rel="stylesheet" href="/missing.css">`)
	})

	mux.HandleFunc("/missing.css", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatal(err)
	}

	if len(report.Pages[0].Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(report.Pages[0].Assets))
	}

	asset := report.Pages[0].Assets[0]
	if asset.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", asset.StatusCode)
	}
	if asset.Error == "" || !strings.Contains(asset.Error, "Not Found") {
		t.Errorf("expected error with 'Not Found', got %q", asset.Error)
	}
}

// TestAnalyze_JSONStructure verifies that the output JSON contains all required fields.
func TestAnalyze_JSONStructure(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `
			<html>
			<head>
				<title>Test Page</title>
				<meta name="description" content="Test desc">
			</head>
			<body>
				<h1>Heading</h1>
				<img src="/img.png">
				<a href="/missing">Broken</a>
			</body>
			</html>
		`)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/img.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1
	opts.IndentJSON = true

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if report.RootURL != server.URL {
		t.Errorf("RootURL = %q, want %q", report.RootURL, server.URL)
	}
	if report.Depth != 1 {
		t.Errorf("Depth = %d, want 1", report.Depth)
	}
	if report.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should be set")
	}
	if len(report.Pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(report.Pages))
	}

	page := report.Pages[0]
	if page.URL == "" {
		t.Error("page.url should not be empty")
	}
	if page.Status == "" {
		t.Error("page.status should be set")
	}
	_ = page.Error

	if !page.SEO.HasTitle || page.SEO.Title != "Test Page" {
		t.Errorf("SEO title mismatch: %+v", page.SEO)
	}

	if len(page.Assets) < 1 {
		t.Errorf("expected at least 1 asset, got %d", len(page.Assets))
	}
	if len(page.Assets) > 0 {
		asset := page.Assets[0]
		if asset.URL == "" || asset.Type == "" {
			t.Errorf("asset missing required fields: %+v", asset)
		}
		_ = asset.Error
	}

	_ = page.BrokenLinks
}

// TestAnalyze_IndentJSON verifies that IndentJSON affects only formatting, not content.
func TestAnalyze_IndentJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><head><title>T</title></head><body>OK</body></html>"))
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1

	opts.IndentJSON = true
	resultIndented, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	opts.IndentJSON = false
	resultCompact, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}

	var rep1, rep2 Report
	var unmarshalErr error

	unmarshalErr = json.Unmarshal(resultIndented, &rep1)
	if unmarshalErr != nil {
		t.Fatalf("unmarshal indented failed: %v", unmarshalErr)
	}

	unmarshalErr = json.Unmarshal(resultCompact, &rep2)
	if unmarshalErr != nil {
		t.Fatalf("unmarshal compact failed: %v", unmarshalErr)
	}

	compareRep1 := rep1
	compareRep2 := rep2
	compareRep1.GeneratedAt = time.Time{}
	compareRep2.GeneratedAt = time.Time{}
	for i := range compareRep1.Pages {
		compareRep1.Pages[i].DiscoveredAt = time.Time{}
	}
	for i := range compareRep2.Pages {
		compareRep2.Pages[i].DiscoveredAt = time.Time{}
	}

	compact1, marshalErr := json.Marshal(compareRep1)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	compact2, marshalErr := json.Marshal(compareRep2)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}

	if string(compact1) != string(compact2) {
		t.Errorf("content differs (excluding timestamps):\nindented:  %s\ncompact: %s",
			string(compact1), string(compact2))
	}

	if !bytes.Contains(resultIndented, []byte("\n  ")) {
		t.Error("indented output should contain newlines with indentation")
	}
}
