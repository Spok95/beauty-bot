package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/Spok95/beauty-bot/internal/dialog"
)

func (b *Bot) buildConsumptionReceipt(ctx context.Context, payload dialog.Payload, title string) string {
	place, _ := payload["place"].(string)
	unit, _ := payload["unit"].(string)

	qty := 0
	if v, ok := payload["qty"].(float64); ok {
		qty = int(v)
	}

	matsSum := 0.0
	if v, ok := payload["mats_sum"].(float64); ok {
		matsSum = v
	}

	matsRounded := 0.0
	if v, ok := payload["mats_rounded"].(float64); ok {
		matsRounded = v
	}

	rent := 0.0
	if v, ok := payload["rent"].(float64); ok {
		rent = v
	}

	total := 0.0
	if v, ok := payload["total"].(float64); ok {
		total = v
	}

	withSub := false
	if v, ok := payload["with_sub"].(bool); ok {
		withSub = v
	}

	comment := ""
	if v, ok := payload["comment"].(string); ok {
		comment = strings.TrimSpace(v)
	}

	placeRU := map[string]string{
		"hall":    "Зал",
		"cabinet": "Кабинет",
	}
	unitRU := map[string]string{
		"hour": "ч",
		"day":  "дн",
	}

	var lines []string

	if title != "" {
		lines = append(lines, title)
		lines = append(lines, "")
	}

	lines = append(lines, "Параметры записи:")
	lines = append(lines, fmt.Sprintf("• Помещение: %s", placeRU[place]))
	lines = append(lines, fmt.Sprintf("• Количество: %d %s", qty, unitRU[unit]))

	if comment != "" {
		lines = append(lines, fmt.Sprintf("• Комментарий: %s", comment))
	}

	if withSub {
		lines = append(lines, "• Абонемент: да")
	} else {
		lines = append(lines, "• Абонемент: нет")
	}

	lines = append(lines, "")
	lines = append(lines, "Материалы:")

	items := b.consParseItems(payload["items"])
	if len(items) == 0 {
		lines = append(lines, "• Материалы не внесены")
	} else {
		for _, it := range items {
			matID := int64(it["mat_id"].(float64))
			q := int64(it["qty"].(float64))

			name := fmt.Sprintf("ID:%d", matID)
			unitLabel := "ед."

			if m, _ := b.materials.GetByID(ctx, matID); m != nil {
				name = materialDisplayName(m.Brand, m.Name)
				unitLabel = materialUnitLabel(string(m.Unit))
			}

			price, _ := b.materials.GetPrice(ctx, matID)
			line := float64(q) * price

			lines = append(lines,
				fmt.Sprintf("• %s — %d %s × %.2f ₽ = %.2f ₽", name, q, unitLabel, price, line),
			)
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Аренда:")

	rentParts := parseRentParts(payload["rent_parts"])
	if len(rentParts) == 0 {
		lines = append(lines, fmt.Sprintf("• Аренда всего: %.2f ₽", rent))
	} else {
		for _, part := range rentParts {
			partQty := payloadInt(part, "qty")
			partRent := payloadFloat(part, "rent")
			need := payloadFloat(part, "need")
			materialsUsed := payloadFloat(part, "materials_used")
			thresholdMet := payloadBool(part, "threshold_met")
			partWithSub := payloadBool(part, "with_sub")
			tariff := payloadString(part, "tariff")

			label := "без абонемента"
			if partWithSub {
				planLimit := payloadInt(part, "plan_limit")
				if planLimit > 0 {
					label = fmt.Sprintf("по абонементу на %d %s", planLimit, unitRU[unit])
				} else {
					label = "по абонементу"
				}
			}

			condition := "условие по материалам не выполнено"
			if thresholdMet {
				condition = "условие по материалам выполнено"
			}
			if partWithSub {
				condition = "оплачено абонементом"
				partRent = 0
			}

			line := fmt.Sprintf(
				"• %d %s %s — %.2f ₽",
				partQty,
				unitRU[unit],
				label,
				partRent,
			)

			details := []string{}

			if !partWithSub {
				if tariff != "" {
					details = append(details, tariff)
				}
				details = append(details, condition)

				if need > 0 {
					details = append(details, fmt.Sprintf("порог %.0f ₽", need))
				}
				if materialsUsed > 0 {
					details = append(details, fmt.Sprintf("зачтено %.0f ₽", materialsUsed))
				}
			} else {
				details = append(details, "абонемент")
			}

			if len(details) > 0 {
				line += " (" + strings.Join(details, "; ") + ")"
			}

			lines = append(lines, line)
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Итого:")
	lines = append(lines, fmt.Sprintf("• Материалы: %.2f ₽", matsSum))

	if matsRounded != matsSum {
		lines = append(lines, fmt.Sprintf("• В зачёт аренды: %.2f ₽", matsRounded))
	}
	lines = append(lines, fmt.Sprintf("• Аренда: %.2f ₽", rent))
	lines = append(lines, fmt.Sprintf("• Всего к оплате: %.2f ₽", total))

	if warn := buildSubscriptionReceiptWarning(payload); warn != "" {
		return warn + "\n\n" + strings.Join(lines, "\n")
	}

	return strings.Join(lines, "\n")
}

func materialUnitLabel(unit string) string {
	switch unit {
	case "g":
		return "г"
	case "ml":
		return "мл"
	case "pcs":
		return "шт"
	default:
		return unit
	}
}

func parseRentParts(v any) []map[string]any {
	if v == nil {
		return nil
	}

	if parts, ok := v.([]map[string]any); ok {
		return parts
	}

	rawParts, ok := v.([]any)
	if !ok {
		return nil
	}

	out := make([]map[string]any, 0, len(rawParts))
	for _, raw := range rawParts {
		if part, ok := raw.(map[string]any); ok {
			out = append(out, part)
		}
	}

	return out
}

func payloadString(payload map[string]any, key string) string {
	v, _ := payload[key].(string)
	return v
}

func payloadBool(payload map[string]any, key string) bool {
	v, _ := payload[key].(bool)
	return v
}

func payloadFloat(payload map[string]any, key string) float64 {
	v, _ := payload[key].(float64)
	return v
}

func payloadInt(payload map[string]any, key string) int {
	switch v := payload[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func buildSubscriptionReceiptWarning(payload dialog.Payload) string {
	withSub := false
	if v, ok := payload["with_sub"].(bool); ok {
		withSub = v
	}
	if !withSub {
		return ""
	}

	unit, _ := payload["unit"].(string)
	unitRU := map[string]string{
		"hour": "ч",
		"day":  "дн",
	}

	var subQty int
	var noSubQty int

	for _, part := range parseRentParts(payload["rent_parts"]) {
		qty := payloadInt(part, "qty")
		if payloadBool(part, "with_sub") {
			subQty += qty
		} else {
			noSubQty += qty
		}
	}

	if noSubQty <= 0 {
		return ""
	}

	return fmt.Sprintf(
		"⚠️ Обратите внимание: абонемента хватает на %d %s, ещё %d %s считаются по тарифу без абонемента.\n\n"+
			"Вы можете:\n"+
			"• вернуться и сначала купить новый абонемент, затем ещё раз посчитать;\n"+
			"• подтвердить текущую сводку и оплатить как есть.",
		subQty,
		unitRU[unit],
		noSubQty,
		unitRU[unit],
	)
}
