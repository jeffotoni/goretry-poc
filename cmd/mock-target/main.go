package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jeffotoni/goretry-poc/internal/logging"
	"github.com/jeffotoni/goretry-poc/internal/pkg/env"
	"github.com/jeffotoni/quick"
)

func main() {
	logger := logging.New(env.GetString("POC_LOG_FORMAT", "json"), "mock-target")
	q := quick.New()
	q.Post("/target", func(c *quick.Ctx) error {
		c.Set("Content-Type", "application/json")
		body := append([]byte(nil), c.Body()...)
		attempt := c.GetHeader("X-Retry-POC-Attempt-ID")
		requestID := c.GetHeader("X-Request-ID")
		status := statusFromHeaders(c.GetHeader("X-Mock-Status"), c.GetHeader("X-Mock-Status-Sequence"), attempt)

		logger.Info("mock target received request",
			"status", status,
			"request_id", requestID,
			"attempt", attempt,
			"body", string(body),
		)

		if retryAfter := strings.TrimSpace(c.GetHeader("X-Mock-Retry-After")); retryAfter != "" {
			c.Set("Retry-After", retryAfter)
		}
		response := map[string]any{
			"status":      status,
			"received_at": time.Now().UTC().Format(time.RFC3339Nano),
			"request_id":  requestID,
			"attempt":     attempt,
			"body":        json.RawMessage(body),
		}
		return c.Status(status).JSON(response)
	})
	q.Get("/health", func(c *quick.Ctx) error {
		c.Set("Content-Type", "application/json")
		return c.Status(http.StatusOK).JSON(map[string]string{"status": "ok"})
	})

	address := env.GetString("POC_MOCK_ADDR", ":8081")
	quickServer, shutdown, err := q.ListenWithShutdown(address)
	if err != nil {
		logger.Error("mock target failed", "error", err)
		os.Exit(1)
	}
	logger.Info("mock target started", "address", quickServer.Addr)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	signal.Stop(signals)
	shutdown()
}

func statusFromHeader(value string) int {
	if value == "" {
		return http.StatusOK
	}
	status, err := strconv.Atoi(value)
	if err != nil || status < 100 || status > 599 {
		return http.StatusBadRequest
	}
	return status
}

func statusFromHeaders(status, sequence, attemptID string) int {
	if strings.TrimSpace(sequence) == "" {
		return statusFromHeader(status)
	}
	attempt := 1
	if index := strings.LastIndex(attemptID, "/"); index >= 0 {
		if parsed, err := strconv.Atoi(attemptID[index+1:]); err == nil && parsed > 0 {
			attempt = parsed
		}
	}
	values := strings.Split(sequence, ",")
	if attempt > len(values) {
		attempt = len(values)
	}
	return statusFromHeader(strings.TrimSpace(values[attempt-1]))
}
