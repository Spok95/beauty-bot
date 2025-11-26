package bot

import (
	"context"
	"log/slog"

	"github.com/Spok95/beauty-bot/internal/domain/consumption"
	"github.com/Spok95/beauty-bot/internal/domain/inventory"
	"github.com/Spok95/beauty-bot/internal/domain/materials"
	subsdomain "github.com/Spok95/beauty-bot/internal/domain/subscriptions"
	payments "github.com/Spok95/beauty-bot/internal/infra/payments"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/catalog"
	"github.com/Spok95/beauty-bot/internal/domain/users"
)

const lowStockThresholdGr = 20.0
const lowStockThresholdPcs = 1.0

type Bot struct {
	api       *tgbotapi.BotAPI
	log       *slog.Logger
	users     *users.Repo
	states    *dialog.Repo
	adminChat int64
	catalog   *catalog.Repo
	materials *materials.Repo
	inventory *inventory.Repo
	cons      *consumption.Repo
	subs      *subsdomain.Repo
	payments  *payments.Service
}

func New(api *tgbotapi.BotAPI, log *slog.Logger,
	usersRepo *users.Repo, statesRepo *dialog.Repo,
	adminChatID int64, catalogRepo *catalog.Repo,
	materialsRepo *materials.Repo, inventoryRepo *inventory.Repo,
	consRepo *consumption.Repo, subsRepo *subsdomain.Repo,
	paymentsSvc *payments.Service) *Bot {

	return &Bot{
		api: api, log: log, users: usersRepo, states: statesRepo,
		adminChat: adminChatID, catalog: catalogRepo,
		materials: materialsRepo, inventory: inventoryRepo,
		cons: consRepo, subs: subsRepo,
		payments: paymentsSvc,
	}
}

func (b *Bot) Run(ctx context.Context, timeoutSec int) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = timeoutSec
	updates := b.api.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case upd := <-updates:
			if upd.Message != nil {
				b.onMessage(ctx, upd)
			} else if upd.CallbackQuery != nil {
				b.onCallback(ctx, upd)
			}
		}
	}
}

func (b *Bot) onMessage(ctx context.Context, upd tgbotapi.Update) {
	msg := upd.Message

	if msg.IsCommand() {
		b.handleCommand(ctx, msg)
		return
	}
	b.handleStateMessage(ctx, msg)
}

func (b *Bot) onCallback(ctx context.Context, upd tgbotapi.Update) {
	b.handleCallback(ctx, upd.CallbackQuery)
}
