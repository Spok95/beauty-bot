package bot

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) showConsumptionRentModeStep(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("С арендой", "cons:rent:with"),
			tgbotapi.NewInlineKeyboardButtonData("Без аренды", "cons:rent:none"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Студийный клиент", "cons:rent:studio"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)

	text := "Выберите тип расхода:"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
		return
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = kb
	b.send(msg)
}

func (b *Bot) showConsumptionPlaceStep(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Общий зал", "cons:place:hall"),
			tgbotapi.NewInlineKeyboardButtonData("Кабинет", "cons:place:cabinet"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)

	text := "Выберите помещение:"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
		return
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = kb
	b.send(msg)
}

func (b *Bot) showConsumptionStudioAmountStep(chatID int64, editMsgID *int) {
	text := "Введите итоговую сумму для студийного клиента, которую нужно указать в аренде.\n\nНапример: 1500"

	if editMsgID != nil {
		b.editTextWithNav(chatID, *editMsgID, text)
		return
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = navKeyboard(true, true)
	b.send(msg)
}

func consumptionCartTitle(place, unit string, qty int) string {
	if place == "studio_client" || unit == "studio_fee" {
		return "Расход/Аренда: студийный клиент"
	}

	if place == "no_rent" || unit == "none" {
		return "Расход материалов: без аренды"
	}

	return fmt.Sprintf(
		"Расход/Аренда: %s, %d %s",
		map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
		qty,
		map[string]string{"hour": "ч", "day": "дн"}[unit],
	)
}
