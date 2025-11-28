package bot

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/consumption"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/xuri/excelize/v2"
)

// главное меню "Установка цен"
func (b *Bot) showPriceMainMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Установить цены на материалы на складах", "price:mat:menu"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Установить новые тарифы на аренду", "price:rent:menu"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)

	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Установка цен — выберите действие", kb))
	} else {
		m := tgbotapi.NewMessage(chatID, "Установка цен — выберите действие")
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// выбор склада для выгрузки цен материалов
func (b *Bot) showPriceMatExportPickWarehouse(ctx context.Context, chatID int64, editMsgID *int) {
	ws, err := b.catalog.ListWarehouses(ctx)
	if err != nil {
		if editMsgID != nil {
			b.editTextAndClear(chatID, *editMsgID, "Ошибка загрузки складов")
		} else {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка загрузки складов"))
		}
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, w := range ws {
		if !w.Active {
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(w.Name, fmt.Sprintf("price:mat:expwh:%d", w.ID)),
		))
	}
	rows = append(rows, navKeyboard(false, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := "Выберите склад для выгрузки цен материалов:"

	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// меню для цен материалов на складах
func (b *Bot) showPriceMatMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬇️ Выгрузить цены на материалы", "price:mat:export"),
			tgbotapi.NewInlineKeyboardButtonData("⬆️ Загрузить цены на материалы", "price:mat:import"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)

	text := "Цены на материалы — выберите действие"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// меню для тарифов аренды
func (b *Bot) showPriceRentMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬇️ Выгрузить цены на аренду", "price:rent:export"),
			tgbotapi.NewInlineKeyboardButtonData("⬆️ Загрузить цены на аренду", "price:rent:import"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)

	text := "Тарифы аренды — выберите действие"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// exportWarehouseMaterialPricesExcel выгружает в Excel цены материалов склада.
func (b *Bot) exportWarehouseMaterialPricesExcel(ctx context.Context, chatID int64, msgID int, whID int64) {
	// 1) склад
	wh, err := b.catalog.GetWarehouseByID(ctx, whID)
	if err != nil || wh == nil {
		b.editTextAndClear(chatID, msgID, "Склад не найден")
		return
	}

	// 2) материалы по складу
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

	header := []interface{}{
		"warehouse_id",
		"warehouse_name",
		"category_id",
		"category_name",
		"brand",
		"material_id",
		"material_name",
		"unit",
		"price_per_unit",
	}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (заголовок)")
		return
	}

	row := 2
	for _, it := range items {
		catName := catNames[it.CategoryID]

		price, _ := b.materials.GetPrice(ctx, it.ID)

		excelRow := []interface{}{
			wh.ID,
			wh.Name,
			it.CategoryID,
			catName,
			it.Brand,
			it.ID,
			it.Name,
			string(it.Unit),
			price,
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

	buf := &bytes.Buffer{}
	if err := f.Write(buf); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка записи файла")
		return
	}

	fileName := fmt.Sprintf("prices_%s_%s.xlsx",
		wh.Name,
		time.Now().Format("20060102_150405"),
	)

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: buf.Bytes(),
	})
	doc.Caption = fmt.Sprintf(
		"Цены материалов склада «%s».\nПри необходимости измените колонку price_per_unit и загрузите файл через «Загрузить цены на материалы».",
		wh.Name,
	)

	b.send(doc)

	b.editTextWithNav(chatID, msgID,
		fmt.Sprintf("Сформирован файл с ценами для склада «%s».", wh.Name))
}

// exportRentRatesExcel выгружает тарифы аренды в Excel.
func (b *Bot) exportRentRatesExcel(ctx context.Context, chatID int64, msgID int) {
	rates, err := b.cons.ListRentRates(ctx)
	if err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка загрузки тарифов аренды")
		return
	}
	if len(rates) == 0 {
		b.editTextAndClear(chatID, msgID, "Тарифы аренды не найдены")
		return
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(f.GetActiveSheetIndex())

	header := []interface{}{
		"id",
		"place",
		"unit",
		"with_subscription",
		"min_qty",
		"threshold_materials",
		"price_with_materials",
		"price_own_materials",
	}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка формирования файла (заголовок)")
		return
	}

	row := 2
	for _, rrate := range rates {
		excelRow := []interface{}{
			rrate.ID,
			rrate.Place,
			rrate.Unit,
			map[bool]string{true: "yes", false: "no"}[rrate.WithSub],
			rrate.MinQty,
			rrate.Threshold,
			rrate.PriceWith,
			rrate.PriceOwn,
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

	buf := &bytes.Buffer{}
	if err := f.Write(buf); err != nil {
		b.editTextAndClear(chatID, msgID, "Ошибка записи файла")
		return
	}

	fileName := fmt.Sprintf("rent_rates_%s.xlsx", time.Now().Format("20060102_150405"))

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: buf.Bytes(),
	})
	doc.Caption = "Тарифы аренды. Измените при необходимости threshold_materials / price_with_materials / price_own_materials и загрузите файл обратно через «Загрузить цены на аренду»."

	b.send(doc)
	b.editTextWithNav(chatID, msgID, "Сформирован файл с тарифами аренды.")
}

// handlePriceRentImportExcel читает Excel-файл с тарифами аренды и
// обновляет threshold/price_with/price_own по id.
// Пустая ячейка => значение не меняем.
func (b *Bot) handlePriceRentImportExcel(ctx context.Context, chatID int64, data []byte) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		b.send(tgbotapi.NewMessage(chatID, "Не удалось прочитать Excel-файл (повреждён или не .xlsx)."))
		return
	}
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(f.GetActiveSheetIndex())
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) < 2 {
		b.send(tgbotapi.NewMessage(chatID, "Файл не содержит данных (нет строк с тарифами)."))
		return
	}

	header := rows[0]
	if len(header) < 8 {
		b.send(tgbotapi.NewMessage(chatID, "Некорректный формат файла: ожидается минимум 8 колонок (id ... price_own_materials)."))
		return
	}

	var (
		totalRows    int
		updatedCount int
	)

	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 8 {
			continue
		}

		idStr := strings.TrimSpace(row[0])
		thrStr := strings.TrimSpace(row[5]) // threshold_materials
		pwStr := strings.TrimSpace(row[6])  // price_with_materials
		poStr := strings.TrimSpace(row[7])  // price_own_materials

		if idStr == "" {
			continue
		}

		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: некорректный id тарифа (%q).", i+1, idStr)))
			return
		}

		var (
			thrPtr *float64
			pwPtr  *float64
			poPtr  *float64
		)

		if thrStr != "" {
			v, err := strconv.ParseFloat(strings.ReplaceAll(thrStr, ",", "."), 64)
			if err != nil || v < 0 {
				b.send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Ошибка в строке %d: некорректный threshold_materials (%q). Используйте неотрицательное число.", i+1, thrStr)))
				return
			}
			thrPtr = &v
		}
		if pwStr != "" {
			v, err := strconv.ParseFloat(strings.ReplaceAll(pwStr, ",", "."), 64)
			if err != nil || v < 0 {
				b.send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Ошибка в строке %d: некорректный price_with_materials (%q). Используйте неотрицательное число.", i+1, pwStr)))
				return
			}
			pwPtr = &v
		}
		if poStr != "" {
			v, err := strconv.ParseFloat(strings.ReplaceAll(poStr, ",", "."), 64)
			if err != nil || v < 0 {
				b.send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("Ошибка в строке %d: некорректный price_own_materials (%q). Используйте неотрицательное число.", i+1, poStr)))
				return
			}
			poPtr = &v
		}

		// Если все три поля пустые — вообще ничего не делаем
		if thrPtr == nil && pwPtr == nil && poPtr == nil {
			totalRows++
			continue
		}

		if err := b.cons.UpdateRentRatePartial(ctx, id, thrPtr, pwPtr, poPtr); err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка обновления тарифа в строке %d (id=%d): %v", i+1, id, err)))
			return
		}

		totalRows++
		updatedCount++
	}

	msg := fmt.Sprintf(
		"Тарифы аренды обновлены из файла.\nСтрок обработано: %d\nТарифов с изменёнными значениями: %d",
		totalRows, updatedCount,
	)
	b.send(tgbotapi.NewMessage(chatID, msg))

	_ = b.states.Set(ctx, chatID, dialog.StatePriceRentMenu, dialog.Payload{})
	b.showPriceRentMenu(chatID, nil)
}

// handlePriceMatImportExcel читает Excel-файл с ценами материалов и
// обновляет price_per_unit для указанных материалов.
// Пустая ячейка price_per_unit означает "оставить старую цену".
func (b *Bot) handlePriceMatImportExcel(ctx context.Context, chatID int64, data []byte) {
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
	if len(header) < 9 {
		b.send(tgbotapi.NewMessage(chatID, "Некорректный формат файла: ожидается минимум 8 колонок (warehouse_id ... price_per_unit)."))
		return
	}

	var (
		totalRows     int
		updatedCount  int
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
		if len(row) < 9 {
			continue
		}

		whIDStr := strings.TrimSpace(row[0])
		matIDStr := strings.TrimSpace(row[4])
		priceStr := strings.TrimSpace(row[8]) // price_per_unit

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

		if priceStr == "" {
			// пустая ячейка — оставляем старую цену
			totalRows++
			continue
		}

		price, err := strconv.ParseFloat(strings.ReplaceAll(priceStr, ",", "."), 64)
		if err != nil || price < 0 {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка в строке %d: некорректный price_per_unit (%q). Используйте неотрицательное число.", i+1, priceStr)))
			return
		}

		if _, err := b.materials.UpdatePrice(ctx, matID, price); err != nil {
			b.send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("Ошибка обновления цены в строке %d (материал %d): %v", i+1, matID, err)))
			return
		}

		totalRows++
		updatedCount++
	}

	msg := fmt.Sprintf(
		"Цены материалов склада «%s» обновлены из файла.\nСтрок обработано: %d\nМатериалов с обновлённой ценой: %d",
		warehouseName, totalRows, updatedCount,
	)
	b.send(tgbotapi.NewMessage(chatID, msg))

	_ = b.states.Set(ctx, chatID, dialog.StatePriceMatMenu, dialog.Payload{})
	b.showPriceMatMenu(chatID, nil)
}

// handleAdmRentMaterialsReport формирует Excel-файл "Аренда и Расходы материалов по мастерам"
// за период [from; toExclusive] и отправляет администратору.
func (b *Bot) handleAdmRentMaterialsReport(
	ctx context.Context,
	chatID int64,
	from, toExclusive time.Time,
) error {
	rows, err := b.cons.ListMasterMaterialsReport(ctx, from, toExclusive)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		msg := tgbotapi.NewMessage(chatID, "За указанный период нет данных по аренде и расходу материалов.")
		b.send(msg)
		return nil
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// Группируем по мастеру
	type sessionKey struct {
		SessionID int64
		Place     string
		Unit      string
	}
	type usageKey struct {
		Place string
		Unit  string
	}
	type masterData struct {
		Rows     []consumption.MasterMaterialsReportRow
		Sessions map[sessionKey]struct{}
		ByUsage  map[usageKey]int // суммарное количество часов/дней по place/unit
		Username string
	}
	masters := make(map[int64]*masterData)

	for _, r := range rows {
		md, ok := masters[r.UserID]
		if !ok {
			md = &masterData{
				Rows:     make([]consumption.MasterMaterialsReportRow, 0),
				Sessions: make(map[sessionKey]struct{}),
				ByUsage:  make(map[usageKey]int),
				Username: r.Username,
			}
			masters[r.UserID] = md
		}
		md.Rows = append(md.Rows, r)

		// учёт аренды: считаем сессию один раз
		sk := sessionKey{SessionID: r.SessionID, Place: r.Place, Unit: r.Unit}
		if _, exists := md.Sessions[sk]; !exists {
			md.Sessions[sk] = struct{}{}
			uk := usageKey{Place: r.Place, Unit: r.Unit}
			md.ByUsage[uk] += r.Qty
		}
	}

	// Удалим дефолтный лист
	defaultSheet := f.GetSheetName(f.GetActiveSheetIndex())
	if defaultSheet != "" {
		_ = f.DeleteSheet(defaultSheet)
	}

	// Для каждого мастера свой лист
	for userID, md := range masters {
		sheetName := fmt.Sprintf("user_%d", userID)
		if len(md.Username) > 0 {
			// чуть более человеко-читаемое имя (но не больше 31 символа, иначе Excel ругается)
			base := md.Username
			if len(base) > 20 {
				base = base[:20]
			}
			sheetName = fmt.Sprintf("%s_%d", base, userID)
		}
		if len(sheetName) > 31 {
			sheetName = sheetName[:31]
		}

		_, err := f.NewSheet(sheetName)
		if err != nil {
			// если какое-то имя не зашло — fallback
			sheetName = fmt.Sprintf("user_%d", userID)
			_, _ = f.NewSheet(sheetName)
		}

		rowIdx := 1

		// Заголовок: информация по мастеру и периоду
		header := fmt.Sprintf("Отчёт по мастеру %s за период %s — %s",
			strings.TrimSpace(md.Username),
			from.Format("02.01.2006"),
			toExclusive.Add(-24*time.Hour).Format("02.01.2006"),
		)
		if err := f.SetCellValue(sheetName, "A1", header); err != nil {
			return err
		}
		if err := f.MergeCell(sheetName, "A1", "F1"); err != nil {
			return err
		}
		rowIdx += 2

		// Статистика по аренде: часы/дни по помещению
		_ = f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowIdx), "Помещение")
		_ = f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowIdx), "Ед.")
		_ = f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowIdx), "Кол-во")
		rowIdx++

		for uk, qty := range md.ByUsage {
			var placeRU string
			switch uk.Place {
			case "hall":
				placeRU = "Зал"
			case "cabinet":
				placeRU = "Кабинет"
			default:
				placeRU = uk.Place
			}
			var unitRU string
			switch uk.Unit {
			case "hour":
				unitRU = "часы"
			case "day":
				unitRU = "дни"
			default:
				unitRU = uk.Unit
			}
			_ = f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowIdx), placeRU)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowIdx), unitRU)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowIdx), qty)
			rowIdx++
		}

		rowIdx += 2

		// Таблица с материалами
		_ = f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowIdx), "Дата")
		_ = f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowIdx), "Материал")
		_ = f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowIdx), "Ед.")
		_ = f.SetCellValue(sheetName, fmt.Sprintf("D%d", rowIdx), "Кол-во")
		_ = f.SetCellValue(sheetName, fmt.Sprintf("E%d", rowIdx), "Цена за ед.")
		_ = f.SetCellValue(sheetName, fmt.Sprintf("F%d", rowIdx), "Сумма")
		rowIdx++

		for _, r := range md.Rows {
			dateStr := r.CreatedAt.Format("02.01.2006 15:04")

			_ = f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowIdx), dateStr)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowIdx), r.MaterialName)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowIdx), r.MaterialUnit)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("D%d", rowIdx), r.MaterialQty)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("E%d", rowIdx), r.UnitPrice)
			_ = f.SetCellValue(sheetName, fmt.Sprintf("F%d", rowIdx), r.Cost)
			rowIdx++
		}
	}

	// активный лист — первый созданный
	if sheets := f.GetSheetList(); len(sheets) > 0 {
		if idx, err := f.GetSheetIndex(sheets[0]); err == nil {
			f.SetActiveSheet(idx)
		}
	}

	filename := fmt.Sprintf("rent_materials_%s_%s.xlsx",
		from.Format("20060102"),
		toExclusive.Add(-24*time.Hour).Format("20060102"),
	)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return err
	}

	doc := tgbotapi.FileBytes{
		Name:  filename,
		Bytes: buf.Bytes(),
	}
	msg := tgbotapi.NewDocument(chatID, doc)
	msg.Caption = "Отчёт по аренде и расходам материалов по мастерам"

	b.send(msg)
	return nil
}
