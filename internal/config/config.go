package config

import (
	"time"

	"github.com/jeffotoni/goretry-poc/internal/pkg/env"
	"github.com/jeffotoni/goretry-poc/internal/retry"
)

const (
	ServerAddressEnv  = "POC_SERVER_ADDR"
	TargetURLEnv      = "POC_DESTINATION_URL"
	RetryActiveEnv    = "POC_RETRY_ACTIVE"
	RetryWindowEnv    = "POC_RETRY_WINDOW"
	RetryBaseDelayEnv = "POC_RETRY_DELAY_BASE"
	RetryMaxDelayEnv  = "POC_RETRY_DELAY_MAX"
	RetryWorkersEnv   = "POC_RETRY_POOL_SIZE"
	RetryQueueEnv     = "POC_RETRY_BUFFER_SIZE"
	RetryTimeoutEnv   = "POC_RETRY_TIMEOUT"
	RetryJitterEnv    = "POC_RETRY_JITTER"
	LogFormatEnv      = "POC_LOG_FORMAT"
)

type Config struct {
	Address   string
	TargetURL string
	LogFormat string
	Retry     retry.Config
}

func Load() Config {
	return Config{
		Address:   env.GetString(ServerAddressEnv, ":8080"),
		TargetURL: env.GetString(TargetURLEnv, "http://127.0.0.1:8081/target"),
		LogFormat: env.GetString(LogFormatEnv, "json"),
		Retry: retry.Config{
			Enabled:     env.GetBool(RetryActiveEnv, true),
			Window:      env.GetDuration(RetryWindowEnv, 3*time.Minute),
			BaseDelay:   env.GetDuration(RetryBaseDelayEnv, 500*time.Millisecond),
			MaxDelay:    env.GetDuration(RetryMaxDelayEnv, 30*time.Second),
			Workers:     env.GetInt(RetryWorkersEnv, 4),
			QueueSize:   env.GetInt(RetryQueueEnv, 256),
			HTTPTimeout: env.GetDuration(RetryTimeoutEnv, 30*time.Second),
			Jitter:      env.GetBool(RetryJitterEnv, true),
		},
	}
}
