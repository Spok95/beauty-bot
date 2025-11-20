package bot

import tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

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
