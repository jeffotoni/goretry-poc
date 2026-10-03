package retry

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeffotoni/goretry-poc/internal/logging"
	req "github.com/jeffotoni/req"
)

var retryableStatuses = map[int]struct{}{
	http.StatusRequestTimeout:      {},
	http.StatusTooManyRequests:     {},
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
}

type Config struct {
	Enabled       bool
	Window        time.Duration
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	Workers       int
	QueueSize     int
	HTTPTimeout   time.Duration
	Jitter        bool
	RetryStatuses map[int]struct{}
}

type Task struct {
	ID         string
	Method     string
	URL        string
	Headers    map[string]string
	Body       []byte
	Created    time.Time
	Deadline   time.Time
	Attempts   int
	RetryAfter time.Duration
}

type AttemptResult struct {
	StatusCode int
	Body       []byte
	Err        error
	RetryAfter time.Duration
}

type DispatchResult struct {
	Task          Task
	Attempt       AttemptResult
	Queued        bool
	QueueFull     bool
	Retryable     bool
	RetryDisabled bool
}

type Stats struct {
	Queued       int64 `json:"queued"`
	Running      int64 `json:"running"`
	Retries      int64 `json:"retries"`
	Succeeded    int64 `json:"succeeded"`
	Exhausted    int64 `json:"exhausted"`
	QueueFull    int64 `json:"queue_full"`
	NonRetryable int64 `json:"non_retryable"`
}

type Manager struct {
	cfg    Config
	client *req.Client
	logger *logging.Logger
	ctx    context.Context
	cancel context.CancelFunc

	queue  chan Task
	stop   chan struct{}
	closed chan struct{}
	once   sync.Once
	wg     sync.WaitGroup

	stats managerStats
	resp  *responseObserver
}

type managerStats struct {
	queued       atomic.Int64
	running      atomic.Int64
	retries      atomic.Int64
	succeeded    atomic.Int64
	exhausted    atomic.Int64
	queueFull    atomic.Int64
	nonRetryable atomic.Int64
}

type responseObserver struct {
	values sync.Map
}

func NewManager(cfg Config, logger *logging.Logger) *Manager {
	if cfg.Window <= 0 {
		cfg.Window = 3 * time.Minute
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}
	if cfg.RetryStatuses == nil {
		cfg.RetryStatuses = retryableStatuses
	}
	if logger == nil {
		logger = logging.New("json", "goretry-poc")
	}

	observer := &responseObserver{}
	managerCtx, managerCancel := context.WithCancel(context.Background())
	httpClient := &http.Client{
		Timeout: cfg.HTTPTimeout,
		Transport: &observingTransport{
			base:     http.DefaultTransport,
			observer: observer,
		},
	}

	return &Manager{
		cfg:    cfg,
		client: req.New(req.WithCustomHTTPClient(httpClient), req.WithTimeout(cfg.HTTPTimeout), req.WithLogger(false)),
		logger: logger,
		ctx:    managerCtx,
		cancel: managerCancel,
		queue:  make(chan Task, cfg.QueueSize),
		stop:   make(chan struct{}),
		closed: make(chan struct{}),
		resp:   observer,
	}
}

func (m *Manager) Start() {
	for i := 0; i < m.cfg.Workers; i++ {
		m.wg.Add(1)
		go m.worker(i + 1)
	}
	go func() {
		m.wg.Wait()
		close(m.closed)
	}()
}

func (m *Manager) Close() {
	m.once.Do(func() {
		m.cancel()
		close(m.stop)
	})
	<-m.closed
}

func (m *Manager) Dispatch(ctx context.Context, task Task) DispatchResult {
	if task.ID == "" {
		task.ID = NewID()
	}
	if task.Method == "" {
		task.Method = http.MethodPost
	}
	if task.Created.IsZero() {
		task.Created = time.Now()
	}
	if task.Deadline.IsZero() {
		task.Deadline = task.Created.Add(m.cfg.Window)
	}
	if task.Headers == nil {
		task.Headers = make(map[string]string)
	}
	task.Body = append([]byte(nil), task.Body...)
	task.Attempts = 1

	result := m.attempt(ctx, task)
	dispatch := DispatchResult{Task: task, Attempt: result, Retryable: m.retryable(result)}
	if result.Err == nil && result.StatusCode >= 200 && result.StatusCode < 400 {
		m.stats.succeeded.Add(1)
		return dispatch
	}
	if !dispatch.Retryable {
		m.stats.nonRetryable.Add(1)
		return dispatch
	}
	if !m.cfg.Enabled {
		dispatch.RetryDisabled = true
		return dispatch
	}
	task.RetryAfter = result.RetryAfter
	if !m.enqueue(task) {
		dispatch.QueueFull = true
	}
	dispatch.Queued = !dispatch.QueueFull
	return dispatch
}

func (m *Manager) Stats() Stats {
	return Stats{
		Queued:       m.stats.queued.Load(),
		Running:      m.stats.running.Load(),
		Retries:      m.stats.retries.Load(),
		Succeeded:    m.stats.succeeded.Load(),
		Exhausted:    m.stats.exhausted.Load(),
		QueueFull:    m.stats.queueFull.Load(),
		NonRetryable: m.stats.nonRetryable.Load(),
	}
}

func (m *Manager) worker(workerID int) {
	defer m.wg.Done()
	for {
		select {
		case <-m.stop:
			return
		case task := <-m.queue:
			m.process(workerID, task)
		}
	}
}

func (m *Manager) process(workerID int, task Task) {
	for {
		if m.expired(task) {
			m.stats.exhausted.Add(1)
			m.logger.Warn("retry window expired", "worker", workerID, "request_id", task.ID, "attempts", task.Attempts)
			return
		}

		delay := m.delay(task.Attempts, task.RetryAfter)
		task.RetryAfter = 0
		if !m.wait(task, delay) {
			m.stats.exhausted.Add(1)
			return
		}

		task.Attempts++
		m.stats.retries.Add(1)
		m.logger.Info("retry attempt", "worker", workerID, "request_id", task.ID, "attempt", task.Attempts)

		result := m.attempt(m.ctx, task)
		if result.Err == nil && result.StatusCode >= 200 && result.StatusCode < 400 {
			m.stats.succeeded.Add(1)
			m.logger.Info("retry succeeded", "worker", workerID, "request_id", task.ID, "attempt", task.Attempts, "status", result.StatusCode)
			return
		}
		if !m.retryable(result) {
			m.stats.nonRetryable.Add(1)
			m.logger.Warn("retry stopped by non-retryable result", "worker", workerID, "request_id", task.ID, "attempt", task.Attempts, "status", result.StatusCode, "error", errorString(result.Err))
			return
		}
		task.RetryAfter = result.RetryAfter
	}
}

func (m *Manager) enqueue(task Task) bool {
	select {
	case <-m.stop:
		return false
	case m.queue <- task:
		m.stats.queued.Add(1)
		return true
	default:
		m.stats.queueFull.Add(1)
		m.logger.Error("retry queue full", "request_id", task.ID)
		return false
	}
}

func (m *Manager) attempt(parent context.Context, task Task) AttemptResult {
	ctx := parent
	if ctx == nil {
		ctx = m.ctx
	}
	if !task.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, task.Deadline)
		defer cancel()
	}

	attemptID := task.ID + "/" + strconv.Itoa(task.Attempts)
	headers := cloneHeaders(task.Headers)
	delete(headers, "Content-Length")
	delete(headers, "Host")
	headers["X-Retry-POC-Attempt-ID"] = attemptID
	delete(headers, "X-Retry-After-Seconds")

	m.stats.running.Add(1)
	defer m.stats.running.Add(-1)
	resp, err := m.client.Post(ctx, task.URL, task.Body, headers)
	result := AttemptResult{Err: err}
	if resp != nil {
		result.StatusCode = resp.StatusCode
		result.Body = append([]byte(nil), resp.Body...)
	}
	if value, ok := m.resp.values.LoadAndDelete(attemptID); ok {
		result.RetryAfter = parseRetryAfter(value.(string), time.Now())
	}
	return result
}

func (m *Manager) retryable(result AttemptResult) bool {
	if result.Err != nil {
		return !errors.Is(result.Err, context.Canceled)
	}
	_, ok := m.cfg.RetryStatuses[result.StatusCode]
	return ok
}

func (m *Manager) expired(task Task) bool {
	return !task.Deadline.IsZero() && !time.Now().Before(task.Deadline)
}

func (m *Manager) wait(task Task, delay time.Duration) bool {
	remaining := time.Until(task.Deadline)
	if remaining <= 0 {
		return false
	}
	if delay > remaining {
		delay = remaining
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return time.Now().Before(task.Deadline)
	case <-m.stop:
		return false
	}
}

func (m *Manager) delay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return minDuration(retryAfter, m.cfg.MaxDelay)
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := m.cfg.BaseDelay
	for i := 1; i < attempt; i++ {
		if delay >= m.cfg.MaxDelay/2 {
			delay = m.cfg.MaxDelay
			break
		}
		delay *= 2
	}
	delay = minDuration(delay, m.cfg.MaxDelay)
	if !m.cfg.Jitter || delay <= 1 {
		return delay
	}
	spread := delay / 5
	return minDuration(delay-spread+time.Duration(mathrand.Int64N(int64(spread*2+1))), m.cfg.MaxDelay)
}

func (m *Manager) String() string {
	return fmt.Sprintf("retry manager workers=%d queue=%d", m.cfg.Workers, m.cfg.QueueSize)
}

func NewID() string {
	var data [16]byte
	if _, err := cryptorand.Read(data[:]); err != nil {
		return fmt.Sprintf("retry-%d", time.Now().UnixNano())
	}
	return strings.TrimRight(hex.EncodeToString(data[:]), "0")
}

func cloneHeaders(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return time.Until(when)
}

type observingTransport struct {
	base     http.RoundTripper
	observer *responseObserver
}

func (t *observingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(request)
	if response != nil {
		if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
			t.observer.values.Store(request.Header.Get("X-Retry-POC-Attempt-ID"), retryAfter)
		}
	}
	return response, err
}
