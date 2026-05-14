package bot

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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

func (b *Bot) showSuppliesJournalList(
	ctx context.Context,
	chatID int64,
	editMsgID *int,
	from, to time.Time,
) {
	list, err := b.inventory.ListSuppliesByPeriod(ctx, from, to)
	if err != nil {
		text := "Ошибка загрузки журнала поставок."
		if editMsgID != nil {
			b.editTextAndClear(chatID, *editMsgID, text)
		} else {
			b.send(tgbotapi.NewMessage(chatID, text))
		}
		return
	}
	if len(list) == 0 {
		text := "За выбранный период поставок не найдено."
		if editMsgID != nil {
			b.editTextAndClear(chatID, *editMsgID, text)
		} else {
			b.send(tgbotapi.NewMessage(chatID, text))
		}
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, s := range list {
		label := s.CreatedAt.Format("02.01.2006 15:04")
		if strings.TrimSpace(s.Comment) != "" {
			label = fmt.Sprintf("%s, %s", label, s.Comment)
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("sup:journal:%d", s.ID)),
		))
	}
	rows = append(rows, navKeyboard(false, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := fmt.Sprintf("Журнал поставок\nПериод: %s — %s (включительно)",
		from.Format("02.01.2006"),
		to.AddDate(0, 0, -1).Format("02.01.2006"),
	)

	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = kb
		b.send(msg)
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

func (b *Bot) showSuppliesPickMaterial(
	ctx context.Context,
	chatID int64,
	editMsgID int,
	page int,
) {
	st, _ := b.states.Get(ctx, chatID)

	whRaw, ok := st.Payload["wh_id"]
	if !ok {
		b.editTextAndClear(chatID, editMsgID, "Склад не выбран")
		return
	}

	whID := int64(whRaw.(float64))

	mats, err := b.materials.ListWithBalanceByWarehouse(ctx, whID)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки материалов")
		return
	}

	if len(mats) == 0 {
		b.editTextAndClear(chatID, editMsgID, "Для выбранного склада материалы не найдены.")
		return
	}

	const perPage = 10

	totalPages := (len(mats) + perPage - 1) / perPage

	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * perPage
	end := start + perPage

	if end > len(mats) {
		end = len(mats)
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}

	for _, m := range mats[start:end] {
		rows = append(rows,
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					materialDisplayName(m.Brand, m.Name),
					fmt.Sprintf("sup:mat:%d", m.ID),
				),
			),
		)
	}

	navRow := []tgbotapi.InlineKeyboardButton{}

	if page > 0 {
		navRow = append(navRow,
			tgbotapi.NewInlineKeyboardButtonData(
				"⬅️",
				fmt.Sprintf("sup:mats:%d", page-1),
			),
		)
	}

	navRow = append(navRow,
		tgbotapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("%d/%d", page+1, totalPages),
			"noop",
		),
	)

	if page < totalPages-1 {
		navRow = append(navRow,
			tgbotapi.NewInlineKeyboardButtonData(
				"➡️",
				fmt.Sprintf("sup:mats:%d", page+1),
			),
		)
	}

	rows = append(rows, navRow)

	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"⬅️ К складам",
				"sup:import",
			),
			tgbotapi.NewInlineKeyboardButtonData(
				"✖️ Отменить",
				"nav:cancel",
			),
		),
	)

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

	text := fmt.Sprintf(
		"Выберите материал\nСтраница %d из %d",
		page+1,
		totalPages,
	)

	b.send(
		tgbotapi.NewEditMessageTextAndMarkup(
			chatID,
			editMsgID,
			text,
			kb,
		),
	)
}

func (b *Bot) handleSuppliesImportExcel(ctx context.Context, chatID int64, u *users.User, data []byte, comment string) {
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
	if len(header) < 9 {
		b.send(tgbotapi.NewMessage(chatID, "Некорректный формат файла: ожидается минимум 9 колонок (warehouse_id ... Количество)."))
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

	// 4) создаём batch для всей поставки из файла
	batchID, err := b.inventory.CreateSupplyBatch(ctx, u.ID, warehouseID, comment)
	if err != nil {
		b.send(tgbotapi.NewMessage(chatID, "Не удалось создать запись поставки (batch)."))
		return
	}

	// 5) проходим по всем строкам, начиная со 2-й (индекс 1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 9 {
			continue
		}
		matIDStr := strings.TrimSpace(row[5])
		qtyStr := strings.TrimSpace(row[8])

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

		allowed, err := b.materials.IsMaterialAllowedInWarehouse(ctx, warehouseID, matID)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка проверки материала в строке %d.", i+1)))
			return
		}
		if !allowed {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: материал %d не относится к категориям склада %d.", i+1, matID, warehouseID)))
			return
		}

		qty, err := strconv.ParseFloat(strings.ReplaceAll(qtyStr, ",", "."), 64)
		if err != nil || qty <= 0 {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: некорректное количество (%q). Используйте положительное число.", i+1, qtyStr)))
			return
		}

		// 5) приёмка на склад. Цена нам в файле не задана — ставим 0, это чисто количественная корректировка.
		note := "supply_excel"
		if comment != "" {
			note = fmt.Sprintf("supply_excel: %s", comment)
		}
		if err := b.inventory.ReceiveWithCost(ctx, u.ID, warehouseID, matID, qty, 0, note, comment, batchID); err != nil {
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

func (b *Bot) exportSupplyExcel(ctx context.Context, chatID int64, msgID int, supplyID int64) {
	items, err := b.inventory.GetSupplyDetails(ctx, supplyID)
	if err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка загрузки данных поставки.")
		return
	}
	if len(items) == 0 {
		b.editTextAndClear(chatID, msgID, "Поставка не найдена.")
		return
	}

	first := items[0]

	// Заголовок/подпись файла
	title := "Поставка"
	if c := strings.TrimSpace(first.Comment); c != "" {
		title = fmt.Sprintf("Поставка %s", c)
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(f.GetActiveSheetIndex())

	// --- Шапка ---

	supplier := strings.TrimSpace(first.Comment)
	if supplier == "" {
		supplier = "не указан"
	}

	// 1: Поставщик
	if err := f.SetCellValue(sheet, "A1",
		fmt.Sprintf("Поставщик: %s", supplier)); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (шапка).")
		return
	}
	// 2: Склад
	if err := f.SetCellValue(sheet, "A2",
		fmt.Sprintf("Склад: %s", first.WarehouseName)); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (шапка).")
		return
	}
	// 3: Дата+время
	if err := f.SetCellValue(sheet, "A3",
		fmt.Sprintf("Дата: %s", first.CreatedAt.Format("02.01.2006 15:04"))); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (шапка).")
		return
	}

	// размазываем шапку на A..D
	if err := f.MergeCell(sheet, "A1", "D1"); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (merge A1:D1).")
		return
	}
	if err := f.MergeCell(sheet, "A2", "D2"); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (merge A2:D2).")
		return
	}
	if err := f.MergeCell(sheet, "A3", "D3"); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (merge A3:D3).")
		return
	}

	// --- Таблица ---

	// заголовок таблицы (строка 5)
	headerRow := []interface{}{"Категория", "Бренд", "Материал", "Количество"}
	if err := f.SetSheetRow(sheet, "A5", &headerRow); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (заголовок таблицы).")
		return
	}

	// данные с 6-й строки
	rowIdx := 6
	for _, it := range items {
		row := []interface{}{
			it.CategoryName,
			it.BrandName,
			it.MaterialName,
			it.Qty,
		}
		cell := fmt.Sprintf("A%d", rowIdx)
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (данные).")
			return
		}
		rowIdx++
	}

	// --- Запись и отправка ---

	buf := &bytes.Buffer{}
	if err := f.Write(buf); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка записи файла.")
		return
	}

	fileName := fmt.Sprintf("supply_%d_%s.xlsx",
		supplyID,
		time.Now().Format("20060102_150405"),
	)

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: buf.Bytes(),
	})
	doc.Caption = title

	b.send(doc)
	b.editTextWithNav(chatID, msgID,
		fmt.Sprintf("Выгружена поставка №%d.", supplyID))
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
			if m.Brand != "" {
				name = fmt.Sprintf("%s / %s", m.Brand, m.Name)
			} else {
				name = m.Name
			}
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
