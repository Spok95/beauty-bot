package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/users"
)

type Bot struct {
	api       *tgbotapi.BotAPI
	log       *slog.Logger
	users     *users.Repo
	states    *dialog.Repo
	adminChat int64
}

func New(api *tgbotapi.BotAPI, log *slog.Logger, usersRepo *users.Repo, statesRepo *dialog.Repo, adminChatID int64) *Bot {
	return &Bot{api: api, log: log, users: usersRepo, states: statesRepo, adminChat: adminChatID}
}

func (b *Bot) Run(ctx context.Context, timeoutSec int) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = timeoutSec
	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case upd := <-updates:
			if upd.Message != nil {
				b.onMessage(ctx, upd)
			} else if upd.CallbackQuery != nil {
				b.onCallback(ctx, upd)
			}
		}
	}
}

func (b *Bot) send(msg tgbotapi.Chattable) {
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send failed", "err", err)
	}
}

/*** NAV HELPERS ***/

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

func (b *Bot) editTextAndClear(chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageTextAndMarkup(
		chatID, messageID, text,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
	)
	b.send(edit)
}

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

func (b *Bot) onMessage(ctx context.Context, upd tgbotapi.Update) {
	msg := upd.Message
	chatID := msg.Chat.ID
	tgID := msg.From.ID

	// Команды
	if msg.IsCommand() {
		switch msg.Command() {
		case "start":
			u, err := b.users.UpsertByTelegram(ctx, tgID, users.RoleMaster)
			if err != nil {
				b.send(tgbotapi.NewMessage(chatID, "Ошибка: не удалось сохранить профиль"))
				return
			}

			// авто-админ
			if msg.From.ID == b.adminChat && (u.Role != users.RoleAdmin || u.Status != users.StatusApproved) {
				if _, err2 := b.users.Approve(ctx, msg.From.ID, users.RoleAdmin); err2 == nil {
					b.send(tgbotapi.NewMessage(chatID, "Привет, админ! /help покажет команды."))
					return
				}
			}
			if u.Role == users.RoleAdmin && u.Status == users.StatusApproved {
				b.send(tgbotapi.NewMessage(chatID, "Привет, админ! /help покажет команды."))
				return
			}

			switch u.Status {
			case users.StatusApproved:
				b.send(tgbotapi.NewMessage(chatID, "Вы уже подтверждены. /help — список команд."))
			case users.StatusRejected:
				// Разрешаем переподать заявку: переводим на ввод ФИО
				_ = b.states.Set(ctx, chatID, dialog.StateAwaitFIO, dialog.Payload{})
				b.askFIO(chatID)
			default: // pending/нет записи fio/роль
				_ = b.states.Set(ctx, chatID, dialog.StateAwaitFIO, dialog.Payload{})
				b.askFIO(chatID)
			}
			return

		case "help":
			b.send(tgbotapi.NewMessage(chatID, "Команды:\n/start — начать регистрацию/работу\n/help — помощь"))
			return

		default:
			b.send(tgbotapi.NewMessage(chatID, "Не знаю такую команду. Наберите /help"))
			return
		}
	}

	// Диалог по состояниям
	st, _ := b.states.Get(ctx, chatID)
	switch st.State {
	case dialog.StateAwaitFIO:
		fio := strings.TrimSpace(msg.Text)
		if fio == "" || len(fio) < 3 {
			b.send(tgbotapi.NewMessage(chatID, "ФИО выглядит пустым. Введите корректно."))
			return
		}
		if _, err := b.users.SetFIO(ctx, tgID, fio); err != nil {
			b.send(tgbotapi.NewMessage(chatID, "Ошибка сохранения ФИО, попробуйте ещё раз."))
			return
		}
		p := st.Payload
		p["fio"] = fio
		_ = b.states.Set(ctx, chatID, dialog.StateAwaitRole, p)

		m := tgbotapi.NewMessage(chatID, "Выберите роль:")
		m.ReplyMarkup = roleKeyboard()
		b.send(m)
		return

	case dialog.StateAwaitRole:
		b.send(tgbotapi.NewMessage(chatID, "Нажмите кнопку выше, чтобы выбрать роль."))
		return

	case dialog.StateAwaitConfirm:
		b.send(tgbotapi.NewMessage(chatID, "Нажмите кнопку выше: «Отправить», «Назад» или «Отменить»."))
		return
	}
}

func (b *Bot) onCallback(ctx context.Context, upd tgbotapi.Update) {
	cb := upd.CallbackQuery
	data := cb.Data
	fromChat := cb.Message.Chat.ID

	// Навигация общая
	if data == "nav:cancel" {
		_ = b.states.Reset(ctx, fromChat)
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Операция отменена.")
		_ = b.answerCallback(cb, "Отменено", false)
		return
	}
	if data == "nav:back" {
		st, _ := b.states.Get(ctx, fromChat)
		switch st.State {
		case dialog.StateAwaitRole:
			// назад к ФИО
			_ = b.states.Set(ctx, fromChat, dialog.StateAwaitFIO, dialog.Payload{})
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Измените ФИО и отправьте сообщением.")
			b.askFIO(fromChat)
		case dialog.StateAwaitConfirm:
			// назад к выбору роли
			_ = b.states.Set(ctx, fromChat, dialog.StateAwaitRole, st.Payload)
			text := "Выберите роль:"
			edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, text, roleKeyboard())
			b.send(edit)
		default:
			b.editTextAndClear(fromChat, cb.Message.MessageID, "Действие неактуально.")
		}
		_ = b.answerCallback(cb, "Назад", false)
		return
	}

	switch {
	// Пользователь выбрал роль → переходим на подтверждение
	case strings.HasPrefix(data, "role:"):
		roleStr := strings.TrimPrefix(data, "role:")
		var role users.Role
		switch roleStr {
		case "administrator":
			role = users.RoleAdministrator
		default:
			role = users.RoleMaster
		}

		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAwaitRole {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		fio, _ := dialog.GetString(st.Payload, "fio")
		p := st.Payload
		p["role"] = string(role)
		_ = b.states.Set(ctx, fromChat, dialog.StateAwaitConfirm, p)

		// редактируем текущее сообщение → экран подтверждения
		confirmText := fmt.Sprintf("Проверьте данные:\n— ФИО: %s\n— Роль: %s\n\nОтправить заявку администратору?", fio, role)
		edit := tgbotapi.NewEditMessageTextAndMarkup(fromChat, cb.Message.MessageID, confirmText, confirmKeyboard())
		b.send(edit)
		_ = b.answerCallback(cb, "Ок", false)
		return

	// Подтверждение отправки заявки
	case data == "rq:send":
		st, _ := b.states.Get(ctx, fromChat)
		if st.State != dialog.StateAwaitConfirm {
			_ = b.answerCallback(cb, "Неактуально", false)
			return
		}
		fio, _ := dialog.GetString(st.Payload, "fio")
		roleStr, _ := dialog.GetString(st.Payload, "role")
		role := users.Role(roleStr)

		// Сохраним роль у пользователя (чтобы админ видел, что хочет)
		_, _ = b.users.UpsertByTelegram(ctx, cb.From.ID, role)

		// Пользователю: меняем экран подтверждения → «Заявка отправлена…» и очищаем клавиатуру
		b.editTextAndClear(fromChat, cb.Message.MessageID, "Заявка отправлена администратору. Ожидайте решения.")
		_ = b.states.Reset(ctx, fromChat)

		// Админу — карточка
		text := fmt.Sprintf(
			"Новая заявка на доступ:\n— ФИО: %s\n— Telegram: @%s (id %d)\n— Роль: %s\n\nОдобрить?",
			fio, cb.From.UserName, cb.From.ID, role,
		)
		kb := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Одобрить", fmt.Sprintf("approve:%d:%s", cb.From.ID, role)),
				tgbotapi.NewInlineKeyboardButtonData("⛔ Отклонить", fmt.Sprintf("reject:%d", cb.From.ID)),
			),
		)
		m := tgbotapi.NewMessage(b.adminChat, text)
		m.ReplyMarkup = kb
		b.send(m)

		_ = b.answerCallback(cb, "Отправлено", false)
		return

	// Админ одобряет
	case strings.HasPrefix(data, "approve:"):
		if fromChat != b.adminChat {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		parts := strings.Split(strings.TrimPrefix(data, "approve:"), ":")
		if len(parts) != 2 {
			_ = b.answerCallback(cb, "Некорректные данные", true)
			return
		}
		tgID, _ := strconv.ParseInt(parts[0], 10, 64)
		role := users.Role(parts[1])

		if _, err := b.users.Approve(ctx, tgID, role); err != nil {
			_ = b.answerCallback(cb, "Ошибка при одобрении", true)
			return
		}

		// Администратору — снять клавиатуру и пометить текст
		newText := cb.Message.Text + "\n\n✅ Заявка подтверждена"
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)

		// Пользователю — явное сообщение
		b.send(tgbotapi.NewMessage(tgID, fmt.Sprintf("Заявка подтверждена. Ваша роль: %s", role)))
		_ = b.answerCallback(cb, "Одобрено", false)
		return

	// Админ отклоняет
	case strings.HasPrefix(data, "reject:"):
		if fromChat != b.adminChat {
			_ = b.answerCallback(cb, "Недостаточно прав", true)
			return
		}
		tgID, _ := strconv.ParseInt(strings.TrimPrefix(data, "reject:"), 10, 64)
		if _, err := b.users.Reject(ctx, tgID); err != nil {
			_ = b.answerCallback(cb, "Ошибка при отклонении", true)
			return
		}

		// Администратору — снять клавиатуру и пометить текст
		newText := cb.Message.Text + "\n\n⛔ Заявка отклонена"
		b.editTextAndClear(fromChat, cb.Message.MessageID, newText)

		// Пользователю — сообщение + сразу предлагается заново подать заявку
		b.send(tgbotapi.NewMessage(tgID, "Заявка отклонена. Введите ФИО, чтобы подать заявку ещё раз."))
		_ = b.states.Set(ctx, tgID, dialog.StateAwaitFIO, dialog.Payload{})
		b.askFIO(tgID)

		_ = b.answerCallback(cb, "Отклонено", false)
		return
	}
}

func (b *Bot) answerCallback(cb *tgbotapi.CallbackQuery, text string, alert bool) error {
	resp := tgbotapi.NewCallback(cb.ID, text)
	resp.ShowAlert = alert
	_, err := b.api.Request(resp)
	return err
}
