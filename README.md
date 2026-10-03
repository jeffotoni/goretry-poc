# In-Memory HTTP Retry POC

[![Go 1.27.1](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![POC](https://img.shields.io/badge/status-POC-f0ad4e)](#in-memory-http-retry-poc)
[![quick](https://img.shields.io/badge/server-quick-0A7E8C)](https://github.com/jeffotoni/quick)
[![req](https://img.shields.io/badge/http%20client-req-0A7E8C)](https://github.com/jeffotoni/req)
[![log](https://img.shields.io/badge/logging-log-0A7E8C)](https://github.com/jeffotoni/log)

This is the default English documentation. The Portuguese version is available in [README_br.md](README_br.md).

This POC demonstrates a Go HTTP server that receives a JSON payload and forwards it to a second service. When the destination presents a temporary failure, the request enters a bounded in-memory queue and is processed again by a worker pool.

The project is intentionally generic and self-contained. It is a learning example for a controlled, observable retry strategy without a persistent queue.

## Libraries Used

This POC uses three small, focused libraries from [github.com/jeffotoni](https://github.com/jeffotoni):

| Library | Role in this POC |
| --- | --- |
| [quick](https://github.com/jeffotoni/quick) | HTTP server and routing layer. It keeps the API setup concise while using Go's standard HTTP stack underneath. |
| [req](https://github.com/jeffotoni/req) | HTTP client used to send the JSON payload to the destination service. A shared client is reused across attempts. |
| [log](https://github.com/jeffotoni/log) | Structured application logging. It supports JSON and text output while keeping fields such as request ID, attempt, worker and status searchable. |

The retry policy is implemented by this POC in internal/retry. The automatic retry feature of req remains disabled here so there is only one retry coordinator, with one queue, one backoff policy and one retry window.

## Quick Overview

~~~text
+------------------+       +---------------------+       +------------------+
| Client           |       | quick server        |       | Destination      |
| POST /dispatch   | ----> | initial attempt     | ----> | receives JSON    |
+------------------+       +----------+----------+       +--------+---------+
                                      |                           |
                         success <---+                            |
                                      |                           |
                         temporary failure                        |
                                      v                           |
                           +----------------+                     |
                           | Bounded queue  |                     |
                           +--------+-------+                     |
                                    v                             |
                           +----------------+                     |
                           | Worker pool    | --------------------+
                           +--------+-------+       retry         |
                                    v                             |
                           backoff + window                       |
                                                                  |
                         final success/failure <------------------+
~~~

Complementary diagram: [docs/retry-flow.svg](docs/retry-flow.svg).

## How It Works

1. The POST /dispatch handler accepts any valid JSON payload.
2. The payload is sent immediately to the destination using a shared req client.
3. If the destination responds successfully, the response is returned to the client.
4. If a temporary failure occurs, the request is added to the bounded queue.
5. The handler returns 202 Accepted instead of keeping the connection open for the entire retry window.
6. A worker waits for the backoff and performs the next attempt.
7. The cycle continues until success, a permanent error, a full queue or window expiration.

The external retry loop is the main policy in this POC. The internal req retry is disabled to avoid two independent retry layers.

## Failures That Are Retried

- Transport errors such as EOF, timeout, connection refused and connection reset.
- HTTP 408 Request Timeout.
- HTTP 429 Too Many Requests.
- HTTP 500 Internal Server Error.
- HTTP 502 Bad Gateway.
- HTTP 503 Service Unavailable.
- HTTP 504 Gateway Timeout.

Other HTTP errors, especially 4xx responses, are treated as permanent and do not enter the queue.

The Retry-After header is respected when the destination returns a valid value. The mock in this POC can generate that header through X-Mock-Retry-After.

## Backoff and Retry Window

The default backoff is:

~~~text
attempt 1: 500 ms
attempt 2: 1 s
attempt 3: 2 s
attempt 4: 4 s
...
~~~

The delay has a maximum limit and can use jitter to prevent multiple requests from being retried at exactly the same time.

The default retry window is three minutes. When it expires, the request is discarded and the event is logged as retry window expired. There is no infinite retry loop.

## Components

~~~text
cmd/retry-poc/       dispatch server
cmd/mock-target/     destination service used by tests
internal/retry/      queue, workers, backoff and retry window
internal/httpapi/    HTTP handlers
internal/config/     POC configuration names and composition
internal/pkg/env/    environment variable parsing and conversion
docs/retry-flow.svg  flow diagram
~~~

## Configuration

All variables in this POC use the POC_ prefix and do not reuse names from any other implementation.

| Variable | Default | Purpose |
| --- | --- | --- |
| POC_SERVER_ADDR | :8080 | Dispatch server address |
| POC_DESTINATION_URL | http://127.0.0.1:8081/target | Destination service URL |
| POC_MOCK_ADDR | :8081 | Mock server address |
| POC_RETRY_ACTIVE | true | Enables or disables the retry queue |
| POC_RETRY_WINDOW | 3m | Maximum request lifetime |
| POC_RETRY_DELAY_BASE | 500ms | Backoff for the first retry |
| POC_RETRY_DELAY_MAX | 30s | Maximum backoff |
| POC_RETRY_POOL_SIZE | 4 | Fixed number of workers |
| POC_RETRY_BUFFER_SIZE | 256 | In-memory queue capacity |
| POC_RETRY_TIMEOUT | 30s | Timeout for each HTTP call |
| POC_RETRY_JITTER | true | Adds random variation to backoff |
| POC_LOG_FORMAT | json | Log format: json or text |

Durations use Go's native format, for example 500ms, 2s and 3m.

### Log Format

The default format is json, because it preserves structured fields and makes queries easier in observability tools. For manual development output, use:

~~~bash
POC_LOG_FORMAT=text go run ./cmd/retry-poc
~~~

JSON:

~~~json
{"time":"2026-10-03T19:30:00Z","level":"INFO","msg":"retry attempt","service":"retry-poc","worker":2,"request_id":"poc-retry-001","attempt":2}
~~~

Text:

~~~text
2026-10-03T19:30:00Z | INFO | - | retry attempt | service=retry-poc | worker=2 | request_id=poc-retry-001 | attempt=2
~~~

The retry package depends on a small logging interface and does not depend on a specific output format. The implementation uses [github.com/jeffotoni/log](https://github.com/jeffotoni/log) underneath, keeping events structured in both formats.

## Requirements

- Go 1.24 or newer.
- Network access to download the public quick, req and log modules.

Download and organize dependencies:

~~~bash
go mod download
go mod tidy
~~~

## Run Locally

Open two terminals at the POC root.

### Terminal 1: Mock Service

~~~bash
go run ./cmd/mock-target
~~~

Expected output:

~~~text
time=... level=INFO msg="mock target started" address=[::]:8081
~~~

### Terminal 2: Retry Server

~~~bash
POC_RETRY_DELAY_BASE=100ms \
POC_RETRY_DELAY_MAX=2s \
POC_RETRY_JITTER=false \
POC_LOG_FORMAT=json \
go run ./cmd/retry-poc
~~~

Expected output:

~~~text
time=... level=INFO msg="retry poc started" address=[::]:8080 target=http://127.0.0.1:8081/target
~~~

The values above reduce delays for testing. If no variable is defined, the defaults from the table are used.

## curl Tests

### 1. Immediate Success

The mock receives the payload and responds with 200.

~~~bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-success-001' \
  -H 'X-Mock-Status: 200' \
  -d '{"message":"success"}'
~~~

Expected result: 200 OK.

### 2. Permanent Error

Status 400 does not enter retry.

~~~bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-4xx-001' \
  -H 'X-Mock-Status: 400' \
  -d '{"message":"permanent error"}'
~~~

Expected result: 400 and only one call to the mock.

### 3. Temporary Failure Followed by Success

The X-Mock-Status-Sequence header defines the status for each attempt. The position is identified by the X-Retry-POC-Attempt-ID header suffix.

~~~bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-retry-001' \
  -H 'X-Mock-Status-Sequence: 502,502,200' \
  -d '{"message":"retry me"}'
~~~

Expected behavior:

~~~text
attempt 1 -> 502 -> queued -> initial response 202
attempt 2 -> 502 -> waits for the next backoff
attempt 3 -> 200 -> retry succeeded
~~~

The mock should print the same payload for all three attempts.

### 4. All Retryable Statuses

~~~bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-statuses-001' \
  -H 'X-Mock-Status-Sequence: 408,429,500,502,503,504,200' \
  -d '{"message":"status sequence"}'
~~~

### 5. Retry-After

The mock returns Retry-After: 1 when it receives X-Mock-Retry-After: 1.

~~~bash
curl -i -X POST http://127.0.0.1:8080/dispatch \
  -H 'Content-Type: application/json' \
  -H 'X-Request-ID: poc-retry-after-001' \
  -H 'X-Mock-Status-Sequence: 429,200' \
  -H 'X-Mock-Retry-After: 1' \
  -d '{"message":"retry after"}'
~~~

The second attempt should occur approximately one second after the first.

### 6. Transport Error

Stop the mock or point the dispatch server to a port without a server:

~~~bash
POC_DESTINATION_URL=http://127.0.0.1:8999/target \
POC_RETRY_DELAY_BASE=100ms \
POC_RETRY_WINDOW=3s \
go run ./cmd/retry-poc
~~~

Errors such as connection refused, EOF and timeout enter the same retry flow.

### 7. Health and Statistics

~~~bash
curl -s http://127.0.0.1:8080/health
curl -s http://127.0.0.1:8080/stats
~~~

Example statistics:

~~~json
{
  "queued": 1,
  "running": 0,
  "retries": 2,
  "succeeded": 1,
  "exhausted": 0,
  "queue_full": 0,
  "non_retryable": 0
}
~~~

## Memory and Concurrency Rules

- The queue has a fixed capacity.
- The number of workers is fixed.
- No new goroutine is created for each attempt.
- Each worker reuses the shared HTTP client.
- The body is copied so it can be sent again.
- The request expires when the retry window ends.
- If the queue is full, the server reports retry_queue_full.

## Idempotency

A timeout does not prove that the destination did not process the request. Every attempt reuses the same X-Request-ID and the same body.

In a real system, the destination should support an idempotency key or a natural resource key. This POC does not guarantee exactly-once delivery.

## Advantages and Trade-offs

| Evaluation | Item |
| --- | --- |
| 🟢 | Does not block the client for the entire retry window |
| 🟢 | Limits queue size, workers and memory |
| 🟢 | Reuses connections through a shared HTTP client |
| 🟢 | Handles transport failures and temporary HTTP statuses |
| 🟢 | Allows error sequences to be tested through headers |
| 🟢 | Keeps the same payload and identifier across attempts |
| 🔴 | Pending requests are lost if the process crashes |
| 🔴 | There is no exactly-once delivery guarantee |
| 🔴 | A prolonged outage can fill the queue |
| 🔴 | The destination must support idempotent reprocessing |
| 🔴 | The POC does not replace a persistent queue |

## Automated Tests

~~~bash
go test ./...
go test -race ./...
go vet ./...
~~~

The tests validate retries for temporary statuses and confirm that permanent errors are not resent.

## Contributing

Contributions are welcome. The project is intentionally small, so changes should remain focused and easy to validate.

Before opening a pull request:

- Open an issue first when proposing a behavioral change or a new feature.
- Create a focused branch from the current default branch.
- Keep the retry policy, queue limits and shutdown behavior covered by tests.
- Run `gofmt` on changed Go files.
- Run `go test ./...`, `go test -race ./...` and `go vet ./...`.
- Update the README and the SVG diagram when the behavior or architecture changes.
- Describe the problem, the solution and the validation performed in the pull request.

Avoid adding persistent storage, unbounded goroutines or a second independent retry mechanism without discussing the trade-offs first. This POC intentionally demonstrates bounded in-memory retry behavior.

## Shutdown

When receiving SIGINT or SIGTERM, the server stops accepting new requests, shuts down the workers and releases the in-memory queue. Because there is no persistence, pending tasks can be lost during shutdown.
