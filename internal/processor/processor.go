package processor

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

type AuthorizeResult struct {
	Authorized    bool
	ReturnCode    string
	ReturnMessage string
}

type Dummy struct {
}

func NewDummy() *Dummy {
	return &Dummy{}
}

func (m *Dummy) Authorize(ctx context.Context, cardToken string, amountCents int64) (AuthorizeResult, error) {

	latency := time.Duration(50+rand.IntN(151)) * time.Millisecond // dado 1: quanto DEMORA (50–200ms)

	select {
	case <-time.After(latency):
		outcome := rand.IntN(100)

		if outcome <= 79 {
			return AuthorizeResult{Authorized: true, ReturnCode: "00"}, nil
		}

		if outcome <= 94 {
			return AuthorizeResult{Authorized: false, ReturnCode: "51", ReturnMessage: "insufficient funds"}, nil
		}

		return AuthorizeResult{}, errors.New("processor unavailable")

	case <-ctx.Done():
		return AuthorizeResult{}, ctx.Err()
	}

}
