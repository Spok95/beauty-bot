package bot

import (
	"context"
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api *tgbotapi.BotAPI
	log *slog.Logger
}

type Config struct {
	Token      string
	TimeoutSec int
}

func New(cfg Config, log *slog.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, err
	}
	api.Debug = false
	return &Bot{api: api, log: log}, nil
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
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Привет! Я бот салона. Команды: /help")
				b.api.Send(msg)
			case "help":
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Доступные команды:\n/start — начать\n/help — помощь")
				b.api.Send(msg)
			default:
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "Не знаю такую команду. Набери /help")
				b.api.Send(msg)
			}
		}
	}
}
