package crawler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"strings"
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
