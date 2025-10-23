package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/Spok95/beauty-bot/internal/domain/inventory"
	"github.com/Spok95/beauty-bot/internal/domain/materials"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/catalog"
	"github.com/Spok95/beauty-bot/internal/domain/users"
)

const lowStockThresholdGr = 20.0

type Bot struct {
	api       *tgbotapi.BotAPI
	log       *slog.Logger
	users     *users.Repo
	states    *dialog.Repo
	adminChat int64
	catalog   *catalog.Repo
	materials *materials.Repo
	inventory *inventory.Repo
}

func New(api *tgbotapi.BotAPI, log *slog.Logger,
	usersRepo *users.Repo, statesRepo *dialog.Repo,
	adminChatID int64, catalogRepo *catalog.Repo,
	materialsRepo *materials.Repo, inventoryRepo *inventory.Repo) *Bot {

	return &Bot{
		api: api, log: log, users: usersRepo, states: statesRepo,
		adminChat: adminChatID, catalog: catalogRepo,
		materials: materialsRepo, inventory: inventoryRepo,
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

func navKeyboard(back bool, cancel bool) tgbotapi.InlineKeyboardMarkup {
	row := []tgbotapi.InlineKeyboardButton{}
	if back {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav:back"))
	}
	if cancel {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("✖️ Отменить", "nav:cancel"))
	}
	return tgbotapi.NewInlineKeyboardMarkup(row)
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

func (b *Bot) askFIO(chatID int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✖️ Отменить", "nav:cancel"),
		),
	)
	m := tgbotapi.NewMessage(chatID, "Введите, пожалуйста, ФИО одной строкой.")
	m.ReplyMarkup = kb
	b.send(m)
}

func roleKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Мастер", "role:master"),
			tgbotapi.NewInlineKeyboardButtonData("Администратор", "role:administrator"),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
}

func confirmKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📨 Отправить", "rq:send"),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
}

// Нижняя панель (ReplyKeyboard) для админа
func adminReplyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			{tgbotapi.NewKeyboardButton("Список команд")},
			{tgbotapi.NewKeyboardButton("Склады"), tgbotapi.NewKeyboardButton("Категории")},
			{tgbotapi.NewKeyboardButton("Материалы")},
			{tgbotapi.NewKeyboardButton("Остатки")},
		},
	}
}

// Бейдж активности
func badge(b bool) string {
	if b {
		return "🟢"
	}
	return "🚫"
}

func (b *Bot) showWarehouseMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Создать склад", "adm:wh:add"),
			tgbotapi.NewInlineKeyboardButtonData("📄 Список складов", "adm:wh:list"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Склады — выберите действие", kb))
	} else {
		m := tgbotapi.NewMessage(chatID, "Склады — выберите действие")
		m.ReplyMarkup = kb
		b.send(m)
	}
}

func (b *Bot) showWarehouseList(ctx context.Context, chatID int64, editMsgID int) {
	items, err := b.catalog.ListWarehouses(ctx)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки складов")
		return
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, w := range items {
		label := fmt.Sprintf("%s %s", badge(w.Active), w.Name)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("adm:wh:menu:%d", w.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Список складов:", kb))
}

func (b *Bot) showWarehouseItemMenu(ctx context.Context, chatID int64, editMsgID int, id int64) {
	w, err := b.catalog.GetWarehouseByID(ctx, id)
	if err != nil || w == nil {
		b.editTextAndClear(chatID, editMsgID, "Склад не найден")
		return
	}
	toggle := "🙈 Скрыть"
	if !w.Active {
		toggle = "👁 Показать"
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}
	// Переименовать — только если активен
	if w.Active {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Переименовать", fmt.Sprintf("adm:wh:rn:%d", id)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(toggle, fmt.Sprintf("adm:wh:tg:%d", id)),
	))
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := fmt.Sprintf("Склад: %s %s\nТип: %s\nСтатус: %v", badge(w.Active), w.Name, w.Type, w.Active)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

func (b *Bot) showCategoryMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Создать категорию", "adm:cat:add"),
			tgbotapi.NewInlineKeyboardButtonData("📄 Список категорий", "adm:cat:list"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Категории — выберите действие", kb))
	} else {
		m := tgbotapi.NewMessage(chatID, "Категории — выберите действие")
		m.ReplyMarkup = kb
		b.send(m)
	}
}

func (b *Bot) showCategoryList(ctx context.Context, chatID int64, editMsgID int) {
	items, err := b.catalog.ListCategories(ctx)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки категорий")
		return
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, c := range items {
		label := fmt.Sprintf("%s %s", badge(c.Active), c.Name)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("adm:cat:menu:%d", c.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Список категорий:", kb))
}

func (b *Bot) showCategoryItemMenu(ctx context.Context, chatID int64, editMsgID int, id int64) {
	c, err := b.catalog.GetCategoryByID(ctx, id)
	if err != nil || c == nil {
		b.editTextAndClear(chatID, editMsgID, "Категория не найдена")
		return
	}
	toggle := "🙈 Скрыть"
	if !c.Active {
		toggle = "👁 Показать"
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	// Переименовать — только если активна
	if c.Active {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Переименовать", fmt.Sprintf("adm:cat:rn:%d", id)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(toggle, fmt.Sprintf("adm:cat:tg:%d", id)),
	))
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := fmt.Sprintf("Категория: %s %s\nСтатус: %v", badge(c.Active), c.Name, c.Active)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

func (b *Bot) showMaterialMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Создать материал", "adm:mat:add"),
			tgbotapi.NewInlineKeyboardButtonData("📄 Список материалов", "adm:mat:list"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Материалы — выберите действие", kb))
	} else {
		m := tgbotapi.NewMessage(chatID, "Материалы — выберите действие")
		m.ReplyMarkup = kb
		b.send(m)
	}
}

func (b *Bot) showMaterialList(ctx context.Context, chatID int64, editMsgID int) {
	items, err := b.materials.List(ctx, false)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки материалов")
		return
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, m := range items {
		label := fmt.Sprintf("%s %s", badge(m.Active), m.Name)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("adm:mat:menu:%d", m.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Список материалов:", kb))
}

func (b *Bot) showMaterialItemMenu(ctx context.Context, chatID int64, editMsgID int, id int64) {
	m, err := b.materials.GetByID(ctx, id)
	if err != nil || m == nil {
		b.editTextAndClear(chatID, editMsgID, "Материал не найден")
		return
	}
	toggle := "🙈 Скрыть"
	if !m.Active {
		toggle = "👁 Показать"
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	if m.Active {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Переименовать", fmt.Sprintf("adm:mat:rn:%d", id)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Единица: pcs/g", fmt.Sprintf("adm:mat:unit:%d", id)),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(toggle, fmt.Sprintf("adm:mat:tg:%d", id)),
	))
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := fmt.Sprintf("Материал: %s %s\nЕд.: %s\nСтатус: %v", badge(m.Active), m.Name, m.Unit, m.Active)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

func (b *Bot) unitKeyboard(id int64) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("pcs", fmt.Sprintf("adm:mat:unit:set:%d:pcs", id)),
			tgbotapi.NewInlineKeyboardButtonData("g", fmt.Sprintf("adm:mat:unit:set:%d:g", id)),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
}

func (b *Bot) showCategoryPick(ctx context.Context, chatID int64, editMsgID int) {
	// список только активных категорий для создания материала
	rows := [][]tgbotapi.InlineKeyboardButton{}
	cats, err := b.catalog.ListCategories(ctx)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки категорий")
		return
	}
	for _, c := range cats {
		if !c.Active {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(c.Name, fmt.Sprintf("adm:mat:pickcat:%d", c.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Выберите категорию:", kb))
}

func (b *Bot) showStockWarehouseList(ctx context.Context, chatID int64, editMsgID *int) {
	ws, err := b.catalog.ListWarehouses(ctx)
	if err != nil {
		if editMsgID != nil {
			b.editTextAndClear(chatID, *editMsgID, "Ошибка загрузки складов")
			return
		}
		b.send(tgbotapi.NewMessage(chatID, "Ошибка загрузки складов"))
		return
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, w := range ws {
		if !w.Active {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(w.Name, fmt.Sprintf("st:list:%d", w.ID)),
		))
	}
	rows = append(rows, navKeyboard(false, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Выберите склад:", kb))
	} else {
		m := tgbotapi.NewMessage(chatID, "Выберите склад:")
		m.ReplyMarkup = kb
		b.send(m)
	}
}

func (b *Bot) showStockMaterialList(ctx context.Context, chatID int64, editMsgID int, whID int64) {
	items, err := b.materials.ListWithBalanceByWarehouse(ctx, whID)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки материалов")
		return
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, it := range items {
		label := fmt.Sprintf("%s: %d %s", it.Name, it.Balance, it.Unit)
		if it.Unit == materials.UnitG {
			if it.Balance < 0 {
				label = "⚠️ " + label + " — закончились"
			} else if it.Balance < lowStockThresholdGr {
				label = "⚠️ " + label + " — мало"
			}
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("st:item:%d:%d", whID, it.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Список материалов:", kb))
}

func (b *Bot) showStockItem(ctx context.Context, chatID int64, editMsgID int, whID, matID int64) {
	// берём материал и баланс
	m, _ := b.materials.GetByID(ctx, matID)
	if m == nil {
		b.editTextAndClear(chatID, editMsgID, "Материал не найден")
		return
	}
	bls, _ := b.materials.ListBalancesByWarehouse(ctx, whID)
	var q float64
	for _, b := range bls {
		if b.MaterialID == matID {
			q = b.Qty
		}
	}
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Приход", fmt.Sprintf("st:in:%d:%d", whID, matID)),
			tgbotapi.NewInlineKeyboardButtonData("➖ Списание", fmt.Sprintf("st:out:%d:%d", whID, matID)),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
	text := fmt.Sprintf("Склад: %d\nМатериал: %s\nОстаток: %.3f %s", whID, m.Name, q, m.Unit)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

// maybeNotifyLowOrNegative Информирование при минусовом/низком остатке (только для материалов в граммах)
func (b *Bot) maybeNotifyLowOrNegative(ctx context.Context, chatID int64, whID, matID int64) {
	m, _ := b.materials.GetByID(ctx, matID)
	if m == nil || m.Unit != materials.UnitG {
		return
	}
	qty, err := b.materials.GetBalance(ctx, whID, matID)
	if err != nil {
		return
	}
	if qty < 0 {
		b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ Материалы:\n— %s\nзакончились.", m.Name)))
	} else if qty < lowStockThresholdGr {
		b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("⚠️ Материалы:\n— %s — %.3f g\nзаканчиваются, не забудьте пополнить остаток на складе.", m.Name, qty)))
	}
}

/*** ADMIN UI ***/

func (b *Bot) adminMenu(chatID int64, editMessageID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Склад", "adm:wh:add"),
			tgbotapi.NewInlineKeyboardButtonData("📄 Склады", "adm:wh:list"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Категория", "adm:cat:add"),
			tgbotapi.NewInlineKeyboardButtonData("📄 Категории", "adm:cat:list"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
	text := "Админ-меню: выберите действие"
	if editMessageID != nil {
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMessageID, text, kb)
		b.send(edit)
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

/*** MESSAGE HANDLER ***/

func (b *Bot) onMessage(ctx context.Context, upd tgbotapi.Update) {
	msg := upd.Message
	chatID := msg.Chat.ID
	tgID := msg.From.ID

	if msg.IsCommand() {
		switch msg.Command() {
		case "start":
			u, err := b.users.UpsertByTelegram(ctx, tgID, users.RoleMaster)
			if err != nil {
				b.send(tgbotapi.NewMessage(chatID, "Ошибка: не удалось сохранить профиль"))
				return
			}
			// авто-админ
			if msg.From.ID == b.adminChat && (u.Role != users.RoleAdmin || u.Status != users.StatusApproved) {
				if _, err2 := b.users.Approve(ctx, msg.From.ID, users.RoleAdmin); err2 == nil {
					m := tgbotapi.NewMessage(chatID, "Привет, админ! Для управления ботом, ты можешь воспользоваться меню с кнопками или посмотреть список команд /help и работать через них.")
					m.ReplyMarkup = adminReplyKeyboard()
					b.send(m)
					return
				}
			}
			if u.Role == users.RoleAdmin && u.Status == users.StatusApproved {
				m := tgbotapi.NewMessage(chatID, "Привет, админ! Для управления ботом, ты можешь воспользоваться меню с кнопками или посмотреть список команд /help и работать через них.")
				m.ReplyMarkup = adminReplyKeyboard()
				b.send(m)
				return
			}

			switch u.Status {
			case users.StatusApproved:
				b.send(tgbotapi.NewMessage(chatID, "Вы уже подтверждены. /help — список команд."))
			case users.StatusRejected:
				_ = b.states.Set(ctx, chatID, dialog.StateAwaitFIO, dialog.Payload{})
				b.askFIO(chatID)
			default:
				_ = b.states.Set(ctx, chatID, dialog.StateAwaitFIO, dialog.Payload{})
				b.askFIO(chatID)
			}
			return

		case "help":
			b.send(tgbotapi.NewMessage(chatID,
				"Команды:\n/start — начать регистрацию/работу\n/help — помощь\n/admin — админ-меню (для админов)"))
			return

		case "admin":
			// Только для admin
			u, _ := b.users.GetByTelegramID(ctx, tgID)
			if u == nil || u.Role != users.RoleAdmin || u.Status != users.StatusApproved {
				b.send(tgbotapi.NewMessage(chatID, "Доступ запрещён"))
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateAdmMenu, dialog.Payload{})
			b.adminMenu(chatID, nil)
			return

		default:
			b.send(tgbotapi.NewMessage(chatID, "Не знаю такую команду. Наберите /help"))
			return
		}
	}

	// Кнопки нижней панели для админа
	if msg.Text == "Список команд" || msg.Text == "Склады" || msg.Text == "Категории" || msg.Text == "Материалы" || msg.Text == "Остатки" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Role != users.RoleAdmin || u.Status != users.StatusApproved {
			// игнорируем для не-админов
			return
		}
		switch msg.Text {
		case "Список команд":
			b.send(tgbotapi.NewMessage(chatID, "Команды:\n/start — начать регистрацию/работу\n/help — помощь\n/admin — админ-меню"))
		case "Склады":
			_ = b.states.Set(ctx, chatID, dialog.StateAdmWhMenu, dialog.Payload{})
			b.showWarehouseMenu(chatID, nil)
		case "Категории":
			_ = b.states.Set(ctx, chatID, dialog.StateAdmCatMenu, dialog.Payload{})
			b.showCategoryMenu(chatID, nil)
		case "Материалы":
			_ = b.states.Set(ctx, chatID, dialog.StateAdmMatMenu, dialog.Payload{})
			b.showMaterialMenu(chatID, nil)
			return
		case "Остатки":
			_ = b.states.Set(ctx, chatID, dialog.StateStockPickWh, dialog.Payload{})
			b.showStockWarehouseList(ctx, chatID, nil)
			return
		}
		return
	}

	// Диалоги (текстовые вводы)
	st, _ := b.states.Get(ctx, chatID)
	switch st.State {
	case dialog.StateAwaitFIO:
		fio := strings.TrimSpace(msg.Text)
		if fio == "" || len(fio) < 3 {
			b.send(tgbotapi.NewMessage(chatID, "ФИО выглядит пустым. Введите корректно."))
			return
		}
		if _, err := b.users.SetFIO(ctx, tgID, fio); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка сохранения ФИО, попробуйте ещё раз."))
			return
		}
		p := st.Payload
		p["fio"] = fio
		_ = b.states.Set(ctx, chatID, dialog.StateAwaitRole, p)
		m := tgbotapi.NewMessage(chatID, "Выберите роль:")
		m.ReplyMarkup = roleKeyboard()
		b.send(m)
		return

	case dialog.StateAdmWhName:
		// ввод названия склада
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		p := st.Payload
		p["wh_name"] = name
		_ = b.states.Set(ctx, chatID, dialog.StateAdmWhType, p)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Расходники", "adm:wh:type:consumables"),
				tgbotapi.NewInlineKeyboardButtonData("Клиентский", "adm:wh:type:client_service"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Выберите тип склада:")
		m.ReplyMarkup = kb
		b.send(m)
		return

	case dialog.StateAdmCatName:
		// ввод названия категории
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		if _, err := b.catalog.CreateCategory(ctx, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при создании категории"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmCatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Категория «%s» создана.", name)))
		b.showCategoryMenu(chatID, nil)
		return

	case dialog.StateAdmWhRename:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		idAny := st.Payload["wh_id"]
		id := int64(idAny.(float64)) // payload приходит из JSON; приведение через float64
		if _, err := b.catalog.UpdateWarehouseName(ctx, id, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при переименовании склада"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmWhMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, "Склад переименован."))
		// Вернём список
		b.showWarehouseMenu(chatID, nil)
		return

	case dialog.StateAdmCatRename:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		idAny := st.Payload["cat_id"]
		id := int64(idAny.(float64))
		if _, err := b.catalog.UpdateCategoryName(ctx, id, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при переименовании категории"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmCatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, "Категория переименована."))
		b.showCategoryMenu(chatID, nil)
		return

	case dialog.StateAdmMatName:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		cidAny := st.Payload["cat_id"]
		catID := int64(cidAny.(float64))
		if _, err := b.materials.Create(ctx, name, catID, materials.UnitG); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при создании материала"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmMatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Материал «%s» создан.", name)))
		b.showMaterialMenu(chatID, nil)
		return

	case dialog.StateAdmMatRename:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			b.send(tgbotapi.NewMessage(chatID, "Название не может быть пустым. Введите ещё раз."))
			return
		}
		idAny := st.Payload["mat_id"]
		id := int64(idAny.(float64))
		if _, err := b.materials.UpdateName(ctx, id, name); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка при переименовании материала"))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmMatMenu, dialog.Payload{})
		b.send(tgbotapi.NewMessage(chatID, "Материал переименован."))
		b.showMaterialMenu(chatID, nil)
		return
	case dialog.StateStockInQty:
		qtyStr := strings.TrimSpace(msg.Text)
		qty, err := strconv.ParseFloat(strings.ReplaceAll(qtyStr, ",", "."), 64)
		if err != nil || qty <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите положительное значение."))
			return
		}
		wh := int64(st.Payload["wh_id"].(float64))
		mat := int64(st.Payload["mat_id"].(float64))
		// actorID — ID из users, получим по telegram_id
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден"))
			return
		}
		if err := b.inventory.Receive(ctx, u.ID, wh, mat, qty, "bot"); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка прихода: "+err.Error()))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateStockItem, dialog.Payload{"wh_id": float64(wh), "mat_id": float64(mat)})
		b.send(tgbotapi.NewMessage(chatID, "Приход проведён"))
		// перерисуем карточку
		b.showStockItem(ctx, chatID, msg.MessageID, wh, mat)
		b.maybeNotifyLowOrNegative(ctx, chatID, wh, mat)
		return

	case dialog.StateStockOutQty:
		qtyStr := strings.TrimSpace(msg.Text)
		qty, err := strconv.ParseFloat(strings.ReplaceAll(qtyStr, ",", "."), 64)
		if err != nil || qty <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите положительное значение."))
			return
		}
		wh := int64(st.Payload["wh_id"].(float64))
		mat := int64(st.Payload["mat_id"].(float64))
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil {
			b.send(tgbotapi.NewMessage(chatID, "Пользователь не найден"))
			return
		}
		if err := b.inventory.WriteOff(ctx, u.ID, wh, mat, qty, "bot"); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка списания: "+err.Error()))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateStockItem, dialog.Payload{"wh_id": float64(wh), "mat_id": float64(mat)})
		b.send(tgbotapi.NewMessage(chatID, "Списание проведено"))
		b.showStockItem(ctx, chatID, msg.MessageID, wh, mat)
		b.maybeNotifyLowOrNegative(ctx, chatID, wh, mat)
		return
	}
}

/*** CALLBACK HANDLER ***/

func (b *Bot) onCallback(ctx context.Context, upd tgbotapi.Update) {
	cb := upd.CallbackQuery
	data := cb.Data
	fromChat := cb.Message.Chat.ID

	// Общая навигация
	if data == "nav:cancel" {
		_ = b.states.Reset(ctx, fromChat)
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Операция отменена.")
		_ = b.answerCallback(cb, "Отменено", false)
		return
	}
	if data == "nav:back" {
		st, _ := b.states.Get(ctx, fromChat)
		switch st.State {
		case dialog.StateAwaitRole:
			_ = b.states.Set(ctx, fromChat, dialog.StateAwaitFIO, dialog.Payload{})
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Измените ФИО и отправьте сообщением.")
			b.askFIO(fromChat)
		case dialog.StateAwaitConfirm:
			_ = b.states.Set(ctx, fromChat, dialog.StateAwaitRole, st.Payload)
			edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите роль:", roleKeyboard())
			b.send(edit)
		case dialog.StateAdmWhType:
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhName, st.Payload)
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Введите название склада сообщением.")
		case dialog.StateAdmWhMenu:
			b.showWarehouseMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		case dialog.StateAdmCatMenu:
			b.showCategoryMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
		case dialog.StateAdmCatRename:
			if idAny, ok := st.Payload["cat_id"]; ok {
				id := int64(idAny.(float64))
				b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{"cat_id": id})
			} else {
				b.showCategoryMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
			}
		case dialog.StateAdmWhRename:
			if idAny, ok := st.Payload["wh_id"]; ok {
				id := int64(idAny.(float64))
				b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{"wh_id": id})
			} else {
				b.showWarehouseMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
			}
		case dialog.StateAdmMatMenu:
			b.showMaterialMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
		case dialog.StateAdmCatName:
			b.showCategoryMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
		case dialog.StateAdmWhName:
			b.showWarehouseMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		case dialog.StateAdmMatList:
			// из списка — назад в меню материалов
			b.showMaterialMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
		case dialog.StateAdmMatItem:
			// из карточки — назад в список
			b.showMaterialList(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{})
		case dialog.StateAdmMatUnit:
			// из выбора единицы — назад в карточку
			if idAny, ok := st.Payload["mat_id"]; ok {
				id := int64(idAny.(float64))
				b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
			} else {
				b.showMaterialMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
			}
		case dialog.StateAdmMatRename:
			// из переименования — назад в карточку
			if idAny, ok := st.Payload["mat_id"]; ok {
				id := int64(idAny.(float64))
				b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
			} else {
				b.showMaterialMenu(fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
			}
		case dialog.StateAdmMatPickCat:
			// из выбора категории при создании — назад в меню материалов
			b.showMaterialMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatMenu, dialog.Payload{})
		case dialog.StateAdmMatName:
			// из ввода имени — назад к выбору категории
			b.showCategoryPick(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatPickCat, dialog.Payload{})
		case dialog.StateStockList:
			b.showStockWarehouseList(ctx, fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateStockPickWh, dialog.Payload{})
		case dialog.StateStockItem:
			if whAny, ok := st.Payload["wh_id"]; ok {
				wh := int64(whAny.(float64))
				b.showStockMaterialList(ctx, fromChat, cb.Message.MessageID, wh)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockList, dialog.Payload{"wh_id": wh})
			} else {
				b.showStockWarehouseList(ctx, fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockPickWh, dialog.Payload{})
			}
		case dialog.StateStockInQty, dialog.StateStockOutQty:
			if whAny, ok := st.Payload["wh_id"]; ok {
				wh := int64(whAny.(float64))
				mat := int64(st.Payload["mat_id"].(float64))
				b.showStockItem(ctx, fromChat, cb.Message.MessageID, wh, mat)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockItem, dialog.Payload{"wh_id": wh, "mat_id": mat})
			} else {
				b.showStockWarehouseList(ctx, fromChat, &cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateStockPickWh, dialog.Payload{})
			}

		default:
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Действие неактуально.")
		}
		_ = b.answerCallback(cb, "Назад", false)
		return
	}

	switch {
	/* ===== Регистрация (как раньше) ===== */

	case strings.HasPrefix(data, "role:"):
		roleStr := strings.TrimPrefix(data, "role:")
		var role users.Role
		if roleStr == "administrator" {
			role = users.RoleAdministrator
		} else {
			role = users.RoleMaster
		}
		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAwaitRole {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		fio, _ := dialog.GetString(st.Payload, "fio")
		p := st.Payload
		p["role"] = string(role)
		_ = b.states.Set(ctx, fromChat, dialog.StateAwaitConfirm, p)
		confirmText := fmt.Sprintf("Проверьте данные:\n— ФИО: %s\n— Роль: %s\n\nОтправить заявку администратору?", fio, role)
		edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, confirmText, confirmKeyboard())
		b.send(edit)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "rq:send":
		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAwaitConfirm {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		fio, _ := dialog.GetString(st.Payload, "fio")
		roleStr, _ := dialog.GetString(st.Payload, "role")
		role := users.Role(roleStr)
		_, _ = b.users.UpsertByTelegram(ctx, cb.From.ID, role)
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Заявка отправлена администратору. Ожидайте решения.")
		_ = b.states.Reset(ctx, fromChat)

		text := fmt.Sprintf(
			"Новая заявка на доступ:\n— ФИО: %s\n— Telegram: @%s (id %d)\n— Роль: %s\n\nОдобрить?",
			fio, cb.From.UserName, cb.From.ID, role,
		)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Одобрить", fmt.Sprintf("approve:%d:%s", cb.From.ID, role)),
				tgbotapi.NewInlineKeyboardButtonData("⛔ Отклонить", fmt.Sprintf("reject:%d", cb.From.ID)),
			),
		)
		m := tgbotapi.NewMessage(b.adminChat, text)
		m.ReplyMarkup = kb
		b.send(m)
		_ = b.answerCallback(cb, "Отправлено", false)
		return

	case strings.HasPrefix(data, "approve:"):
		if fromChat != b.adminChat {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		parts := strings.Split(strings.TrimPrefix(data, "approve:"), ":")
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		tgID, _ := strconv.ParseInt(parts[0], 10, 64)
		role := users.Role(parts[1])
		if _, err := b.users.Approve(ctx, tgID, role); err != nil {
			_ = b.answerCallback(cb, "Ошибка при одобрении", true)
			return
		}
		newText := cb.Message.Text + "\n\n✅ Заявка подтверждена"
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)
		b.send(tgbotapi.NewMessage(tgID, fmt.Sprintf("Заявка подтверждена. Ваша роль: %s", role)))
		_ = b.answerCallback(cb, "Одобрено", false)
		return

	case strings.HasPrefix(data, "reject:"):
		if fromChat != b.adminChat {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		tgID, _ := strconv.ParseInt(strings.TrimPrefix(data, "reject:"), 10, 64)
		if _, err := b.users.Reject(ctx, tgID); err != nil {
			_ = b.answerCallback(cb, "Ошибка при отклонении", true)
			return
		}
		newText := cb.Message.Text + "\n\n⛔ Заявка отклонена"
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)
		b.send(tgbotapi.NewMessage(tgID, "Заявка отклонена. Введите ФИО, чтобы подать заявку ещё раз."))
		_ = b.states.Set(ctx, tgID, dialog.StateAwaitFIO, dialog.Payload{})
		b.askFIO(tgID)
		_ = b.answerCallback(cb, "Отклонено", false)
		return

	/* ===== Админ-меню: склады/категории ===== */

	case data == "adm:wh:add":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhName, dialog.Payload{})
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Введите название склада сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:wh:list":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		// показываем список с кнопками-элементами
		b.showWarehouseList(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:wh:menu:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:wh:menu:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{"wh_id": id})
		b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:wh:rn:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:wh:rn:"), 10, 64)
		w, _ := b.catalog.GetWarehouseByID(ctx, id)
		if w == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if !w.Active {
			b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
			_ = b.answerCallback(cb, "Склад скрыт. Сначала включите его.", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhRename, dialog.Payload{"wh_id": id})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите новое название склада сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:wh:tg:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:wh:tg:"), 10, 64)
		w, _ := b.catalog.GetWarehouseByID(ctx, id)
		if w == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		_, err := b.catalog.SetWarehouseActive(ctx, id, !w.Active)
		if err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		// перерисовываем меню элемента
		b.showWarehouseItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Готово", false)
		return

	case strings.HasPrefix(data, "adm:wh:type:"):
		// выбор типа при создании
		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAdmWhType {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		whName, _ := dialog.GetString(st.Payload, "wh_name")
		tStr := strings.TrimPrefix(data, "adm:wh:type:")
		var t catalog.WarehouseType
		if tStr == "client_service" {
			t = catalog.WHTClientService
		} else {
			t = catalog.WHTConsumables
		}

		if _, err := b.catalog.CreateWarehouse(ctx, whName, t); err != nil {
			_ = b.answerCallback(cb, "Ошибка создания склада", true)
			return
		}
		// подтверждение и возврат в меню «Склады»
		b.editTextAndClear(fromChat, cb.Message.MessageID, fmt.Sprintf("Склад «%s» создан (%s).", whName, t))
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmWhMenu, dialog.Payload{})
		b.showWarehouseMenu(fromChat, nil)
		_ = b.answerCallback(cb, "Создано", false)
		return

	case data == "adm:cat:add":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatName, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите название категории сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:cat:list":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{})
		b.showCategoryList(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:cat:menu:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:cat:menu:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatMenu, dialog.Payload{"cat_id": id})
		b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:cat:rn:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:cat:rn:"), 10, 64)
		c, _ := b.catalog.GetCategoryByID(ctx, id)
		if c == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Категория не найдена")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if !c.Active {
			b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
			_ = b.answerCallback(cb, "Категория скрыта. Сначала включите её.", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmCatRename, dialog.Payload{"cat_id": id})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите новое название категории сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:cat:tg:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:cat:tg:"), 10, 64)
		c, _ := b.catalog.GetCategoryByID(ctx, id)
		if c == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Категория не найдена")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		_, err := b.catalog.SetCategoryActive(ctx, id, !c.Active)
		if err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		b.showCategoryItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Готово", false)
		return

	case data == "adm:mat:add":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatPickCat, dialog.Payload{})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Сначала выберите категорию для материала.")
		b.showCategoryPick(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:mat:list":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatList, dialog.Payload{})
		b.showMaterialList(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:menu:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:menu:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
		b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:pickcat:"):
		cid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:pickcat:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatName, dialog.Payload{"cat_id": cid})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите название материала сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:rn:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:rn:"), 10, 64)
		m, _ := b.materials.GetByID(ctx, id)
		if m == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Материал не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		if !m.Active {
			b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
			_ = b.answerCallback(cb, "Материал скрыт. Сначала включите его.", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatRename, dialog.Payload{"mat_id": id})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите новое название материала сообщением.")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:mat:tg:"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:mat:tg:"), 10, 64)
		m, _ := b.materials.GetByID(ctx, id)
		if m == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Материал не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		_, err := b.materials.SetActive(ctx, id, !m.Active)
		if err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.answerCallback(cb, "Готово", false)
		return

	case strings.HasPrefix(data, "adm:mat:unit:set:"):
		// формат: adm:mat:unit:set:<id>:<unit>
		payload := strings.TrimPrefix(data, "adm:mat:unit:set:")
		parts := strings.SplitN(payload, ":", 2)
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || id <= 0 {
			_ = b.answerCallback(cb, "Некорректный ID", true)
			return
		}
		unit := materials.Unit(parts[1])

		if _, err := b.materials.UpdateUnit(ctx, id, unit); err != nil {
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		// Показать карточку и зафиксировать состояние, чтобы Back вернул в неё
		b.showMaterialItemMenu(ctx, fromChat, cb.Message.MessageID, id)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatItem, dialog.Payload{"mat_id": id})
		_ = b.answerCallback(cb, "Обновлено", false)
		return

	case strings.HasPrefix(data, "adm:mat:unit:"):
		tail := strings.TrimPrefix(data, "adm:mat:unit:")
		if strings.HasPrefix(tail, "set:") {
			// этот колбэк обрабатывается в кейсе выше
			return
		}
		id, err := strconv.ParseInt(tail, 10, 64)
		if err != nil || id <= 0 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmMatUnit, dialog.Payload{"mat_id": id})
		kb := b.unitKeyboard(id)
		edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите единицу измерения:", kb)
		b.send(edit)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Остатки: выбор склада -> список
	case strings.HasPrefix(data, "st:list:"):
		whID, _ := strconv.ParseInt(strings.TrimPrefix(data, "st:list:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockList, dialog.Payload{"wh_id": whID})
		b.showStockMaterialList(ctx, fromChat, cb.Message.MessageID, whID)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Выбор строки из списка -> карточка
	case strings.HasPrefix(data, "st:item:"):
		parts := strings.Split(strings.TrimPrefix(data, "st:item:"), ":")
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		whID, _ := strconv.ParseInt(parts[0], 10, 64)
		matID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockItem, dialog.Payload{"wh_id": whID, "mat_id": matID})
		b.showStockItem(ctx, fromChat, cb.Message.MessageID, whID, matID)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Приход: запрос количества
	case strings.HasPrefix(data, "st:in:"):
		parts := strings.Split(strings.TrimPrefix(data, "st:in:"), ":")
		whID, _ := strconv.ParseInt(parts[0], 10, 64)
		matID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockInQty, dialog.Payload{"wh_id": whID, "mat_id": matID})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество для прихода (число, например 10.5)")
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Списание: запрос количества
	case strings.HasPrefix(data, "st:out:"):
		parts := strings.Split(strings.TrimPrefix(data, "st:out:"), ":")
		whID, _ := strconv.ParseInt(parts[0], 10, 64)
		matID, _ := strconv.ParseInt(parts[1], 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateStockOutQty, dialog.Payload{"wh_id": whID, "mat_id": matID})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество для списания (число, например 3)")
		_ = b.answerCallback(cb, "Ок", false)
		return
	}
}

func (b *Bot) answerCallback(cb *tgbotapi.CallbackQuery, text string, alert bool) error {
	resp := tgbotapi.NewCallback(cb.ID, text)
	resp.ShowAlert = alert
	_, err := b.api.Request(resp)
	return err
}
