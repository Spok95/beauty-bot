package bot

import (
	"context"

	"github.com/Spok95/beauty-bot/internal/dialog"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) showCurrentConsumptionCheck(ctx context.Context, chatID int64, telegramID int64) {
	st, _ := b.states.Get(ctx, chatID)
	if st == nil || st.Payload == nil {
		b.send(tgbotapi.NewMessage(chatID, "Текущего чека нет. Начните через «Расход/Аренда»."))
		return
	}

	payload := st.Payload

	if !hasConsumptionDraft(payload) {
		b.send(tgbotapi.NewMessage(chatID, "Текущего чека нет. Начните через «Расход/Аренда»."))
		return
	}

	switch st.State {
	case dialog.StateConsSummary:
		b.showConsumptionReceiptForConfirm(ctx, chatID, 0, telegramID, payload)

	case dialog.StateConsFinalComment:
		msg := tgbotapi.NewMessage(chatID,
			"Сначала введите комментарий мастера или нажмите «Пропустить» в предыдущем сообщении.")
		b.send(msg)

	default:
		items := b.consParseItems(payload["items"])

		_ = b.states.Set(ctx, chatID, dialog.StateConsCart, payload)

		b.showConsCart(
			ctx,
			chatID,
			nil,
			payload["place"].(string),
			payload["unit"].(string),
			payloadInt(payload, "qty"),
			items,
		)
	}
}

func hasConsumptionDraft(payload dialog.Payload) bool {
	if payload == nil {
		return false
	}

	if _, ok := payload["place"].(string); !ok {
		return false
	}

	if _, ok := payload["unit"].(string); !ok {
		return false
	}

	if _, ok := payload["qty"].(float64); !ok {
		return false
	}

	items := parsePayloadItems(payload["items"])
	return len(items) > 0
}

func parsePayloadItems(v any) []map[string]any {
	if v == nil {
		return nil
	}

	if items, ok := v.([]map[string]any); ok {
		return items
	}

	rawItems, ok := v.([]any)
	if !ok {
		return nil
	}

	out := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item, ok := raw.(map[string]any); ok {
			out = append(out, item)
		}
	}

	return out
}
