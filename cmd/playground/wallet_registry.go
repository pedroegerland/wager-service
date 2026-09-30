package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const unknownOwner = "unknown"

func isUnknown(name string) bool { return strings.EqualFold(strings.TrimSpace(name), unknownOwner) }

type registeredWallet struct {
	walletID  uuid.UUID
	playerID  uuid.UUID
	ownerName string
	balance   int64
	currency  string
	version   int64
	createdAt time.Time
}

func (s *session) db(ctx context.Context) (*pgxpool.Pool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pool != nil {
		return s.pool, nil
	}
	pool, err := pgxpool.New(ctx, s.databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres (%s): %w", s.databaseURL, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres (%s): %w", s.databaseURL, err)
	}
	s.pool = pool
	return pool, nil
}

func (s *session) registerWallet(ctx context.Context, walletID uuid.UUID, owner string) error {
	pool, err := s.db(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO playground_wallets (wallet_id, owner_name) VALUES ($1, $2) ON CONFLICT (wallet_id) DO NOTHING`, walletID, owner)
	return err
}

func (s *session) walletOwner(ctx context.Context, walletID uuid.UUID) (string, bool, error) {
	pool, err := s.db(ctx)
	if err != nil {
		return "", false, err
	}
	var owner string
	err = pool.QueryRow(ctx, `SELECT owner_name FROM playground_wallets WHERE wallet_id = $1`, walletID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return owner, true, nil
}

func (s *session) registeredWallets(ctx context.Context, owner string) ([]registeredWallet, error) {
	pool, err := s.db(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `SELECT p.wallet_id, w.player_id, p.owner_name, w.balance, w.currency, w.version, p.created_at
		FROM playground_wallets p JOIN wallets w ON w.id = p.wallet_id
		WHERE $1 = '' OR lower(p.owner_name) = lower($1)
		ORDER BY p.created_at DESC
		LIMIT 100`, strings.TrimSpace(owner))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []registeredWallet
	for rows.Next() {
		var w registeredWallet
		if err := rows.Scan(&w.walletID, &w.playerID, &w.ownerName, &w.balance, &w.currency, &w.version, &w.createdAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *session) wallets(args []string) error {
	owner := strings.Join(args, " ")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := s.registeredWallets(ctx, owner)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		if owner == "" {
			fmt.Println(yellow("nenhuma carteira aberta pelo playground ainda; use: open [valor]"))
		} else {
			fmt.Println(yellow(fmt.Sprintf("nenhuma carteira de %q; use: open [valor]", owner)))
		}
		return nil
	}
	fmt.Printf("%d carteiras (mais recente primeiro); retome com: use <id> ou use <nome>; '%s' = sem dono\n", len(list), unknownOwner)
	fmt.Printf("  %-36s  %-16s  %12s  %7s  %s\n", "walletId", "dono", "saldo", "versão", "aberta em")
	for _, w := range list {
		line := fmt.Sprintf("  %-36s  %-16s  %12s  %7d  %s", w.walletID, w.ownerName, fmtCents(w.balance)+" "+w.currency, w.version, w.createdAt.Local().Format("02/01 15:04:05"))
		if w.walletID.String() == s.walletID {
			line = green(line + "  <- atual")
		}
		fmt.Println(line)
	}
	return nil
}

func (s *session) use(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: use <walletId> | use <nome> | use %s  (wallets lista as opções)", unknownOwner)
	}
	target := strings.Join(args, " ")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if id, err := uuid.Parse(target); err == nil {
		return s.resume(ctx, id)
	}
	list, err := s.registeredWallets(ctx, target)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return fmt.Errorf("nenhuma carteira de %q; 'wallets' lista todas", target)
	}
	if len(list) > 1 {
		fmt.Println(yellow(fmt.Sprintf("há %d carteiras sem dono; retomando a mais recente. Para outra, use o id:", len(list))))
		for _, w := range list {
			fmt.Printf("  %s  %s %s  aberta em %s\n", w.walletID, fmtCents(w.balance), w.currency, w.createdAt.Local().Format("02/01 15:04:05"))
		}
	}
	return s.resume(ctx, list[0].walletID)
}

func (s *session) resume(_ context.Context, walletID uuid.UUID) error {
	r, err := s.call("GET", "/wallets/"+walletID.String(), "wallet-admin", nil, nil)
	if err != nil {
		return err
	}
	if r.status != 200 {
		s.show(r)
		return fmt.Errorf("carteira %s não encontrada", walletID)
	}
	s.walletID = r.str("id")
	s.playerID = r.str("playerId")
	s.balance = r.amount("balance")
	s.ops = map[string]operation{}
	s.order = nil
	fmt.Println(green(fmt.Sprintf("retomada a carteira %s (jogador %s, saldo %s BRL, versão %v)", s.walletID, s.playerID, s.balance, r.body["version"])))
	fmt.Println(yellow("as operações de sessões anteriores não estão em 'ops'; para refund/rollback delas informe o valor: refund <id> <valor>"))
	return nil
}

func (s *session) apply(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: apply <walletId> [nome]  (nome padrão: %s)", s.owner)
	}
	walletID, err := uuid.Parse(args[0])
	if err != nil {
		return fmt.Errorf("%q não é um walletId; 'wallets' lista as registradas, e o id de qualquer carteira aparece na resposta do POST /wallets", args[0])
	}
	name := s.owner
	if len(args) > 1 {
		name = strings.TrimSpace(strings.Join(args[1:], " "))
	}
	if isUnknown(name) {
		return fmt.Errorf("informe o nome do novo dono: apply %s <nome> (a sessão está como %s)", walletID, unknownOwner)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if owner, found, err := s.walletOwner(ctx, walletID); err != nil {
		return err
	} else if found && !isUnknown(owner) {
		return fmt.Errorf("a carteira %s já pertence a %q; só carteiras sem dono podem ser vinculadas", walletID, owner)
	}
	if existing, err := s.registeredWallets(ctx, name); err != nil {
		return err
	} else if len(existing) > 0 {
		return fmt.Errorf("%s já tem a carteira %s; cada pessoa tem uma só", name, existing[0].walletID)
	}
	r, err := s.call("GET", "/wallets/"+walletID.String(), "wallet-admin", nil, nil)
	if err != nil {
		return err
	}
	if r.status != 200 {
		s.show(r)
		return fmt.Errorf("carteira %s não encontrada na API", walletID)
	}
	pool, err := s.db(ctx)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO playground_wallets (wallet_id, owner_name) VALUES ($1, $2)
		ON CONFLICT (wallet_id) DO UPDATE SET owner_name = EXCLUDED.owner_name
		WHERE lower(playground_wallets.owner_name) = 'unknown'`, walletID, name)
	if err != nil {
		if strings.Contains(err.Error(), "playground_wallets_one_per_owner") {
			return fmt.Errorf("%s já tem carteira; cada pessoa tem uma só", name)
		}
		return err
	}
	fmt.Println(green(fmt.Sprintf("carteira %s (saldo %s) vinculada a %s; retome com: use %s", walletID, r.amount("balance"), name, name)))
	return nil
}

func fmtCents(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}
