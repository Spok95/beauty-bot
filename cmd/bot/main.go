package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Spok95/beauty-bot/internal/bot"
	"github.com/Spok95/beauty-bot/internal/config"
	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/catalog"
	"github.com/Spok95/beauty-bot/internal/domain/consumption"
	"github.com/Spok95/beauty-bot/internal/domain/inventory"
	"github.com/Spok95/beauty-bot/internal/domain/materials"
	subs "github.com/Spok95/beauty-bot/internal/domain/subscriptions"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	"github.com/Spok95/beauty-bot/internal/infra/db"
	httpx "github.com/Spok95/beauty-bot/internal/infra/http"
	"github.com/Spok95/beauty-bot/internal/infra/logger"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/subosito/gotenv"

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

	// Диагностика: куда реально подключились
	var dbName, addr string
	var port int
	if err := sqlDB.QueryRow(`select current_database(), inet_server_addr()::text, inet_server_port()`).Scan(&dbName, &addr, &port); err == nil {
		log.Info("db identity", "db", dbName, "addr", addr, "port", port)
	} else {
		log.Warn("db identity probe failed", "err", err)
	}
	// Список миграций на диске
	if entries, err := os.ReadDir("migrations"); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				log.Info("migration file", "name", e.Name())
			}
		}
	} else {
		log.Warn("cannot read migrations dir", "err", err)
	}

	// goose просто использует уже готовое подключение:
	return goose.Up(sqlDB, "migrations")
}

func main() {
	// Загружаем переменные окружения из .env (если файл есть).
	_ = gotenv.Load(".env.local") // необязательно; удобно для локальных переопределений
	_ = gotenv.Load()             // подхватит .env

	cfg, err := config.Load("config/example.yaml")
	if err != nil {
		panic(err)
	}

	log := logger.New(cfg.App.Env)

	// В DEV полезно видеть, какой DSN действительно используется
	log.Info("using DSN", "dsn", cfg.Postgres.DSN)

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

	usersRepo := users.NewRepo(pool)
	stateRepo := dialog.NewRepo(pool)
	catalogRepo := catalog.NewRepo(pool)
	materialsRepo := materials.NewRepo(pool)
	inventoryRepo := inventory.NewRepo(pool)
	consRepo := consumption.NewRepo(pool)
	subsRepo := subs.NewRepo(pool)
	rateRepo := consumption.NewRateRepo(pool)

	srv := httpx.New(cfg.HTTP.Addr, cfg.Metrics.Enabled)
	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Error("http server error", "err", err)
		}
	}()
	log.Info("HTTP server started", "addr", cfg.HTTP.Addr)

	api, err := tgbotapi.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		log.Error("telegram init failed", "err", err)
		return
	}
	if cfg.App.Env == "dev" {
		api.Debug = true
	}

	tg := bot.New(api, log, usersRepo, stateRepo, cfg.Telegram.AdminChatID, catalogRepo, materialsRepo, inventoryRepo, consRepo, subsRepo, rateRepo)

	go func() {
		if err := tg.Run(ctx, cfg.Telegram.RequestTimeoutSec); err != nil {
			log.Error("telegram runtime error", "err", err)
		}
	}()
	log.Info("telegram bot started")

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("graceful shutdown complete")
}
