package payments

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Spok95/beauty-bot/internal/domain/consumption"
)

type Handler struct {
	log  *slog.Logger
	cons *consumption.Repo
}

func NewHandler(log *slog.Logger, cons *consumption.Repo) *Handler {
	return &Handler{
		log:  log,
		cons: cons,
	}
}

// ServeHTTP эмулирует "успешную оплату":
// /payments/pay?invoice=123 -> помечаем invoices.status='paid' и показываем простую HTML-страницу.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	invoiceStr := r.URL.Query().Get("invoice")
	if invoiceStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("missing invoice parameter"))
		return
	}

	invoiceID, err := strconv.ParseInt(invoiceStr, 10, 64)
	if err != nil || invoiceID <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid invoice parameter"))
		return
	}

	if err := h.cons.SetInvoiceStatus(ctx, invoiceID, "paid"); err != nil {
		h.log.Error("failed to mark invoice as paid",
			"invoice_id", invoiceID,
			"err", err,
		)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("failed to update invoice status"))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w,
		"<html><body><h1>Оплата прошла</h1><p>Инвойс #%d помечен как оплаченный.</p></body></html>",
		invoiceID,
	)
}
