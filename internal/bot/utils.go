package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Spok95/beauty-bot/internal/dialog"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

/*** HELPERS ***/

func (b *Bot) answerCallback(cb *tgbotapi.CallbackQuery, text string, alert bool) error {
	resp := tgbotapi.NewCallback(cb.ID, text)
	resp.ShowAlert = alert
	_, err := b.api.Request(resp)
	return err
}

func (b *Bot) consParseItems(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		if mm, ok2 := v.([]map[string]any); ok2 {
			return mm
		}
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// parseSupItems достаёт []map[string]any из payload["items"]
func (b *Bot) parseSupItems(v any) []map[string]any {
	items := []map[string]any{}
	arr, ok := v.([]any)
	if !ok {
		if mm, ok2 := v.([]map[string]any); ok2 {
			return mm
		}
		return items
	}
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

// clearPrevStep убрать inline-кнопки у прошлого шага, если он был
func (b *Bot) clearPrevStep(ctx context.Context, chatID int64) {
	st, _ := b.states.Get(ctx, chatID)
	if st == nil || st.Payload == nil {
		return
	}
	if v, ok := st.Payload["last_mid"]; ok {
		mid := int(v.(float64)) // payload хранится через JSON
		// просто чистим markup, текст оставляем как есть
		rm := tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}}
		b.send(tgbotapi.NewEditMessageReplyMarkup(chatID, mid, rm))
	}
}

// saveLastStep сохранить id текущего бот-сообщения как «последний»
func (b *Bot) saveLastStep(ctx context.Context, chatID int64, nextState dialog.State, payload dialog.Payload, newMID int) {
	if payload == nil {
		payload = dialog.Payload{}
	}
	payload["last_mid"] = float64(newMID)
	_ = b.states.Set(ctx, chatID, nextState, payload)
}

func (b *Bot) send(msg tgbotapi.Chattable) {
	if _, err := b.api.Send(msg); err != nil {
		b.log.Error("send failed", "err", err)
	}
}

// downloadTelegramFile скачивает файл по FileID через Telegram API.
func (b *Bot) downloadTelegramFile(fileID string) ([]byte, error) {
	url, err := b.api.GetFileDirectURL(fileID)
	if err != nil {
		return nil, fmt.Errorf("get file url: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram returned status %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

func (b *Bot) editTextAndClear(chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageTextAndMarkup(
		chatID, messageID, text,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}},
	)
	b.send(edit)
}

func (b *Bot) editTextWithNav(chatID int64, messageID int, text string) {
	kb := navKeyboard(true, true)
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, messageID, text, kb)
	b.send(edit)
}

// Бейдж активности
func badge(b bool) string {
	if b {
		return "🟢"
	}
	return "🚫"
}

func materialDisplayName(brand, name string) string {
	brand = strings.TrimSpace(brand)
	name = strings.TrimSpace(name)

	if brand == "" {
		return name
	}
	if name == "" {
		return brand
	}

	lowerBrand := strings.ToLower(brand)
	lowerName := strings.ToLower(name)

	if strings.HasPrefix(lowerName, lowerBrand+" ") {
		return name
	}

	return brand + " " + name
}
