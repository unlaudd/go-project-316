package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"code/crawler"
)

func main() {
	app := &cli.App{
		Name:    "hexlet-go-crawler",
		Usage:   "analyze a website structure",
		Version: "0.1.0",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "depth",
				Aliases: []string{"d"},
				Value:   10,
				Usage:   "crawl depth",
			},
			&cli.IntFlag{
				Name:    "retries",
				Value:   1,
				Usage:   "number of retries for failed requests",
			},
			&cli.DurationFlag{
				Name:    "delay",
				Value:   0,
				Usage:   "delay between requests (example: 200ms, 1s)",
			},
			&cli.DurationFlag{
				Name:    "timeout",
				Value:   15 * time.Second,
				Usage:   "per-request timeout",
			},
			&cli.Float64Flag{
				Name:    "rps",
				Value:   0,
				Usage:   "limit requests per second (overrides delay)",
			},
			&cli.StringFlag{
				Name:    "user-agent",
				Aliases: []string{"ua"},
				Usage:   "custom user agent",
			},
			&cli.IntFlag{
				Name:    "workers",
				Aliases: []string{"w"},
				Value:   4,
				Usage:   "number of concurrent workers",
			},
			&cli.BoolFlag{
				Name:    "pretty",
				Aliases: []string{"p"},
				Usage:   "pretty-print JSON output",
			},
		},
		Action: run,
	}

	// Обработка сигналов для корректной отмены через context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.RunContext(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

func run(c *cli.Context) error {
    // Проверка наличия URL
    if c.NArg() == 0 {
        if err := cli.ShowAppHelp(c); err != nil {
            fmt.Fprintf(os.Stderr, "failed to show help: %v\n", err)
        }
        return nil
    }

	url := c.Args().First()

	opts := crawler.DefaultOptions()
	opts.URL = url
	opts.Depth = c.Int("depth")
	opts.Retries = c.Int("retries")
	opts.Delay = c.Duration("delay")
	opts.Timeout = c.Duration("timeout")
	opts.RPS = c.Float64("rps")
	opts.UserAgent = c.String("user-agent")
	opts.Concurrency = c.Int("workers")
	opts.IndentJSON = c.Bool("pretty")
	opts.HTTPClient = &http.Client{
		Timeout: opts.Timeout,
	}

	ctx := c.Context

	result, err := crawler.Analyze(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	// Выводим отчёт в stdout
	if len(result) > 0 {
		fmt.Println(string(result))
	}

	return nil
}
