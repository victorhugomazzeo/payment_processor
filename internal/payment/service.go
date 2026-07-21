package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/victorhugomazzeo/payment-processor/internal/db"
	"github.com/victorhugomazzeo/payment-processor/internal/processor"
)

type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	proc    *processor.Dummy
	now     func() time.Time
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
		pool:    pool,
		queries: queries,
		proc:    proc,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) CreatePayment(ctx context.Context, args CreatePaymentArgs) (db.Payment, error) {

	tx, err := s.pool.Begin(ctx)
	if err != nil {

		return db.Payment{}, fmt.Errorf("creating transaction: %w", err)
	}

	defer tx.Rollback(ctx)

	q := s.queries.WithTx(tx)

	paymentID, err := uuid.NewV7()
	if err != nil {
		return db.Payment{}, fmt.Errorf("creating paymentId uuidv7: %w", err)
	}

	now := s.now()

	payment, err := q.CreatePayment(ctx, db.CreatePaymentParams{
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

	err = q.CreatePaymentEvent(ctx, db.CreatePaymentEventParams{
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

	if err := tx.Commit(ctx); err != nil {
		return db.Payment{}, fmt.Errorf("performing commit: %w", err)
	}

	return payment, nil
}
