package bot

import (
	"context"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/beauty-bot/internal/domain/users"
)

type Bot struct {
	api     *tgbotapi.BotAPI
	log     *slog.Logger
	users   *users.Repo
	adminID int64
}

func (b *Bot) send(msg tgbotapi.Chattable) {
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("telegram send failed", "err", err)
	}
}

type Config struct {
	Token      string
	TimeoutSec int
	AdminID    int64
}

func New(cfg Config, log *slog.Logger, usersRepo *users.Repo) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, err
	}
	api.Debug = false
	return &Bot{api: api, log: log, users: usersRepo, adminID: cfg.AdminID}, nil
}

func (b *Bot) Run(ctx context.Context, timeoutSec int) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = timeoutSec
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return nil
		case upd := <-updates:
			if upd.Message == nil {
				continue
			}
			switch upd.Message.Command() {
			case "start":
				from := upd.Message.From
				if from == nil {
					continue
				}
				role := users.RoleMaster
				if b.adminID != 0 && int64(from.ID) == b.adminID {
					role = users.RoleAdmin
				}
				_, err := b.users.UpsertFromTelegram(ctx, users.Telegram{
					ID:        int64(from.ID),
					Username:  from.UserName,
					FirstName: from.FirstName,
					LastName:  from.LastName,
				}, role)
				if err != nil {
					b.log.Error("user upsert failed", "err", err)
					b.send(tgbotapi.NewMessage(upd.Message.Chat.ID, "Не удалось зарегистрировать пользователя, попробуйте позже."))
					continue
				}
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Привет! Я бот салона. Команды: /help")
				b.send(msg)

			case "help":
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Доступные команды:\n/start — начать\n/help — помощь")
				b.send(msg)

			default:
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Не знаю такую команду. Набери /help")
				b.send(msg)
			}
		}
	}
}
