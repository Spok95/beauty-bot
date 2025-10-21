package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"github.com/Spok95/beauty-bot/internal/config"
	"github.com/Spok95/beauty-bot/internal/infra/db"
	httpx "github.com/Spok95/beauty-bot/internal/infra/http"
	"github.com/Spok95/beauty-bot/internal/infra/logger"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func runMigrations(dsn string, log *slog.Logger) error {
	// Открываем sql.DB через драйвер "pgx" (а не "postgres")
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()

	// goose просто использует уже готовое подключение:
	return goose.Up(sqlDB, "migrations")
}

func main() {
	cfg, err := config.Load("config/example.yaml")
	if err != nil {
		panic(err)
	}

	log := logger.New(cfg.App.Env)

	if err := runMigrations(cfg.Postgres.DSN, log); err != nil {
		log.Error("migrations failed", "err", err)
		return
	}
	log.Info("migrations applied")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.Postgres.DSN)
	if err != nil {
		log.Error("db connect failed", "err", err)
		return
	}
	defer pool.Close()
	log.Info("db connected")

	srv := httpx.New(cfg.HTTP.Addr, cfg.Metrics.Enabled)
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
