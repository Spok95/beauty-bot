package payments

import (
	"context"
	"fmt"
	"strings"
)

type Service struct {
	baseURL string
}

func NewService(baseURL string) *Service {
	return &Service{baseURL: strings.TrimRight(baseURL, "/")}
}

// PaymentURL строит ссылку на оплату инвойса.
// В тестовом варианте это просто наш же HTTP-сервер.
func (s *Service) PaymentURL(invoiceID int64) string {
	return fmt.Sprintf("%s/payments/pay?invoice=%d", s.baseURL, invoiceID)
}

// CreatePayment — интерфейс на будущее, сейчас просто строит URL.
func (s *Service) CreatePayment(
	_ context.Context,
	invoiceID int64,
	amount float64,
	description string,
) (string, error) {
	// amount и description пока никуда не ходят,
	// но сигнатура пригодится для реальной интеграции.
	_ = amount
	_ = description

	return s.PaymentURL(invoiceID), nil
}
