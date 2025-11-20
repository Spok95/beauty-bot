package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Spok95/beauty-bot/internal/domain/consumption"
	"github.com/Spok95/beauty-bot/internal/domain/inventory"
	"github.com/Spok95/beauty-bot/internal/domain/materials"
	subsdomain "github.com/Spok95/beauty-bot/internal/domain/subscriptions"
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
}

func New(api *tgbotapi.BotAPI, log *slog.Logger,
	usersRepo *users.Repo, statesRepo *dialog.Repo,
	adminChatID int64, catalogRepo *catalog.Repo,
	materialsRepo *materials.Repo, inventoryRepo *inventory.Repo,
	consRepo *consumption.Repo, subsRepo *subsdomain.Repo) *Bot {

	return &Bot{
		api: api, log: log, users: usersRepo, states: statesRepo,
		adminChat: adminChatID, catalog: catalogRepo,
		materials: materialsRepo, inventory: inventoryRepo,
		cons: consRepo, subs: subsRepo,
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

// rentPartMeta — «кусок» сессии: либо по конкретному абонементу, либо без абонемента.
type rentPartMeta struct {
	WithSub   bool  // true — часть по абонементу, false — без абонемента
	Qty       int   // сколько часов/дней в этой части
	SubID     int64 // 0 — нет абонемента (часть без абонемента)
	PlanLimit int   // номинальный лимит плана (30, 50, ...) — для текста и выбора тарифа
}

func (b *Bot) showConsCart(ctx context.Context, chatID int64, editMsgID *int, place, unit string, qty int, items []map[string]any) {
	lines := []string{fmt.Sprintf("Расход/Аренда: %s, %d %s", map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place], qty, map[string]string{"hour": "ч", "day": "дн"}[unit])}
	var sum float64
	for _, it := range items {
		matID := int64(it["mat_id"].(float64))
		q := int64(it["qty"].(float64))
		name := fmt.Sprintf("ID:%d", matID)
		if m, _ := b.materials.GetByID(ctx, matID); m != nil {
			name = m.Name
		}
		price, _ := b.materials.GetPrice(ctx, matID)
		line := float64(q) * price
		sum += line
		lines = append(lines, fmt.Sprintf("• %s — %d × %.2f = %.2f ₽", name, q, price, line))
	}
	lines = append(lines, fmt.Sprintf("\nСумма материалов: %.2f ₽", sum))

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Добавить материал", "cons:additem")),
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("🧮 Посчитать", "cons:calc")),
		navKeyboard(true, true).InlineKeyboard[0],
	)

	text := strings.Join(lines, "\n")
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// notifyLowOrNegativeBatch — собирает по складам/категориям и шлёт одним сообщением
func (b *Bot) notifyLowOrNegativeBatch(ctx context.Context, pairs [][2]int64) {
	// обработаем каждую пару (wh, mat) только один раз
	seen := make(map[[2]int64]struct{})
	groups := map[int64]map[int64][]string{} // whID -> catID -> lines

	for _, p := range pairs {
		key := [2]int64{p[0], p[1]}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		whID, matID := p[0], p[1]

		m, err := b.materials.GetByID(ctx, matID)
		if err != nil || m == nil {
			continue
		}
		bal, err := b.inventory.GetBalance(ctx, whID, matID)
		if err != nil {
			continue
		}

		var warnLine string
		switch m.Unit {
		case "g":
			if bal <= 0 {
				warnLine = fmt.Sprintf("— %s — закончились.", m.Name)
			} else if bal < lowStockThresholdGr {
				warnLine = fmt.Sprintf("— %s — %.0f g — мало", m.Name, bal)
			}
		case "pcs":
			if bal <= 0 {
				warnLine = fmt.Sprintf("— %s — закончились.", m.Name)
			} else if bal < lowStockThresholdPcs {
				warnLine = fmt.Sprintf("— %s — %.0f шт — мало", m.Name, bal)
			}
		default:
			// прочие единицы — без алертов
		}

		if warnLine == "" {
			continue
		}
		if _, ok := groups[whID]; !ok {
			groups[whID] = map[int64][]string{}
		}
		groups[whID][m.CategoryID] = append(groups[whID][m.CategoryID], warnLine)
	}

	if len(groups) == 0 {
		return
	}

	for whID, cats := range groups {
		whName := fmt.Sprintf("ID:%d", whID)
		if wh, err := b.catalog.GetWarehouseByID(ctx, whID); err == nil && wh != nil {
			whName = wh.Name
		}

		var bld strings.Builder
		bld.WriteString("⚠️ Материалы:\n")
		bld.WriteString(fmt.Sprintf("Склад: %s\n", whName))

		for catID, lines := range cats {
			catName := fmt.Sprintf("Категория #%d", catID)
			if cat, err := b.catalog.GetCategoryByID(ctx, catID); err == nil && cat != nil {
				catName = cat.Name
			}
			bld.WriteString(fmt.Sprintf("— %s:\n", catName))
			for _, ln := range lines {
				if !strings.HasSuffix(ln, "\n") {
					bld.WriteString(ln + "\n")
				} else {
					bld.WriteString(ln)
				}
			}
		}
		b.notifyStockRecipients(ctx, strings.TrimSpace(bld.String()))
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
