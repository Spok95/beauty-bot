package bot

import (
	"context"
	"encoding/base64"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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

func (b *Bot) showMaterialWarehousePicker(chatID int64, editMsgID int) {
	ctx := context.Background()

	// тут используем тот же источник складов, что и в stocks/admin_warehouses
	warehouses, err := b.catalog.ListWarehouses(ctx)
	if err != nil || len(warehouses) == 0 {
		b.editTextAndClear(chatID, editMsgID, "Не удалось получить список складов")
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for _, wh := range warehouses {
		if !wh.Active { // если у тебя есть флаг Active
			continue
		}
		cb := fmt.Sprintf("adm:mat:wh:%d", wh.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(wh.Name, cb),
		))
	}

	// навигация Назад / Отменить
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := "Выберите склад, к которому будет привязан новый материал:"
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

func (b *Bot) showMaterialList(ctx context.Context, chatID int64, editMsgID int) {
	categories, err := b.catalog.ListCategories(ctx)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки категорий")
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}

	for _, c := range categories {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%s %s", badge(c.Active), c.Name),
				fmt.Sprintf("adm:mat:list:cat:%d", c.ID),
			),
		))
	}

	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Список материалов:\n\nВыберите категорию:", kb))
}

func (b *Bot) showMaterialBrandList(ctx context.Context, chatID int64, editMsgID int, catID int64) {
	categoryName := fmt.Sprintf("ID:%d", catID)
	if c, _ := b.catalog.GetCategoryByID(ctx, catID); c != nil {
		categoryName = c.Name
	}

	brands, err := b.materials.ListBrandsByCategory(ctx, catID)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки брендов")
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}

	for _, br := range brands {
		if br == "" {
			br = "Без бренда"
		}

		b64 := base64.StdEncoding.EncodeToString([]byte(br))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"🏷 "+br,
				fmt.Sprintf("adm:mat:list:brand:%d:%s", catID, b64),
			),
		))
	}

	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := fmt.Sprintf(
		"Список материалов:\n\nКатегория: %s\n\nВыберите бренд:",
		categoryName,
	)

	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

func (b *Bot) showMaterialListByBrand(ctx context.Context, chatID int64, editMsgID int, catID int64, brand string) {
	categoryName := fmt.Sprintf("ID:%d", catID)
	if c, _ := b.catalog.GetCategoryByID(ctx, catID); c != nil {
		categoryName = c.Name
	}

	materialsList, err := b.materials.ListByCategoryAndBrand(ctx, catID, brand)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки материалов")
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}

	for _, m := range materialsList {
		label := fmt.Sprintf("%s %s", badge(m.Active), materialDisplayName(m.Brand, m.Name))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("adm:mat:menu:%d", m.ID)),
		))
	}

	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := fmt.Sprintf(
		"Список материалов:\n\nКатегория: %s\nБренд: %s\n\nВыберите материал:",
		categoryName,
		brand,
	)

	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
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

	matName := materialDisplayName(m.Brand, m.Name)

	text := fmt.Sprintf(
		"Материал: %s %s\nКатегория: %s\nЕд.: %s\nСтатус: %v",
		badge(m.Active), matName, catName, m.Unit, m.Active,
	)

	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}

// showMaterialBrandPick показывает список брендов по категории + кнопку "Новый бренд".
func (b *Bot) showMaterialBrandPick(ctx context.Context, chatID int64, editMsgID int, catID int64) {
	brands, err := b.materials.ListBrandsByCategory(ctx, catID)
	if err != nil {
		b.editTextAndClear(chatID, editMsgID, "Ошибка загрузки брендов")
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}

	// существующие бренды
	for _, br := range brands {
		if br == "" {
			continue // пустой бренд из списка не показываем
		}
		b64 := base64.StdEncoding.EncodeToString([]byte(br))
		cbData := fmt.Sprintf("adm:mat:brand:%d:%s", catID, b64)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(br, cbData),
		))
	}

	// кнопка "Новый бренд"
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(
			"➕ Новый бренд",
			fmt.Sprintf("adm:mat:brand:new:%d", catID),
		),
	))

	// навигация Назад / Отменить
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	text := "Выберите бренд для материала (или создайте новый):"
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, text, kb))
}
