package crawler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnalyze_InvalidURL(t *testing.T) {
	ctx := context.Background()
	opts := DefaultOptions()
	opts.URL = ""

	_, err := Analyze(ctx, opts)
	if err == nil {
		t.Error("expected error for empty URL, got nil")
	}
}

func TestAnalyze_SuccessWithMockClient(t *testing.T) {
	// Создаём тестовый сервер
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write([]byte("<html><body>Test</body></html>"))
		if writeErr != nil {
    		t.Errorf("failed to write response: %v", writeErr)
    		return
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := DefaultOptions()
	opts.URL = server.URL
	opts.Depth = 1
	opts.IndentJSON = true

	result, err := Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if len(result) == 0 {
		t.Error("expected non-empty JSON result")
	}

	// Простая проверка, что в ответе есть ожидаемые поля
	expectedFields := []string{"root_url", "generated_at", "pages", "http_status"}
	for _, field := range expectedFields {
		if !contains(result, field) {
			t.Errorf("result missing expected field: %s", field)
		}
	}
}

func contains(data []byte, substr string) bool {
	return len(data) >= len(substr) && 
		(string(data) == substr || 
		 len(substr) == 0 || 
		 findSubstring(data, substr))
}

func findSubstring(data []byte, substr string) bool {
	s := string(data)
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
