package bot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

func (b *Bot) send(msg tgbotapi.Chattable) {
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send failed", "err", err)
	}
}

/*** NAV HELPERS ***/

// downloadTelegramFile скачивает файл по FileID через Telegram API.
func (b *Bot) downloadTelegramFile(fileID string) ([]byte, error) {
	url, err := b.api.GetFileDirectURL(fileID)
	if err != nil {
		return nil, fmt.Errorf("get file url: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram returned status %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

func (b *Bot) editTextAndClear(chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageTextAndMarkup(
		chatID, messageID, text,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
	)
	b.send(edit)
}

func (b *Bot) editTextWithNav(chatID int64, messageID int, text string) {
	kb := navKeyboard(true, true)
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, kb)
	b.send(edit)
}

// Бейдж активности
func badge(b bool) string {
	if b {
		return "🟢"
	}
	return "🚫"
}

// rentPartMeta — «кусок» сессии: либо по конкретному абонементу, либо без абонемента.
type rentPartMeta struct {
	WithSub   bool  // true — часть по абонементу, false — без абонемента
	Qty       int   // сколько часов/дней в этой части
	SubID     int64 // 0 — нет абонемента (часть без абонемента)
	PlanLimit int   // номинальный лимит плана (30, 50, ...) — для текста и выбора тарифа
}

// splitQtyBySubscriptions делит qty по активным абонементам (FIFO), остаток — без абонемента.
// Использует новую модель: несколько абонементов за месяц, поле PlanLimit, ListActiveByPlaceUnitMonth.
func (b *Bot) splitQtyBySubscriptions(
	ctx context.Context,
	userID int64,
	place, unit string,
	qty int,
) ([]rentPartMeta, error) {
	metas := make([]rentPartMeta, 0, 3)

	if qty <= 0 {
		return metas, nil
	}

	remaining := qty

	// 1) части по абонементам (если есть)
	if b.subs != nil {
		month := time.Now().Format("2006-01")
		subs, err := b.subs.ListActiveByPlaceUnitMonth(ctx, userID, place, unit, month)
		if err == nil {
			for _, s := range subs {
				left := s.TotalQty - s.UsedQty
				if left <= 0 {
					continue
				}
				if remaining <= 0 {
					break
				}
				use := remaining
				if left < use {
					use = left
				}
				metas = append(metas, rentPartMeta{
					WithSub:   true,
					Qty:       use,
					SubID:     s.ID,
					PlanLimit: s.PlanLimit,
				})
				remaining -= use
			}
		}
	}

	// 2) то, что не покрыто абонементами — часть без абонемента
	if remaining > 0 {
		metas = append(metas, rentPartMeta{
			WithSub:   false,
			Qty:       remaining,
			SubID:     0,
			PlanLimit: 0,
		})
	}

	return metas, nil
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

// showSubsMenu Меню «Абонементы» для админа
func (b *Bot) showSubsMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Создать абонемент", "adm:subs:add"),
			// tgbotapi.NewInlineKeyboardButtonData("📄 Список (текущий месяц)", "adm:subs:list"), // позже
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
	text := "Абонементы — выберите действие"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// showSubsPickUser — выбор мастера для абонемента
func (b *Bot) showSubsPickUser(ctx context.Context, chatID int64, editMsgID int) {
	list, err := b.users.ListByRole(ctx, users.RoleMaster, users.StatusApproved)
	if err != nil || len(list) == 0 {
		b.editTextAndClear(chatID, editMsgID, "Нет утверждённых мастеров.")
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, u := range list {
		title := strings.TrimSpace(u.Username) // в Username у нас «ФИО/отображаемое имя»
		if title == "" {
			title = fmt.Sprintf("id %d", u.ID)
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(title, fmt.Sprintf("adm:subs:user:%d", u.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Выберите мастера:", kb))
}

// showSubsPickPlaceUnit Выбор места/единицы
func (b *Bot) showSubsPickPlaceUnit(chatID int64, editMsgID int, uid int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			// Сразу задаём и место и единицу:
			tgbotapi.NewInlineKeyboardButtonData("Зал (часы)", fmt.Sprintf("adm:subs:pu:%d:hall:hour", uid)),
			tgbotapi.NewInlineKeyboardButtonData("Кабинет (дни)", fmt.Sprintf("adm:subs:pu:%d:cabinet:day", uid)),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Выберите помещение:", kb))
}

// clearPrevStep убрать inline-кнопки у прошлого шага, если он был
func (b *Bot) clearPrevStep(ctx context.Context, chatID int64) {
	st, _ := b.states.Get(ctx, chatID)
	if st == nil || st.Payload == nil {
		return
	}
	if v, ok := st.Payload["last_mid"]; ok {
		mid := int(v.(float64)) // payload хранится через JSON
		// просто чистим markup, текст оставляем как есть
		rm := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}
		b.send(tgbotapi.NewEditMessageReplyMarkup(chatID, mid, rm))
	}
}

// saveLastStep сохранить id текущего бот-сообщения как «последний»
func (b *Bot) saveLastStep(ctx context.Context, chatID int64, nextState dialog.State, payload dialog.Payload, newMID int) {
	if payload == nil {
		payload = dialog.Payload{}
	}
	payload["last_mid"] = float64(newMID)
	_ = b.states.Set(ctx, chatID, nextState, payload)
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
