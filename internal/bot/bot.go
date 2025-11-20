package bot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
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
	"github.com/xuri/excelize/v2"
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
	if len(header) < 8 {
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
		if len(row) < 8 {
			continue
		}

		whIDStr := strings.TrimSpace(row[0])
		matIDStr := strings.TrimSpace(row[4])
		priceStr := strings.TrimSpace(row[7]) // price_per_unit

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
