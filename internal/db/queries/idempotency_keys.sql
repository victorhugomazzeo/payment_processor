-- name: CreateIdempotencyKey :exec
INSERT INTO idempotency_keys 
(
    idempotency_key, 
    merchant_id, 
    idempotency_operation, 
    request_hash,
    created_at
) 
VALUES ($1, $2, $3, $4, $5);

-- name: GetIdempotencyKey :one
SELECT
    idempotency_key, 
    merchant_id, 
    payment_id,
    idempotency_operation, 
    request_hash,
    response_status,
    response,
    created_at
FROM
    idempotency_keys
WHERE   
    idempotency_key=$1 AND 
    merchant_id=$2;

-- name: UpdateIdempotencyKeyPaymentID :exec
UPDATE
    idempotency_keys
SET
    payment_id=$1
WHERE
    idempotency_key=$2 AND 
    merchant_id=$3;

-- name: UpdateIdempotencyKeyResponse :exec
UPDATE
    idempotency_keys
SET
    response_status=$1,
    response=$2
WHERE
    idempotency_key=$3 AND 
    merchant_id=$4;