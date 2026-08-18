package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Spok95/beauty-bot/internal/dialog"
	subsdomain "github.com/Spok95/beauty-bot/internal/domain/subscriptions"
	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const consumptionPaymentInstructions = `

Оплатить можно одним из этих способов:
- Переводом по номеру телефона 9253091910 (Сбер)
- Переводом по номеру карты 4377723782554202 (Т-Банк)
- По ссылке: https://yookassa.ru/my/i/aR-nyBvQF8oX/l`

func (b *Bot) finalizeConsumption(ctx context.Context, chatID int64, editMsgID int, telegramID int64, telegramUsername string, payload dialog.Payload) {
	respond := func(text string) {
		if editMsgID != 0 {
			b.editTextAndClear(chatID, editMsgID, text)
			return
		}
		b.send(tgbotapi.NewMessage(chatID, text))
	}

	u, _ := b.users.GetByTelegramID(ctx, telegramID)
	if u == nil || u.Status != "approved" {
		respond("Нет доступа")
		return
	}

	place := payload["place"].(string)
	unit := payload["unit"].(string)
	qty := int(payload["qty"].(float64))
	items := b.consParseItems(payload["items"])
	mats := payload["mats_sum"].(float64)
	rounded := payload["mats_rounded"].(float64)
	rent := payload["rent"].(float64)
	total := payload["total"].(float64)
	noRent := isConsumptionNoRent(payload)

	var comment string
	if v, ok := payload["comment"].(string); ok {
		comment = v
	}

	var finalComment string
	if v, ok := payload["final_comment"].(string); ok {
		finalComment = v
	}

	// Склад обязателен только если есть материалы для списания.
	// Для сценария «аренда без материалов» склад не выбирается и не нужен.
	whID := payloadInt64(payload["warehouse_id"])
	if len(items) > 0 && whID <= 0 {
		respond("Склад не выбран. Начните расчёт заново.")
		return
	}

	withSub := false
	if v, ok := payload["with_sub"].(bool); ok {
		withSub = v
	}

	// создаём сессию + позиции
	sessionPayload := map[string]any{
		"items_count": len(items),
	}

	if noRent {
		sessionPayload["no_rent"] = true
	}

	if isConsumptionStudioClient(payload) {
		sessionPayload["studio_client"] = true
		sessionPayload["rent_mode"] = "studio_client"
		sessionPayload["studio_fee"] = rent
	}

	if warehouseID := payloadInt64(payload["warehouse_id"]); warehouseID > 0 {
		sessionPayload["warehouse_id"] = warehouseID
	}

	if warehouseName, ok := payload["warehouse_name"].(string); ok && strings.TrimSpace(warehouseName) != "" {
		sessionPayload["warehouse_name"] = warehouseName
	}

	if comment != "" {
		sessionPayload["comment"] = comment
	}

	if finalComment != "" {
		sessionPayload["final_comment"] = finalComment
	}

	if rentParts, ok := payload["rent_parts"]; ok && rentParts != nil {
		sessionPayload["rent_parts"] = rentParts
	}

	sid, err := b.cons.CreateSession(ctx, u.ID, place, unit, qty, withSub, mats, rounded, rent, total, sessionPayload)

	if err != nil {
		respond("Не удалось создать сессию")
		return
	}
	// Учёт абонемента: спишем использованное количество (часы/дни) за текущий месяц
	if withSub && b.subs != nil {
		// разбиваем сессию на части по тем же правилам (старые/новые абонементы + без абонемента)
		metas, _ := b.splitQtyBySubscriptions(ctx, u.ID, place, unit, qty)
		month := time.Now().Format("2006-01")

		for _, m := range metas {
			if !m.WithSub || m.SubID == 0 || m.Qty <= 0 {
				continue
			}

			if err := b.subs.AddUsage(ctx, m.SubID, m.Qty); err != nil {
				if errors.Is(err, subsdomain.ErrInsufficientLimit) && b.adminChat != 0 {
					// сигнал админу, что по конкретному абонементу лимит уже выбит
					b.send(tgbotapi.NewMessage(b.adminChat,
						fmt.Sprintf("⚠️ Не удалось списать %d %s абонемента (id=%d) для мастера id %d: недостаточно лимита.",
							m.Qty,
							map[string]string{"hour": "часов", "day": "дней"}[unit],
							m.SubID,
							u.ID,
						)))
				}
			}
		}

		// после списаний проверим, есть ли ещё активные абонементы по этому месту/единице
		if subsAfter, err := b.subs.ListActiveByPlaceUnitMonth(ctx, u.ID, place, unit, month); err == nil && len(subsAfter) == 0 {
			// всё по этому помещению выработано — предложим купить новый абонемент
			msg := tgbotapi.NewMessage(chatID,
				"Абонемент по этому помещению полностью использован.\nХотите приобрести новый абонемент?")
			msg.ReplyMarkup = b.subBuyPlaceKeyboard()
			b.send(msg)
		}
	}

	// Добавляем сумму материалов к абонементам
	if partsRaw, ok := payload["rent_parts"]; ok && partsRaw != nil && b.subs != nil && mats > 0 {
		if parts, ok := partsRaw.([]any); ok {
			// Считаем общий объём часов/дней по частям с абонементом
			totalSubQty := 0
			for _, pr := range parts {
				mp, ok := pr.(map[string]any)
				if !ok {
					continue
				}
				withSub, _ := mp["with_sub"].(bool)
				if !withSub {
					continue
				}
				qtyF, okQty := mp["qty"].(float64)
				if !okQty {
					continue
				}
				totalSubQty += int(qtyF)
			}

			if totalSubQty > 0 {
				for _, pr := range parts {
					mp, ok := pr.(map[string]any)
					if !ok {
						continue
					}

					withSub, _ := mp["with_sub"].(bool)
					if !withSub {
						continue
					}

					subIDF, okID := mp["sub_id"].(float64)
					qtyF, okQty := mp["qty"].(float64)
					if !okID || !okQty {
						continue
					}
					partQty := int(qtyF)
					if partQty <= 0 {
						continue
					}

					subID := int64(subIDF)
					// Фактическая сумма материалов, приходящаяся на этот абонемент
					matsForSub := mats * float64(partQty) / float64(totalSubQty)

					// Ошибку можно залогировать, но не валить весь консумпшен
					_ = b.subs.AddMaterialsUsage(ctx, subID, matsForSub)
				}
			}
		}
	}

	pairs := make([][2]int64, 0, len(items))
	// позиции + списание
	for _, it := range items {
		matID := int64(it["mat_id"].(float64))
		q := int64(it["qty"].(float64))
		price, _ := b.materials.GetPrice(ctx, matID)
		cost := float64(q) * price

		// списание (разрешено уходить в минус)
		if err := b.inventory.Consume(ctx, u.ID, whID, matID, float64(q), "consumption"); err != nil {
			respond("Ошибка списания")
			return
		}
		_ = b.cons.AddItem(ctx, sid, matID, float64(q), price, cost)
		pairs = append(pairs, [2]int64{whID, matID})
	}

	// инвойс (pending)
	invoiceComment := comment
	if finalComment != "" {
		if invoiceComment != "" {
			invoiceComment += "\n"
		}
		invoiceComment += finalComment
	}

	_, err = b.cons.CreateInvoice(ctx, u.ID, sid, total, invoiceComment)
	if err != nil {
		respond("Не удалось создать счёт.")
		return
	}

	b.notifyLowOrNegativeBatch(ctx, pairs)
	// уведомление admin о подтверждённой сессии расхода/аренды
	// Важно: это уведомление отправляем только пользователям с ролью admin.
	// Роль administrator сюда не включаем.
	if admins, err := b.users.ListByRole(ctx, users.RoleAdmin, users.StatusApproved); err == nil && len(admins) > 0 {
		// кто подтвердил
		u, _ := b.users.GetByTelegramID(ctx, telegramID)

		// соберём удобочитаемый текст
		placeRU := map[string]string{"hall": "Зал", "cabinet": "Кабинет"}
		unitRU := map[string]string{"hour": "ч", "day": "дн"}
		var sb strings.Builder

		_, _ = fmt.Fprintf(&sb, "✅ Подтверждена сессия расхода/аренды\n")
		if u != nil {
			_, _ = fmt.Fprintf(&sb, "Мастер: %s (@%s, id %d)\n", strings.TrimSpace(u.Username), telegramUsername, telegramID)
		} else {
			_, _ = fmt.Fprintf(&sb, "Мастер: @%s (id %d)\n", telegramUsername, telegramID)
		}
		if isConsumptionStudioClient(payload) {
			_, _ = fmt.Fprintf(&sb, "Тип: студийный клиент\n")
		} else if noRent {
			_, _ = fmt.Fprintf(&sb, "Тип: без аренды\n")
		} else {
			_, _ = fmt.Fprintf(&sb, "Помещение: %s\nКол-во: %d %s\n", placeRU[place], qty, unitRU[unit])
		}
		if comment != "" {
			_, _ = fmt.Fprintf(&sb, "Комментарий: %s\n", comment)
		}

		if finalComment != "" {
			_, _ = fmt.Fprintf(&sb, "Комментарий мастера: %s\n", finalComment)
		}

		// материалы
		_, _ = fmt.Fprintf(&sb, "Материалы:\n")
		if len(items) == 0 {
			_, _ = fmt.Fprintf(&sb, "• Материалы не внесены\n")
		}
		var matsSum float64
		for _, it := range items {
			matID := int64(it["mat_id"].(float64))
			q := int64(it["qty"].(float64))
			name := fmt.Sprintf("ID:%d", matID)
			if m, _ := b.materials.GetByID(ctx, matID); m != nil { // repo уже есть
				name = materialDisplayName(m.Brand, m.Name)
			}
			price, _ := b.materials.GetPrice(ctx, matID)
			line := float64(q) * price
			matsSum += line
			_, _ = fmt.Fprintf(&sb, "• %s — %d × %.2f = %.2f ₽\n", name, q, price, line)
		}

		// финансы: округлённая сумма материалов, аренда, итого — у нас уже посчитаны
		if isConsumptionStudioClient(payload) {
			_, _ = fmt.Fprintf(&sb, "\nМатериалы: %.2f ₽\nАренда: студийный клиент %.2f ₽\nИтого: %.2f ₽",
				mats, rent, total)
		} else if noRent {
			_, _ = fmt.Fprintf(&sb, "\nМатериалы: %.2f ₽\nАренда: без аренды\nИтого: %.2f ₽",
				mats, total)
		} else {
			_, _ = fmt.Fprintf(&sb, "\nМатериалы (факт): %.2f ₽, округл.: %.2f ₽\nАренда: %.2f ₽\nИтого: %.2f ₽",
				mats, rounded, rent, total)
		}

		notificationText := sb.String()
		seen := map[int64]struct{}{}
		for _, admin := range admins {
			if admin == nil || admin.TelegramID == 0 {
				continue
			}
			if _, ok := seen[admin.TelegramID]; ok {
				continue
			}
			seen[admin.TelegramID] = struct{}{}
			b.send(tgbotapi.NewMessage(admin.TelegramID, notificationText))
		}
	} else if err != nil {
		b.log.Error("failed to load admins for consumption notification", "err", err)
	}

	// сообщение мастеру о завершении расчёта
	receiptText := b.buildConsumptionReceipt(ctx, payload, "✅ Сессия подтверждена.\n\nЧек:")
	respond(receiptText)

	// реквизиты оплаты отправляем отдельным сообщением после чека
	b.send(tgbotapi.NewMessage(chatID, strings.TrimSpace(consumptionPaymentInstructions)))

	_ = b.states.Set(ctx, chatID, dialog.StateIdle, dialog.Payload{})
}
