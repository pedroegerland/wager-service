-- tabela de apoio do playground (cmd/playground): quem abriu cada carteira,
-- para retomar uma sessao pelo nome. Nao faz parte do dominio.
CREATE TABLE playground_wallets (
    wallet_id   UUID PRIMARY KEY REFERENCES wallets (id),
    owner_name  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT playground_wallets_owner_not_blank CHECK (length(trim(owner_name)) > 0)
);
CREATE INDEX playground_wallets_owner_idx ON playground_wallets (lower(owner_name), created_at DESC);
