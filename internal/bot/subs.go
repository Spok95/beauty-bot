package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// showSubsMenu Меню «Абонементы» для админа
func (b *Bot) showSubsMenu(chatID int64, editMsgID *int) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Создать абонемент", "adm:subs:add"),
			// tgbotapi.NewInlineKeyboardButtonData("📄 Список (текущий месяц)", "adm:subs:list"), // позже
		),
		navKeyboard(false, true).InlineKeyboard[0],
	)
	text := "Абонементы — выберите действие"
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// showSubsPickUser — выбор мастера для абонемента
func (b *Bot) showSubsPickUser(ctx context.Context, chatID int64, editMsgID int) {
	list, err := b.users.ListByRole(ctx, users.RoleMaster, users.StatusApproved)
	if err != nil || len(list) == 0 {
		b.editTextAndClear(chatID, editMsgID, "Нет утверждённых мастеров.")
		return
	}

	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, u := range list {
		title := strings.TrimSpace(u.Username) // в Username у нас «ФИО/отображаемое имя»
		if title == "" {
			title = fmt.Sprintf("id %d", u.ID)
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(title, fmt.Sprintf("adm:subs:user:%d", u.ID)),
		))
	}
	rows = append(rows, navKeyboard(true, true).InlineKeyboard[0])

	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Выберите мастера:", kb))
}

// showSubsPickPlaceUnit Выбор места/единицы
func (b *Bot) showSubsPickPlaceUnit(chatID int64, editMsgID int, uid int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			// Сразу задаём и место и единицу:
			tgbotapi.NewInlineKeyboardButtonData("Зал (часы)", fmt.Sprintf("adm:subs:pu:%d:hall:hour", uid)),
			tgbotapi.NewInlineKeyboardButtonData("Кабинет (дни)", fmt.Sprintf("adm:subs:pu:%d:cabinet:day", uid)),
		),
		navKeyboard(true, true).InlineKeyboard[0],
	)
	b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, editMsgID, "Выберите помещение:", kb))
}

// splitQtyBySubscriptions делит qty по активным абонементам (FIFO), остаток — без абонемента.
// Использует новую модель: несколько абонементов за месяц, поле PlanLimit, ListActiveByPlaceUnitMonth.
func (b *Bot) splitQtyBySubscriptions(
	ctx context.Context,
	userID int64,
	place, unit string,
	qty int,
) ([]rentPartMeta, error) {
	metas := make([]rentPartMeta, 0, 3)

	if qty <= 0 {
		return metas, nil
	}

	remaining := qty

	// 1) части по абонементам (если есть)
	if b.subs != nil {
		month := time.Now().Format("2006-01")
		subs, err := b.subs.ListActiveByPlaceUnitMonth(ctx, userID, place, unit, month)
		if err == nil {
			for _, s := range subs {
				left := s.TotalQty - s.UsedQty
				if left <= 0 {
					continue
				}
				if remaining <= 0 {
					break
				}
				use := remaining
				if left < use {
					use = left
				}
				metas = append(metas, rentPartMeta{
					WithSub:   true,
					Qty:       use,
					SubID:     s.ID,
					PlanLimit: s.PlanLimit,
				})
				remaining -= use
			}
		}
	}

	// 2) то, что не покрыто абонементами — часть без абонемента
	if remaining > 0 {
		metas = append(metas, rentPartMeta{
			WithSub:   false,
			Qty:       remaining,
			SubID:     0,
			PlanLimit: 0,
		})
	}

	return metas, nil
}
