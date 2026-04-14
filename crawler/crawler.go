package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Analyze — точка входа в краулер.
// Принимает context для отмены и Options с параметрами.
// Возвращает JSON-отчёт в виде []byte и ошибку.
func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	// Валидация входных параметров
	if opts.URL == "" {
		return nil, fmt.Errorf("URL is required")
	}

	// Если клиент не передан — создаём дефолтный (для безопасности)
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: opts.Timeout}
	} else {
		// Убедимся, что у клиента есть таймаут
		if client.Timeout == 0 {
			client.Timeout = opts.Timeout
		}
	}

	// Инициализация отчёта
	report := Report{
		RootURL:    opts.URL,
		Depth:      opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages:      make([]PageReport, 0),
	}

	// Обход начинаем с корневой страницы
	pageReport := crawlPage(ctx, client, opts.URL, 0, opts)
	report.Pages = append(report.Pages, pageReport)

	// Формирование JSON
	indent := ""
	if opts.IndentJSON {
		indent = "  "
	}

	result, err := json.MarshalIndent(report, "", indent)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal report: %w", err)
	}

	return result, nil
}

// crawlPage выполняет запрос к одной странице и возвращает отчёт о ней
func crawlPage(ctx context.Context, client *http.Client, url string, depth int, opts Options) PageReport {
	report := PageReport{
		URL:   url,
		Depth: depth,
	}

	// Проверка контекста перед запросом
	select {
	case <-ctx.Done():
		report.Status = "skipped"
		report.Error = ctx.Err().Error()
		return report
	default:
	}

	// Создание запроса
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		report.Status = "error"
		report.Error = fmt.Sprintf("failed to create request: %v", err)
		return report
	}

	// Установка User-Agent
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	} else {
		req.Header.Set("User-Agent", "hexlet-go-crawler/1.0")
	}

	// Выполнение запроса с повторами
	var resp *http.Response
	for attempt := 0; attempt <= opts.Retries; attempt++ {
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		if attempt < opts.Retries {
			// Простая задержка между попытками
			select {
			case <-ctx.Done():
				report.Status = "skipped"
				report.Error = ctx.Err().Error()
				return report
			case <-time.After(opts.Delay):
			}
		}
	}

	if err != nil {
		report.Status = "error"
		report.Error = fmt.Sprintf("request failed: %v", err)
		return report
	}
	defer func() {
    	if closeErr := resp.Body.Close(); closeErr != nil {
        	// Логируем, но не прерываем выполнение — тело уже прочитано
        	// В продакшене здесь можно использовать logger.Warn(closeErr)
        	_ = closeErr
    	}
	}()

	report.HTTPStatus = resp.StatusCode
	report.Status = "ok"

	return report
}