package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/victorhugomazzeo/payment-processor/internal/db"
	"github.com/victorhugomazzeo/payment-processor/internal/processor"
)

type Service struct {
	pool           *pgxpool.Pool
	queries        *db.Queries
	proc           *processor.Dummy
	now            func() time.Time
	authTimeout    time.Duration
	outcomeTimeout time.Duration
}

type CreatePaymentArgs struct {
	MerchantID     uuid.UUID
	CardToken      string
	CardLast4      string
	CardBrand      string
	AmountCents    int64
	IdempotencyKey string
}

func NewService(pool *pgxpool.Pool, queries *db.Queries, proc *processor.Dummy) *Service {
	return &Service{
		pool:           pool,
		queries:        queries,
		proc:           proc,
		now:            func() time.Time { return time.Now().UTC() },
		authTimeout:    time.Second,
		outcomeTimeout: 3 * time.Second,
	}
}

func (s *Service) CreatePayment(ctx context.Context, args CreatePaymentArgs) (db.Payment, error) {

	trx, err := s.pool.Begin(ctx)
	if err != nil {

		return db.Payment{}, fmt.Errorf("creating transaction: %w", err)
	}

	defer trx.Rollback(ctx)

	paymentID, err := uuid.NewV7()

	if err != nil {
		return db.Payment{}, fmt.Errorf("creating paymentId uuidv7: %w", err)
	}

	now := s.now()

	queries := s.queries.WithTx(trx)

	payment, err := queries.CreatePayment(ctx, db.CreatePaymentParams{
		ID:         paymentID,
		MerchantID: args.MerchantID,
		Status:     string(StatusCreated),
		CreatedAt:  now,
		Amount:     args.AmountCents,
		Currency:   string(CurrencyBRL),
		CardToken:  args.CardToken,
		CardLast4:  args.CardLast4,
		CardBrand:  args.CardBrand,
	})

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == fkViolationCode {
			return db.Payment{}, ErrMerchantNotFound
		}
		return db.Payment{}, fmt.Errorf("creating payment: %w", err)
	}

	paymentEventID, err := uuid.NewV7()
	if err != nil {
		return db.Payment{}, fmt.Errorf("creating paymentEventId uuidv7: %w", err)
	}

	err = queries.CreatePaymentEvent(ctx, db.CreatePaymentEventParams{
		ID:           paymentEventID,
		PaymentID:    paymentID,
		FromStatus:   string(StatusCreated),
		ToStatus:     string(StatusCreated),
		EventType:    string(EventTypePaymentCreated),
		EventDetails: nil,
		CreatedAt:    now,
	})

	if err != nil {
		return db.Payment{}, fmt.Errorf("creating paymentEvent: %w", err)
	}

	err = queries.UpdateIdempotencyKeyPaymentID(ctx, db.UpdateIdempotencyKeyPaymentIDParams{
		PaymentID:      uuid.NullUUID{UUID: paymentID, Valid: true},
		IdempotencyKey: args.IdempotencyKey,
		MerchantID:     args.MerchantID,
	})

	if err != nil {
		return db.Payment{}, fmt.Errorf("updating Idempotence key: %w", err)
	}

	if err := trx.Commit(ctx); err != nil {

		return db.Payment{}, fmt.Errorf("performing commit: %w", err)
	}

	payment, err = s.processPayment(ctx, payment)
	if err != nil {
		return db.Payment{}, err
	}

	return payment, nil
}

func (s *Service) processPayment(ctx context.Context, payment db.Payment) (db.Payment, error) {

	baseCtx := context.WithoutCancel(ctx)

	authCtx, cancelAuthCtx := context.WithTimeout(baseCtx, s.authTimeout)
	defer cancelAuthCtx()

	authResult, authErr := s.proc.Authorize(authCtx, payment.CardToken, payment.Amount)

	ctx, cancel := context.WithTimeout(baseCtx, s.outcomeTimeout)

	defer cancel()

	if authErr != nil {

		slog.Error("authorize failed", "payment_id", payment.ID, "error", authErr)

		paymentEventID, err := uuid.NewV7()
		if err != nil {
			return db.Payment{}, fmt.Errorf("creating paymentEventId uuidv7: %w", err)
		}

		errorDetails, err := json.Marshal(EventErrorDetails{
			Error: authErr.Error(),
		})
		if err != nil {
			return db.Payment{}, fmt.Errorf("marshal event details: %w", err)
		}

		err = s.queries.CreatePaymentEvent(ctx, db.CreatePaymentEventParams{
			ID:           paymentEventID,
			PaymentID:    payment.ID,
			FromStatus:   string(StatusCreated),
			ToStatus:     string(StatusCreated),
			EventType:    string(EventTypePaymentError),
			EventDetails: errorDetails,
			CreatedAt:    s.now(),
		})

		if err != nil {
			slog.Error("recording error event failed", "payment_id", payment.ID, "error", err)

			return db.Payment{}, fmt.Errorf("creating payment error event: %w", err)
		}

		return db.Payment{}, fmt.Errorf("authorizing payment: %w", authErr)
	}

	trx, err := s.pool.Begin(ctx)
	if err != nil {

		slog.Error("recording error begin transaction failed", "payment_id", payment.ID, "error", err, "authorized", authResult.Authorized, "return_code", authResult.ReturnCode)
		return db.Payment{}, fmt.Errorf("creating transaction: %w", err)
	}

	defer trx.Rollback(ctx)

	queries := s.queries.WithTx(trx)

	processedPayment, err := s.persistProcessedPayment(ctx, payment.ID, queries, authResult)

	if err != nil {

		slog.Error("persist payment error", "payment_id", payment.ID, "error", err, "authorized", authResult.Authorized, "return_code", authResult.ReturnCode)
		return db.Payment{}, err
	}

	if err := trx.Commit(ctx); err != nil {

		slog.Error("commit error", "payment_id", payment.ID, "error", err, "authorized", authResult.Authorized, "return_code", authResult.ReturnCode)
		return db.Payment{}, fmt.Errorf("performing commit: %w", err)
	}

	return processedPayment, nil
}

func (s *Service) persistProcessedPayment(ctx context.Context, paymentID uuid.UUID, queries *db.Queries, authResult processor.AuthorizeResult) (db.Payment, error) {

	toStatus := StatusDenied
	eventType := EventTypePaymentDenied

	if authResult.Authorized {
		toStatus = StatusAuthorized
		eventType = EventTypePaymentAuthorized
	}

	payment, err := queries.UpdatePaymentStatus(ctx, db.UpdatePaymentStatusParams{
		Status: string(toStatus),
		ID:     paymentID,
	})
	if err != nil {
		return db.Payment{}, fmt.Errorf("updating payment: %w", err)
	}

	details, err := json.Marshal(EventProcessedDetails{
		ReturnCode:    authResult.ReturnCode,
		ReturnMessage: authResult.ReturnMessage,
	})
	if err != nil {
		return db.Payment{}, fmt.Errorf("marshal event details: %w", err)
	}

	paymentEventID, err := uuid.NewV7()
	if err != nil {
		return db.Payment{}, fmt.Errorf("creating paymentEventId uuidv7: %w", err)
	}

	err = queries.CreatePaymentEvent(ctx, db.CreatePaymentEventParams{
		ID:           paymentEventID,
		PaymentID:    paymentID,
		FromStatus:   string(StatusCreated),
		ToStatus:     string(toStatus),
		EventType:    string(eventType),
		EventDetails: details,
		CreatedAt:    s.now(),
	})
	if err != nil {
		return db.Payment{}, fmt.Errorf("creating paymentEvent: %w", err)
	}

	return payment, nil

}
