package bot

import (
	"context"
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
