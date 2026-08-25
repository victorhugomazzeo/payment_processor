package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

const merchantIDHeader = "Merchant-Id"

const merchantNotFoundMessage = "Merchant-Id not found"

type merchantCtxKey struct{}

func MerchantFromContext(ctx context.Context) (uuid.UUID, bool) {

	v, ok := ctx.Value(merchantCtxKey{}).(uuid.UUID)

	return v, ok
}

func RequireMerchant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		m := r.Header.Get(merchantIDHeader)
		if m == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "missing Merchant-Id header",
			})
			return
		}

		id, err := uuid.Parse(m)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "invalid Merchant-Id header",
			})
			return
		}

		ctx := context.WithValue(r.Context(), merchantCtxKey{}, id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
