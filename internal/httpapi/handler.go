package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jeffotoni/goretry-poc/internal/retry"
	"github.com/jeffotoni/quick"
)

type Handler struct {
	Manager   *retry.Manager
	TargetURL string
}

type dispatchResponse struct {
	Status     string `json:"status"`
	RequestID  string `json:"request_id"`
	Attempt    int    `json:"attempt"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Error      string `json:"error,omitempty"`
	Body       string `json:"body,omitempty"`
}

func (h *Handler) Dispatch(c *quick.Ctx) error {
	c.Set("Content-Type", "application/json")
	body := append([]byte(nil), c.Body()...)
	if len(body) == 0 || !json.Valid(body) {
		return c.Status(http.StatusBadRequest).JSON(map[string]any{
			"status": "invalid_json",
			"error":  "request body must contain valid JSON",
		})
	}

	requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
	if requestID == "" {
		requestID = retry.NewID()
	}

	result := h.Manager.Dispatch(c.Request.Context(), retry.Task{
		ID:      requestID,
		Method:  http.MethodPost,
		URL:     h.TargetURL,
		Headers: requestHeaders(c),
		Body:    body,
	})

	if result.Attempt.Err == nil && result.Attempt.StatusCode >= 200 && result.Attempt.StatusCode < 400 {
		c.Response.Header().Set("Content-Type", "application/json")
		return c.Status(result.Attempt.StatusCode).Byte(result.Attempt.Body)
	}

	if result.Queued {
		return c.Status(http.StatusAccepted).JSON(dispatchResponse{
			Status:     "retry_queued",
			RequestID:  requestID,
			Attempt:    result.Task.Attempts,
			HTTPStatus: result.Attempt.StatusCode,
			Error:      errorString(result.Attempt.Err),
		})
	}

	if result.QueueFull {
		return c.Status(http.StatusServiceUnavailable).JSON(dispatchResponse{
			Status:    "retry_queue_full",
			RequestID: requestID,
			Attempt:   result.Task.Attempts,
			Error:     "retry queue is full",
		})
	}

	if result.RetryDisabled && result.Retryable {
		return c.Status(http.StatusBadGateway).JSON(dispatchResponse{
			Status:     "retry_disabled",
			RequestID:  requestID,
			Attempt:    result.Task.Attempts,
			HTTPStatus: result.Attempt.StatusCode,
			Error:      errorString(result.Attempt.Err),
		})
	}

	status := result.Attempt.StatusCode
	if status < 400 {
		status = http.StatusBadGateway
	}
	return c.Status(status).JSON(dispatchResponse{
		Status:     "failed",
		RequestID:  requestID,
		Attempt:    result.Task.Attempts,
		HTTPStatus: result.Attempt.StatusCode,
		Error:      errorString(result.Attempt.Err),
		Body:       string(result.Attempt.Body),
	})
}

func (h *Handler) Health(c *quick.Ctx) error {
	c.Set("Content-Type", "application/json")
	return c.Status(http.StatusOK).JSON(map[string]string{"status": "ok"})
}

func (h *Handler) Stats(c *quick.Ctx) error {
	c.Set("Content-Type", "application/json")
	return c.Status(http.StatusOK).JSON(h.Manager.Stats())
}

func requestHeaders(c *quick.Ctx) map[string]string {
	result := make(map[string]string)
	for key, values := range c.GetReqHeadersAll() {
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Host") {
			continue
		}
		if len(values) > 0 {
			result[key] = strings.Join(values, ",")
		}
	}
	return result
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
