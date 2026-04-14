package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/net/html"
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

// crawlPage выполняет запрос к странице и собирает отчёт
func crawlPage(ctx context.Context, client *http.Client, url string, depth int, opts Options) PageReport {
	report := PageReport{URL: url, Depth: depth}

	// Проверка контекста
	if err := ctx.Err(); err != nil {
		report.Status = "skipped"
		report.Error = err.Error()
		return report
	}

	// Выполняем запрос с повторами
	resp, err := fetchWithRetries(ctx, client, url, opts)
	if err != nil {
		report.Status = "error"
		report.Error = fmt.Sprintf("request failed: %v", err)
		return report
	}
	defer func() { _ = resp.Body.Close() }()

	report.HTTPStatus = resp.StatusCode
	report.Status = "ok"

	// Обрабатываем контент страницы
	analyzePageContent(ctx, client, resp.Body, opts.URL, &report, opts)

	report.DiscoveredAt = time.Now().UTC()
	return report
}

// fetchWithRetries выполняет HTTP-запрос с повторами при ошибках
func fetchWithRetries(ctx context.Context, client *http.Client, url string, opts Options) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	ua := opts.UserAgent
	if ua == "" {
		ua = "hexlet-go-crawler/1.0"
	}
	req.Header.Set("User-Agent", ua)

	var resp *http.Response
	var lastErr error

	for attempt := 0; attempt <= opts.Retries; attempt++ {
		resp, lastErr = client.Do(req)
		if lastErr == nil {
			return resp, nil
		}
		if attempt < opts.Retries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(opts.Delay):
				// продолжаем следующую попытку
			}
		}
	}
	return nil, lastErr
}

// analyzePageContent парсит HTML, извлекает SEO и проверяет ссылки
func analyzePageContent(
	ctx context.Context,
	client *http.Client,
	body io.Reader,
	baseURL string,
	report *PageReport,
	opts Options,
) {
	doc, err := html.Parse(body)
	if err != nil {
		// Ошибка парсинга не ломает страницу
		return
	}

	// 🆕 Извлекаем SEO-метрики
	seo := extractSEO(doc)
	if seo.HasTitle || seo.HasDescription || seo.HasH1 {
		report.SEO = seo
	}

	// Проверяем ссылки на "битость"
	links := extractLinks(baseURL, doc)
	for _, link := range links {
		if ctx.Err() != nil {
			break
		}
		if broken := checkLink(ctx, client, link); broken != nil {
			report.BrokenLinks = append(report.BrokenLinks, *broken)
		}
	}
}
