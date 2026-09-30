-- uma pessoa tem uma carteira; 'unknown' significa sem dono e pode repetir.
-- Registros antigos com dono repetido ficam só com o mais recente.
DELETE FROM playground_wallets p
USING playground_wallets newer
WHERE lower(p.owner_name) = lower(newer.owner_name)
  AND lower(p.owner_name) <> 'unknown'
  AND newer.created_at > p.created_at;

CREATE UNIQUE INDEX playground_wallets_one_per_owner
    ON playground_wallets (lower(owner_name)) WHERE lower(owner_name) <> 'unknown';
