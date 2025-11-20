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
	"github.com/Spok95/beauty-bot/internal/domain/materials"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/xuri/excelize/v2"
)

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

func (b *Bot) showStocksMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬇️ Выгрузить остатки", "stock:export"),
			tgbotapi.NewInlineKeyboardButtonData("⬆️ Загрузить остатки", "stock:import"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)

	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Остатки — выберите действие", kb))
	} else {
		m := tgbotapi.NewMessage(chatID, "Остатки — выберите действие")
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// выбор склада для выгрузки остатков
func (b *Bot) showStockExportPickWarehouse(ctx context.Context, chatID int64, editMsgID *int) {
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
	administrator := u != nil && u.Status == users.StatusApproved && u.Role == users.RoleAdministrator

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, w := range ws {
		if !w.Active {
			continue
		}
		if administrator && w.Type != catalog.WHTClientService {
			// администратор салона видит только клиентский склад
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(w.Name, fmt.Sprintf("stock:expwh:%d", w.ID)),
		))
	}
	rows = append(rows, navKeyboard(false, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

	text := "Выберите склад для выгрузки остатков:"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
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

func (b *Bot) exportWarehouseMaterialsExcel(ctx context.Context, chatID int64, msgID int, whID int64) {
	// 1) склад
	wh, err := b.catalog.GetWarehouseByID(ctx, whID)
	if err != nil || wh == nil {
		b.editTextAndClear(chatID, msgID, "Склад не найден")
		return
	}

	// 2) материалы с балансами по складу
	mats, err := b.materials.ListWithBalanceByWarehouse(ctx, whID)
	if err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка загрузки материалов")
		return
	}
	if len(mats) == 0 {
		b.editTextAndClear(chatID, msgID, "На этом складе нет материалов")
		return
	}

	// 3) категории
	cats, err := b.catalog.ListCategories(ctx)
	if err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка загрузки категорий")
		return
	}
	catNames := make(map[int64]string, len(cats))
	for _, c := range cats {
		catNames[c.ID] = c.Name
	}

	// 4) Excel
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(f.GetActiveSheetIndex())

	// Заголовок
	header := []interface{}{
		"warehouse_id",
		"warehouse_name",
		"category_id",
		"category_name",
		"material_id",
		"material_name",
		"unit",
		"Количество", // эту колонку админ будет заполнять сам
	}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (заголовок)")
		return
	}

	// Данные
	row := 2
	for _, m := range mats {
		catName := catNames[m.CategoryID]
		excelRow := []interface{}{
			wh.ID,
			wh.Name,
			m.CategoryID,
			catName,
			m.ID,
			m.Name,
			string(m.Unit),
			"", // Количество — пусто
		}
		cell, err := excelize.CoordinatesToCellName(1, row)
		if err != nil {
			b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (ячейки)")
			return
		}
		if err := f.SetSheetRow(sheet, cell, &excelRow); err != nil {
			b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (строки)")
			return
		}
		row++
	}

	// 5) Пишем в буфер
	buf := &bytes.Buffer{}
	if err := f.Write(buf); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка записи файла")
		return
	}

	// 6) Отправляем документ в Telegram
	fileName := fmt.Sprintf("materials_%s_%s.xlsx",
		wh.Name,
		time.Now().Format("20060102_150405"),
	)

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: buf.Bytes(),
	})
	doc.Caption = fmt.Sprintf(
		"Материалы склада «%s».\nЗаполните колонку «Количество» и загрузите файл через кнопку «Загрузить поступление».",
		wh.Name,
	)

	b.send(doc)

	// Обновим текст исходного сообщения
	b.editTextWithNav(chatID, msgID,
		fmt.Sprintf("Сформирован файл с материалами для склада «%s».", wh.Name))
}

// handleStocksImportExcel читает Excel-файл с остатками и
// подгоняет фактический остаток под qty из файла:
// если qty > текущего остатка — делаем приход,
// если qty < текущего остатка — списываем разницу.
func (b *Bot) handleStocksImportExcel(ctx context.Context, chatID int64, u *users.User, data []byte) {
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

	header := rows[0]
	if len(header) < 8 {
		b.send(tgbotapi.NewMessage(chatID, "Некорректный формат файла: ожидается минимум 8 колонок (warehouse_id ... qty)."))
		return
	}

	var (
		totalRows     int
		totalIn       float64
		totalOut      float64
		warehouseID   int64
		warehouseName string
	)

	if len(rows[1]) >= 2 {
		whIDStr := strings.TrimSpace(rows[1][0])
		if whIDStr != "" {
			if id, err := strconv.ParseInt(whIDStr, 10, 64); err == nil {
				warehouseID = id
			}
		}
		warehouseName = strings.TrimSpace(rows[1][1])
	}

	if warehouseID == 0 {
		b.send(tgbotapi.NewMessage(chatID, "Не удалось определить склад (проверьте колонку warehouse_id в файле)."))
		return
	}
	if warehouseName == "" {
		warehouseName = fmt.Sprintf("ID %d", warehouseID)
	}

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 8 {
			continue
		}

		whIDStr := strings.TrimSpace(row[0])
		matIDStr := strings.TrimSpace(row[4])
		qtyStr := strings.TrimSpace(row[7]) // qty

		if whIDStr == "" || matIDStr == "" {
			continue
		}

		whID, err := strconv.ParseInt(whIDStr, 10, 64)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: некорректный warehouse_id (%q).", i+1, whIDStr)))
			return
		}
		if whID != warehouseID {
			// для простоты считаем, что файл только по одному складу
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: в файле обнаружен другой склад (warehouse_id %d).", i+1, whID)))
			return
		}

		matID, err := strconv.ParseInt(matIDStr, 10, 64)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: некорректный material_id (%q).", i+1, matIDStr)))
			return
		}

		var newQty float64
		if qtyStr == "" {
			// по ТЗ: пусто = 0
			newQty = 0
		} else {
			newQty, err = strconv.ParseFloat(strings.ReplaceAll(qtyStr, ",", "."), 64)
			if err != nil || newQty < 0 {
				b.send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Ошибка в строке %d: некорректное qty (%q). Используйте неотрицательное число.", i+1, qtyStr)))
				return
			}
		}

		curQty, err := b.materials.GetBalance(ctx, whID, matID)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка получения остатка в строке %d (материал %d): %v", i+1, matID, err)))
			return
		}

		delta := newQty - curQty
		if delta > 0 {
			// нужно добавить до фактического остатка
			if err := b.inventory.Receive(ctx, u.ID, whID, matID, delta, "inventory_excel"); err != nil {
				b.send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Ошибка прихода в строке %d (материал %d): %v", i+1, matID, err)))
				return
			}
			totalIn += delta
		} else if delta < 0 {
			// нужно списать лишнее
			if err := b.inventory.WriteOff(ctx, u.ID, whID, matID, -delta, "inventory_excel"); err != nil {
				b.send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Ошибка списания в строке %d (материал %d): %v", i+1, matID, err)))
				return
			}
			totalOut += -delta
		}
		totalRows++
	}

	msg := fmt.Sprintf(
		"Остатки по складу «%s» обновлены из файла.\nСтрок обработано: %d\nПриход всего: %.3f\nСписание всего: %.3f",
		warehouseName, totalRows, totalIn, totalOut,
	)
	b.send(tgbotapi.NewMessage(chatID, msg))

	_ = b.states.Set(ctx, chatID, dialog.StateStockMenu, dialog.Payload{})
	b.showStocksMenu(chatID, nil)
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

// exportWarehouseStocksExcel выгружает текущие остатки склада в Excel.
func (b *Bot) exportWarehouseStocksExcel(ctx context.Context, chatID int64, msgID int, whID int64) {
	// 1) склад
	wh, err := b.catalog.GetWarehouseByID(ctx, whID)
	if err != nil || wh == nil {
		b.editTextAndClear(chatID, msgID, "Склад не найден")
		return
	}

	// 2) материалы с балансами
	items, err := b.materials.ListWithBalanceByWarehouse(ctx, whID)
	if err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка загрузки материалов")
		return
	}
	if len(items) == 0 {
		b.editTextAndClear(chatID, msgID, "На этом складе нет материалов")
		return
	}

	// 3) категории
	cats, err := b.catalog.ListCategories(ctx)
	if err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка загрузки категорий")
		return
	}
	catNames := make(map[int64]string, len(cats))
	for _, c := range cats {
		catNames[c.ID] = c.Name
	}

	// 4) Excel
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(f.GetActiveSheetIndex())

	// заголовок
	header := []interface{}{
		"warehouse_id",
		"warehouse_name",
		"category_id",
		"category_name",
		"material_id",
		"material_name",
		"unit",
		"qty", // текущий остаток; админ может изменить на фактический
	}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (заголовок)")
		return
	}

	// строки
	row := 2
	for _, it := range items {
		catName := catNames[it.CategoryID]
		excelRow := []interface{}{
			wh.ID,
			wh.Name,
			it.CategoryID,
			catName,
			it.ID,
			it.Name,
			string(it.Unit),
			it.Balance, // текущий остаток
		}
		cell, err := excelize.CoordinatesToCellName(1, row)
		if err != nil {
			b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (ячейки)")
			return
		}
		if err := f.SetSheetRow(sheet, cell, &excelRow); err != nil {
			b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (строки)")
			return
		}
		row++
	}

	// 5) в буфер
	buf := &bytes.Buffer{}
	if err := f.Write(buf); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка записи файла")
		return
	}

	// 6) отправка в Telegram
	fileName := fmt.Sprintf("stocks_%s_%s.xlsx",
		wh.Name,
		time.Now().Format("20060102_150405"),
	)

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: buf.Bytes(),
	})
	doc.Caption = fmt.Sprintf(
		"Остатки склада «%s».\nПри необходимости измените колонку qty и загрузите файл через «Загрузить остатки».",
		wh.Name,
	)

	b.send(doc)

	b.editTextWithNav(chatID, msgID,
		fmt.Sprintf("Сформирован файл с остатками для склада «%s».", wh.Name))
}
