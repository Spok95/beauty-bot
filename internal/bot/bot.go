package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/beauty-bot/internal/domain/consumption"
	"github.com/Spok95/beauty-bot/internal/domain/inventory"
	"github.com/Spok95/beauty-bot/internal/domain/materials"
	subs "github.com/Spok95/beauty-bot/internal/domain/subscriptions"
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
	subs      *subs.Repo
	rates     *consumption.RateRepo
}

func New(api *tgbotapi.BotAPI, log *slog.Logger,
	usersRepo *users.Repo, statesRepo *dialog.Repo,
	adminChatID int64, catalogRepo *catalog.Repo,
	materialsRepo *materials.Repo, inventoryRepo *inventory.Repo,
	consRepo *consumption.Repo, subsRepo *subs.Repo,
	rateRepo *consumption.RateRepo) *Bot {

	return &Bot{
		api: api, log: log, users: usersRepo, states: statesRepo,
		adminChat: adminChatID, catalog: catalogRepo,
		materials: materialsRepo, inventory: inventoryRepo,
		cons: consRepo, subs: subsRepo, rates: rateRepo,
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

func (b *Bot) getConsumablesWarehouseID(ctx context.Context) (int64, error) {
	ws, err := b.catalog.ListWarehouses(ctx)
	if err != nil {
		return 0, err
	}
	for _, w := range ws {
		if w.Active && w.Type == "consumables" {
			return w.ID, nil
		}
	}
	return 0, fmt.Errorf("склад Расходники не найден/не активен")
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

// adminReplyKeyboard Нижняя панель (ReplyKeyboard) для админа
func adminReplyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			{tgbotapi.NewKeyboardButton("Склады"), tgbotapi.NewKeyboardButton("Категории")},
			{tgbotapi.NewKeyboardButton("Материалы")},
			{tgbotapi.NewKeyboardButton("Остатки"), tgbotapi.NewKeyboardButton("Поставки")},
			{tgbotapi.NewKeyboardButton("Абонементы")},
			{tgbotapi.NewKeyboardButton("Установка тарифов")},
			{tgbotapi.NewKeyboardButton("Список команд")},
		},
	}
}

func masterReplyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			{tgbotapi.NewKeyboardButton("Расход/Аренда")},
			{tgbotapi.NewKeyboardButton("Мои абонементы")},
			{tgbotapi.NewKeyboardButton("Купить абонемент")},
			{tgbotapi.NewKeyboardButton("Список команд")},
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

	// Переключатель активности
	toggle := "🙈 Скрыть"
	if !m.Active {
		toggle = "👁 Показать"
	}

	// Кнопки
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

	// Получаем название категории
	catName := fmt.Sprintf("ID:%d", m.CategoryID)
	if c, _ := b.catalog.GetCategoryByID(ctx, m.CategoryID); c != nil {
		catName = c.Name
	}

	text := fmt.Sprintf(
		"Материал: %s %s\nКатегория: %s\nЕд.: %s\nСтатус: %v",
		badge(m.Active), m.Name, catName, m.Unit, m.Active,
	)

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
			if it.Balance <= 0 {
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
	m, err := b.materials.GetByID(ctx, matID)
	if err != nil || m == nil {
		b.editTextAndClear(chatID, editMsgID, "Материал не найден")
		return
	}

	// Имя и тип склада
	w, _ := b.catalog.GetWarehouseByID(ctx, whID)
	whTitle := fmt.Sprintf("ID:%d", whID)
	if w != nil {
		// человекочитаемый тип
		t := "неизвестный"
		switch w.Type {
		case catalog.WHTConsumables:
			t = "расходники"
		case catalog.WHTClientService:
			t = "клиентский"
		}
		whTitle = fmt.Sprintf("%s (%s)", w.Name, t)
	}

	// Текущий остаток (может быть отрицательным)
	qty, err := b.materials.GetBalance(ctx, whID, matID)
	if err != nil {
		qty = 0
	}

	// Кнопки действий
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Приход", fmt.Sprintf("st:in:%d:%d", whID, matID)),
			tgbotapi.NewInlineKeyboardButtonData("➖ Списание", fmt.Sprintf("st:out:%d:%d", whID, matID)),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)

	text := fmt.Sprintf(
		"Склад: %s\nМатериал: %s\nОстаток: %.3f %s",
		whTitle, m.Name, qty, m.Unit,
	)

	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

func (b *Bot) showSuppliesMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Приёмка", "sup:add"),
			tgbotapi.NewInlineKeyboardButtonData("📄 Журнал", "sup:list"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Поставки — выберите действие", kb))
	} else {
		m := tgbotapi.NewMessage(chatID, "Поставки — выберите действие")
		m.ReplyMarkup = kb
		b.send(m)
	}
}

func (b *Bot) showSuppliesPickWarehouse(ctx context.Context, chatID int64, editMsgID *int) {
	ws, err := b.catalog.ListWarehouses(ctx)
	if err != nil {
		b.editTextAndClear(chatID, *editMsgID, "Ошибка загрузки складов")
		return
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, w := range ws {
		if !w.Active {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(w.Name, fmt.Sprintf("sup:wh:%d", w.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Выберите склад:", kb))
}

func (b *Bot) showSuppliesPickMaterial(ctx context.Context, chatID int64, editMsgID int) {
	mats, err := b.materials.List(ctx, true) // только активные
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки материалов")
		return
	}
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, m := range mats {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(m.Name, fmt.Sprintf("sup:mat:%d", m.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Выберите материал:", kb))
}

// parseSupItems достаёт []map[string]any из payload["items"]
func (b *Bot) parseSupItems(v any) []map[string]any {
	items := []map[string]any{}
	arr, ok := v.([]any)
	if !ok {
		if mm, ok2 := v.([]map[string]any); ok2 {
			return mm
		}
		return items
	}
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

// showSuppliesCart Показ корзины поставки: список позиций и итог
func (b *Bot) showSuppliesCart(ctx context.Context, chatID int64, editMsgID *int, whID int64, items []map[string]any) {
	// имя склада
	whName := fmt.Sprintf("ID:%d", whID)
	if w, _ := b.catalog.GetWarehouseByID(ctx, whID); w != nil {
		whName = w.Name
	}

	lines := []string{fmt.Sprintf("Поставка (склад: %s):", whName)}
	var total float64
	for _, it := range items {
		matID := int64(it["mat_id"].(float64))
		qty := int64(it["qty"].(float64))
		price := it["price"].(float64)
		name := fmt.Sprintf("ID:%d", matID)
		if m, _ := b.materials.GetByID(ctx, matID); m != nil {
			name = m.Name
		}
		lineTotal := float64(qty) * price
		total += lineTotal
		lines = append(lines, fmt.Sprintf("• %s — %d × %.2f = %.2f ₽", name, qty, price, lineTotal))
	}
	lines = append(lines, fmt.Sprintf("\nИтого: %.2f ₽", total))

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Добавить позицию", "sup:additem"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Провести", "sup:confirm"),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)

	text := strings.Join(lines, "\n")
	st, _ := b.states.Get(ctx, chatID)
	if editMsgID != nil {
		// редактируем существующее сообщение корзины
		edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb)
		b.send(edit)
		// сохраняем шаг + id сообщения корзины для «Назад/Отмена»
		b.saveLastStep(ctx, chatID, dialog.StateSupCart, st.Payload, *editMsgID)
	} else {
		// отправляем новое сообщение корзины
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = kb
		sent, _ := b.api.Send(msg)
		// сохраняем шаг + id нового сообщения корзины
		b.saveLastStep(ctx, chatID, dialog.StateSupCart, st.Payload, sent.MessageID)
	}
}

func (b *Bot) consParseItems(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		if mm, ok2 := v.([]map[string]any); ok2 {
			return mm
		}
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
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

func (b *Bot) subBuyPlaceKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Общий зал (часы)", "subbuy:place:hall"),
			tgbotapi.NewInlineKeyboardButtonData("Кабинет (дни)", "subbuy:place:cabinet"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
}

// maybeNotifyLowOrNegative Информирование при минусовом/низком остатке (только для материалов в граммах)
func (b *Bot) maybeNotifyLowOrNegative(ctx context.Context, _ int64, whID, matID int64) {
	// 1) Остаток
	bal, err := b.inventory.GetBalance(ctx, whID, matID)
	if err != nil {
		return
	}

	// 2) Материал (имя + ед.)
	m, _ := b.materials.GetByID(ctx, matID)
	name := fmt.Sprintf("ID:%d", matID)
	unit := "g"
	if m != nil {
		name = m.Name
		if s := string(m.Unit); s != "" {
			unit = s
		}
	}

	// 3) Порог по ед. измерения
	var thr float64
	switch unit {
	case "g":
		thr = lowStockThresholdGr
	case "pcs":
		thr = lowStockThresholdPcs
	default:
		// прочие единицы сейчас не сигналим
		return
	}

	// 4) Сообщение
	var text string
	if bal < 0 {
		text = fmt.Sprintf("⚠️ Материалы:\n— %s\nзакончились.", name)
	} else if bal >= 0 && bal < thr {
		// подпись единицы в тексте
		unitRU := "g"
		if unit == "pcs" {
			unitRU = "шт"
		}
		text = fmt.Sprintf("⚠️ Материалы:\n— %s — %.0f %s заканчиваются…", name, bal, unitRU)
	} else {
		return
	}

	// 5) Рассылка — админ-чат + все администраторы (+админы)
	b.notifyStockRecipients(ctx, text)
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

// notifyStockRecipients Шлём оповещение в админ-чат и всем администраторам (role=administrator) + дублируем админам (role=admin) на всякий случай.
func (b *Bot) notifyStockRecipients(ctx context.Context, text string) {
	// не шлём одному и тому же chat_id дважды
	sent := map[int64]struct{}{}
	sendOnce := func(chatID int64) {
		if chatID == 0 {
			return
		}
		if _, ok := sent[chatID]; ok {
			return
		}
		b.send(tgbotapi.NewMessage(chatID, text))
		sent[chatID] = struct{}{}
	}

	// 1) админ-чат (может быть личка или группа)
	sendOnce(b.adminChat)

	// 2) подтверждённые администраторы
	if list, err := b.users.ListByRole(ctx, users.RoleAdministrator, users.StatusApproved); err == nil {
		for _, u := range list {
			sendOnce(u.TelegramID)
		}
	}

	// 3) подтверждённые админы
	if list, err := b.users.ListByRole(ctx, users.RoleAdmin, users.StatusApproved); err == nil {
		for _, u := range list {
			sendOnce(u.TelegramID)
		}
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

			if u.Role == users.RoleMaster && u.Status == users.StatusApproved {
				m := tgbotapi.NewMessage(chatID, "Готово! Для учёта материалов и аренды жми «Расход/Аренда» или используй /rent.")
				m.ReplyMarkup = masterReplyKeyboard()
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
				"Команды:\n/start — начать регистрацию/работу\n/help — помощь"))
			return

		case "admin":
			// Только для admin — показываем техсообщение без меню
			u, _ := b.users.GetByTelegramID(ctx, tgID)
			if u == nil || u.Role != users.RoleAdmin || u.Status != users.StatusApproved {
				b.send(tgbotapi.NewMessage(chatID, "Доступ запрещён"))
				return
			}
			b.send(tgbotapi.NewMessage(chatID,
				"Раздел администрирования временно выключен. Настройка тарифов будет доступна через кнопку «Установка тарифов»."))
			return

		case "rent":
			u, _ := b.users.GetByTelegramID(ctx, tgID)
			if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
				b.send(tgbotapi.NewMessage(chatID, "Доступ запрещён."))
				return
			}
			_ = b.states.Set(ctx, chatID, dialog.StateConsPlace, dialog.Payload{})
			kb := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
					tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
				),
				navKeyboard(false, true).InlineKeyboard[0],
			)
			m := tgbotapi.NewMessage(chatID, "Выберите помещение:")
			m.ReplyMarkup = kb
			b.send(m)
			return

		default:
			b.send(tgbotapi.NewMessage(chatID, "Не знаю такую команду. Наберите /help"))
			return
		}
	}

	// Нижняя панель мастера
	if msg.Text == "Расход/Аренда" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateConsPlace, dialog.Payload{})
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Выберите помещение:")
		m.ReplyMarkup = kb
		b.send(m)
		return
	}

	if msg.Text == "Мои абонементы" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			return
		}
		month := time.Now().Format("2006-01")
		list, err := b.subs.ListByUserMonth(ctx, u.ID, month)
		if err != nil || len(list) == 0 {
			b.send(tgbotapi.NewMessage(chatID, "На текущий месяц абонементов нет."))
			return
		}
		var sb strings.Builder
		sb.WriteString("Мои абонементы (текущий месяц):\n")
		placeRU := map[string]string{"hall": "Зал", "cabinet": "Кабинет"}
		unitRU := map[string]string{"hour": "ч", "day": "дн"}
		for _, s := range list {
			left := s.TotalQty - s.UsedQty
			if left < 0 {
				left = 0
			}
			sb.WriteString(fmt.Sprintf("— %s, %s: %d/%d (остаток %d)\n",
				placeRU[s.Place], unitRU[s.Unit], s.UsedQty, s.TotalQty, left))
		}
		b.send(tgbotapi.NewMessage(chatID, sb.String()))
		return
	}

	if msg.Text == "Купить абонемент" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateSubBuyPlace, dialog.Payload{})
		m := tgbotapi.NewMessage(chatID, "Выберите тип абонемента:")
		m.ReplyMarkup = b.subBuyPlaceKeyboard()
		b.send(m)
		return
	}

	// "Список команд" — доступно всем подтверждённым
	if msg.Text == "Список команд" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved {
			b.send(tgbotapi.NewMessage(chatID, "Сначала пройдите регистрацию: /start"))
			return
		}
		if u.Role == users.RoleMaster {
			b.send(tgbotapi.NewMessage(chatID, "Команды мастера:\n/rent — расход/аренда\n/help — помощь"))
		} else if u.Role == users.RoleAdmin {
			b.send(tgbotapi.NewMessage(chatID, "Команды админа:\n/admin — админ-меню\n/help — помощь"))
		} else {
			b.send(tgbotapi.NewMessage(chatID, "Команды:\n/help — помощь"))
		}
		return
	}

	// Кнопки нижней панели для админа
	if msg.Text == "Склады" || msg.Text == "Категории" || msg.Text == "Материалы" || msg.Text == "Остатки" || msg.Text == "Поставки" || msg.Text == "Абонементы" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Role != users.RoleAdmin || u.Status != users.StatusApproved {
			// игнорируем для не-админов
			return
		}
		switch msg.Text {
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
		case "Поставки":
			_ = b.states.Set(ctx, chatID, dialog.StateSupMenu, dialog.Payload{})
			b.showSuppliesMenu(chatID, nil)
			return
		case "Абонементы":
			_ = b.states.Set(ctx, chatID, dialog.StateAdmSubsMenu, dialog.Payload{})
			b.showSubsMenu(chatID, nil)
			return
		}
		return
	}

	if msg.Text == "Установка тарифов" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Role != users.RoleAdmin || u.Status != users.StatusApproved {
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesPickPU, dialog.Payload{
			"place": "hall", "unit": "hour", "with_sub": false,
		})
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал / час", "rates:pu:hall:hour"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет / день", "rates:pu:cabinet:day"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Абонемент: выкл", "rates:sub:tg"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📄 Показать ступени", "rates:list"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Установка тарифов — выберите набор параметров:")
		m.ReplyMarkup = kb
		b.send(m)
		return
	}

	// Триггеры расхода/аренды по тексту (доступно всем подтверждённым ролям)
	if msg.Text == "/rent" || msg.Text == "/consumption" {
		u, _ := b.users.GetByTelegramID(ctx, tgID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			b.send(tgbotapi.NewMessage(chatID, "Доступ запрещён."))
			return
		}
		_ = b.states.Set(ctx, chatID, dialog.StateConsPlace, dialog.Payload{})
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
			),
			navKeyboard(false, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, "Выберите помещение:")
		m.ReplyMarkup = kb
		b.send(m)
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

	case dialog.StateSupQty:
		// Чистим прошлую клавиатуру под сообщением шага "количество"
		b.clearPrevStep(ctx, chatID)

		qtyStr := strings.TrimSpace(msg.Text)
		qtyStr = strings.ReplaceAll(qtyStr, ",", ".")
		// только целые числа: граммы/шт, без дробной части
		if strings.Contains(qtyStr, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число без дробной части (используем граммы/шт)."))
			return
		}
		n, err := strconv.ParseInt(qtyStr, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите целое положительное значение."))
			return
		}
		// сохраняем целое значение; payload сериализуется как float64 — это ок
		st.Payload["qty"] = float64(n)
		_ = b.states.Set(ctx, chatID, dialog.StateSupUnitPrice, st.Payload)
		m := tgbotapi.NewMessage(chatID, "Введите цену за единицу (руб)")
		m.ReplyMarkup = navKeyboard(true, true)
		sent, _ := b.api.Send(m)

		// сохраняем last_mid и переключаемся на шаг цены
		b.saveLastStep(ctx, chatID, dialog.StateSupUnitPrice, st.Payload, sent.MessageID)
		return

	case dialog.StateSupUnitPrice:
		b.clearPrevStep(ctx, chatID)

		st, _ := b.states.Get(ctx, chatID)
		if st == nil || st.Payload == nil {
			// начнем заново
			_ = b.states.Set(ctx, chatID, dialog.StateSupPickWh, dialog.Payload{})
			b.showSuppliesPickWarehouse(ctx, chatID, nil)
			return
		}
		whF, okWh := st.Payload["wh_id"].(float64)
		matF, okMat := st.Payload["mat_id"].(float64)
		if !okWh || !okMat {
			// контекст потерян — возвращаем на выбор склада
			_ = b.states.Set(ctx, chatID, dialog.StateSupPickWh, dialog.Payload{})
			b.showSuppliesPickWarehouse(ctx, chatID, nil)
			return
		}
		whID := int64(whF)
		matID := int64(matF)

		priceStr := strings.TrimSpace(msg.Text)
		price, err := strconv.ParseFloat(strings.ReplaceAll(priceStr, ",", "."), 64)
		if err != nil || price < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное число. Введите цену (руб)."))
			return
		}
		qty := int64(st.Payload["qty"].(float64)) // мы сохраняли как float64, но значение целое

		// Добавляем позицию в payload["items"]
		items := b.parseSupItems(st.Payload["items"])
		items = append(items, map[string]any{
			"mat_id": float64(matID), // через float64, чтобы без проблем сериализовалось
			"qty":    float64(qty),
			"price":  price,
		})
		st.Payload["items"] = items

		// Переходим в корзину
		_ = b.states.Set(ctx, chatID, dialog.StateSupCart, st.Payload)
		b.showSuppliesCart(ctx, chatID, nil, whID, items)
		return

	case dialog.StateConsQty:
		s := strings.TrimSpace(msg.Text)
		s = strings.ReplaceAll(s, ",", ".")
		if strings.Contains(s, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число (часов/дней)."))
			return
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное значение. Введите целое положительное число."))
			return
		}
		st.Payload["qty"] = float64(n)
		// корзина пустая
		st.Payload["items"] = []map[string]any{}
		_ = b.states.Set(ctx, chatID, dialog.StateConsCart, st.Payload)
		b.showConsCart(ctx, chatID, nil, st.Payload["place"].(string), st.Payload["unit"].(string), int(n), []map[string]any{})
		return

	case dialog.StateConsMatQty:
		s := strings.TrimSpace(msg.Text)
		s = strings.ReplaceAll(s, ",", ".")
		if strings.Contains(s, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число (граммы/шт)."))
			return
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное значение. Введите целое положительное число."))
			return
		}
		items := b.consParseItems(st.Payload["items"])
		items = append(items, map[string]any{
			"mat_id": st.Payload["mat_id"],
			"qty":    float64(n),
		})
		st.Payload["items"] = items
		_ = b.states.Set(ctx, chatID, dialog.StateConsCart, st.Payload)
		b.showConsCart(ctx, chatID, nil, st.Payload["place"].(string), st.Payload["unit"].(string), int(st.Payload["qty"].(float64)), items)
		return

	case dialog.StateAdmSubsEnterQty:
		s := strings.TrimSpace(msg.Text)
		if strings.Contains(s, ",") {
			s = strings.ReplaceAll(s, ",", ".")
		}
		if strings.Contains(s, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число (без дробной части)."))
			return
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное значение. Введите целое положительное число."))
			return
		}

		st.Payload["total"] = float64(n)
		_ = b.states.Set(ctx, chatID, dialog.StateAdmSubsConfirm, st.Payload)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		uid := int64(st.Payload["uid"].(float64))
		month := time.Now().Format("2006-01")

		// Для превью: найдём пользователя по uid
		var title string
		if u, _ := b.users.GetByID(ctx, uid); u != nil {
			title = strings.TrimSpace(u.Username) // у нас «ФИО/отображаемое имя» хранится в Username
			if title == "" {
				title = fmt.Sprintf("id %d", u.ID)
			}
		} else {
			title = fmt.Sprintf("id %d", uid)
		}

		preview := fmt.Sprintf(
			"Подтвердите создание абонемента:\nМастер: %s\nМесяц: %s\nМесто: %s\nЕдиница: %s\nОбъём: %d",
			title, month,
			map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
			map[string]string{"hour": "часы", "day": "дни"}[unit],
			n,
		)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "adm:subs:confirm"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, preview)
		m.ReplyMarkup = kb
		b.send(m)
		return

	case dialog.StateAdmRatesCreateMin:
		s := strings.TrimSpace(msg.Text)
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое положительное число"))
			return
		}
		st.Payload["min"] = float64(n)
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreateMax, st.Payload)
		b.send(tgbotapi.NewMessage(chatID, "Введите максимальное значение диапазона или «-» для бесконечности"))
		return

	case dialog.StateAdmRatesCreateMax:
		s := strings.TrimSpace(msg.Text)
		if s == "-" {
			st.Payload["max"] = nil
		} else {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil || n <= 0 {
				b.send(tgbotapi.NewMessage(chatID, "Введите целое положительное число или «-»"))
				return
			}
			st.Payload["max"] = float64(n)
		}
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreateThreshold, st.Payload)
		b.send(tgbotapi.NewMessage(chatID, "Введите порог материалов на единицу (например 100 или 1000)"))
		return

	case dialog.StateAdmRatesCreateThreshold:
		s := strings.TrimSpace(msg.Text)
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil || x < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите число (>= 0)"))
			return
		}
		st.Payload["thr"] = x
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreatePriceWith, st.Payload)
		b.send(tgbotapi.NewMessage(chatID, "Цена за ед., если порог выполнен (руб)"))
		return

	case dialog.StateAdmRatesCreatePriceWith:
		s := strings.TrimSpace(msg.Text)
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil || x < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите число (>= 0)"))
			return
		}
		st.Payload["pwith"] = x
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesCreatePriceOwn, st.Payload)
		b.send(tgbotapi.NewMessage(chatID, "Цена за ед., если порог НЕ выполнен (руб)"))
		return

	case dialog.StateAdmRatesCreatePriceOwn:
		s := strings.TrimSpace(msg.Text)
		x, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil || x < 0 {
			b.send(tgbotapi.NewMessage(chatID, "Введите число (>= 0)"))
			return
		}
		st.Payload["pown"] = x
		_ = b.states.Set(ctx, chatID, dialog.StateAdmRatesConfirm, st.Payload)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}

		minQty := int(st.Payload["min"].(float64))
		var maxTxt string
		if st.Payload["max"] == nil {
			maxTxt = "∞"
		} else {
			maxTxt = fmt.Sprintf("%d", int(st.Payload["max"].(float64)))
		}
		thr := st.Payload["thr"].(float64)
		pwith := st.Payload["pwith"].(float64)
		pown := st.Payload["pown"].(float64)

		preview := fmt.Sprintf(
			"Ступень:\n— %s / %s (%s)\n— Диапазон: %d–%s\n— Порог: %.0f\n— Цена с материалами: %.2f\n— Цена со своими: %.2f\n\nСохранить?",
			map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
			map[string]string{"hour": "час", "day": "день"}[unit],
			map[bool]string{true: "с абонементом", false: "без абонемента"}[withSub],
			minQty, maxTxt, thr, pwith, pown,
		)

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("💾 Сохранить", "rates:save")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, preview)
		m.ReplyMarkup = kb
		b.send(m)
		return

	case dialog.StateSubBuyQty:
		s := strings.TrimSpace(msg.Text)
		s = strings.ReplaceAll(s, ",", ".")
		if strings.Contains(s, ".") {
			b.send(tgbotapi.NewMessage(chatID, "Введите целое число (без дробной части)."))
			return
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 {
			b.send(tgbotapi.NewMessage(chatID, "Некорректное значение. Введите целое положительное число."))
			return
		}
		st.Payload["qty"] = float64(n)
		_ = b.states.Set(ctx, chatID, dialog.StateSubBuyConfirm, st.Payload)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		month := time.Now().Format("2006-01")
		txt := fmt.Sprintf("Подтвердите покупку абонемента:\nМесяц: %s\nМесто: %s\nЕдиница: %s\nОбъём: %d",
			month,
			map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
			map[string]string{"hour": "часы", "day": "дни"}[unit],
			n,
		)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "subbuy:confirm")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(chatID, txt)
		m.ReplyMarkup = kb
		b.send(m)
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
		case dialog.StateSupPickWh, dialog.StateSupMenu:
			b.showSuppliesMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateSupMenu, dialog.Payload{})
		case dialog.StateSupPickMat:
			b.showSuppliesPickWarehouse(ctx, fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateSupPickWh, dialog.Payload{})
		case dialog.StateSupQty:
			b.showSuppliesPickMaterial(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateSupPickMat, st.Payload)
		case dialog.StateSupUnitPrice:
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество (число, например 250)")
			_ = b.states.Set(ctx, fromChat, dialog.StateSupQty, st.Payload)
		case dialog.StateSupConfirm:
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите цену за единицу (руб)")
			_ = b.states.Set(ctx, fromChat, dialog.StateSupUnitPrice, st.Payload)
		case dialog.StateSupCart:
			// Возврат к редактированию последней добавленной позиции
			items := b.parseSupItems(st.Payload["items"])
			if len(items) == 0 {
				// Корзина пуста — вернём меню поставок
				_ = b.states.Set(ctx, fromChat, dialog.StateSupMenu, dialog.Payload{})
				b.showSuppliesMenu(fromChat, &cb.Message.MessageID)
				return
			}
			last := items[len(items)-1]
			// Удаляем последнюю позицию из корзины — будем вводить её заново
			items = items[:len(items)-1]

			// Собираем payload для шага ввода цены (предыдущий шаг после qty)
			payload := dialog.Payload{
				"wh_id":  st.Payload["wh_id"],
				"mat_id": last["mat_id"],
				"qty":    last["qty"],
				"items":  items,
			}
			_ = b.states.Set(ctx, fromChat, dialog.StateSupUnitPrice, payload)
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите цену за единицу (руб)")
			return
		case dialog.StateConsQty:
			// назад к выбору помещения
			_ = b.states.Set(ctx, fromChat, dialog.StateConsPlace, st.Payload)
			kb := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
					tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
				),
				navKeyboard(false, true).InlineKeyboard[0],
			)
			b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите помещение:", kb))
		case dialog.StateConsCart:
			// назад к вводу количества часов/дней
			b.editTextWithNav(fromChat, cb.Message.MessageID, fmt.Sprintf("Введите количество (%s):", map[string]string{"hour": "часы", "day": "дни"}[st.Payload["unit"].(string)]))
			_ = b.states.Set(ctx, fromChat, dialog.StateConsQty, st.Payload)
		case dialog.StateConsMatPick:
			// назад — снова корзина
			items := b.consParseItems(st.Payload["items"])
			_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
			b.showConsCart(ctx, fromChat, &cb.Message.MessageID, st.Payload["place"].(string), st.Payload["unit"].(string), int(st.Payload["qty"].(float64)), items)
		case dialog.StateConsMatQty:
			// назад к выбору материала
			_ = b.states.Set(ctx, fromChat, dialog.StateConsMatPick, st.Payload)
			mats, _ := b.materials.List(ctx, true)
			rows := [][]tgbotapi.InlineKeyboardButton{}
			for _, m := range mats {
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(m.Name, fmt.Sprintf("cons:mat:%d", m.ID)),
				))
			}
			rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
			kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
			b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите материал:", kb))
		case dialog.StateConsSummary:
			// назад в корзину
			items := b.consParseItems(st.Payload["items"])
			_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
			b.showConsCart(ctx, fromChat, &cb.Message.MessageID, st.Payload["place"].(string), st.Payload["unit"].(string), int(st.Payload["qty"].(float64)), items)

		case dialog.StateAdmSubsMenu:
			b.showSubsMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsMenu, dialog.Payload{})

		case dialog.StateAdmSubsPickUser:
			b.showSubsMenu(fromChat, &cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsMenu, dialog.Payload{})

		case dialog.StateAdmSubsPickPlaceUnit:
			b.showSubsPickUser(ctx, fromChat, cb.Message.MessageID)
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickUser, dialog.Payload{})

		case dialog.StateAdmSubsEnterQty:
			// Назад к выбору места/единицы
			if v, ok := st.Payload["uid"]; ok {
				uid := int64(v.(float64))
				b.showSubsPickPlaceUnit(fromChat, cb.Message.MessageID, uid)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickPlaceUnit, st.Payload)
			} else {
				b.showSubsPickUser(ctx, fromChat, cb.Message.MessageID)
				_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickUser, dialog.Payload{})
			}

		case dialog.StateAdmSubsConfirm:
			// назад к вводу количества
			b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите объём на месяц (целое число):")
			_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsEnterQty, st.Payload)

		case dialog.StateSubBuyQty:
			_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyPlace, st.Payload)
			b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID,
				"Выберите тип абонемента:", b.subBuyPlaceKeyboard()))
		case dialog.StateSubBuyConfirm:
			_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyQty, st.Payload)
			b.editTextWithNav(fromChat, cb.Message.MessageID,
				fmt.Sprintf("Введите объём (%s):", map[string]string{"hour": "часы", "day": "дни"}[st.Payload["unit"].(string)]))

		default:
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Действие неактуально.")
		}
		_ = b.answerCallback(cb, "Назад", false)
		return
	}

	switch {
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
		b.send(tgbotapi.NewMessage(tgID, fmt.Sprintf("Заявка подтверждена, нажмите /start, чтобы обновить меню. Ваша роль: %s", role)))
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

	case data == "adm:subs:add":
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickUser, dialog.Payload{})
		b.showSubsPickUser(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:subs:user:"):
		uid, _ := strconv.ParseInt(strings.TrimPrefix(data, "adm:subs:user:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsPickPlaceUnit, dialog.Payload{"uid": float64(uid)})
		b.showSubsPickPlaceUnit(fromChat, cb.Message.MessageID, uid)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "adm:subs:pu:"):
		// формат: adm:subs:pu:<uid>:<place>:<unit>
		parts := strings.Split(strings.TrimPrefix(data, "adm:subs:pu:"), ":")
		if len(parts) != 3 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		uid, _ := strconv.ParseInt(parts[0], 10, 64)
		place := parts[1] // "hall"|"cabinet"
		unit := parts[2]  // "hour"|"day"
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsEnterQty, dialog.Payload{
			"uid": float64(uid), "place": place, "unit": unit,
		})
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите объём на месяц (целое число):")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "adm:subs:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		uid := int64(st.Payload["uid"].(float64))
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		total := int(st.Payload["total"].(float64))
		month := time.Now().Format("2006-01")

		if _, err := b.subs.CreateOrSetTotal(ctx, uid, place, unit, month, total); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка сохранения абонемента")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		b.editTextAndClear(fromChat, cb.Message.MessageID, "Абонемент сохранён.")
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmSubsMenu, dialog.Payload{})
		b.showSubsMenu(fromChat, nil)
		_ = b.answerCallback(cb, "Готово", false)
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

		// Поставки
	case data == "sup:add":
		b.clearPrevStep(ctx, fromChat)

		_ = b.states.Set(ctx, fromChat, dialog.StateSupPickWh, dialog.Payload{})
		b.showSuppliesPickWarehouse(ctx, fromChat, &cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "sup:additem":
		b.clearPrevStep(ctx, fromChat)

		st, _ := b.states.Get(ctx, fromChat)
		_ = b.states.Set(ctx, fromChat, dialog.StateSupPickMat, st.Payload) // wh_id и items остаются
		b.showSuppliesPickMaterial(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "sup:wh:"):
		whID, _ := strconv.ParseInt(strings.TrimPrefix(data, "sup:wh:"), 10, 64)
		_ = b.states.Set(ctx, fromChat, dialog.StateSupPickMat, dialog.Payload{"wh_id": whID})
		b.showSuppliesPickMaterial(ctx, fromChat, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "sup:mat:"):
		matID, _ := strconv.ParseInt(strings.TrimPrefix(data, "sup:mat:"), 10, 64)
		st, _ := b.states.Get(ctx, fromChat)
		wh := int64(st.Payload["wh_id"].(float64))
		// ВАЖНО: переносим корзину, иначе она теряется
		payload := dialog.Payload{
			"wh_id":  wh,
			"mat_id": matID,
		}
		if items, ok := st.Payload["items"]; ok {
			payload["items"] = items
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateSupQty, payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество (число, например 250)")
		b.saveLastStep(ctx, fromChat, dialog.StateSupQty, payload, cb.Message.MessageID)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "sup:list":
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Журнал поставок: добавим позже (период/экспорт).")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "sup:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		wh := int64(st.Payload["wh_id"].(float64))
		items := b.parseSupItems(st.Payload["items"])
		if len(items) == 0 {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Корзина пуста. Добавьте хотя бы одну позицию.")
			_ = b.answerCallback(cb, "Пусто", true)
			return
		}
		u, err := b.users.GetByTelegramID(ctx, cb.From.ID)
		if err != nil || u == nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Пользователь не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		// Проводим каждую позицию одной транзакцией на позицию
		for _, it := range items {
			mat := int64(it["mat_id"].(float64))
			qty := int64(it["qty"].(float64))
			price := it["price"].(float64)
			if err := b.inventory.ReceiveWithCost(ctx, u.ID, wh, mat, float64(qty), price, "supply"); err != nil {
				b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка приёмки: "+err.Error())
				_ = b.answerCallback(cb, "Ошибка", true)
				return
			}
			// Обновим цену на последнюю закупочную
			_, _ = b.materials.UpdatePrice(ctx, mat, price)
		}

		// Очистим корзину и вернёмся в меню поставок
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Поставка проведена.")
		_ = b.states.Set(ctx, fromChat, dialog.StateSupMenu, dialog.Payload{})
		b.showSuppliesMenu(fromChat, nil)
		_ = b.answerCallback(cb, "Готово", false)
		return

		// Выбор помещения
	case strings.HasPrefix(data, "cons:place:"):
		place := strings.TrimPrefix(data, "cons:place:")
		unit := "hour"
		if place == "cabinet" {
			unit = "day"
		}
		st, _ := b.states.Get(ctx, fromChat)
		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}

		_ = b.states.Set(ctx, fromChat, dialog.StateConsQty, dialog.Payload{
			"place": place, "unit": unit, "with_sub": withSub,
		})
		b.editTextWithNav(fromChat, cb.Message.MessageID, fmt.Sprintf("Введите количество (%s):", map[string]string{"hour": "часы", "day": "дни"}[unit]))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Добавить материал
	case data == "cons:additem":
		st, _ := b.states.Get(ctx, fromChat)
		_ = b.states.Set(ctx, fromChat, dialog.StateConsMatPick, st.Payload)
		// список активных материалов
		mats, _ := b.materials.List(ctx, true)
		rows := [][]tgbotapi.InlineKeyboardButton{}
		for _, m := range mats {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(m.Name, fmt.Sprintf("cons:mat:%d", m.ID)),
			))
		}
		rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
		kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Выберите материал:", kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

	case strings.HasPrefix(data, "cons:mat:"):
		matID, _ := strconv.ParseInt(strings.TrimPrefix(data, "cons:mat:"), 10, 64)
		st, _ := b.states.Get(ctx, fromChat)
		st.Payload["mat_id"] = float64(matID)
		_ = b.states.Set(ctx, fromChat, dialog.StateConsMatQty, st.Payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите количество (целое, g/шт)")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:calc":
		st, _ := b.states.Get(ctx, fromChat)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		qty := int(st.Payload["qty"].(float64))
		items := b.consParseItems(st.Payload["items"])

		// 1) стоимость материалов
		var mats float64
		for _, it := range items {
			matID := int64(it["mat_id"].(float64))
			q := int64(it["qty"].(float64))
			price, _ := b.materials.GetPrice(ctx, matID)
			mats += float64(q) * price
		}

		// 2) авто-детект абонемента
		withSub := false
		var subLeft *int // для показа остатка
		subLimitForPricing := qty

		if u, _ := b.users.GetByTelegramID(ctx, cb.From.ID); u != nil {
			month := time.Now().Format("2006-01")
			if s, err := b.subs.GetActive(ctx, u.ID, place, unit, month); err == nil && s != nil {
				left := s.TotalQty - s.UsedQty
				if left >= qty {
					withSub = true
					subLeft = &left
					subLimitForPricing = s.TotalQty // для подбора ступени по общему объёму тарифа
				} else if left > 0 && left < qty {
					// Остатка не хватает на эту сессию → считаем БЕЗ абонемента, но покажем остаток в сводке.
					subLeft = &left
					withSub = false
					subLimitForPricing = qty
				}
			}
		}

		// 3) расчёт по ступеням через доменный метод
		rent, tariff, rounded, need, _, err := b.cons.ComputeRent(ctx, place, unit, withSub, qty, mats, subLimitForPricing)
		if err != nil {
			b.send(tgbotapi.NewMessage(fromChat,
				fmt.Sprintf("⚠️ Нет активных тарифов для: %s / %s (%s). Настройте тарифы.",
					map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
					map[string]string{"hour": "час", "day": "день"}[unit],
					map[bool]string{true: "с абонементом", false: "без абонемента"}[withSub],
				)))
			return
		}
		total := rent + mats

		// 4) сохраняем в payload
		st.Payload["with_sub"] = withSub
		st.Payload["mats_sum"] = mats
		st.Payload["mats_rounded"] = rounded
		st.Payload["need"] = need
		st.Payload["rent"] = rent
		st.Payload["total"] = total
		if subLeft != nil {
			st.Payload["sub_left"] = float64(*subLeft)
		} else {
			delete(st.Payload, "sub_left")
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateConsSummary, st.Payload)

		// 5) вывод сводки (покажем остаток, если есть)
		subBadge := ""
		if withSub {
			subBadge = " (с абонементом)"
		}
		subLine := ""
		if v, ok := st.Payload["sub_left"].(float64); ok {
			left := int(v)
			if withSub {
				subLine = fmt.Sprintf("\nОстаток абонемента: %d %s", left, map[string]string{"hour": "часов", "day": "дней"}[unit])
			} else if left > 0 {
				subLine = fmt.Sprintf("\nАбонемент: остаток %d %s (на эту сессию не хватает, расчёт без абонемента).",
					left, map[string]string{"hour": "часов", "day": "дней"}[unit])
			}
		}

		txt := fmt.Sprintf(
			"Сводка затрат для оплаты%s:\nПомещение: %s\nКол-во: %d %s\nМатериалы: %.2f ₽ (для порога учтено %.0f ₽; порог %.0f ₽)\nАренда: %.2f ₽ (%s)\nИтого к оплате: %.2f ₽%s",
			subBadge,
			map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
			qty, map[string]string{"hour": "ч", "day": "дн"}[unit],
			mats, rounded, need,
			rent, tariff, total, subLine,
		)

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "cons:confirm")),
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("✏️ Изменить", "cons:edit")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, txt, kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:edit":
		st, _ := b.states.Get(ctx, fromChat)
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		qty := int(st.Payload["qty"].(float64))
		items := b.consParseItems(st.Payload["items"])
		_ = b.states.Set(ctx, fromChat, dialog.StateConsCart, st.Payload)
		b.showConsCart(ctx, fromChat, &cb.Message.MessageID, place, unit, qty, items)
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "cons:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Status != "approved" {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Нет доступа")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		qty := int(st.Payload["qty"].(float64))
		items := b.consParseItems(st.Payload["items"])
		mats := st.Payload["mats_sum"].(float64)
		rounded := st.Payload["mats_rounded"].(float64)
		rent := st.Payload["rent"].(float64)
		total := st.Payload["total"].(float64)

		// найдём склад Расходники (только с него списываем)
		whID, err := b.getConsumablesWarehouseID(ctx)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Склад 'Расходники' не найден")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}

		// создаём сессию + позиции
		sid, err := b.cons.CreateSession(ctx, u.ID, place, unit, qty, withSub, mats, rounded, rent, total, map[string]any{
			"items_count": len(items),
		})
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Не удалось создать сессию")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		// Учёт абонемента: спишем использованное количество (часы/дни) за текущий месяц
		if withSub && b.subs != nil {
			month := time.Now().Format("2006-01")
			if s, err := b.subs.GetActive(ctx, u.ID, place, unit, month); err == nil && s != nil {
				_ = b.subs.AddUsage(ctx, s.ID, qty)
			} else {
				// просто подсветим админу, что абонемента нет
				if b.adminChat != 0 {
					b.send(tgbotapi.NewMessage(b.adminChat,
						fmt.Sprintf("ℹ️ У мастера id %d нет абонемента на %s/%s (%s), но сессия проведена с флагом абонемента.",
							u.ID, map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
							map[string]string{"hour": "час", "day": "день"}[unit], month)))
				}
			}
		}

		pairs := make([][2]int64, 0, len(items))
		// позиции + списание
		for _, it := range items {
			matID := int64(it["mat_id"].(float64))
			q := int64(it["qty"].(float64))
			price, _ := b.materials.GetPrice(ctx, matID)
			cost := float64(q) * price

			// списание (разрешено уходить в минус)
			if err := b.inventory.Consume(ctx, u.ID, whID, matID, float64(q), "consumption"); err != nil {
				b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка списания")
				_ = b.answerCallback(cb, "Ошибка", true)
				return
			}
			_ = b.cons.AddItem(ctx, sid, matID, float64(q), price, cost)
			pairs = append(pairs, [2]int64{whID, matID})
		}
		// инвойс (pending)
		_, _ = b.cons.CreateInvoice(ctx, u.ID, sid, total)

		b.notifyLowOrNegativeBatch(ctx, pairs)
		// уведомление админу о подтверждённой сессии расхода/аренды
		if b.adminChat != 0 {
			// кто подтвердил
			u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)

			// читаем данные из payload текущей сессии
			place := st.Payload["place"].(string)
			unit := st.Payload["unit"].(string)
			qtyI := int(st.Payload["qty"].(float64))
			items := b.consParseItems(st.Payload["items"])

			// соберём удобочитаемый текст
			placeRU := map[string]string{"hall": "Зал", "cabinet": "Кабинет"}
			unitRU := map[string]string{"hour": "ч", "day": "дн"}
			var sb strings.Builder

			_, _ = fmt.Fprintf(&sb, "✅ Подтверждена сессия расхода/аренды\n")
			if u != nil {
				_, _ = fmt.Fprintf(&sb, "Мастер: %s (@%s, id %d)\n", strings.TrimSpace(u.Username), cb.From.UserName, cb.From.ID)
			} else {
				_, _ = fmt.Fprintf(&sb, "Мастер: @%s (id %d)\n", cb.From.UserName, cb.From.ID)
			}
			_, _ = fmt.Fprintf(&sb, "Помещение: %s\nКол-во: %d %s\n", placeRU[place], qtyI, unitRU[unit])

			// материалы
			_, _ = fmt.Fprintf(&sb, "Материалы:\n")
			var matsSum float64
			for _, it := range items {
				matID := int64(it["mat_id"].(float64))
				q := int64(it["qty"].(float64))
				name := fmt.Sprintf("ID:%d", matID)
				if m, _ := b.materials.GetByID(ctx, matID); m != nil { // repo уже есть
					name = m.Name
				}
				price, _ := b.materials.GetPrice(ctx, matID)
				line := float64(q) * price
				matsSum += line
				_, _ = fmt.Fprintf(&sb, "• %s — %d × %.2f = %.2f ₽\n", name, q, price, line)
			}

			// финансы: округлённая сумма материалов, аренда, итого — у нас уже посчитаны
			rounded := st.Payload["mats_rounded"].(float64)
			rent := st.Payload["rent"].(float64)
			matsFact := st.Payload["mats_sum"].(float64)
			total := rent + matsFact
			_, _ = fmt.Fprintf(&sb, "\nМатериалы (факт): %.2f ₽, округл.: %.2f ₽\nАренда: %.2f ₽\nИтого: %.2f ₽",
				matsFact, rounded, rent, total)

			b.send(tgbotapi.NewMessage(b.adminChat, sb.String()))
		}

		b.editTextAndClear(fromChat, cb.Message.MessageID, "Сессия подтверждена. Списание материалов и расчёт завершены.")
		_ = b.states.Set(ctx, fromChat, dialog.StateIdle, dialog.Payload{})
		_ = b.answerCallback(cb, "Готово", false)
		return

		// Покупка абонемента — выбор места
	case strings.HasPrefix(data, "subbuy:place:"):
		place := strings.TrimPrefix(data, "subbuy:place:")
		unit := "hour"
		if place == "cabinet" {
			unit = "day"
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateSubBuyQty, dialog.Payload{
			"place": place, "unit": unit,
		})
		b.editTextWithNav(fromChat, cb.Message.MessageID,
			fmt.Sprintf("Введите объём (%s):", map[string]string{"hour": "часы", "day": "дни"}[unit]))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Покупка абонемента — подтверждение
	case data == "subbuy:confirm":
		st, _ := b.states.Get(ctx, fromChat)
		u, _ := b.users.GetByTelegramID(ctx, cb.From.ID)
		if u == nil || u.Status != users.StatusApproved || u.Role != users.RoleMaster {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Доступ запрещён.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		qty := int(st.Payload["qty"].(float64))
		month := time.Now().Format("2006-01")

		if _, err := b.subs.AddOrCreateTotal(ctx, u.ID, place, unit, month, qty); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Не удалось оформить абонемент.")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Абонемент оформлен/пополнен.")
		_ = b.states.Set(ctx, fromChat, dialog.StateIdle, dialog.Payload{})
		_ = b.answerCallback(cb, "Готово", false)
		return

	// Переключение place/unit
	case strings.HasPrefix(data, "rates:pu:"):
		parts := strings.Split(strings.TrimPrefix(data, "rates:pu:"), ":")
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		place, unit := parts[0], parts[1]
		st, _ := b.states.Get(ctx, fromChat)
		if st.Payload == nil {
			st.Payload = dialog.Payload{}
		}
		st.Payload["place"] = place
		st.Payload["unit"] = unit
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesPickPU, st.Payload)

		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}
		toggle := "Абонемент: выкл"
		if withSub {
			toggle = "Абонемент: вкл"
		}

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал / час", "rates:pu:hall:hour"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет / день", "rates:pu:cabinet:day"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(toggle, "rates:sub:tg"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📄 Показать ступени", "rates:list"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, "Установка тарифов — выберите набор параметров:", kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Тумблер абонемента
	case data == "rates:sub:tg":
		st, _ := b.states.Get(ctx, fromChat)
		cur := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			cur = v
		}
		st.Payload["with_sub"] = !cur
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesPickSub, st.Payload)

		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		toggle := "Абонемент: выкл"
		if !cur {
			toggle = "Абонемент: вкл"
		}

		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Общий зал / час", "rates:pu:hall:hour"),
				tgbotapi.NewInlineKeyboardButtonData("Кабинет / день", "rates:pu:cabinet:day"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(toggle, "rates:sub:tg"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📄 Показать ступени", "rates:list"),
			),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID,
			fmt.Sprintf("Установка тарифов — %s / %s", map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place], map[string]string{"hour": "час", "day": "день"}[unit]), kb))
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Показ списка ступеней
	case data == "rates:list":
		st, _ := b.states.Get(ctx, fromChat)
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}

		rates, err := b.cons.ListRates(ctx, place, unit, withSub)
		if err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка загрузки тарифов")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		lines := []string{
			fmt.Sprintf("Тарифы: %s / %s (%s)",
				map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
				map[string]string{"hour": "час", "day": "день"}[unit],
				map[bool]string{true: "с абонементом", false: "без абонемента"}[withSub],
			),
		}
		for _, r := range rates {
			maxTxt := "∞"
			if r.MaxQty != nil {
				maxTxt = fmt.Sprintf("%d", *r.MaxQty)
			}
			status := "🟢"
			if !r.Active {
				status = "🚫"
			}
			lines = append(lines,
				fmt.Sprintf("%s %d–%s: порог %.0f; с мат. %.2f; свои %.2f",
					status, r.MinQty, maxTxt, r.Threshold, r.PriceWith, r.PriceOwn),
			)
		}

		text := strings.Join(lines, "\n")
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Добавить ступень", "rates:add")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		b.send(tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, text, kb))
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesList, st.Payload)
		_ = b.answerCallback(cb, "Ок", false)
		return

		// Старт добавления ступени
	case data == "rates:add":
		st, _ := b.states.Get(ctx, fromChat) // <-- додали
		if st.Payload == nil {
			st.Payload = dialog.Payload{}
		}
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesCreateMin, st.Payload)
		b.editTextWithNav(fromChat, cb.Message.MessageID, "Введите минимальное значение диапазона (целое число, например 1)")
		_ = b.answerCallback(cb, "Ок", false)
		return

	case data == "rates:save":
		st, _ := b.states.Get(ctx, fromChat)
		place := st.Payload["place"].(string)
		unit := st.Payload["unit"].(string)
		withSub := false
		if v, ok := st.Payload["with_sub"].(bool); ok {
			withSub = v
		}
		minQty := int(st.Payload["min"].(float64))

		var maxPtr *int
		if st.Payload["max"] != nil {
			m := int(st.Payload["max"].(float64))
			maxPtr = &m
		}
		thr := st.Payload["thr"].(float64)
		pwith := st.Payload["pwith"].(float64)
		pown := st.Payload["pown"].(float64)

		if _, err := b.cons.CreateRate(ctx, place, unit, withSub, minQty, maxPtr, thr, pwith, pown); err != nil {
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Ошибка сохранения тарифной ступени")
			_ = b.answerCallback(cb, "Ошибка", true)
			return
		}

		b.editTextAndClear(fromChat, cb.Message.MessageID, "Ступень сохранена.")

		rates, _ := b.cons.ListRates(ctx, place, unit, withSub)
		lines := []string{"Обновлённый список:"}
		for _, r := range rates {
			maxTxt := "∞"
			if r.MaxQty != nil {
				maxTxt = fmt.Sprintf("%d", *r.MaxQty)
			}
			status := "🟢"
			if !r.Active {
				status = "🚫"
			}
			lines = append(lines,
				fmt.Sprintf("%s %d–%s: порог %.0f; с мат. %.2f; свои %.2f",
					status, r.MinQty, maxTxt, r.Threshold, r.PriceWith, r.PriceOwn),
			)
		}
		text := strings.Join(lines, "\n")
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Добавить ступень", "rates:add")),
			navKeyboard(true, true).InlineKeyboard[0],
		)
		m := tgbotapi.NewMessage(fromChat, text)
		m.ReplyMarkup = kb
		b.send(m)
		_ = b.states.Set(ctx, fromChat, dialog.StateAdmRatesList, st.Payload)
		_ = b.answerCallback(cb, "Сохранено", false)
		return
	}
}

func (b *Bot) answerCallback(cb *tgbotapi.CallbackQuery, text string, alert bool) error {
	resp := tgbotapi.NewCallback(cb.ID, text)
	resp.ShowAlert = alert
	_, err := b.api.Request(resp)
	return err
}
