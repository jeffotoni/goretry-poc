package retry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeffotoni/goretry-poc/internal/logging"
)

func TestDispatchRetriesTemporaryStatus(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls.Add(1) < 3 {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	manager := NewManager(Config{
		Enabled:     true,
		Window:      2 * time.Second,
		BaseDelay:   time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
		Workers:     1,
		QueueSize:   1,
		HTTPTimeout: time.Second,
		Jitter:      false,
	}, logging.New("json", "retry-test"))
	manager.Start()
	defer manager.Close()

	result := manager.Dispatch(context.Background(), Task{URL: server.URL, Body: []byte(`{"hello":"world"}`)})
	if !result.Queued {
		t.Fatalf("expected retry to be queued: %+v", result)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if manager.Stats().Succeeded == 1 {
			if got := calls.Load(); got != 3 {
				t.Fatalf("expected 3 calls, got %d", got)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("retry did not succeed: %+v", manager.Stats())
}

func TestDispatchDoesNotRetryPermanentStatus(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	manager := NewManager(Config{
		Enabled:     true,
		Window:      time.Second,
		BaseDelay:   time.Millisecond,
		MaxDelay:    time.Millisecond,
		Workers:     1,
		QueueSize:   1,
		HTTPTimeout: time.Second,
		Jitter:      false,
	}, logging.New("json", "retry-test"))
	manager.Start()
	defer manager.Close()

	result := manager.Dispatch(context.Background(), Task{URL: server.URL, Body: []byte(`{}`)})
	if result.Queued || result.Retryable {
		t.Fatalf("did not expect retry: %+v", result)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one call, got %d", got)
	}
}
