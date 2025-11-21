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
		title := strings.TrimSpace(u.Username)
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

// rentPartMeta — «кусок» сессии: либо по конкретному абонементу, либо без абонемента.
type rentPartMeta struct {
	WithSub   bool  // true — часть по абонементу, false — без абонемента
	Qty       int   // сколько часов/дней в этой части
	SubID     int64 // 0 — нет абонемента (часть без абонемента)
	PlanLimit int   // номинальный лимит плана (30, 50, ...) — для текста и выбора тарифа
}

func (b *Bot) showConsCart(ctx context.Context, chatID int64, editMsgID *int, place, unit string, qty int, items []map[string]any) {
	lines := []string{fmt.Sprintf("Расход/Аренда: %s, %d %s", map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place], qty, map[string]string{"hour": "ч", "day": "дн"}[unit])}
	var sum float64
	for _, it := range items {
		matID := int64(it["mat_id"].(float64))
		q := int64(it["qty"].(float64))
		name := fmt.Sprintf("ID:%d", matID)
		if m, _ := b.materials.GetByID(ctx, matID); m != nil {
			name = m.Name
		}
		price, _ := b.materials.GetPrice(ctx, matID)
		line := float64(q) * price
		sum += line
		lines = append(lines, fmt.Sprintf("• %s — %d × %.2f = %.2f ₽", name, q, price, line))
	}
	lines = append(lines, fmt.Sprintf("\nСумма материалов: %.2f ₽", sum))

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("➕ Добавить материал", "cons:additem")),
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("🧮 Посчитать", "cons:calc")),
		navKeyboard(true, true).InlineKeyboard[0],
	)

	text := strings.Join(lines, "\n")
	if editMsgID != nil {
		b.send(tgbotapi.NewEditMessageTextAndMarkup(chatID, *editMsgID, text, kb))
	} else {
		m := tgbotapi.NewMessage(chatID, text)
		m.ReplyMarkup = kb
		b.send(m)
	}
}

// notifyLowOrNegativeBatch — собирает по складам/категориям и шлёт одним сообщением
func (b *Bot) notifyLowOrNegativeBatch(ctx context.Context, pairs [][2]int64) {
	// обработаем каждую пару (wh, mat) только один раз
	seen := make(map[[2]int64]struct{})
	groups := map[int64]map[int64][]string{} // whID -> catID -> lines

	for _, p := range pairs {
		key := [2]int64{p[0], p[1]}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		whID, matID := p[0], p[1]

		m, err := b.materials.GetByID(ctx, matID)
		if err != nil || m == nil {
			continue
		}
		bal, err := b.inventory.GetBalance(ctx, whID, matID)
		if err != nil {
			continue
		}

		var warnLine string
		switch m.Unit {
		case "g":
			if bal <= 0 {
				warnLine = fmt.Sprintf("— %s — закончились.", m.Name)
			} else if bal < lowStockThresholdGr {
				warnLine = fmt.Sprintf("— %s — %.0f g — мало", m.Name, bal)
			}
		case "pcs":
			if bal <= 0 {
				warnLine = fmt.Sprintf("— %s — закончились.", m.Name)
			} else if bal < lowStockThresholdPcs {
				warnLine = fmt.Sprintf("— %s — %.0f шт — мало", m.Name, bal)
			}
		default:
			// прочие единицы — без алертов
		}

		if warnLine == "" {
			continue
		}
		if _, ok := groups[whID]; !ok {
			groups[whID] = map[int64][]string{}
		}
		groups[whID][m.CategoryID] = append(groups[whID][m.CategoryID], warnLine)
	}

	if len(groups) == 0 {
		return
	}

	for whID, cats := range groups {
		whName := fmt.Sprintf("ID:%d", whID)
		if wh, err := b.catalog.GetWarehouseByID(ctx, whID); err == nil && wh != nil {
			whName = wh.Name
		}

		var bld strings.Builder
		bld.WriteString("⚠️ Материалы:\n")
		bld.WriteString(fmt.Sprintf("Склад: %s\n", whName))

		for catID, lines := range cats {
			catName := fmt.Sprintf("Категория #%d", catID)
			if cat, err := b.catalog.GetCategoryByID(ctx, catID); err == nil && cat != nil {
				catName = cat.Name
			}
			bld.WriteString(fmt.Sprintf("— %s:\n", catName))
			for _, ln := range lines {
				if !strings.HasSuffix(ln, "\n") {
					bld.WriteString(ln + "\n")
				} else {
					bld.WriteString(ln)
				}
			}
		}
		b.notifyStockRecipients(ctx, strings.TrimSpace(bld.String()))
	}
}
