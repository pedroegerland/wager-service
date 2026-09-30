package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type operation struct {
	ext, kind, amount, ref string
	key                    string
}

type session struct {
	apiURL      string
	keycloakURL string
	awsEndpoint string

	provider string
	playerID string
	walletID string
	balance  string
	mu       sync.Mutex
	ops      map[string]operation
	order    []string
	tokens   map[string]string
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newSession() *session {
	return &session{
		apiURL:      envOr("API_URL", "http://localhost:8080"),
		keycloakURL: envOr("KEYCLOAK_URL", "http://localhost:8180"),
		awsEndpoint: envOr("AWS_ENDPOINT_URL", "http://localhost:4566"),
		provider:    "provider-a",
		ops:         map[string]operation{},
		tokens:      map[string]string{},
	}
}

func (s *session) prompt() string {
	if s.walletID == "" {
		return fmt.Sprintf("[%s | sem carteira] > ", s.provider)
	}
	return fmt.Sprintf("[%s | carteira %s | %s BRL] > ", s.provider, s.walletID[:8], s.balance)
}

func (s *session) remember(op operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.ops[op.ext]; !seen {
		s.order = append(s.order, op.ext)
	}
	s.ops[op.ext] = op
}

func (s *session) newExt(kind string) string {
	return strings.ToLower(kind) + "-" + uuid.NewString()[:6]
}

func (s *session) run(args []string) error {
	cmd, rest := args[0], args[1:]
	handlers := map[string]func([]string) error{
		"help": s.help, "open": s.open, "wallet": s.wallet, "ledger": s.ledger, "reconcile": s.reconcile,
		"bet": s.bet, "win": s.win, "loss": s.loss, "refund": s.refund, "rollback": s.rollback,
		"replay": s.replay, "conflict": s.conflict, "race": s.race, "race2": s.race2,
		"sqs": s.viaSQS, "tx": s.tx, "ops": s.listOps, "as": s.as,
	}
	h, ok := handlers[cmd]
	if !ok {
		return fmt.Errorf("comando desconhecido %q (help lista todos)", cmd)
	}
	return h(rest)
}

func (s *session) help([]string) error {
	fmt.Print(`carteira (token wallet-admin)
  open [valor]            abre carteira nova para um jogador novo (padrão 100.00)
  wallet                  mostra saldo e versão
  ledger                  lista os lançamentos
  reconcile               recalcula o saldo pelo ledger

operações (token do provedor atual)
  bet <valor> [id]        BET; id opcional para você escolher o externalTransactionId
  win <valor> [id]        WIN
  loss                    LOSS com 0.00
  refund <id-da-aposta>   REFUND devolvendo a aposta
  rollback <id>           ROLLBACK de BET, WIN ou REFUND
  replay <id>             reenvia exatamente o mesmo payload e chave -> idempotentReplay
  conflict <id> <valor>   mesma chave, valor diferente -> 409
  sqs <kind> <valor> [ref] manda pela fila em vez de HTTP (mesma idempotência)
  tx <id>                 consulta a transação pelo externalTransactionId

concorrência
  race <valor> [n]        n cópias da mesma aposta em paralelo (padrão 50) -> um débito
  race2 <a> <b>           duas apostas distintas ao mesmo tempo (ex.: 80 80 sobre 100)

sessão
  ops                     operações enviadas nesta sessão
  as <provider-a|provider-b|wallet-admin>   troca o token usado nas operações
  quit
`)
	return nil
}

func (s *session) as(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("uso: as <provider-a|provider-b|wallet-admin>")
	}
	s.provider = args[0]
	fmt.Println("operações agora usam o token de", s.provider)
	return nil
}

func (s *session) listOps([]string) error {
	if len(s.order) == 0 {
		fmt.Println("nenhuma operação ainda")
		return nil
	}
	for _, id := range s.order {
		op := s.ops[id]
		line := fmt.Sprintf("  %-22s %-9s %8s", op.ext, op.kind, op.amount)
		if op.ref != "" {
			line += "  ref=" + op.ref
		}
		fmt.Println(line)
	}
	return nil
}
