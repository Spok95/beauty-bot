package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/Spok95/beauty-bot/internal/config"
	httpx "github.com/Spok95/beauty-bot/internal/infra/http"
	"github.com/Spok95/beauty-bot/internal/infra/logger"
)

func main() {
	cfg, err := config.Load("config/example.yaml")
	if err != nil {
		panic(err)
	}

	log := logger.New(cfg.App.Env)
	srv := httpx.New(cfg.HTTP.Addr, cfg.Metrics.Enabled)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Error("http server error", "err", err)
		}
	}()
	log.Info("HTTP server started", "addr", cfg.HTTP.Addr)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("graceful shutdown complete")
}
