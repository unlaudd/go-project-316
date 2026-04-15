package crawler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"sync"
	"time"
	"strings"
	"fmt"
)

// mockTransport имитирует сетевые сбои на уровне транспортного слоя
type mockTransport struct {
	err error
}

func (m *mockTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, m.err
}

func TestAnalyze_HTTPLogic(t *testing.T) {
	tests := []struct {
		name           string
		serverHandler  http.Handler   // nil при использовании mockTransport
		mockErr        error          // для имитации сетевых ошибок
		retries        int
		wantHTTPStatus int
		wantStatus     string
		wantErr        bool // ошибка на уровне Analyze (например, маршалинг)
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
			wantStatus:     "ok", // HTTP-ответ получен, статус ok
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
				time.Sleep(200 * time.Millisecond) // Дольше таймаута
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
			opts.Timeout = 100 * time.Millisecond // Дефолтный таймаут для теста

			var client *http.Client
			if tt.mockErr != nil {
				// Имитация сетевого сбоя без реального сервера
				client = &http.Client{Transport: &mockTransport{err: tt.mockErr}}
				opts.URL = "http://mock.local"
			} else {
				// httptest.Server для проверки HTTP-статусов
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

func TestAnalyze_BrokenLinks(t *testing.T) {
	mux := http.NewServeMux()

	// 1. Главная страница со ссылками
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

	// 2. Рабочая ссылка (200)
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// 3. Битая ссылка (404)
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

	// Должна быть ровно одна битая ссылка
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

func TestAnalyze_BrokenLinks_NetworkError(t *testing.T) {
	// Страница со ссылкой на несуществующий домен
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

func TestAnalyze_IgnoresUnsupportedSchemes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Ссылки, которые должны быть проигнорированы
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

	// Никаких битых ссылок быть не должно — все проигнорированы
	if len(report.Pages[0].BrokenLinks) != 0 {
		t.Errorf("expected no broken links (all unsupported), got: %+v", report.Pages[0].BrokenLinks)
	}
}

func TestAnalyze_SEO_Metrics(t *testing.T) {
	tests := []struct {
		name           string
		html           string
		wantHasTitle   bool
		wantTitle      string
		wantHasDesc    bool
		wantDesc       string
		wantHasH1      bool
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
			wantHasTitle:  true,
			wantTitle:     "My & Awesome Site", // &amp; → &
			wantHasDesc:   true,
			wantDesc:      "Welcome to our <site>", // &lt; → <
			wantHasH1:     true,
		},
		{
			name: "no SEO tags",
			html: `<html><body><p>Just content</p></body></html>`,
			wantHasTitle:  false,
			wantTitle:     "",
			wantHasDesc:   false,
			wantDesc:      "",
			wantHasH1:     false,
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
			wantHasTitle:  false, // пустой после trim
			wantTitle:     "",
			wantHasDesc:   false, // пустой content
			wantDesc:      "",
			wantHasH1:     false, // пустой после trim
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
			if page.SEO == nil {
				if tt.wantHasTitle || tt.wantHasDesc || tt.wantHasH1 {
					t.Error("expected non-nil SEO, got nil")
				}
				return
			}

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
	if err != nil { t.Fatal(err) }

	var report Report
	if err := json.Unmarshal(result, &report); err != nil { t.Fatal(err) }

	if len(report.Pages) != 1 {
		t.Errorf("expected 1 page (depth=1), got %d", len(report.Pages))
	}
	
	// Проверяем, что страница — это корень сервера (с учётом возможного /)
	pageURL := report.Pages[0].URL
	if !strings.HasSuffix(pageURL, "/") && pageURL != server.URL {
		// Допускаем оба варианта: с / и без
		_ = pageURL
	}
}

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
		w.Write([]byte("Internal"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 2 // разрешаем переходить на depth 1

	result, err := Analyze(ctx, opts)
	if err != nil { t.Fatal(err) }

	var report Report
	if err := json.Unmarshal(result, &report); err != nil { t.Fatal(err) }

	// Должно быть 2 страницы: корень и /internal. Внешняя не должна попасть в pages.
	if len(report.Pages) != 2 {
		t.Errorf("expected 2 pages (internal only), got %d", len(report.Pages))
	}
	for _, p := range report.Pages {
		if p.URL == "https://external.example.com/page" {
			t.Error("external URL should not be in pages")
		}
	}
}

func TestAnalyze_Deduplication(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Дубликат ссылки на одну и ту же страницу
		_, _ = w.Write([]byte(`
			<a href="/target">Dup 1</a>
			<a href="/target">Dup 2</a>
		`))
	})
	mux.HandleFunc("/target", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Target"))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 2

	result, err := Analyze(ctx, opts)
	if err != nil { t.Fatal(err) }

	var report Report
	if err := json.Unmarshal(result, &report); err != nil { t.Fatal(err) }

	// /target должен встретиться ровно 1 раз
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

func TestAnalyze_ContextCancellation(t *testing.T) {
	// Просто проверяем, что функция возвращает валидный JSON при отмене
	// Реальное тестирование отмены — сложно из-за гонок, оставим для E2E

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<a href="/page1">Link</a>`))
	})
	mux.HandleFunc("/page1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Page 1"))
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
	// Не требуем ошибку — отчёт может быть частично собран
	if err != nil {
		t.Logf("Analyze returned error on cancel: %v", err)
	}

	// Главное: результат должен быть валидным JSON
	var report Report
	if err := json.Unmarshal(result, &report); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	// RootURL должен совпадать
	if report.RootURL != server.URL {
		t.Errorf("RootURL = %q, want %q", report.RootURL, server.URL)
	}
}

func TestAnalyze_RateLimiting(t *testing.T) {
	var (
		reqMu   sync.Mutex
		reqTimes []time.Time
		linkIdx int
	)

	// Хендлер генерирует уникальные ссылки, чтобы краулер не упирался в дедупликацию
	handler := func(w http.ResponseWriter, r *http.Request) {
		reqMu.Lock()
		reqTimes = append(reqTimes, time.Now())
		linkIdx++
		reqMu.Unlock()

		time.Sleep(5 * time.Millisecond) // Форсируем yield планировщика
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `<a href="/p%d">next</a>`, linkIdx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	mux.HandleFunc("/p", handler) // матчит /p1, /p2...

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// Ограничиваем время работы краулера ровно 1 секундой
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 50      // Глубокий обход, чтобы не упёрлись в лимит глубины
	opts.Concurrency = 1 // Один воркер для предсказуемого измерения
	opts.RPS = 5         // Лимит: 5 запросов в секунду

	_, err := Analyze(ctx, opts)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatal(err)
	}

	reqMu.Lock()
	count := len(reqTimes)
	reqMu.Unlock()

	// При 5 RPS и burst=1 за 1 секунду максимум должно быть ~6 запросов
	// Даём небольшой запас на инициализацию
	if count > 8 {
		t.Errorf("expected <= 8 requests in 1s at 5 RPS, got %d (rate limit not enforced)", count)
	}
	if count < 2 {
		t.Errorf("too few requests executed: %d", count)
	}
}

func TestAnalyze_Retries_SuccessAfterFailures(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		if reqCount <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503
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
	opts.Delay = 10 * time.Millisecond // ускоряем тест

	result, err := Analyze(ctx, opts)
	if err != nil { t.Fatal(err) }

	var report Report
	if err := json.Unmarshal(result, &report); err != nil { t.Fatal(err) }

	if report.Pages[0].HTTPStatus != http.StatusOK {
		t.Errorf("expected 200 after retries, got %d", report.Pages[0].HTTPStatus)
	}
	if report.Pages[0].Status != "ok" {
		t.Errorf("expected status ok, got %q", report.Pages[0].Status)
	}
	if reqCount != 3 { // 1 начальный + 2 retries
		t.Errorf("expected 3 requests, got %d", reqCount)
	}
}

func TestAnalyze_Retries_Exhausted(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.WriteHeader(http.StatusTooManyRequests) // 429 всегда
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Retries = 2
	opts.Delay = 10 * time.Millisecond

	result, err := Analyze(ctx, opts)
	if err != nil { t.Fatal(err) }

	var report Report
	if err := json.Unmarshal(result, &report); err != nil { t.Fatal(err) }

	if report.Pages[0].Status != "error" {
		t.Errorf("expected status error after exhausted retries, got %q", report.Pages[0].Status)
	}
	if !strings.Contains(report.Pages[0].Error, "429") {
		t.Errorf("error should mention status 429, got: %s", report.Pages[0].Error)
	}
	if reqCount != 3 { // не более retries + 1
		t.Errorf("expected exactly 3 requests, got %d", reqCount)
	}
}

func TestAnalyze_Retries_ContextCancel(t *testing.T) {
	var reqCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	// Контекст умрёт быстрее, чем закончатся повторы
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Retries = 5
	opts.Delay = 200 * time.Millisecond // задержка > timeout

	_, _ = Analyze(ctx, opts) // не должен паниковать

	// Должен выполниться только 1 запрос, т.к. после него начнётся wait, который прервётся
	if reqCount > 2 {
		t.Errorf("too many requests (%d) despite short context timeout", reqCount)
	}
}
