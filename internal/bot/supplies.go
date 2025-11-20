package bot

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/catalog"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/xuri/excelize/v2"
)

func (b *Bot) showSuppliesMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬇️ Выгрузить материалы", "sup:export"),
			tgbotapi.NewInlineKeyboardButtonData("⬆️ Загрузить поступление", "sup:import"),
		),
		tgbotapi.NewInlineKeyboardRow(
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
	u, _ := b.users.GetByTelegramID(ctx, chatID)
	salonAdmin := u != nil && u.Status == users.StatusApproved && u.Role == users.RoleAdministrator

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, w := range ws {
		if !w.Active {
			continue
		}
		if salonAdmin && w.Type != catalog.WHTClientService {
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

func (b *Bot) showSuppliesExportPickWarehouse(ctx context.Context, chatID int64, editMsgID *int) {
	ws, err := b.catalog.ListWarehouses(ctx)
	if err != nil {
		if editMsgID != nil {
			b.editTextAndClear(chatID, *editMsgID, "Ошибка загрузки складов")
		} else {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка загрузки складов"))
		}
		return
	}
	u, _ := b.users.GetByTelegramID(ctx, chatID)
	salonAdmin := u != nil && u.Status == users.StatusApproved && u.Role == users.RoleAdministrator

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, w := range ws {
		if !w.Active {
			continue
		}
		if salonAdmin && w.Type != catalog.WHTClientService {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(w.Name, fmt.Sprintf("sup:expwh:%d", w.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])
	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

	text := "Выберите склад для выгрузки материалов:"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = kb
		b.send(msg)
	}
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

func (b *Bot) handleSuppliesImportExcel(ctx context.Context, chatID int64, u *users.User, data []byte) {
	// 1) открываем Excel из байтов
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		b.send(tgbotapi.NewMessage(chatID, "Не удалось прочитать Excel-файл (повреждён или не .xlsx)."))
		return
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(f.GetActiveSheetIndex())
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) < 2 {
		b.send(tgbotapi.NewMessage(chatID, "Файл не содержит данных (нет строк с материалами)."))
		return
	}

	// 2) проверим хотя бы первую строку заголовка по количеству колонок
	header := rows[0]
	if len(header) < 8 {
		b.send(tgbotapi.NewMessage(chatID, "Некорректный формат файла: ожидается минимум 8 колонок (warehouse_id ... Количество)."))
		return
	}

	var (
		totalRows     int
		totalQty      float64
		warehouseID   int64
		warehouseName string
	)

	// warehouse_id возьмём из первой строки данных (2-я строка файла)
	if len(rows[1]) >= 2 {
		whIDStr := strings.TrimSpace(rows[1][0])
		if whIDStr != "" {
			if id, err := strconv.ParseInt(whIDStr, 10, 64); err == nil {
				warehouseID = id
			}
		}
		if len(rows[1]) >= 2 {
			warehouseName = strings.TrimSpace(rows[1][1])
		}
	}

	// 3) если warehouseID не удалось вытащить — ругаемся
	if warehouseID == 0 {
		b.send(tgbotapi.NewMessage(chatID, "Не удалось определить склад (проверьте колонку warehouse_id в файле)."))
		return
	}

	// 4) проходим по всем строкам, начиная со 2-й (индекс 1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 8 {
			continue
		}
		matIDStr := strings.TrimSpace(row[4])
		qtyStr := strings.TrimSpace(row[7])

		if matIDStr == "" || qtyStr == "" {
			// пустая строка или количество не задано — пропускаем
			continue
		}

		matID, err := strconv.ParseInt(matIDStr, 10, 64)
		if err != nil {
			// сообщаем, в какой строке ошибка
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: некорректный material_id (%q). Исправьте файл и попробуйте снова.", i+1, matIDStr)))
			return
		}

		qty, err := strconv.ParseFloat(strings.ReplaceAll(qtyStr, ",", "."), 64)
		if err != nil || qty <= 0 {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: некорректное количество (%q). Используйте положительное число.", i+1, qtyStr)))
			return
		}

		// 5) приёмка на склад. Цена нам в файле не задана — ставим 0, это чисто количественная корректировка.
		if err := b.inventory.ReceiveWithCost(ctx, u.ID, warehouseID, matID, qty, 0, "supply_excel"); err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка приёмки в строке %d (материал %d): %v", i+1, matID, err)))
			return
		}

		totalRows++
		totalQty += qty
	}

	if warehouseName == "" {
		warehouseName = fmt.Sprintf("ID %d", warehouseID)
	}

	// 6) успех: возвращаем в меню поставок
	msg := fmt.Sprintf(
		"Поступление из файла проведено.\nСклад: %s\nСтрок обработано: %d\nВсего количества: %.2f",
		warehouseName, totalRows, totalQty,
	)
	b.send(tgbotapi.NewMessage(chatID, msg))

	_ = b.states.Set(ctx, chatID, dialog.StateSupMenu, dialog.Payload{})
	b.showSuppliesMenu(chatID, nil)
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
