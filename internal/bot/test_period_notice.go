package bot

import (
	"context"
	"time"

	"github.com/Spok95/beauty-bot/internal/domain/users"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const testPeriodNoticeText = `Уведомление о тестовом периоде

Срок тестовой эксплуатации бота истекает 10.09.2026.
Для согласования завершения проекта и дальнейшей эксплуатации бота необходимо связаться с разработчиком.`

// RunTestPeriodAdminNotices sends a daily test-period notice to approved admins
// at 07:00 Moscow time from 04.09.2026 through 10.09.2026 inclusive.
func (b *Bot) RunTestPeriodAdminNotices(ctx context.Context) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		b.log.Error("test period notice timezone load failed", "err", err)
		return
	}

	start := time.Date(2026, time.September, 4, 7, 0, 0, 0, location)
	end := time.Date(2026, time.September, 10, 7, 0, 0, 0, location)

	for {
		next, ok := nextTestPeriodNotice(time.Now(), start, end)
		if !ok {
			b.log.Info("test period admin notices finished")
			return
		}

		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			b.sendTestPeriodAdminNotice(ctx)
		}
	}
}

func nextTestPeriodNotice(now, start, end time.Time) (time.Time, bool) {
	now = now.In(start.Location())
	if now.Before(start) {
		return start, true
	}
	if now.After(end) {
		return time.Time{}, false
	}

	candidate := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, start.Location())
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	if candidate.Before(start) {
		candidate = start
	}
	if candidate.After(end) {
		return time.Time{}, false
	}
	return candidate, true
}

func (b *Bot) sendTestPeriodAdminNotice(ctx context.Context) {
	admins, err := b.users.ListByRole(ctx, users.RoleAdmin, users.StatusApproved)
	if err != nil {
		b.log.Error("test period notice admin list failed", "err", err)
		return
	}

	sent := make(map[int64]struct{}, len(admins))
	for _, admin := range admins {
		if admin == nil || admin.TelegramID == 0 {
			continue
		}
		if _, exists := sent[admin.TelegramID]; exists {
			continue
		}
		sent[admin.TelegramID] = struct{}{}

		if _, err := b.api.Send(tgbotapi.NewMessage(admin.TelegramID, testPeriodNoticeText)); err != nil {
			b.log.Warn("test period notice send failed", "telegram_id", admin.TelegramID, "err", err)
		}
	}
}
