package bot

import (
	"context"
	"fmt"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/catalog"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) allowedWarehousesForUser(ctx context.Context, u *users.User) ([]catalog.Warehouse, error) {
	all, err := b.catalog.ListWarehouses(ctx)
	if err != nil {
		return nil, err
	}

	var out []catalog.Warehouse

	for _, w := range all {
		if !w.Active {
			continue
		}

		switch u.Role {
		case users.RoleMaster, users.RoleAdministrator, users.RoleAdmin:
			if w.Type == catalog.WHTConsumables || w.Type == catalog.WHTOther {
				out = append(out, w)
			}
		}
	}

	return out, nil
}

func (b *Bot) showConsumptionWarehousePick(ctx context.Context, chatID int64, editMsgID *int, u *users.User) {
	warehouses, err := b.allowedWarehousesForUser(ctx, u)
	if err != nil {
		b.send(tgbotapi.NewMessage(chatID, "Ошибка загрузки складов."))
		return
	}

	if len(warehouses) == 0 {
		b.send(tgbotapi.NewMessage(chatID, "Нет доступных активных складов. Обратитесь к администратору."))
		return
	}

	if len(warehouses) == 1 {
		w := warehouses[0]
		payload := dialog.Payload{
			"warehouse_id":   float64(w.ID),
			"warehouse_name": w.Name,
		}

		_ = b.states.Set(ctx, chatID, dialog.StateConsComment, payload)
		b.showConsumptionCommentStep(chatID, editMsgID)
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}

	for _, w := range warehouses {
		label := fmt.Sprintf("%s (%s)", w.Name, warehouseTypeLabel(w.Type))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("cons:wh:%d", w.ID)),
		))
	}

	rows = append(rows, navKeyboard(false, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, "Выберите склад:", kb))
	} else {
		msg := tgbotapi.NewMessage(chatID, "Выберите склад:")
		msg.ReplyMarkup = kb
		b.send(msg)
	}

	_ = b.states.Set(ctx, chatID, dialog.StateConsWhPick, dialog.Payload{})
}

func (b *Bot) showConsumptionCommentStep(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Пропустить", "cons:comment_skip"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)

	text := "Введите дату за которую подаете данные или если дата совпадает, то нажмите «Пропустить»."

	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
		return
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = kb
	b.send(msg)
}
