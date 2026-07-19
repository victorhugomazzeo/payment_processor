-- name: CreateMerchant :one
INSERT INTO merchants (id, name, fee_bps, created_at) 
VALUES ($1, $2, $3, $4)
RETURNING id, name, fee_bps, created_at;

-- name: GetMerchant :one
SELECT 
    id,
    name,
    fee_bps,
    created_at
FROM    
    merchants
WHERE   
    merchants.id=$1;