# wager-service

Serviço de carteira para provedores de jogos: recebe `BET`, `WIN`, `LOSS`, `REFUND` e
`ROLLBACK` por HTTP e por SQS, mantém o saldo com ledger append-only e publica eventos
via transactional outbox. Escrito em Go 1.27 com Uber Fx, PostgreSQL, SQS (LocalStack)
e Keycloak.

As decisões de projeto estão em [ARCHITECTURE.md](ARCHITECTURE.md).

## Pré-requisitos

- Docker + Docker Compose v2
- Go 1.27 (só para rodar os testes fora do container)
- `make` (opcional, os comandos estão todos listados abaixo)

Portas usadas no host: 8080 (nginx na frente das 3 instâncias), 8081-8083 (cada
instância), 8180 (Keycloak), 5432 (Postgres), 4566 (LocalStack).

## Subindo tudo

O caminho curto, que faz tudo e termina com um fluxo de exemplo:

```sh
make demo        # = scripts/local-up.sh
```

O mesmo na mão:

```sh
cp .env.example .env      # valores locais, sem segredo real
docker compose up --build -d
```

O compose sobe, nesta ordem: Postgres, LocalStack (cria as filas), Keycloak (importa o
realm `wager`), `migrate up`, três instâncias da API (`api-1`, `api-2`, `api-3`) e um
nginx em `:8080` distribuindo entre elas. A API espera as dependências responderem antes
de abrir a porta, então o primeiro `up` demora um pouco (Keycloak leva ~30s).

```sh
curl -s localhost:8080/health/ready
# {"postgres":"ok","sqs":"ok"}
```

## Variáveis de ambiente

Todas em `.env.example`. As que importam:

| Variável | Para quê |
| --- | --- |
| `DATABASE_URL` | conexão pgx |
| `MIGRATE_ON_START` | `true` aplica migrations no boot (o compose usa o serviço `migrate`, então fica `false`) |
| `OIDC_ISSUER` | `iss` esperado nos tokens (`http://localhost:8180/realms/wager`) |
| `OIDC_JWKS_URL` | de onde buscar as chaves; dentro do compose é `http://keycloak:8080/...` |
| `OIDC_AUDIENCE` | `aud` exigido (`wager-api`) |
| `SQS_INBOUND_QUEUE_URL`, `SQS_DLQ_URL`, `SQS_EVENTS_QUEUE_URL` | filas |
| `SQS_VISIBILITY_TIMEOUT`, `SQS_MAX_RECEIVE_COUNT` | tem que bater com o redrive configurado em `deploy/localstack/init-queues.sh` |
| `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` | token bucket por chamador (por instância) |
| `REFERENCE_MAX_ATTEMPTS`, `REFERENCE_BASE_BACKOFF`, `REFERENCE_MAX_BACKOFF` | política de espera por referência |
| `OUTBOX_LEASE`, `OUTBOX_POLL_INTERVAL`, `OUTBOX_BATCH_SIZE` | publisher da outbox |
| `CONSUMER_ENABLED`, `OUTBOX_ENABLED` | desligar workers numa instância específica |

## Filas

Criadas automaticamente pelo script montado em `/etc/localstack/init/ready.d/`:

- `wager-transactions.fifo` — entrada, com redrive para a DLQ após 5 recebimentos
- `wager-transactions-dlq.fifo`
- `wallet-events.fifo` — destino da outbox

Para recriar sem derrubar o resto: `docker compose restart localstack`.

## Migrations

Ficam em `migrations/` e são embutidas no binário (`golang-migrate`).

```sh
docker compose run --rm migrate up          # ou: make migrate-up
docker compose run --rm migrate down        # reverte tudo
docker compose run --rm migrate steps -1    # reverte uma
docker compose run --rm migrate version
```

Fora do docker: `DATABASE_URL=postgres://wager:wager@localhost:5432/wager?sslmode=disable go run ./cmd/migrate up`.

## Autenticação

Keycloak sobe com o realm `wager` importado de `deploy/keycloak/realm-wager.json`, com
quatro clients (`client_credentials`):

| client | secret | role | uso |
| --- | --- | --- | --- |
| `provider-a` | `provider-a-secret` | `provider` (claim `provider_id=provider-a`) | envia e consulta as próprias transações |
| `provider-b` | `provider-b-secret` | `provider` (`provider_id=provider-b`) | idem, isolado de A |
| `wallet-admin` | `wallet-admin-secret` | `internal` | carteiras, ledger, reconciliação, leitura de qualquer transação |
| `outsider` | `outsider-secret` | nenhuma | testes negativos |

Pegar um token:

```sh
TOKEN=$(scripts/token.sh provider-a)      # ou: make token CLIENT=wallet-admin
```

Console admin: http://localhost:8180 (admin/admin).

## Exemplos

```sh
ADMIN=$(scripts/token.sh wallet-admin)
PROV=$(scripts/token.sh provider-a)
PLAYER=$(uuidgen | tr A-F a-f)

# abrir carteira
curl -s localhost:8080/wallets -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d "{\"playerId\":\"$PLAYER\",\"initialBalance\":{\"amount\":\"1000.00\",\"currency\":\"BRL\"}}"
# {"id":"0192...","playerId":"...","balance":{"amount":"1000.00","currency":"BRL"},"version":1}
WALLET=<id acima>

# aposta
curl -s localhost:8080/wagering/transactions -H "Authorization: Bearer $PROV" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: provider-a:tx-1' \
  -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-1\",\"playerId\":\"$PLAYER\",\"walletId\":\"$WALLET\",\"roundId\":\"round-1\",\"gameId\":\"fortune-chimp\",\"kind\":\"BET\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}"
# {"transactionId":"...","status":"PROCESSED","balance":{"amount":"975.00","currency":"BRL"},"idempotentReplay":false}

# mesma chamada de novo -> idempotentReplay: true, mesmo saldo de antes

# refund
curl -s localhost:8080/wagering/transactions -H "Authorization: Bearer $PROV" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: provider-a:tx-2' \
  -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-2\",\"referenceExternalTransactionId\":\"tx-1\",\"playerId\":\"$PLAYER\",\"walletId\":\"$WALLET\",\"roundId\":\"round-1\",\"gameId\":\"fortune-chimp\",\"kind\":\"REFUND\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}"

# consultas
curl -s localhost:8080/wallets/$WALLET -H "Authorization: Bearer $ADMIN"
curl -s "localhost:8080/wallets/$WALLET/ledger?limit=50" -H "Authorization: Bearer $ADMIN"
curl -s localhost:8080/providers/provider-a/wagering/transactions/tx-1 -H "Authorization: Bearer $PROV"
curl -s -X POST localhost:8080/wallets/$WALLET/reconciliation -H "Authorization: Bearer $ADMIN"

# pela fila
awslocal sqs send-message --queue-url http://localhost:4566/000000000000/wager-transactions.fifo \
  --message-group-id $WALLET --message-deduplication-id $(uuidgen) \
  --message-body "{\"messageId\":\"msg-1\",\"type\":\"WagerTransactionRequested\",\"occurredAt\":\"2026-09-08T12:00:00Z\",\"data\":{\"providerId\":\"provider-a\",\"externalTransactionId\":\"tx-3\",\"idempotencyKey\":\"provider-a:tx-3\",\"playerId\":\"$PLAYER\",\"walletId\":\"$WALLET\",\"roundId\":\"round-2\",\"gameId\":\"fortune-chimp\",\"kind\":\"BET\",\"money\":{\"amount\":\"10.00\",\"currency\":\"BRL\"}}}"
```

Métricas em `GET /metrics` (formato Prometheus) em cada instância.

### Teste você mesmo

Com o ambiente no ar, `make play` abre um prompt que conversa com a API e com a fila.
Ele pega os tokens no Keycloak sozinho, guarda a carteira e as operações da sessão, e
tem atalhos para os cenários do enunciado:

```
$ make play
[provider-a | sem carteira] > open 100.00
[provider-a | carteira 01a0f3b7 | 100.00 BRL] > bet 25.00 aposta-1
[provider-a | carteira 01a0f3b7 | 75.00 BRL] > replay aposta-1        # idempotentReplay: true, mesmo saldo
[provider-a | carteira 01a0f3b7 | 75.00 BRL] > conflict aposta-1 26.00 # 409 IDEMPOTENCY_KEY_CONFLICT
[provider-a | carteira 01a0f3b7 | 75.00 BRL] > refund aposta-1        # crédito de volta
[provider-a | carteira 01a0f3b7 | 100.00 BRL] > rollback aposta-1     # 422 REFERENCE_ALREADY_REVERSED
[provider-a | carteira 01a0f3b7 | 100.00 BRL] > race 5.00 50          # 50 cópias em paralelo: 1 processada, 49 replays
[provider-a | carteira 01a0f3b7 | 95.00 BRL] > open 100.00
[provider-a | carteira 01a0f3c1 | 100.00 BRL] > race2 80.00 80.00     # uma PROCESSED, uma INSUFFICIENT_FUNDS, saldo 20.00
[provider-a | carteira 01a0f3c1 | 20.00 BRL] > sqs BET 10.00          # pela fila; mesma idempotência
[provider-a | carteira 01a0f3c1 | 10.00 BRL] > ledger
[provider-a | carteira 01a0f3c1 | 10.00 BRL] > reconcile
[provider-a | carteira 01a0f3c1 | 10.00 BRL] > as provider-b          # troca o token: provider-b não vê as transações de A
```

`help` (ou `comandos`, `cmds`, `ações`) lista tudo. Valores aceitam inteiro: `bet 25` vira `25.00`. `refund`/`rollback` antes da aposta existir mostram o `PENDING_REFERENCE`
e a resolução quando a aposta chega. Também dá para rodar direto: `go run ./cmd/playground`
(variáveis `API_URL`, `KEYCLOAK_URL`, `AWS_ENDPOINT_URL` para apontar para outro lugar).

### Respostas de erro

| Situação | HTTP | `code` |
| --- | --- | --- |
| JSON inválido, campo faltando, valor mal formatado, `LOSS` com valor, `OPENING` | 400 | `VALIDATION_ERROR` (com `field`) |
| sem token / token inválido ou expirado | 401 | `UNAUTHENTICATED` |
| role errada, `providerId` do body diferente do token | 403 | `FORBIDDEN` |
| carteira ou transação inexistente | 404 | `NOT_FOUND` |
| `Idempotency-Key` repetida com payload diferente | 409 | `IDEMPOTENCY_KEY_CONFLICT` |
| `externalTransactionId` já usado com outra chave | 409 | `EXTERNAL_TRANSACTION_ID_REUSED` |
| segunda carteira para o mesmo jogador/moeda | 409 | `WALLET_ALREADY_EXISTS` |
| rejeição de negócio (persistida) | 422 | corpo normal com `status: REJECTED` e `failureCode` |
| aguardando referência | 202 | corpo normal com `status: PENDING_REFERENCE`, sem `balance` |
| muitas requisições | 429 | `RATE_LIMITED` + `Retry-After` |
| Postgres/SQS fora | 503 | `UNAVAILABLE` + `Retry-After` |

Os `failureCode` estão documentados em [ARCHITECTURE.md](ARCHITECTURE.md#códigos-de-rejeição).

## Testes

Tudo de uma vez, com o ambiente no ar:

```sh
make verify      # = scripts/verify.sh: gofmt, vet, unitários com -race, integração in-process e contra as 3 instâncias
```

Por partes:

```sh
make test          # go test ./...          (unitários + validação do grafo Fx)
make test-race     # go test -race ./...
make vet           # go vet ./...
```

Os unitários não precisam de nada rodando. Eles cobrem o domínio inteiro, o caso de uso
e o roteador HTTP com um store em memória (`internal/app/port/porttest`), além dos
helpers de cada adaptador (decodificação de mensagem SQS, tradução de erros do pgx,
cursor do ledger, backoffs, config). Os de integração usam containers reais e
ficam atrás da build tag `integration` para não quebrar o `go test ./...` numa máquina
sem docker.

```sh
make deps-up            # postgres + keycloak + localstack + migrations
make test-integration   # go test -race -tags integration ./test/integration/... ./internal/adapter/sqs/...
```

Nesse modo a suíte sobe a aplicação dentro do processo de teste (uma instância, porta
aleatória) e bate nela por HTTP. Os cenários cobertos:

- contratos HTTP, códigos de erro, paginação do ledger, reconciliação
- autenticação real (token ausente, inválido, adulterado), isolamento entre provedores,
  restrição das rotas internas, rate limit
- 50 envios paralelos da mesma aposta -> um débito
- duas apostas de 80.00 sobre 100.00 -> uma processada, uma rejeitada, saldo 20.00
- carteiras distintas em paralelo
- `REFUND`/`ROLLBACK` antes da referência: resolução posterior e expiração
- consumidor SQS: processamento, reentrega do mesmo `messageId`, mensagem inválida na DLQ,
  corrida HTTP x SQS, e interrupção entre commit e `DeleteMessage`
  (`internal/adapter/sqs/operation_consumer_integration_test.go`)
- outbox: dois publishers disputando, lease abandonado recuperado, evento chegando na fila
- constraints do schema: ledger imutável, saldo negativo, unicidades; `migrate down/up`
- ciclo de vida Fx: start/stop, falha de start com dependência errada, reinício
  preservando idempotência e pendências

Para rodar contra as três instâncias reais (os testes de concorrência e replay
distribuem as chamadas diretamente entre `:8081`, `:8082` e `:8083`):

```sh
docker compose up --build -d
make test-multi
```

Também dá para apontar tudo para o nginx com `API_URL=http://localhost:8080 go test -tags integration ./test/integration/...`;
aí os testes que dependem da configuração in-process (rate limit baixo, TTL curto de referência) são pulados.

### Simulando falhas na mão

```sh
docker compose stop postgres        # a API responde 503 e o consumidor adia as mensagens
docker compose start postgres

docker compose kill -s SIGTERM api-2   # para de receber, termina o que está em andamento, sai
docker compose up -d api-2

docker compose pause api-1          # outbox/pendências dessa instância são assumidas pelas outras
docker compose unpause api-1
```

## Layout

```
cmd/wager-service      binário principal (HTTP + consumidor + workers)
cmd/migrate            aplica/reverte migrations
cmd/playground         REPL para testar a API e a fila na mão (make play)
internal/domain        money, wallet, wager, event — regras de negócio, sem dependências externas
internal/app/port      interfaces que os casos de uso precisam (repositórios, unit of work, publisher, clock, métricas)
internal/app/wagering  processamento de operações: process_operation, business_rules, pending_reference
internal/app/wallets   abertura, consultas e reconciliação de carteira
internal/adapter       postgres, sqs, httpapi, auth — implementam as portas
internal/worker        outbox_publisher e pending_reference_retrier
internal/fxapp         um arquivo por módulo Fx (core, persistence, messaging, auth, usecase, http, worker)
migrations/            SQL versionado, embutido no binário
deploy/                realm do Keycloak, init do LocalStack, nginx
test/integration       suíte com containers reais
```

Sem comentários no código de propósito: os nomes de arquivo, tipo e função dizem o que
cada coisa faz, e o porquê está neste README e no `ARCHITECTURE.md`.
