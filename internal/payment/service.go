package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
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
	MerchantID  uuid.UUID
	CardToken   string
	CardLast4   string
	CardBrand   string
	AmountCents int64
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

	tx1, err := s.pool.Begin(ctx)
	if err != nil {

		return db.Payment{}, fmt.Errorf("creating transaction: %w", err)
	}

	defer tx1.Rollback(ctx)

	queries := s.queries.WithTx(tx1)

	createdPayment, err := s.persistNewPayment(ctx, queries, args)

	if err != nil {
		return db.Payment{}, err
	}

	if err := tx1.Commit(ctx); err != nil {
		return db.Payment{}, fmt.Errorf("performing commit: %w", err)
	}

	base := context.WithoutCancel(ctx)

	authCtx, cancelAuthCtx := context.WithTimeout(base, s.authTimeout)
	defer cancelAuthCtx()

	authResult, authErr := s.proc.Authorize(authCtx, args.CardToken, args.AmountCents)

	ctx2, cancel := context.WithTimeout(base, s.outcomeTimeout)
	defer cancel()

	if authErr != nil {

		slog.Error("authorize failed", "payment_id", createdPayment.ID, "error", authErr)

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

		err = s.queries.CreatePaymentEvent(ctx2, db.CreatePaymentEventParams{
			ID:           paymentEventID,
			PaymentID:    createdPayment.ID,
			FromStatus:   string(StatusCreated),
			ToStatus:     string(StatusCreated),
			EventType:    string(EventTypePaymentError),
			EventDetails: errorDetails,
			CreatedAt:    s.now(),
		})

		if err != nil {
			slog.Error("recording error event failed", "payment_id", createdPayment.ID, "error", err)

			return db.Payment{}, fmt.Errorf("creating payment error event: %w", err)
		}

		return db.Payment{}, fmt.Errorf("authorizing payment: %w", authErr)
	}

	tx2, err := s.pool.Begin(ctx2)
	if err != nil {

		slog.Error("recording error begin transaction failed", "payment_id", createdPayment.ID, "error", err, "authorized", authResult.Authorized, "return_code", authResult.ReturnCode)
		return db.Payment{}, fmt.Errorf("creating transaction: %w", err)
	}

	defer tx2.Rollback(ctx2)

	queries = s.queries.WithTx(tx2)

	updatedPayment, err := s.persistProcessedPayment(ctx2, createdPayment.ID, queries, authResult)
	if err != nil {

		slog.Error("persist payment error", "payment_id", createdPayment.ID, "error", err, "authorized", authResult.Authorized, "return_code", authResult.ReturnCode)
		return db.Payment{}, err
	}

	if err := tx2.Commit(ctx2); err != nil {

		slog.Error("commit error", "payment_id", createdPayment.ID, "error", err, "authorized", authResult.Authorized, "return_code", authResult.ReturnCode)
		return db.Payment{}, fmt.Errorf("performing commit: %w", err)
	}

	return updatedPayment, nil
}

func (s *Service) persistNewPayment(ctx context.Context, queries *db.Queries, args CreatePaymentArgs) (db.Payment, error) {

	paymentID, err := uuid.NewV7()
	if err != nil {
		return db.Payment{}, fmt.Errorf("creating paymentId uuidv7: %w", err)
	}

	now := s.now()

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

	return payment, nil
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
