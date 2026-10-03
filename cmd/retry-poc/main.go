package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/jeffotoni/goretry-poc/internal/config"
	"github.com/jeffotoni/goretry-poc/internal/httpapi"
	"github.com/jeffotoni/goretry-poc/internal/logging"
	"github.com/jeffotoni/goretry-poc/internal/retry"
	"github.com/jeffotoni/quick"
)

func main() {
	cfg := config.Load()
	logger := logging.New(cfg.LogFormat, "retry-poc")
	manager := retry.NewManager(cfg.Retry, logger)
	manager.Start()

	handler := &httpapi.Handler{Manager: manager, TargetURL: cfg.TargetURL}
	q := quick.New()
	q.Post("/dispatch", handler.Dispatch)
	q.Get("/health", handler.Health)
	q.Get("/stats", handler.Stats)

	quickServer, shutdown, err := q.ListenWithShutdown(cfg.Address)
	if err != nil {
		manager.Close()
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
	logger.Info("retry poc started", "address", quickServer.Addr, "target", cfg.TargetURL, "retry", manager.String())

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	signal.Stop(signals)

	shutdown()
	manager.Close()
	logger.Info("retry poc stopped")
}
