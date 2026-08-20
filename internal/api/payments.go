package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/victorhugomazzeo/payment-processor/internal/db"
	"github.com/victorhugomazzeo/payment-processor/internal/payment"
)

type CreatePaymentRequest struct {
	CardToken   string `json:"card_token"`
	CardLast4   string `json:"card_last4"`
	CardBrand   string `json:"card_brand"`
	AmountCents int64  `json:"amount_cents"`
}

type CreatePaymentResponse struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	AmountCents int64     `json:"amount_cents"`
}

type Handler struct {
	svc *payment.Service
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(svc *payment.Service) *Handler {
	return &Handler{
		svc: svc,
	}
}

func (h *Handler) CreatePayment(w http.ResponseWriter, r *http.Request) {

	id, ok := MerchantFromContext(r.Context())
	if !ok {
		slog.Error("merchant missing from context: RequireMerchant not wired", "method", r.Method, "path", r.URL.Path)

		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "internal server error",
		})
		return
	}

	var req CreatePaymentRequest
	err := json.NewDecoder(r.Body).Decode(&req)

	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid request body",
		})
		return
	}

	idempotencyKey, valid := IdempotencyKeyFromContext(r.Context())
	if !valid {

		slog.Error("idempotency key missing from context: Idempotency.Middleware not wired", "method", r.Method, "path", r.URL.Path)

		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "internal server error",
		})
		return
	}

	svcArgs, err := req.ToServiceArgs(id, idempotencyKey)

	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	p, err := h.svc.CreatePayment(r.Context(), svcArgs)

	if err != nil {

		if errors.Is(err, payment.ErrMerchantNotFound) {
			writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
				Error: "merchant_id not found",
			})
			return
		}

		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "internal server error",
		})
		return
	}

	res := toResponse(p)

	switch p.Status {

	case string(payment.StatusAuthorized):
		writeJSON(w, http.StatusCreated, res)

	case string(payment.StatusDenied):
		writeJSON(w, http.StatusPaymentRequired, res)

	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "internal server error",
		})
		slog.Error("invalid payment status", "status", p.Status)
		return
	}

}

func writeJSON(w http.ResponseWriter, code int, v any) {

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	if v != nil {
		err := json.NewEncoder(w).Encode(v)
		if err != nil {
			slog.Error("error during encoding", "error", err)
		}
	}
}

func (r CreatePaymentRequest) ToServiceArgs(merchantID uuid.UUID, idempotencyKey string) (payment.CreatePaymentArgs, error) {

	if r.CardToken == "" {
		return payment.CreatePaymentArgs{}, errors.New("invalid card_token")
	}

	hasNonDigit := strings.ContainsFunc(r.CardLast4, func(c rune) bool {
		return c < '0' || c > '9'
	})

	if len(r.CardLast4) != 4 || hasNonDigit {
		return payment.CreatePaymentArgs{}, errors.New("invalid card_last4")
	}
	if r.CardBrand == "" {
		return payment.CreatePaymentArgs{}, errors.New("invalid card_brand")
	}
	if r.AmountCents <= 0 {
		return payment.CreatePaymentArgs{}, errors.New("invalid amount_cents")
	}

	return payment.CreatePaymentArgs{
		MerchantID:     merchantID,
		CardToken:      r.CardToken,
		CardLast4:      r.CardLast4,
		CardBrand:      r.CardBrand,
		AmountCents:    r.AmountCents,
		IdempotencyKey: idempotencyKey,
	}, nil
}

func toResponse(p db.Payment) CreatePaymentResponse {
	return CreatePaymentResponse{
		ID:          p.ID.String(),
		Status:      p.Status,
		CreatedAt:   p.CreatedAt,
		AmountCents: p.Amount,
	}
}
