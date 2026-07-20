-- name: CreatePaymentEvent :exec
INSERT INTO payment_events
(
    id, 
    payment_id, 
    from_status, 
    to_status,
    event_type,
    event_details,
    created_at
) 
VALUES ($1, $2, $3, $4, $5, $6, $7);