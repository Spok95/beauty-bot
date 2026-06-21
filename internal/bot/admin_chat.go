package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/adminchat"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleAdminChatMessage(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	tgID := msg.From.ID

	if len(b.adminIDs) == 0 {
		b.send(tgbotapi.NewMessage(chatID,
			"Админ-чат не настроен. Сообщение не отправлено."))
		return
	}

	u, _ := b.users.GetByTelegramID(ctx, tgID)
	if u == nil || u.Status != users.StatusApproved {
		b.send(tgbotapi.NewMessage(chatID, "Нет доступа к чату."))
		return
	}

	in := buildAdminChatInput(msg, u)

	st, _ := b.states.Get(ctx, chatID)

	if st.Payload != nil {
		if rawReplyID, ok := st.Payload["reply_to_admin_chat_message_id"]; ok {
			switch v := rawReplyID.(type) {
			case int64:
				in.ReplyToMessageID = v

			case float64:
				in.ReplyToMessageID = int64(v)
			}
		}
	}

	stored, err := b.adminChatRepo.Create(ctx, in)
	if err != nil {
		b.log.Error("failed to store admin chat message", "err", err)
		b.send(tgbotapi.NewMessage(chatID,
			"Не удалось сохранить сообщение. Попробуйте позже."))
		return
	}

	if st.Payload != nil {
		delete(st.Payload, "reply_to_admin_chat_message_id")
		_ = b.states.Set(ctx, chatID, dialog.StateChatAdmin, st.Payload)
	}

	recipients := b.adminChatRecipients(ctx, msg.Chat.ID)
	if len(recipients) == 0 {
		b.send(tgbotapi.NewMessage(chatID,
			"Сообщение сохранено, но нет других администраторов для пересылки."))
		return
	}

	header := b.adminChatHeader(stored, u)

	for _, adminID := range recipients {
		b.send(tgbotapi.NewMessage(adminID, header))
		b.sendAdminChatPayload(adminID, stored)
	}

	b.forwardAdminReplyToOriginalSender(ctx, stored, u)

	doneText := fmt.Sprintf(
		"Сообщение сохранено и отправлено администраторам. ID: #%d",
		stored.ID,
	)

	if stored.ReplyToMessageID > 0 && (u.Role == users.RoleAdmin || u.Role == users.RoleAdministrator) {
		doneText += "\nОтвет также отправлен автору исходного сообщения."
	}

	doneText += "\n\nМожно отправить следующее сообщение или нажать «Отменить»."

	doneMsg := tgbotapi.NewMessage(chatID, doneText)
	doneMsg.ReplyMarkup = adminChatCancelKeyboard()
	b.send(doneMsg)
}

func buildAdminChatInput(msg *tgbotapi.Message, u *users.User) adminchat.CreateMessageInput {
	in := adminchat.CreateMessageInput{
		SenderUserID:         u.ID,
		SenderTelegramID:     msg.From.ID,
		SenderUsername:       strings.TrimSpace(u.Username),
		SenderRole:           string(u.Role),
		MessageType:          "text",
		Text:                 strings.TrimSpace(msg.Text),
		TelegramMessageID:    int64(msg.MessageID),
		TelegramMediaGroupID: msg.MediaGroupID,
	}

	if msg.From.UserName != "" {
		in.SenderUsername = strings.TrimSpace(in.SenderUsername)
	}

	switch {
	case msg.Document != nil:
		in.MessageType = "document"
		in.Caption = strings.TrimSpace(msg.Caption)
		in.TelegramFileID = msg.Document.FileID
		in.TelegramFileUniqueID = msg.Document.FileUniqueID
		in.FileName = msg.Document.FileName
		in.MimeType = msg.Document.MimeType
		in.FileSize = int64(msg.Document.FileSize)

	case len(msg.Photo) > 0:
		photo := msg.Photo[len(msg.Photo)-1]
		in.MessageType = "photo"
		in.Caption = strings.TrimSpace(msg.Caption)
		in.TelegramFileID = photo.FileID
		in.TelegramFileUniqueID = photo.FileUniqueID
		in.FileSize = int64(photo.FileSize)

	case msg.Video != nil:
		in.MessageType = "video"
		in.Caption = strings.TrimSpace(msg.Caption)
		in.TelegramFileID = msg.Video.FileID
		in.TelegramFileUniqueID = msg.Video.FileUniqueID
		in.MimeType = msg.Video.MimeType
		in.FileSize = int64(msg.Video.FileSize)

	case msg.Audio != nil:
		in.MessageType = "audio"
		in.Caption = strings.TrimSpace(msg.Caption)
		in.TelegramFileID = msg.Audio.FileID
		in.TelegramFileUniqueID = msg.Audio.FileUniqueID
		in.MimeType = msg.Audio.MimeType
		in.FileSize = int64(msg.Audio.FileSize)

	case msg.Voice != nil:
		in.MessageType = "voice"
		in.TelegramFileID = msg.Voice.FileID
		in.TelegramFileUniqueID = msg.Voice.FileUniqueID
		in.MimeType = msg.Voice.MimeType
		in.FileSize = int64(msg.Voice.FileSize)
	}

	return in
}

func (b *Bot) adminChatRecipients(ctx context.Context, senderChatID int64) []int64 {
	seen := map[int64]struct{}{}
	out := []int64{}

	add := func(tgID int64) {
		if tgID == 0 || tgID == senderChatID {
			return
		}

		if _, ok := seen[tgID]; ok {
			return
		}

		seen[tgID] = struct{}{}
		out = append(out, tgID)
	}

	for adminID := range b.adminIDs {
		add(adminID)
	}

	if admins, err := b.users.ListByRole(ctx, users.RoleAdmin, users.StatusApproved); err == nil {
		for _, u := range admins {
			add(u.TelegramID)
		}
	}

	if administrators, err := b.users.ListByRole(ctx, users.RoleAdministrator, users.StatusApproved); err == nil {
		for _, u := range administrators {
			add(u.TelegramID)
		}
	}

	return out
}

func (b *Bot) adminChatHeader(m *adminchat.Message, u *users.User) string {
	role := roleLabel(users.Role(m.SenderRole))

	if role == "" {
		role = "Пользователь"
	}

	name := strings.TrimSpace(m.SenderUsername)
	if name == "" && u != nil {
		name = strings.TrimSpace(u.Username)
	}

	if name == "" {
		name = fmt.Sprintf("id %d", m.SenderTelegramID)
	}

	replyPart := ""

	if m.ReplyToMessageID > 0 {
		replyPart = fmt.Sprintf("\nОтвет на сообщение: #%d", m.ReplyToMessageID)
	}

	return fmt.Sprintf(
		"💬 Админ-чат #%d%s\nОт: %s\nРоль: %s\nTelegram ID: %d\nТип: %s",
		m.ID,
		replyPart,
		name,
		role,
		m.SenderTelegramID,
		adminChatMessageTypeLabel(m.MessageType),
	)
}

func adminChatMessageTypeLabel(t string) string {
	switch t {
	case "text":
		return "текст"
	case "document":
		return "файл"
	case "photo":
		return "фото"
	case "video":
		return "видео"
	case "audio":
		return "аудио"
	case "voice":
		return "голосовое"
	default:
		return t
	}
}

func (b *Bot) sendAdminChatPayload(chatID int64, m *adminchat.Message) {
	switch m.MessageType {
	case "document":
		doc := tgbotapi.NewDocument(chatID, tgbotapi.FileID(m.TelegramFileID))
		if m.Caption != "" {
			doc.Caption = m.Caption
		}
		b.send(doc)

	case "photo":
		photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(m.TelegramFileID))
		if m.Caption != "" {
			photo.Caption = m.Caption
		}
		b.send(photo)

	case "video":
		video := tgbotapi.NewVideo(chatID, tgbotapi.FileID(m.TelegramFileID))
		if m.Caption != "" {
			video.Caption = m.Caption
		}
		b.send(video)

	case "audio":
		audio := tgbotapi.NewAudio(chatID, tgbotapi.FileID(m.TelegramFileID))
		if m.Caption != "" {
			audio.Caption = m.Caption
		}
		b.send(audio)

	case "voice":
		voice := tgbotapi.NewVoice(chatID, tgbotapi.FileID(m.TelegramFileID))
		b.send(voice)

	default:
		if m.Text != "" {
			b.send(tgbotapi.NewMessage(chatID, m.Text))
		}
	}
}

func adminChatCancelKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✖️ Отменить", "nav:cancel"),
		),
	)
}

func (b *Bot) sendAdminChatHistoryMedia(chatID int64, m *adminchat.Message) {
	if m.TelegramFileID == "" {
		b.send(tgbotapi.NewMessage(chatID, "У этого сообщения нет вложения."))
		return
	}

	header := fmt.Sprintf(
		"📎 Вложение из истории #%d\nТип: %s",
		m.ID,
		adminChatMessageTypeLabel(m.MessageType),
	)

	if m.Caption != "" {
		header += "\nПодпись: " + m.Caption
	}

	if m.FileName != "" {
		header += "\nФайл: " + m.FileName
	}

	b.send(tgbotapi.NewMessage(chatID, header))
	b.sendAdminChatPayload(chatID, m)
}

func (b *Bot) forwardAdminReplyToOriginalSender(ctx context.Context, stored *adminchat.Message, sender *users.User) {
	if stored == nil || stored.ReplyToMessageID <= 0 {
		return
	}

	if sender == nil || sender.Role != users.RoleAdmin && sender.Role != users.RoleAdministrator {
		return
	}

	parent, err := b.adminChatRepo.GetByID(ctx, stored.ReplyToMessageID)
	if err != nil || parent == nil {
		return
	}

	if parent.SenderTelegramID == 0 || parent.SenderTelegramID == stored.SenderTelegramID {
		return
	}

	parentRole := users.Role(parent.SenderRole)
	if parentRole != users.RoleMaster &&
		parentRole != users.RoleAdministrator &&
		parentRole != users.RoleAdmin {
		return
	}

	adminName := strings.TrimSpace(stored.SenderUsername)
	if adminName == "" {
		adminName = fmt.Sprintf("id %d", stored.SenderTelegramID)
	}

	header := fmt.Sprintf(
		"💬 Ответ администратора на ваше сообщение #%d\nОт: %s\nТип: %s",
		parent.ID,
		adminName,
		adminChatMessageTypeLabel(stored.MessageType),
	)

	b.send(tgbotapi.NewMessage(parent.SenderTelegramID, header))
	b.sendAdminChatPayload(parent.SenderTelegramID, stored)
}
