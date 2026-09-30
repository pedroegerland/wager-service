package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type operation struct {
	ext, kind, amount, ref string
	key                    string
	messageID, sqsBody     string
}

type session struct {
	apiURL      string
	keycloakURL string
	awsEndpoint string
	databaseURL string
	owner       string
	pool        *pgxpool.Pool

	provider string
	playerID string
	walletID string
	balance  string
	mu       sync.Mutex
	ops      map[string]operation
	order    []string
	tokens   map[string]cachedToken
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
		databaseURL: envOr("DATABASE_URL", "postgres://wager:wager@localhost:5432/wager?sslmode=disable"),
		owner:       envOr("PLAYGROUND_USER", unknownOwner),
		provider:    "provider-a",
		ops:         map[string]operation{},
		tokens:      map[string]cachedToken{},
	}
}

func (s *session) prompt() string {
	if s.walletID == "" {
		return fmt.Sprintf("[%s | %s | sem carteira] > ", s.owner, s.provider)
	}
	return fmt.Sprintf("[%s | %s | carteira %s | %s BRL] > ", s.owner, s.provider, s.walletID[:8], s.balance)
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
		"help": s.help, "comandos": s.help, "comando": s.help, "cmds": s.help, "cmd": s.help, "acoes": s.help, "ações": s.help,
		"open": s.open, "wallet": s.wallet, "ledger": s.ledger, "reconcile": s.reconcile,
		"bet": s.bet, "win": s.win, "loss": s.loss, "refund": s.refund, "rollback": s.rollback,
		"replay": s.replay, "conflict": s.conflict, "race": s.race, "race2": s.race2,
		"sqs": s.viaSQS, "tx": s.tx, "ops": s.listOps, "as": s.as,
		"events": s.events, "dlq": s.dlq, "poison": s.poison, "redeliver": s.redeliver,
		"flood": s.flood, "unblock": s.unblock, "auth": s.auth, "health": s.health, "metrics": s.metrics,
		"login": s.loginCmd, "wallets": s.wallets, "use": s.use, "apply": s.apply,
	}
	h, ok := handlers[cmd]
	if !ok {
		return fmt.Errorf("comando desconhecido %q (help lista todos)", cmd)
	}
	return h(rest)
}

func (s *session) help([]string) error {
	fmt.Print(`valores: xx.yy; um inteiro vale como reais (25 -> 25.00, 25.5 -> 25.50)

carteira (token wallet-admin)
  open [valor] [unknown]  abre carteira nova (padrão 100.00) no seu nome; cada pessoa tem uma só.
                          'open <valor> unknown' abre uma sem dono, que pode ser vinculada depois com apply
  wallets [nome]          lista as carteiras abertas pelo playground (todas, ou só as de um nome)
  use <id|nome>           retoma uma carteira pelo walletId ou pelo nome do dono ('use unknown': a última sem dono)
  apply <id> [nome]       vincula uma carteira sem dono a alguém que ainda não tem carteira; nome padrão o seu
  wallet                  mostra saldo e versão
  ledger                  lista os lançamentos
  reconcile               recalcula o saldo pelo ledger

operações (token do provedor atual)
  bet <valor> [id]        BET; id opcional para você escolher o externalTransactionId
  win <valor> [id]        WIN
  loss                    LOSS com 0.00 (não recebe valor)
  refund <id-da-aposta>   REFUND devolvendo a aposta
  rollback <id>           ROLLBACK de BET, WIN ou REFUND
  replay <id>             reenvia exatamente o mesmo payload e chave -> idempotentReplay
  conflict <id> <valor>   mesma chave, valor diferente -> 409
  tx <id>                 consulta a transação pelo externalTransactionId

fila (LocalStack)
  sqs <kind> <valor> [ref] manda pela fila em vez de HTTP (mesma idempotência)
                          kind: BET | WIN | LOSS (valor 0) | REFUND | ROLLBACK
                          ref: externalTransactionId da operação referenciada; obrigatório em
                               REFUND (aponta uma BET) e ROLLBACK (BET, WIN ou REFUND), opcional em WIN
  redeliver [id]          reenvia o mesmo envelope (mesmo messageId; padrão o último sqs) -> inbox ignora, saldo não muda
  poison                  manda uma mensagem inválida e mostra ela chegando na DLQ
  dlq                     lê (e remove) o que está na DLQ
  events [n|all]          lê (e remove) até n eventos de wallet-events.fifo (padrão 50), em ordem de
                          horário, mostrando os da carteira atual; 'all' mostra também os das outras

concorrência
  race <valor> [n]        n cópias da mesma aposta em paralelo (padrão 50) -> um débito
  race2 <a> <b>           duas apostas distintas ao mesmo tempo (ex.: 80 80 sobre 100)

proteções e operação
  auth                    bateria de chamadas sem token, token inválido, provedor errado, role errada
  flood [n]               n GETs rápidos com o token atual até receber 429 (padrão 400)
  unblock [client|all]    zera o rate limit do chamador (padrão o provedor atual) em cada instância
                          via DELETE /rate-limits/{sub} com o token wallet-admin; 'all' zera todos
  health                  /health/live e /health/ready no nginx e em cada instância
  metrics [filtro]        /metrics de cada instância, linhas contendo o filtro (padrão wager_)

sessão
  ops                     operações enviadas nesta sessão
  as <provider-a|provider-b|wallet-admin>   troca o token usado nas operações
  login                   renova os tokens agora (eles valem 5 min e são renovados sozinhos quando vencem)
  help | comandos | cmds | ações   esta lista
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
