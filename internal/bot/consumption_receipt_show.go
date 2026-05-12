package bot

import (
	"context"

	"github.com/Spok95/beauty-bot/internal/dialog"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) showConsumptionReceiptForConfirm(
	ctx context.Context,
	chatID int64,
	editMsgID int,
	telegramID int64,
	payload dialog.Payload,
) {
	calculatedPayload, userError, err := b.calculateConsumptionReceiptPayload(ctx, telegramID, payload)
	if err != nil {
		if editMsgID != 0 {
			b.editTextAndClear(chatID, editMsgID, userError)
		} else {
			b.send(tgbotapi.NewMessage(chatID, userError))
		}
		return
	}

	_ = b.states.Set(ctx, chatID, dialog.StateConsSummary, calculatedPayload)

	txt := b.buildConsumptionReceipt(ctx, calculatedPayload, "Проверь перед подтверждением:")

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", "cons:confirm"),
		),
	}

	withSub := false
	if v, ok := calculatedPayload["with_sub"].(bool); ok {
		withSub = v
	}

	if withSub {
		for _, part := range parseRentParts(calculatedPayload["rent_parts"]) {
			if !payloadBool(part, "with_sub") {
				rows = append(rows, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("Купить абонемент", "cons:buy_sub"),
				))
				break
			}
		}
	}

	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

	if editMsgID != 0 {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, txt, kb))
		return
	}

	msg := tgbotapi.NewMessage(chatID, txt)
	msg.ReplyMarkup = kb
	b.send(msg)
}
