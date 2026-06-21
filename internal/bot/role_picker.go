package bot

import (
	"fmt"

	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func roleLabel(role users.Role) string {
	switch role {
	case users.RoleMaster:
		return "Мастер"
	case users.RoleAdministrator:
		return "Администратор"
	case users.RoleAdmin:
		return "Админ"
	default:
		return string(role)
	}
}

func (b *Bot) showRolePicker(chatID int64, roles []users.Role) {
	rows := [][]tgbotapi.InlineKeyboardButton{}

	for _, role := range roles {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				roleLabel(role),
				fmt.Sprintf("role:switch:%s", role),
			),
		))
	}

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID, "Выберите роль:")
	msg.ReplyMarkup = kb
	b.send(msg)
}

func (b *Bot) sendMenuForRole(chatID int64, role users.Role) {
	switch role {
	case users.RoleAdmin:
		m := tgbotapi.NewMessage(chatID,
			"Привет, админ! Для управления ботом используйте меню с кнопками.")
		m.ReplyMarkup = adminReplyKeyboard()
		b.send(m)

	case users.RoleAdministrator:
		m := tgbotapi.NewMessage(chatID,
			"Готово! Для работы со складами используйте кнопки снизу.")
		m.ReplyMarkup = salonAdminReplyKeyboard()
		b.send(m)

	case users.RoleMaster:
		m := tgbotapi.NewMessage(chatID,
			"Готово! Для учёта материалов и аренды жми «Расход/Аренда».")
		m.ReplyMarkup = masterReplyKeyboard()
		b.send(m)

	default:
		b.send(tgbotapi.NewMessage(chatID, "Роль не распознана. Обратитесь к администратору."))
	}
}
