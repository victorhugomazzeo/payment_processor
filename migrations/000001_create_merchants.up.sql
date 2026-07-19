CREATE DOMAIN payment_status AS text
    CHECK (VALUE IN ('created','authorized','captured','settled','denied','voided','abandoned','refunded','disputed'));

CREATE TABLE merchants (
    id uuid PRIMARY KEY,
    name text NOT NULL,
    fee_bps integer NOT NULL CHECK (fee_bps BETWEEN 0 AND 10000),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE payments (
    id uuid PRIMARY KEY,
    merchant_id uuid NOT NULL,
    status payment_status NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    amount bigint NOT NULL CHECK (amount > 0),
    currency text NOT NULL CHECK (currency = 'BRL'),
    card_token text NOT NULL,
    card_last4 text NOT NULL CHECK (card_last4 ~ '^[0-9]{4}$'),
    card_brand text NOT NULL,
    CONSTRAINT fk_payments_merchant FOREIGN KEY (merchant_id) REFERENCES merchants(id)
);

CREATE INDEX ix_payments_merchant_id ON payments (merchant_id);

CREATE TABLE payment_events (
    id uuid PRIMARY KEY,
    payment_id uuid NOT NULL,
    from_status payment_status NOT NULL,
    to_status payment_status NOT NULL,
    event_type text NOT NULL,
    event_details jsonb NULL,
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_payment_events_payment FOREIGN KEY (payment_id) REFERENCES payments(id)
);

CREATE INDEX ix_payment_events_payment_id ON payment_events (payment_id);

CREATE DOMAIN account_type AS text
    CHECK (VALUE IN ('external','merchant','platform_fees','dispute_hold'));

CREATE TABLE accounts (
    id uuid PRIMARY KEY,
    account_type account_type NOT NULL,
    merchant_id uuid REFERENCES merchants (id),
    created_at timestamptz NOT NULL,
    currency text NOT NULL CHECK (currency = 'BRL'),
    CONSTRAINT accounts_merchant_id_check CHECK (
        (account_type = 'merchant' AND merchant_id IS NOT NULL)
        OR (account_type <> 'merchant' AND merchant_id IS NULL)
    ),
    CONSTRAINT accounts_type_merchant_uq
        UNIQUE NULLS NOT DISTINCT (account_type, merchant_id)
);

CREATE DOMAIN entry_type AS text
    CHECK (VALUE IN ('credit','debit'));

CREATE DOMAIN movement_type AS text
    CHECK (VALUE IN ('settlement','refund', 'dispute_hold'));

CREATE TABLE ledger_entries (
    id uuid PRIMARY KEY,
    payment_id uuid NOT NULL,
    account_id uuid NOT NULL,
    entry_type entry_type NOT NULL,
    movement_type movement_type NOT NULL,
    amount bigint NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_ledger_entries_payment FOREIGN KEY (payment_id) REFERENCES payments(id),
    CONSTRAINT fk_ledger_entries_account FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE INDEX ix_ledger_entries_payment_id ON ledger_entries (payment_id);
CREATE INDEX ix_ledger_entries_account_id ON ledger_entries (account_id);
REVOKE UPDATE, DELETE ON ledger_entries FROM payments;

CREATE DOMAIN idempotency_operation AS text
    CHECK (VALUE IN ('create','capture','refund', 'void'));

CREATE TABLE idempotency_keys (
    idempotency_key text NOT NULL,
    merchant_id uuid NOT NULL,
    payment_id uuid NOT NULL,
    idempotency_operation idempotency_operation NOT NULL,
    request_hash text NOT NULL,
    response_status integer NOT NULL,
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT idempotency_keys_pk PRIMARY KEY (merchant_id, idempotency_key),
    CONSTRAINT fk_idempotency_keys_merchant_id FOREIGN KEY (merchant_id) REFERENCES merchants(id),
    CONSTRAINT fk_idempotency_keys_payment_id FOREIGN KEY (payment_id) REFERENCES payments(id)
);

CREATE TABLE outbox (
    id uuid PRIMARY KEY,
    payment_id uuid not null,
    routing text not null,
    message jsonb not null,
    published_at timestamptz NULL,
    created_at timestamptz not null,
    CONSTRAINT fk_outbox_payment_id FOREIGN KEY (payment_id) REFERENCES payments(id)
);

CREATE INDEX ix_outbox_pending ON outbox (created_at) WHERE published_at IS NULL