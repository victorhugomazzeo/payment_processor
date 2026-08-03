package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/victorhugomazzeo/payment-processor/internal/db"
	"github.com/victorhugomazzeo/payment-processor/internal/payment"
)

const idempotencyKeyHeader = "Idempotency-Key"

const uniqueViolationCode = "23505"

const retryAfterHeader = "Retry-After"

const retryAfterInSeconds = "10"

type idempotencyCtxKey struct{}

type Idempotency struct {
	q           *db.Queries
	now         func() time.Time
	claimMaxAge time.Duration
}

type responseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

var _ http.ResponseWriter = (*responseRecorder)(nil)

func NewIdempotency(q *db.Queries) *Idempotency {
	return &Idempotency{
		q: q,
		now: func() time.Time {
			return time.Now().UTC()
		},
		claimMaxAge: 60 * time.Second,
	}
}

func (i *Idempotency) Middleware(operation string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		key := r.Header.Get(idempotencyKeyHeader)
		if key == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "missing Idempotency-Key header",
			})
			return
		}

		merchantID, ok := MerchantFromContext(r.Context())

		if !ok {
			slog.Error("merchant missing from context: RequireMerchant not wired", "method", r.Method, "path", r.URL.Path)

			writeJSON(w, http.StatusInternalServerError, errorResponse{
				Error: "internal server error",
			})
			return
		}

		body, err := io.ReadAll(r.Body)

		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "invalid request body",
			})
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))

		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])

		err = i.q.CreateIdempotencyKey(r.Context(),
			db.CreateIdempotencyKeyParams{
				IdempotencyKey:       key,
				MerchantID:           merchantID,
				IdempotencyOperation: operation,
				RequestHash:          hash,
				CreatedAt:            i.now(),
			})

		if err != nil {

			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
				i.resolveRetry(w, r, merchantID, key, hash)
				return
			}

			slog.Error("error during creation of idempotency key", "error", err, "method", r.Method, "path", r.URL.Path)

			writeJSON(w, http.StatusInternalServerError, errorResponse{
				Error: "internal server error",
			})
			return
		}

		ctx := context.WithValue(r.Context(), idempotencyCtxKey{}, key)

		rec := &responseRecorder{}

		next.ServeHTTP(rec, r.WithContext(ctx))

		maps.Copy(w.Header(), rec.Header())

		statusCode := rec.status

		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		w.WriteHeader(statusCode)
		w.Write(rec.body.Bytes())
	})
}

func IdempotencyKeyFromContext(ctx context.Context) (string, bool) {

	v, ok := ctx.Value(idempotencyCtxKey{}).(string)

	return v, ok
}

// resolveRetry decides the response for a request whose idempotency key already exists.
// Each gap in the original request's lifecycle leaves a distinct signature in the claim row, so the row alone tells us where the original stopped.
func (i *Idempotency) resolveRetry(w http.ResponseWriter, r *http.Request, merchantID uuid.UUID, key, hash string) {
	k, err := i.q.GetIdempotencyKey(r.Context(), db.GetIdempotencyKeyParams{
		IdempotencyKey: key,
		MerchantID:     merchantID,
	})
	if err != nil {
		slog.Error("error during get of idempotency key", "error", err, "method", r.Method, "path", r.URL.Path)
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "internal server error",
		})
		return
	}

	// Universal gate: the same key with a different body is a client bug, never a retry.
	if k.RequestHash != hash {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
			Error: "Idempotency-Key reused with a different request body",
		})
		return
	}

	// A recorded response means the original request completed. Replay it verbatim: the stored bytes are already encoded JSON.
	if k.ResponseStatus.Valid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(k.ResponseStatus.Int32))
		w.Write(k.Response)
		return
	}

	// A payment without a recorded response means the original died between creating the payment and saving the response.
	// Rebuild the answer from the payment row itself.
	if k.PaymentID.Valid {
		p, err := i.q.GetPayment(r.Context(), k.PaymentID.UUID)
		if err != nil {
			slog.Error("error during get of payment", "error", err, "method", r.Method, "path", r.URL.Path)
			writeJSON(w, http.StatusInternalServerError, errorResponse{
				Error: "internal server error",
			})
			return
		}

		switch p.Status {

		case string(payment.StatusCreated):
			// The original died before the payment reached an outcome. The janitor will resolve it (created -> abandoned); ask the client to wait.
			w.Header().Set(retryAfterHeader, retryAfterInSeconds)
			writeJSON(w, http.StatusConflict, errorResponse{Error: "payment for this Idempotency-Key is still being resolved, retry later"})
			return

		case string(payment.StatusAuthorized):
			writeJSON(w, http.StatusCreated, toResponse(p))
			return

		case string(payment.StatusDenied):
			writeJSON(w, http.StatusPaymentRequired, toResponse(p))
			return

		default:
			slog.Error("invalid payment status", "status", p.Status, "method", r.Method, "path", r.URL.Path)
			writeJSON(w, http.StatusInternalServerError, errorResponse{
				Error: "internal server error",
			})
			return
		}
	}

	// Only the claim exists. Age decides: the 10s request lifetime cap guarantees no request is still alive after claimMaxAge.
	age := i.now().Sub(k.CreatedAt)

	if age <= i.claimMaxAge {
		w.Header().Set(retryAfterHeader, retryAfterInSeconds)
		writeJSON(w, http.StatusConflict, errorResponse{Error: "payment for this Idempotency-Key is still being resolved, retry later"})
		return
	}

	// Dead by construction: nothing was created and nothing will be. The key is burned; the client must retry with a fresh one
	writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: "stale Idempotency-Key: no payment was created, retry with a new key"})
}

func (rec *responseRecorder) Header() http.Header {

	if rec.header == nil {
		rec.header = make(http.Header)
	}

	return rec.header
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	return rec.body.Write(b)
}

func (rec *responseRecorder) WriteHeader(statusCode int) {
	rec.status = statusCode
}
