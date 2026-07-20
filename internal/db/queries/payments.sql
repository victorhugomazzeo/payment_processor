-- name: CreatePayment :one
INSERT INTO payments 
(
    id, 
    merchant_id, 
    status, 
    created_at,
    amount,
    currency,
    card_token,
    card_last4,
    card_brand
) 
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, merchant_id, status, created_at, amount, currency, card_token, card_last4, card_brand;

-- name: GetPayment :one
SELECT
    id, 
    merchant_id, 
    status, 
    created_at,
    amount,
    currency,
    card_token,
    card_last4,
    card_brand
FROM
    payments
WHERE   
    id=$1;

-- name: UpdatePaymentStatus :exec
UPDATE payments SET status=$1 WHERE id=$2;