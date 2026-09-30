# wager-service

Serviço de carteira para provedores de jogos: recebe `BET`, `WIN`, `LOSS`, `REFUND` e
`ROLLBACK` por HTTP e por SQS, mantém o saldo com ledger append-only e publica eventos
via transactional outbox. Escrito em Go 1.27 com Uber Fx, PostgreSQL, SQS (LocalStack)
e Keycloak.

As decisões de projeto estão em [ARCHITECTURE.md](ARCHITECTURE.md).

## Pré-requisitos

- Docker Desktop (macOS, Windows) ou Docker Engine + compose plugin (Linux)
- Go 1.27 para testes e playground; a API em si roda em container
- `curl`; `make` é opcional, cada alvo é um script em `scripts/`

Os alvos que dependem da infra (`make demo`, `make play`, `make verify`,
`make test-integration`, `make test-multi`) passam por `scripts/ensure-infra.sh`, que
detecta o sistema, inicia o Docker Desktop se estiver parado (macOS, Windows, WSL) ou o
serviço (`systemctl`, Linux), confere as ferramentas com a dica de instalação de cada
sistema, cria o `.env` e sobe só o que não estiver respondendo. `go test ./...`,
`go test -race ./...` e `go vet ./...` não precisam de nada rodando.

No Windows, rode no Git Bash ou no WSL (`scripts/play.sh`, `scripts/local-up.sh`,
`scripts/verify.sh` funcionam nos dois); no PowerShell puro só os comandos `docker compose`
e `go` diretos.

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

Reverter e reaplicar com a API no ar recria os tipos do schema, e cada conexão do pool
falha uma vez por statement cacheado (Postgres responde "cached plan must not change
result type", que a API devolve como 503 com `Retry-After`). Depois de `migrate up`,
`docker compose restart api-1 api-2 api-3` deixa tudo limpo.

## Autenticação

Keycloak sobe com o realm `wager` importado de `deploy/keycloak/realm-wager.json`, com
quatro clients (`client_credentials`):

| client | secret | role | uso |
| --- | --- | --- | --- |
| `provider-a` | `provider-a-secret` | `provider` (claim `provider_id=provider-a`) | envia e consulta as próprias transações |
| `provider-b` | `provider-b-secret` | `provider` (`provider_id=provider-b`) | idem, isolado de A |
| `wallet-admin` | `wallet-admin-secret` | `internal` | carteiras, ledger, reconciliação, leitura de qualquer transação |
| `outsider` | `outsider-secret` | nenhuma | testes negativos |
| `short-lived` | `short-lived-secret` | `internal`, token de 1s | teste de token expirado |

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

### Rate limit

Cada chamador (o `sub` do token, ou o IP quando não há token) tem um token bucket por
instância: `RATE_LIMIT_RPS` por segundo com `RATE_LIMIT_BURST` de folga. Estourou, a
resposta é 429 com `Retry-After`. O bucket enche sozinho com o tempo; para liberar na hora,
o serviço interno pode zerar:

```sh
ADMIN=$(scripts/token.sh wallet-admin)
SUB=<claim sub do token bloqueado>            # o playground descobre com: unblock
curl -X DELETE localhost:8081/rate-limits/$SUB -H "Authorization: Bearer $ADMIN"   # um chamador nesta instância
curl -X DELETE localhost:8081/rate-limits      -H "Authorization: Bearer $ADMIN"   # todos os buckets desta instância
```

Como o limite é por instância, o reset também é: repita em `:8082` e `:8083`, ou use
`unblock` no `make play`, que faz as três. Essas rotas exigem a role `internal` e não
passam pelo limitador, para um administrador bloqueado conseguir se liberar.

### Teste você mesmo

`make play` abre um prompt que conversa com a API e com as filas. Se o ambiente não estiver
no ar, ele sobe tudo antes (o mesmo que `make demo`, sem o fluxo de exemplo).
Ele pega os tokens no Keycloak sozinho e guarda a carteira e as operações da sessão, então
dá para reproduzir cada cenário do desafio sem escrever `curl`. A lista de comandos aparece
na abertura e volta com `help`, `comandos`, `cmds` ou `ações`.

Valores aceitam `xx.yy`, inteiro (`25` vira `25.00`) ou uma casa (`2.5` vira `2.50`).
`<id>` é sempre o `externalTransactionId`; quando você não informa um, o prompt gera
(`bet-3f9a1c`) e imprime. `ops` lista os ids da sessão.

| Comando | Argumentos | O que faz | Exemplo |
| --- | --- | --- | --- |
| `open [valor]` | valor inicial, padrão `100.00` | abre carteira para um jogador novo (token `wallet-admin`) | `open 1000` |
| `wallet` | | saldo e versão da carteira atual | `wallet` |
| `ledger` | | lançamentos da carteira, com saldo antes e depois | `ledger` |
| `reconcile` | | recalcula o saldo pelo ledger e compara | `reconcile` |
| `bet <valor> [id]` | valor > 0; id opcional | `BET`, débito | `bet 25 aposta-1` |
| `win <valor> [id]` | valor > 0; id opcional | `WIN`, crédito | `win 10` |
| `loss` | | `LOSS` com `0.00`; não move saldo nem versão | `loss` |
| `refund <id> [valor]` | id de uma `BET`; valor só se a aposta não for desta sessão | `REFUND` devolvendo a aposta inteira | `refund aposta-1` |
| `rollback <id> [valor]` | id de `BET`, `WIN` ou `REFUND` | `ROLLBACK`, movimento contrário ao original | `rollback aposta-1` |
| `replay <id>` | id de operação desta sessão | reenvia payload e chave idênticos; espera `idempotentReplay: true` e o saldo da época | `replay aposta-1` |
| `conflict <id> <valor>` | id desta sessão; valor diferente | mesma `Idempotency-Key` com payload diferente; espera 409 | `conflict aposta-1 26` |
| `tx <id>` | qualquer id do provedor atual | consulta a transação: status, `failureCode`, `nextAttemptAt` | `tx aposta-1` |
| `race <valor> [n]` | n padrão 50 | n cópias da mesma aposta em paralelo; espera 1 processada e n-1 replays | `race 5 50` |
| `race2 <a> <b>` | dois valores | duas apostas distintas ao mesmo tempo; com 100 de saldo e 80/80 espera uma `INSUFFICIENT_FUNDS` | `race2 80 80` |
| `sqs <kind> <valor> [ref]` | kind: `BET`, `WIN`, `LOSS` (valor 0), `REFUND`, `ROLLBACK`; ref: id referenciado, obrigatório em `REFUND`/`ROLLBACK`, opcional em `WIN` | publica em `wager-transactions.fifo` e espera o consumidor | `sqs BET 10`, `sqs REFUND 10 bet-3f9a1c` |
| `redeliver [id]` | id enviado por `sqs`; padrão o último | reenvia o mesmo envelope e `messageId`; a inbox ignora e o saldo não muda | `redeliver` |
| `poison` | | manda mensagem inválida e acompanha até a DLQ | `poison` |
| `dlq` | | lê e remove o que está na DLQ, com o motivo | `dlq` |
| `events [n]` | n padrão 50 | lê e remove eventos de `wallet-events.fifo`; `*` marca a carteira atual | `events 20` |
| `flood [n]` | n padrão 400 | n GETs rápidos com o token atual; espera 429 com `Retry-After` | `flood 500` |
| `unblock [client\|all]` | `provider-a`, `provider-b`, `wallet-admin` ou `all`; padrão o provedor atual | zera o bucket do chamador em cada instância (`DELETE /rate-limits/{sub}` com `wallet-admin`); sem isso o 429 só some quando o bucket enche de novo | `unblock`, `unblock all` |
| `auth` | | oito chamadas (sem token, inválido, provedor errado, role errada e as que devem passar) com o código esperado | `auth` |
| `health` | | live e ready no nginx e nas três instâncias | `health` |
| `metrics [filtro]` | filtro padrão `wager_` | linhas de `/metrics` de cada instância | `metrics outbox` |
| `as <client>` | `provider-a`, `provider-b`, `wallet-admin` | troca o token das operações; `provider-b` não enxerga o que é de A | `as provider-b` |
| `ops` | | operações desta sessão | `ops` |
| `quit` | | sai | |

Um roteiro que passa por tudo (os ids `aposta-1` e `aposta-x` precisam ser inéditos para o
provedor; rodando de novo, troque os nomes ou deixe o playground gerar):

```
open 100
bet 25 aposta-1
replay aposta-1            # idempotentReplay: true, saldo 75.00
conflict aposta-1 26       # 409 IDEMPOTENCY_KEY_CONFLICT
refund aposta-1            # crédito, saldo 100.00
rollback aposta-1          # 422 REFERENCE_ALREADY_REVERSED
rollback aposta-x 5        # 202 PENDING_REFERENCE (a aposta ainda não existe)
bet 5 aposta-x             # o worker resolve o rollback em seguida; confira com tx
race 5 50                  # 1 processada, 49 replays
open 100
race2 80 80                # uma PROCESSED, uma INSUFFICIENT_FUNDS, saldo 20.00
sqs BET 10                 # pela fila
redeliver                  # mesma mensagem de novo, nada muda
poison                     # inválida vai para a DLQ
events                     # o que a outbox publicou
flood                      # 429 depois do burst
unblock                    # libera o provedor sem esperar o refill
auth                       # bateria de autorização
as provider-b
tx aposta-1                # 404: outro provedor não enxerga
ledger
reconcile
```

Ao abrir, o prompt pergunta se quer a saída com cores (`S`/`sim` ou `N`/`não`; Enter é sim).
Verde é sucesso, vermelho é falha, amarelo é dica. A pergunta é pulada quando a entrada vem
de um pipe ou quando `NO_COLOR` está definido.

Também roda direto com `go run ./cmd/playground`; `API_URL`, `KEYCLOAK_URL` e
`AWS_ENDPOINT_URL` apontam para outro ambiente.

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
- autenticação real (token ausente, inválido, adulterado, expirado), isolamento entre
  provedores, restrição das rotas internas, rate limit
- 50 envios paralelos da mesma aposta -> um débito
- duas apostas de 80.00 sobre 100.00 -> uma processada, uma rejeitada, saldo 20.00
- carteiras distintas em paralelo
- `REFUND`/`ROLLBACK` antes da referência: resolução posterior e expiração
- consumidor SQS: processamento, reentrega do mesmo `messageId`, mensagem inválida na DLQ,
  corrida HTTP x SQS, e interrupção entre commit e `DeleteMessage`
  (`internal/adapter/sqs/operation_consumer_integration_test.go`)
- outbox: dois publishers disputando, lease abandonado recuperado, retry com backoff após
  falha do broker, evento chegando na fila
- atomicidade: erro no meio da unit of work não deixa carteira, transação nem ledger
- reconciliação detectando divergência (resposta, métrica) sem alterar o saldo; `LOSS`
  gerando só `WagerTransactionProcessed`
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

O código não tem comentários: cada arquivo trata de um assunto e o nome de cada função diz
o que ela faz. O porquê de cada decisão está no `ARCHITECTURE.md`; se algo aqui não bater
com o código, o código está certo e o documento precisa de correção.
