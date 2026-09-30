CREATE TYPE wager_origin AS ENUM ('INTERNAL', 'EXTERNAL');
CREATE TYPE wager_kind AS ENUM ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK');
CREATE TYPE wager_status AS ENUM ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED');
CREATE TYPE ledger_direction AS ENUM ('DEBIT', 'CREDIT');

-- amounts everywhere are minor units (cents) in BIGINT; never floats

CREATE TABLE wallets (
    id          UUID PRIMARY KEY,
    player_id   UUID NOT NULL,
    currency    CHAR(3) NOT NULL,
    balance     BIGINT NOT NULL,
    version     BIGINT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0),
    CONSTRAINT wallets_version_positive CHECK (version >= 1),
    CONSTRAINT wallets_player_currency_unique UNIQUE (player_id, currency)
);

CREATE TABLE wager_transactions (
    id                                  UUID PRIMARY KEY,
    origin                              wager_origin NOT NULL,
    kind                                wager_kind NOT NULL,
    status                              wager_status NOT NULL,
    wallet_id                           UUID NOT NULL REFERENCES wallets (id),
    player_id                           UUID NOT NULL,
    currency                            CHAR(3) NOT NULL,
    amount                              BIGINT NOT NULL,
    provider_id                         TEXT,
    external_transaction_id             TEXT,
    idempotency_key                     TEXT,
    payload_hash                        TEXT,
    round_id                            TEXT,
    game_id                             TEXT,
    reference_external_transaction_id   TEXT,
    reference_transaction_id            UUID REFERENCES wager_transactions (id),
    failure_code                        TEXT,
    balance_after                       BIGINT,
    attempts                            INT NOT NULL DEFAULT 0,
    next_attempt_at                     TIMESTAMPTZ,
    created_at                          TIMESTAMPTZ NOT NULL,
    updated_at                          TIMESTAMPTZ NOT NULL,
    processed_at                        TIMESTAMPTZ,

    CONSTRAINT wager_tx_amount_non_negative CHECK (amount >= 0),
    CONSTRAINT wager_tx_loss_is_zero CHECK (kind <> 'LOSS' OR amount = 0),
    CONSTRAINT wager_tx_others_positive CHECK (kind = 'LOSS' OR amount > 0),
    -- internal vs external metadata: all or nothing
    CONSTRAINT wager_tx_origin_shape CHECK (
        (origin = 'EXTERNAL' AND kind <> 'OPENING'
            AND provider_id IS NOT NULL AND external_transaction_id IS NOT NULL
            AND idempotency_key IS NOT NULL AND payload_hash IS NOT NULL
            AND round_id IS NOT NULL AND game_id IS NOT NULL)
        OR
        (origin = 'INTERNAL' AND kind = 'OPENING' AND status = 'PROCESSED'
            AND provider_id IS NULL AND external_transaction_id IS NULL
            AND idempotency_key IS NULL AND payload_hash IS NULL
            AND round_id IS NULL AND game_id IS NULL
            AND reference_external_transaction_id IS NULL
            AND reference_transaction_id IS NULL)
    ),
    CONSTRAINT wager_tx_reversal_has_reference CHECK (
        kind NOT IN ('REFUND', 'ROLLBACK') OR reference_external_transaction_id IS NOT NULL
    ),
    CONSTRAINT wager_tx_bet_loss_no_reference CHECK (
        kind NOT IN ('BET', 'LOSS') OR reference_external_transaction_id IS NULL
    ),
    CONSTRAINT wager_tx_failure_code_when_terminal_failure CHECK (
        (status IN ('REJECTED', 'FAILED')) = (failure_code IS NOT NULL)
    ),
    CONSTRAINT wager_tx_pending_reference_schedule CHECK (
        status <> 'PENDING_REFERENCE' OR next_attempt_at IS NOT NULL
    )
);

-- idempotency: one row per key and one row per external id, per provider
CREATE UNIQUE INDEX wager_tx_provider_key_unique
    ON wager_transactions (provider_id, idempotency_key) WHERE origin = 'EXTERNAL';
CREATE UNIQUE INDEX wager_tx_provider_external_id_unique
    ON wager_transactions (provider_id, external_transaction_id) WHERE origin = 'EXTERNAL';
-- a wallet gets exactly one OPENING
CREATE UNIQUE INDEX wager_tx_single_opening
    ON wager_transactions (wallet_id) WHERE kind = 'OPENING';
-- a referenced operation can be successfully reversed only once
CREATE UNIQUE INDEX wager_tx_single_reversal
    ON wager_transactions (provider_id, reference_external_transaction_id)
    WHERE kind IN ('REFUND', 'ROLLBACK') AND status = 'PROCESSED';
CREATE INDEX wager_tx_pending_reference_due
    ON wager_transactions (next_attempt_at) WHERE status = 'PENDING_REFERENCE';
CREATE INDEX wager_tx_wallet_idx ON wager_transactions (wallet_id);

CREATE TABLE wallet_ledger_entries (
    seq             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id              UUID NOT NULL UNIQUE,
    wallet_id       UUID NOT NULL REFERENCES wallets (id),
    transaction_id  UUID NOT NULL REFERENCES wager_transactions (id),
    direction       ledger_direction NOT NULL,
    amount          BIGINT NOT NULL,
    balance_before  BIGINT NOT NULL,
    balance_after   BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    CONSTRAINT ledger_amount_positive CHECK (amount > 0),
    CONSTRAINT ledger_balances_non_negative CHECK (balance_before >= 0 AND balance_after >= 0),
    CONSTRAINT ledger_arithmetic CHECK (
        (direction = 'DEBIT' AND balance_after = balance_before - amount) OR
        (direction = 'CREDIT' AND balance_after = balance_before + amount)
    ),
    CONSTRAINT ledger_wallet_tx_unique UNIQUE (wallet_id, transaction_id)
);
CREATE INDEX ledger_wallet_seq_idx ON wallet_ledger_entries (wallet_id, seq);

-- append-only: no UPDATE or DELETE, ever
CREATE FUNCTION ledger_entries_are_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'wallet_ledger_entries is append-only (%)', TG_OP
        USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_no_update_or_delete
    BEFORE UPDATE OR DELETE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_are_immutable();

CREATE TABLE inbox_messages (
    consumer_name   TEXT NOT NULL,
    message_id      TEXT NOT NULL,
    payload_hash    TEXT NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL,
    completed_at    TIMESTAMPTZ,
    PRIMARY KEY (consumer_name, message_id)
);

CREATE TABLE outbox_events (
    id              UUID PRIMARY KEY,
    aggregate_id    UUID NOT NULL,
    event_type      TEXT NOT NULL,
    payload         JSONB NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    attempts        INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    locked_by       TEXT,
    locked_until    TIMESTAMPTZ,
    published_at    TIMESTAMPTZ,
    last_error      TEXT
);
CREATE INDEX outbox_unpublished_idx ON outbox_events (next_attempt_at) WHERE published_at IS NULL;
