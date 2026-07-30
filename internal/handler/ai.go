package handler

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type AIDeps struct {
	openCodeURL  string
	ShuttingDown <-chan struct{}
}

func NewAIDeps(openCodeURL string, shuttingDown <-chan struct{}) *AIDeps {
	return &AIDeps{openCodeURL: strings.TrimRight(openCodeURL, "/"), ShuttingDown: shuttingDown}
}

func (d *AIDeps) httpClient() *http.Client {
	return &http.Client{Timeout: 0}
}

func (d *AIDeps) jsonClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute}
}

func (d *AIDeps) proxyJSON(c *fiber.Ctx) error {
	targetPath := c.Params("*")
	if targetPath == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing path"})
	}

	url := d.openCodeURL + "/" + targetPath
	if c.Request().URI().QueryString() != nil {
		url += "?" + string(c.Request().URI().QueryString())
	}

	var body io.Reader
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
		body = strings.NewReader(string(c.Body()))
	}

	req, err := http.NewRequest(c.Method(), url, body)
	if err != nil {
		slog.Error("ai proxy: create request failed", "error", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream error"})
	}
	req.Header.Set("Content-Type", c.Get("Content-Type"))

	resp, err := d.jsonClient().Do(req)
	if err != nil {
		slog.Error("ai proxy: request failed", "error", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream error"})
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("ai proxy: read response failed", "error", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream error"})
	}

	c.Set("Content-Type", resp.Header.Get("Content-Type"))
	return c.Status(resp.StatusCode).Send(respBody)
}

func (d *AIDeps) proxyGlobalSSE(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		eventURL := d.openCodeURL + "/global/event"

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			select {
			case <-d.ShuttingDown:
				cancel()
			case <-ctx.Done():
			}
		}()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventURL, nil)
		if err != nil {
			slog.Error("ai sse: create request failed", "error", err)
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")

		resp, err := d.httpClient().Do(req)
		if err != nil {
			slog.Error("ai sse: connect failed", "error", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Error("ai sse: unexpected status", "status", resp.StatusCode)
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if _, err := w.WriteString(line + "\n"); err != nil {
				return
			}
			if strings.HasPrefix(line, "data:") || line == "" {
				if err := w.Flush(); err != nil {
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			slog.Error("ai sse: read error", "error", err)
		}
	})

	return nil
}
