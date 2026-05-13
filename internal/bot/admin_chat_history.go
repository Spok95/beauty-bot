package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/Spok95/beauty-bot/internal/domain/adminchat"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const adminChatHistoryPageSize = 10

func (b *Bot) showAdminChatHistory(ctx context.Context, chatID int64, page int) {
	if page < 0 {
		page = 0
	}

	offset := page * adminChatHistoryPageSize

	items, err := b.adminChatRepo.Last(ctx, adminChatHistoryPageSize, offset)
	if err != nil {
		b.log.Error("failed to load admin chat history", "err", err)

		b.send(tgbotapi.NewMessage(chatID,
			"Не удалось загрузить историю чата."))
		return
	}

	if len(items) == 0 {
		b.send(tgbotapi.NewMessage(chatID,
			"История чата пока пуста."))
		return
	}

	var lines []string

	lines = append(lines,
		fmt.Sprintf("💬 История админ-чата\nСтраница: %d\n", page+1))

	for _, m := range items {
		lines = append(lines, formatAdminChatHistoryItem(&m))
		lines = append(lines, "--------------------")
	}

	msg := tgbotapi.NewMessage(chatID, strings.Join(lines, "\n"))

	var rows [][]tgbotapi.InlineKeyboardButton

	navRow := []tgbotapi.InlineKeyboardButton{}

	if page > 0 {
		navRow = append(navRow,
			tgbotapi.NewInlineKeyboardButtonData(
				"⬅️ Назад",
				fmt.Sprintf("adminchat:history:%d", page-1),
			),
		)
	}

	if len(items) == adminChatHistoryPageSize {
		navRow = append(navRow,
			tgbotapi.NewInlineKeyboardButtonData(
				"➡️ Далее",
				fmt.Sprintf("adminchat:history:%d", page+1),
			),
		)
	}

	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	rows = append(rows,
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				"✖️ Закрыть",
				"nav:cancel",
			),
		),
	)

	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)

	b.send(msg)
}

func formatAdminChatHistoryItem(m *adminchat.Message) string {
	role := roleLabel(users.Role(m.SenderRole))
	if role == "" {
		role = m.SenderRole
	}

	name := strings.TrimSpace(m.SenderUsername)
	if name == "" {
		name = fmt.Sprintf("id %d", m.SenderTelegramID)
	}

	text := strings.TrimSpace(m.Text)

	if text == "" {
		text = strings.TrimSpace(m.Caption)
	}

	if text == "" && m.FileName != "" {
		text = m.FileName
	}

	if text == "" {
		text = "(без текста)"
	}

	if len(text) > 300 {
		text = text[:300] + "..."
	}

	return fmt.Sprintf(
		"#%d | %s\nОт: %s\nРоль: %s\nТип: %s\nДата: %s\n\n%s",
		m.ID,
		adminChatMessageTypeLabel(m.MessageType),
		name,
		role,
		m.MessageType,
		m.CreatedAt.Format("02.01.2006 15:04"),
		text,
	)
}
