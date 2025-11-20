package bot

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
