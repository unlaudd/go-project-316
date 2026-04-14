# hexlet-go-crawler
### Hexlet tests and linter status:
[![Actions Status](https://github.com/unlaudd/go-project-316/actions/workflows/hexlet-check.yml/badge.svg)](https://github.com/unlaudd/go-project-316/actions)

Парсер сайтов на Go для анализа структуры веб-ресурсов.

## Быстрый старт

```bash
# Клонировать и перейти в директорию
git clone <repo>
cd hexlet-go-crawler

# Установить зависимости
go mod tidy

# Скомпилировать
make build

# Запустить с параметрами
make run URL=https://example.com
```

## Установка зависимостей
```bash
go mod tidy
```

## Команды Make

|        Команда     |                Описание                              |
|--------------------|------------------------------------------------------|
| make build         | Скомпилировать бинарник в bin/hexlet-go-crawler      |
| make test          | Запустить тесты с проверкой покрытия и race detector |
| make run URL=<url> | Запустить краулер против указанного URL              |
| make lint          | Проверить код через golangci-lint                    |
| make tidy          | Очистить go.mod от неиспользуемых зависимостей       |

## Примеры использования

```bash
# Базовый запуск
make run URL=https://example.com

# С кастомными параметрами
go run ./cmd/hexlet-go-crawler \
  --depth 2 \
  --timeout 30s \
  --workers 8 \
  --user-agent "MyBot/1.0" \
  --pretty \
  https://example.com

# Помощь
go run ./cmd/hexlet-go-crawler --help
```

## Глубина обхода (Depth)

Параметр `--depth` определяет максимальное количество переходов от стартовой страницы:
- `depth = 0` → обрабатывается только стартовая страница.
- `depth = 1` → стартовая + ссылки первого уровня (по умолчанию).
- `depth = N` → обход продолжается до N-го уровня вложенности.

**Ограничения:**
- Краулер следует только по ссылкам внутри **исходного домена**. Внешние ссылки проверяются на битость, но не добавляются в очередь обхода.
- Дубликаты URL автоматически отсекаются (каждая страница появляется в отчёте ровно один раз).
- При отмене (`Ctrl+C` или `context cancellation`) краулер завершает текущие запросы и возвращает валидный JSON с уже собранными страницами.

```bash
# Пример: обход до 3-го уровня
make run URL=https://example.com DEPTH=3

## Формат отчёта

```json
{
  "root_url": "https://example.com",
  "depth": 1,
  "generated_at": "2024-05-18T12:34:56Z",
  "pages": [
    {
      "url": "https://example.com",
      "depth": 0,
      "http_status": 200,
      "status": "ok",
      "error": ""
    }
  ]
}
```

## Ограничение скорости (`--delay` / `--rps`)

Краулер позволяет глобально ограничивать частоту HTTP-запросов, чтобы не перегружать целевой сайт:

| Флаг | Описание | Приоритет |
|------|----------|-----------|
| `--delay=200ms` | Фиксированная пауза между запросами | Низкий |
| `--rps=5` | Целевое количество запросов в секунду | **Высокий** (перекрывает `--delay`) |

Если оба параметра не указаны, запросы отправляются максимально быстро (ограничено только `--timeout` и `--workers`).

### Примеры использования
```bash
# 5 запросов в секунду (интервал ~200ms)
make run URL=https://example.com RPS=5

# Фиксированная задержка 1 секунда
make run URL=https://example.com DELAY=1s
```

## Архитектура

* cmd/hexlet-go-crawler/ — CLI-интерфейс
* crawler/ — основная логика краулера
* Тестируемость: http.Client инжектируется через Options, что позволяет подменять его в тестах

## Структура проекта

```
.
├── cmd/
│   └── hexlet-go-crawler/
│       └── main.go
├── crawler/
│   ├── crawler.go
│   ├── options.go
│   └── report.go
├── internal/
│   └── httpclient/
│       └── client.go
├── Makefile
├── go.mod
├── go.sum
└── README.md
```