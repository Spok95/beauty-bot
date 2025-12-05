package bot

import (
	"fmt"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func roleKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Мастер", "role:master"),
			tgbotapi.NewInlineKeyboardButtonData("Администратор", "role:administrator"),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
}

func confirmKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📨 Отправить", "rq:send"),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
}

func navKeyboard(back bool, cancel bool) tgbotapi.InlineKeyboardMarkup {
	row := []tgbotapi.InlineKeyboardButton{}
	if back {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "nav:back"))
	}
	if cancel {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData("✖️ Отменить", "nav:cancel"))
	}
	return tgbotapi.NewInlineKeyboardMarkup(row)
}

func (b *Bot) unitKeyboard(id int64) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("pcs", fmt.Sprintf("adm:mat:unit:set:%d:pcs", id)),
			tgbotapi.NewInlineKeyboardButtonData("g", fmt.Sprintf("adm:mat:unit:set:%d:g", id)),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
}

func (b *Bot) subBuyPlaceKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Общий зал (часы)", "subbuy:place:hall"),
			tgbotapi.NewInlineKeyboardButtonData("Кабинет (дни)", "subbuy:place:cabinet"),
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
}

// adminReplyKeyboard Нижняя панель (ReplyKeyboard) для админа
func adminReplyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			{tgbotapi.NewKeyboardButton("Склады")},
			{tgbotapi.NewKeyboardButton("Категории"), tgbotapi.NewKeyboardButton("Материалы")},
			{tgbotapi.NewKeyboardButton("Инвентаризация"), tgbotapi.NewKeyboardButton("Поставки")},
			{tgbotapi.NewKeyboardButton("Установка цен"), tgbotapi.NewKeyboardButton("Установка тарифов")},
			{tgbotapi.NewKeyboardButton("Аренда и Расходы материалов по мастерам")},
			{tgbotapi.NewKeyboardButton("Оповещение всем")},
		},
	}
}

func masterReplyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			{tgbotapi.NewKeyboardButton("Расход/Аренда")},
			{tgbotapi.NewKeyboardButton("Просмотр остатков")},
			{tgbotapi.NewKeyboardButton("Мои абонементы"), tgbotapi.NewKeyboardButton("Купить абонемент")},
			{tgbotapi.NewKeyboardButton("Чат с админом")},
		},
	}
}

func salonAdminReplyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]tgbotapi.KeyboardButton{
			{tgbotapi.NewKeyboardButton("Категории"), tgbotapi.NewKeyboardButton("Материалы")},
			{tgbotapi.NewKeyboardButton("Инвентаризация"), tgbotapi.NewKeyboardButton("Поставки")},
			{tgbotapi.NewKeyboardButton("Чат с админом")},
		},
	}
}
