package bot

import (
	"context"
	"fmt"

	"github.com/Spok95/beauty-bot/internal/dialog"
	"github.com/Spok95/beauty-bot/internal/domain/consumption"
)

func (b *Bot) calculateConsumptionReceiptPayload(
	ctx context.Context,
	telegramID int64,
	payload dialog.Payload,
) (dialog.Payload, string, error) {
	if payload == nil {
		return nil, "Сессия устарела. Начните заново через кнопку «Расход/Аренда».", fmt.Errorf("empty payload")
	}

	placeRaw, okP := payload["place"]
	unitRaw, okU := payload["unit"]
	qtyRaw, okQ := payload["qty"]
	itemsRaw, okItems := payload["items"]
	if !okP || !okU || !okQ || !okItems {
		return nil, "Эта корзина уже неактуальна. Начните новую сессию через меню «Расход/Аренда».", fmt.Errorf("missing payload fields")
	}

	place, ok1 := placeRaw.(string)
	unit, ok2 := unitRaw.(string)
	qtyF, ok3 := qtyRaw.(float64)
	if !ok1 || !ok2 || !ok3 {
		return nil, "Эта корзина уже неактуальна. Начните новую сессию через меню «Расход/Аренда».", fmt.Errorf("invalid payload fields")
	}

	qty := int(qtyF)
	items := b.consParseItems(itemsRaw)

	var mats float64
	for _, it := range items {
		matID := int64(it["mat_id"].(float64))
		q := int64(it["qty"].(float64))
		price, _ := b.materials.GetPrice(ctx, matID)
		mats += float64(q) * price
	}

	u, _ := b.users.GetByTelegramID(ctx, telegramID)

	var metas []rentPartMeta
	if u != nil {
		metas, _ = b.splitQtyBySubscriptions(ctx, u.ID, place, unit, qty)
	}

	if len(metas) == 0 {
		metas = []rentPartMeta{{
			WithSub:   false,
			Qty:       qty,
			SubID:     0,
			PlanLimit: 0,
		}}
	}

	withSub := false
	for _, m := range metas {
		if m.WithSub {
			withSub = true
			break
		}
	}

	parts := make([]consumption.RentSplitPartInput, 0, len(metas))
	for _, m := range metas {
		p := consumption.RentSplitPartInput{
			WithSub: m.WithSub,
			Qty:     m.Qty,
		}

		if m.WithSub && m.PlanLimit > 0 {
			p.SubLimitForPricing = m.PlanLimit
		} else {
			p.SubLimitForPricing = m.Qty
		}

		parts = append(parts, p)
	}

	calcRent, rounded, needTotal, partResults, err := b.cons.ComputeRentSplit(ctx, place, unit, mats, parts)
	if err != nil || len(partResults) == 0 {
		text := fmt.Sprintf(
			"⚠️ Нет активных тарифов для: %s / %s (%s). Настройте тарифы.",
			map[string]string{"hall": "Зал", "cabinet": "Кабинет"}[place],
			map[string]string{"hour": "час", "day": "день"}[unit],
			map[bool]string{true: "с абонементом", false: "без абонемента"}[withSub],
		)
		return nil, text, fmt.Errorf("rent split failed")
	}

	rentToPay := 0.0
	for i, pr := range partResults {
		if !metas[i].WithSub {
			rentToPay += pr.Rent
		}
	}

	total := mats + rentToPay

	payload["with_sub"] = withSub
	payload["mats_sum"] = mats
	payload["mats_rounded"] = rounded
	payload["need_total"] = needTotal
	payload["rent_calc"] = calcRent
	payload["rent"] = rentToPay
	payload["total"] = total

	partsPayload := make([]map[string]any, 0, len(partResults))
	for i, pr := range partResults {
		m := metas[i]

		mp := map[string]any{
			"with_sub":       m.WithSub,
			"qty":            m.Qty,
			"rent":           pr.Rent,
			"tariff":         pr.Tariff,
			"need":           pr.Need,
			"materials_used": pr.MaterialsUsed,
			"threshold_met":  pr.ThresholdMet,
		}

		if m.WithSub {
			mp["sub_id"] = m.SubID
			mp["plan_limit"] = m.PlanLimit
		}

		partsPayload = append(partsPayload, mp)
	}

	payload["rent_parts"] = partsPayload

	return payload, "", nil
}
