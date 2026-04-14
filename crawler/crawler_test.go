package crawler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
