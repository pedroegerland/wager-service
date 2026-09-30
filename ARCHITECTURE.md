# Arquitetura

Notas sobre as decisões do serviço. A ordem segue mais ou menos o caminho de uma
operação: dinheiro, agregados, transação SQL, idempotência, concorrência, referências,
mensageria, auth, composição, observabilidade e o que ficou de fora.

## Organização

Hexagonal por cima do layout padrão de Go:

- `internal/domain` — `money`, `wallet`, `wager`, `event`. Só depende da stdlib e de
  `google/uuid`. Nenhum import de Fx, pgx, SQS ou HTTP.
- `internal/app` — casos de uso e as **portas** em `app/port` (um arquivo por
  interface). `wagering` tem `process_operation.go` (fluxo e idempotência),
  `business_rules.go` (regras por tipo) e `pending_reference.go` (espera e retomada);
  `wallets` tem abertura, consultas e reconciliação.
- `internal/adapter` — implementações: `postgres`, `sqs`, `httpapi`, `auth`.
- `internal/worker` — loops de fundo (outbox e referências pendentes). Falam só com
  as portas e com o `Processor`.
- `internal/fxapp` — o único lugar que conhece `fx`; um arquivo por módulo.

O código não tem comentários: cada arquivo carrega um assunto e o nome de cada função
descreve o que ela faz. As razões das decisões ficam aqui.

Os agregados têm campos privados, construtores que validam e métodos de transição.
Criação (`wallet.Open`, `wager.NewExternal`, `wager.NewOpening`) é separada de
reidratação (`wallet.Rehydrate`, `wager.Rehydrate(Snapshot)`); reidratar não dispara
transição, movimentação nem evento. O repositório conversa com a transação via
`Snapshot`, que é a forma plana persistida.

## Money

`int64` em centavos + código de moeda (`^[A-Z]{3}$`). Escala fixa de 2. Limite:
±92.233.720.368.547.758,07. Parsing por regexp `^-?[0-9]+\.[0-9]{2}$` seguido de
`strconv.ParseInt`, que já falha em overflow. Soma, subtração e negação checam
overflow; comparação e aritmética exigem a mesma moeda e devolvem erro (não `false`)
quando não é.

Entradas externas passam por `ParseNonNegative`. **Não há normalização**: `"25"`,
`"25.0"`, `"2.5e1"`, `" 25.00"` são rejeitados, então a string que entra é
exatamente a que vai para o hash de idempotência. Valores negativos são permitidos
em `Money` para diferenças internas (reconciliação), nunca no saldo.

Persistência: `BIGINT` nas tabelas, moeda em `CHAR(3)` na carteira e na transação.
O ledger não repete a moeda; ele é lido sempre no contexto da carteira.

## Wallet e ledger

`(player_id, currency)` é único. Saldo `>= 0` via `CHECK`. Versão começa em 1 e só
sobe em `Debit`/`Credit`. A abertura com saldo positivo produz `wallet.OpeningEntry`
(0 → saldo) sem mexer na versão, porque faz parte da criação.

`LedgerEntry` valida `balanceAfter = balanceBefore ± amount` no construtor e na
reidratação; o banco repete a mesma regra em `ledger_arithmetic`. `UNIQUE (wallet_id,
transaction_id)` garante um lançamento por transação. Um trigger `BEFORE UPDATE OR
DELETE` levanta exceção: o ledger é append-only inclusive para quem tem acesso direto
ao banco. Correção financeira é um novo lançamento (na prática, um `ROLLBACK`).

Não implementei partidas dobradas.

## WagerTransaction

Uma tabela para as duas origens, com `origin` (`INTERNAL`/`EXTERNAL`) e um `CHECK`
(`wager_tx_origin_shape`) que obriga todos os metadados externos em `EXTERNAL` e
proíbe todos em `INTERNAL`. `OPENING` só existe como `INTERNAL`, único por carteira
(`wager_tx_single_opening`), e é rejeitado com 400 se vier por HTTP ou SQS
(`ParseExternalKind` não o aceita).

### Máquina de estados

```
PENDING ──► PROCESSED
   │  ▲
   │  └──── PENDING_REFERENCE ──► REJECTED
   │              ▲   │
   └──────────────┘   └──────────► FAILED
   └──► REJECTED
   └──► FAILED
```

`PROCESSED`, `REJECTED` e `FAILED` são terminais; `transition()` recusa qualquer
mudança a partir deles com `domain.TransitionError`. `PENDING_REFERENCE` pode
voltar a `PENDING_REFERENCE` (cada tentativa incrementa `attempts` e reagenda
`next_attempt_at`).

Transitório × permanente: erro de infraestrutura durante o processamento (Postgres
fora, timeout, deadlock) faz a transação SQL inteira dar rollback — a linha nem chega
a existir. Para HTTP é 503; para SQS a mensagem fica na fila e volta depois. `FAILED`
existe no modelo e no schema para registrar uma falha permanente auditável, mas hoje
nenhum caminho de código chega nele: como o processamento é síncrono dentro de uma
única transação, não há estado intermediário para "falhar de forma durável".

Aceite: operações sem dependência são concluídas síncronas sem commit intermediário,
como o enunciado permite. Um `PENDING` confirmado no banco só existe na forma de
`PENDING_REFERENCE`, e esse é retomado por qualquer instância pelo worker.

### Tipos

| Tipo | Efeito | Regras |
| --- | --- | --- |
| `BET` | débito | valor > 0; saldo suficiente ou `INSUFFICIENT_FUNDS` |
| `WIN` | crédito | valor > 0; referência opcional (deve ser `BET` da mesma rodada) |
| `LOSS` | nenhum | valor exatamente `0.00`; sem ledger, sem bump de versão; gera `WagerTransactionProcessed` |
| `REFUND` | crédito | referência obrigatória, deve ser `BET` `PROCESSED` de mesmo valor |
| `ROLLBACK` | contrário ao original | referência obrigatória, deve ser `BET`/`WIN`/`REFUND` `PROCESSED` de mesmo valor |

Referência e operação devem coincidir em provedor (a busca já é por
`(provider_id, external_transaction_id)`), jogador, carteira, moeda e rodada; senão
`REFERENCE_MISMATCH`. Valor diferente também é `REFERENCE_MISMATCH` (sem reversão
parcial).

**REFUND × ROLLBACK sobre a mesma aposta**: uma referência aceita uma única reversão
bem-sucedida, de qualquer tipo. O índice parcial `wager_tx_single_reversal` sobre
`(provider_id, reference_external_transaction_id) WHERE kind IN ('REFUND','ROLLBACK')
AND status = 'PROCESSED'` impõe isso no banco; o caso de uso consulta antes e devolve
`REFERENCE_ALREADY_REVERSED`. Rejeitadas não contam. Para desfazer um `REFUND`
faz-se `ROLLBACK` apontando para o `REFUND` (que debita o valor de volta). Assim o
mesmo débito nunca é devolvido duas vezes.

Reversão que precisaria debitar mais do que há (rollback de `WIN`/`REFUND`) é
rejeitada com `REVERSAL_INSUFFICIENT_FUNDS`, código distinto do de aposta sem saldo.

### Códigos de rejeição

Rejeições são persistidas (`status = REJECTED`, `failure_code`), replayáveis e
definitivas. Entradas corrigíveis nunca viram transação: são 400 sem persistir.

| `failureCode` | Quando |
| --- | --- |
| `INSUFFICIENT_FUNDS` | `BET` maior que o saldo |
| `REVERSAL_INSUFFICIENT_FUNDS` | `ROLLBACK` de `WIN`/`REFUND` maior que o saldo |
| `REFERENCE_NOT_FOUND` | referência não apareceu dentro do orçamento de tentativas |
| `REFERENCE_NOT_PROCESSED` | referência existe mas terminou `REJECTED`/`FAILED` |
| `REFERENCE_MISMATCH` | jogador, carteira, moeda, rodada ou valor diferentes da referência |
| `REFERENCE_KIND_NOT_ALLOWED` | ex.: `REFUND` de um `WIN`, `ROLLBACK` de um `LOSS` |
| `REFERENCE_ALREADY_REVERSED` | já houve reversão `PROCESSED` dessa referência |
| `WALLET_MISMATCH` | `playerId` do corpo não é o dono da carteira |
| `CURRENCY_MISMATCH` | moeda da operação difere da moeda da carteira |
| `INTERNAL_ERROR` | reservado para `FAILED` |

Fiz `WALLET_MISMATCH` e `CURRENCY_MISMATCH` como rejeições (e não 400) porque só dá
para saber depois de carregar a carteira, e porque um provedor mandando dinheiro para
a carteira errada é algo que vale ficar registrado.

## Transação SQL e repositórios

`port.UnitOfWork.Do(ctx, fn)` abre uma transação pgx (`READ COMMITTED`), monta um
`port.Repos` com os cinco repositórios ligados àquela `pgx.Tx`, e faz commit se `fn`
retornar nil. Todo caso de uso roda dentro de um único `Do`. `DoSnapshot` é
`REPEATABLE READ` + `READ ONLY` para leituras que precisam de uma visão consistente
(reconciliação, GETs).

Uma operação externa faz, nesta ordem e na mesma transação:

1. `inbox.Register` (só SQS)
2. `SELECT ... FOR UPDATE` na carteira
3. busca por `(provider_id, idempotency_key)` → replay ou conflito
4. busca por `(provider_id, external_transaction_id)` → conflito de chave
5. regras de negócio no domínio (`Processor.applyBusinessRules` em `business_rules.go`)
6. `INSERT` da transação, `INSERT` do ledger, `UPDATE` da carteira com `WHERE version = $lida`, `INSERT`s na outbox
7. `inbox.Complete`
8. commit

Se qualquer passo falhar, nada fica. Se o `INSERT` bater numa unique (alguém concluiu
a mesma operação entre os passos 3 e 6, o que o lock torna improvável mas não
impossível se a carteira do payload for outra), o `Processor` roda a transação inteira
de novo uma vez e cai no replay.

Biblioteca: `pgx/v5` com SQL escrito à mão. Sem `sqlc`, sem ORM. Erros do driver são
traduzidos em `postgres.translateError` para os sentinelas de `port` (`ErrNotFound`,
`ErrConflict` com o nome da constraint, `ErrStale`, `ErrUnavailable`).

## Idempotência

Persistida em duas uniques parciais (`origin = 'EXTERNAL'`) em `wager_transactions`:
`(provider_id, idempotency_key)` e `(provider_id, external_transaction_id)`.

- HTTP: chave vem do header `Idempotency-Key` (obrigatório, nunca substituído).
- SQS: chave é `data.idempotencyKey`. Além disso a inbox deduplica por
  `(consumer_name, message_id)` do envelope.
- Mesma chave, mesmo hash → resultado persistido, `idempotentReplay: true`,
  `balance` = `balance_after` da transação (o saldo observado na hora, mesmo que a
  carteira já tenha mudado).
- Mesma chave, hash diferente → 409 `IDEMPOTENCY_KEY_CONFLICT`.
- Mesmo `externalTransactionId`, chave diferente → 409 `EXTERNAL_TRANSACTION_ID_REUSED`.
- Replay de rejeição devolve 422 de novo; replay de pendência devolve 202.

**Hash do payload**: SHA-256 do JSON canônico de
`providerId, externalTransactionId, playerId, walletId, roundId, gameId, kind,
money{amount, currency}` e, se presente, `referenceExternalTransactionId`. Chaves em
ordem alfabética (é um `map` no Go, `encoding/json` ordena), sem espaços, `amount`
como a string recebida. Fora do hash: chave de idempotência, `correlationId`,
`messageId`, headers. O mesmo `wagering.Operation` é construído pelo handler HTTP e
pelo consumidor SQS, então o hash é idêntico nos dois caminhos por construção — o
teste `TestSQSProcessAndRedeliver` confirma o replay cruzado.

## Concorrência

Coordenação **por carteira**, com três camadas:

1. **Lock pessimista**: `SELECT ... FOR UPDATE` na linha da carteira, tomado antes de
   qualquer leitura de transação. Todas as escritas para a mesma carteira serializam
   aí; carteiras diferentes não se veem.
2. **Verificação otimista**: `UPDATE wallets ... WHERE version = $lida`. Com o lock isso
   nunca deveria falhar; se falhar vira `ErrStale` e o caso de uso reexecuta.
3. **Constraints**: `balance >= 0`, uniques de idempotência, unique de lançamento,
   unique de reversão. Valem mesmo se a aplicação tiver um bug ou se a deduplicação
   do FIFO deixar passar algo.

Não há lock global, advisory lock ou mutex de processo. Tudo é linha de banco, então
funciona igual com uma ou dez instâncias. O cenário "100.00 com duas apostas de 80.00"
é o teste `TestConcurrentTwoBetsOnHundred`, rodado 5 vezes; a variante `make
test-multi` distribui as chamadas diretamente entre três containers.

Ordem de locks para evitar deadlock: o caminho HTTP/SQS trava só a carteira. O worker
de referências trava primeiro a transação (`FOR UPDATE SKIP LOCKED`) e depois a
carteira; como ninguém mais trava linhas de transação, não há ciclo.

## Referências pendentes

`REFUND`/`ROLLBACK` (ou `WIN` com referência) cuja referência ainda não existe — ou
existe mas ainda não é terminal — vira `PENDING_REFERENCE` com `attempts = 1` e
`next_attempt_at = now + backoff`. Emite `WagerTransactionPendingReference`. Por
HTTP a resposta é 202; por SQS a mensagem é removida (a pendência já está durável).

`worker.PendingReferenceRetrier` roda em todas as instâncias: a cada
`REFERENCE_POLL_INTERVAL` faz `SELECT ... WHERE status = 'PENDING_REFERENCE' AND
next_attempt_at <= now() LIMIT 1 FOR UPDATE SKIP LOCKED`, trava a carteira e chama o
mesmo `apply` do caminho normal. Backoff exponencial: `base * 2^attempts` limitado a
`REFERENCE_MAX_BACKOFF` (2s, 4s, 8s, ... até 2m). Depois de
`REFERENCE_MAX_ATTEMPTS` esperas, a próxima avaliação rejeita com
`REFERENCE_NOT_FOUND` e emite `WagerTransactionRejected`.

Se a referência existe mas está `PENDING_REFERENCE` (ex.: rollback de um refund que
ainda espera a aposta), a operação continua esperando com o mesmo backoff. Se a
referência terminou `REJECTED`/`FAILED`, rejeita na hora com `REFERENCE_NOT_PROCESSED`.

## Inbox e outbox

**Inbox** (`inbox_messages`): PK `(consumer_name, message_id)`, `payload_hash`
(SHA-256 do corpo bruto), `received_at`, `completed_at`. O `INSERT ... ON CONFLICT DO
NOTHING` acontece na mesma transação das mudanças de domínio. Numa reentrega
concorrente, a unique faz o segundo `INSERT` esperar o primeiro commitar; depois ele
lê a linha, confere o hash e devolve o resultado persistido. Hash diferente para o
mesmo `messageId` é erro permanente → DLQ. A mensagem SQS só é apagada depois do
commit; se o processo cair antes de `DeleteMessage`, a reentrega cai no caso
duplicado (`TestCrashBetweenCommitAndDelete`).

**Outbox** (`outbox_events`): `id` = `eventId`, `aggregate_id`, `event_type`,
`payload` (envelope inteiro em JSONB, snapshot imutável), `occurred_at`, `attempts`,
`next_attempt_at`, `locked_by`, `locked_until`, `published_at`, `last_error`. Os
eventos são inseridos no mesmo commit da transação de negócio — o publisher só vê
linhas commitadas, então nada é publicado antes do commit.

`worker.OutboxPublisher` roda em todas as instâncias. Cada passada faz um `UPDATE ...
WHERE id IN (SELECT ... FOR UPDATE SKIP LOCKED) RETURNING` que toma um lease de
`OUTBOX_LEASE` sobre até `OUTBOX_BATCH_SIZE` linhas devidas e não travadas (ou com
lease vencido, ou já travadas por ele mesmo). Publica uma a uma; sucesso → `published_at`, falha → `attempts++`,
`next_attempt_at` com backoff (1s, 2s, ... até 1m), lease liberado. Um publisher que
morre depois do `SendMessage` e antes de marcar deixa a linha com lease; quando vence,
outra instância republica **com o mesmo `eventId`** e o FIFO descarta pela
`MessageDeduplicationId` (janela de 5 min). Consumidores devem deduplicar por
`eventId` além disso. `TestOutboxCompetingPublishers` verifica `attempts = 1` em
todas as linhas com dois publishers disputando.

**Destino**: `wallet-events.fifo`. `MessageGroupId` = `aggregateId` (carteira), para
ordem por carteira. Atributos `eventType` e `eventId` na mensagem para filtragem sem
parsear o corpo.

### Eventos

Envelope: `eventId` (UUIDv7), `eventType`, `aggregateId` (carteira), `correlationId`
(header `X-Correlation-ID` ou `messageId`/`correlationId` da mensagem), `causationId`
(id da transação), `occurredAt` (RFC 3339 UTC), `version` (1), `data`.

| Evento | Gatilho | `data` |
| --- | --- | --- |
| `WagerTransactionProcessed` | operação `PROCESSED`, incluindo `LOSS` e `OPENING` | transactionId, walletId, playerId, kind, money, balanceAfter, providerId/externalTransactionId/roundId/gameId (vazios em OPENING), referenceTransactionId, processedAt |
| `WagerTransactionRejected` | rejeição definitiva | + failureCode, rejectedAt |
| `WagerTransactionPendingReference` | cada vez que a operação é reagendada | referenceExternalTransactionId, attempt, nextAttemptAt |
| `WalletBalanceChanged` | ledger gravado | walletId, transactionId, direction, money, balanceBefore, balanceAfter, walletVersion |

Tipo e versão são fixados pelos construtores em `domain/event`.

## Consumidor SQS

`wager-transactions.fifo` com redrive para `wager-transactions-dlq.fifo` após 5
recebimentos (`maxReceiveCount` no init do LocalStack = `SQS_MAX_RECEIVE_COUNT`).
`VisibilityTimeout` 30s. Long polling de 10s, lotes de até 10, `SQS_WORKERS`
goroutines por instância; mensagens do mesmo lote são tratadas em sequência para não
desfazer a ordem do grupo localmente.

Para o produtor: `MessageGroupId` = `walletId` (ordem por carteira, paralelismo entre
carteiras), `MessageDeduplicationId` = qualquer id único da tentativa de envio (o
`messageId` do envelope serve). A deduplicação real é da inbox, não do FIFO.

Tratamento:

- **Inválida** (JSON quebrado, tipo desconhecido, campos faltando, valor mal
  formado, `OPENING`): enviada para a DLQ com atributo `reason` e apagada, sem passar
  pela inbox. Não vale a pena ocupar 5 ciclos de visibilidade com algo que nunca vai
  funcionar.
- **Permanente** (carteira inexistente, conflito de chave, hash divergente): idem.
- **Rejeição de negócio**: é um resultado; commit e delete.
- **Transitória** (banco fora, timeout): `ChangeMessageVisibility` com backoff por
  `ApproximateReceiveCount` (2s, 4s, ... até o visibility timeout); depois de 5
  recebimentos o redrive manda para a DLQ.

Em `SIGTERM`: o loop para de fazer `ReceiveMessage`; o que já foi recebido termina
(o contexto de processamento não é cancelado pelo shutdown) dentro de
`SHUTDOWN_TIMEOUT - 5s`; o que sobrou no lote tem a visibilidade zerada para voltar
imediatamente a outra instância.

## Autenticação e autorização

Keycloak 26 como IdP, realm `wager` importado na subida. Clients com
`client_credentials`; cada provedor é um client com um protocol mapper que coloca a
claim fixa `provider_id` no access token, e um mapper de audience que adiciona
`wager-api`. Roles de realm `provider` e `internal` atribuídas ao service account.

Validação (`adapter/auth`): `go-oidc` com `NewRemoteKeySet` apontando para o JWKS e
`NewVerifier` com o issuer esperado e `ClientID = wager-api` (checa `aud`). Assinatura,
`exp`, `iss` e `aud` são verificados localmente; sem chamada ao IdP por requisição.
Chaves são cacheadas e rotacionadas pelo `go-oidc`. Issuer e URL do JWKS são
configurados separados porque dentro do compose a API chega no Keycloak por
`keycloak:8080` enquanto os tokens carregam `localhost:8180`; `KC_HOSTNAME` fixa o
issuer independente de por onde o token foi pedido.

Modelo de permissão:

| Rota | Quem |
| --- | --- |
| `POST /wallets`, `GET /wallets/*`, `POST /wallets/*/reconciliation`, `DELETE /rate-limits[/{sub}]` | `internal` |
| `POST /wagering/transactions` | `provider` cujo `provider_id` == `providerId` do corpo, ou `internal` |
| `GET /providers/{p}/wagering/transactions/{ext}` | `provider` com `provider_id == p`, ou `internal` |
| `GET /wagering/transactions/{id}` | `internal`; `provider` só se a transação for dele (senão 404, para não revelar ids) |
| `/health/*`, `/metrics` | públicos |

O `providerId` do corpo nunca é sobrescrito pelo token: se divergirem é 403. Isso
mantém o contrato explícito e evita que um provedor "herde" outro por acidente.

Mensageria: o LocalStack não impõe IAM, então aqui o controle fica documentado: em
produção a fila de entrada aceita `SendMessage` só dos provedores (uma policy por
principal), e o consumidor tem `ReceiveMessage`/`DeleteMessage`/
`ChangeMessageVisibility` na fila de entrada e `SendMessage` na DLQ e na de eventos.
O consumidor continua validando domínio, dono da carteira e chaves como se a mensagem
fosse hostil.

### Rate limit

Não estava no enunciado, mas um provedor com bug (ou alguém com um token vazado) não
deveria conseguir derrubar o serviço. `httpapi.RateLimiter` é um token bucket
(`x/time/rate`) por chamador: `sub` do token quando autenticado, IP (via `X-Real-IP`
do nginx) quando não. `RATE_LIMIT_RPS`/`RATE_LIMIT_BURST`, resposta 429 com
`Retry-After`, buckets ociosos são varridos a cada minuto. É **por instância** — com
três instâncias o limite efetivo é ~3x. Um limite global precisaria de Redis ou
similar e não me pareceu justificado aqui.

Um chamador bloqueado volta sozinho conforme o bucket enche, mas um provedor legítimo
que tomou 429 por um pico não deveria depender disso. `DELETE /rate-limits/{sub}` zera
o bucket de um chamador e `DELETE /rate-limits` zera todos, na instância que atendeu a
chamada. As duas rotas exigem `internal` e ficam fora do limitador, para o próprio
administrador não se trancar. Como o estado é por instância, o reset também é; o
playground (`unblock`) repete a chamada nas três.

## Uso do Fx

`fxapp.Options` monta sete módulos: `core` (logger, métricas, clock), `persistence`
(pool, unit of work, migrations opcionais), `messaging` (client SQS, publisher),
`auth` (verifier), `usecases`, `http`, `workers`. Tudo por construtor; nenhum
singleton global.

Ciclo de vida:

- `OnStart` do pool faz `Ping` com retry (30 × 1s); do SQS, `GetQueueAttributes`;
  do auth, um `GET` no JWKS. Sem isso o start falha — melhor cair no boot do que na
  primeira requisição. `TestFxStartStop` confirma que um JWKS inválido impede o start.
- O servidor HTTP faz `Listen` sincronamente no `OnStart` (porta ocupada = erro de
  start) e `Shutdown` no `OnStop` com `SHUTDOWN_TIMEOUT`.
- Workers têm `Start`/`Stop` com `context.CancelFunc` + `done` channel; `Stop` espera
  o loop devolver ou o contexto do Fx expirar.
- Fx executa `OnStop` na ordem inversa dos `OnStart`: primeiro o consumidor e os
  workers, depois o servidor HTTP, por último o pool. Nada tenta usar uma conexão
  fechada.

`fx.ValidateApp` roda em `go test ./...` (`internal/fxapp/app_test.go`) e pega
dependência faltando sem subir nada.

## Observabilidade

Logs JSON via `log/slog` com `correlationId`, `messageId`, `transactionId`,
`walletId`, `providerId` quando disponíveis. Não loga token, corpo completo nem
valores além de status e códigos.

Métricas Prometheus em `/metrics` (registry próprio, para os testes poderem criar
várias instâncias):

| Métrica | O que |
| --- | --- |
| `wager_transactions_total{kind,status}` | resultados |
| `wager_idempotent_replays_total` | replays |
| `wager_idempotency_conflicts_total` | chave reutilizada com payload diferente |
| `wager_concurrency_conflicts_total` | unique/version conflict que causou reexecução |
| `wager_processing_seconds{kind}` | latência |
| `wager_reference_retries_total` | reavaliações de pendência |
| `wallet_reconciliation_divergences_total` | reconciliações inconsistentes |
| `outbox_published_total`, `outbox_retries_total`, `outbox_lag_seconds` | outbox |
| `inbox_duplicates_total`, `consumer_retries_total`, `consumer_dlq_total` | consumidor |

Health: `/health/live` responde sempre; `/health/ready` faz `Ping` no Postgres e
`GetQueueAttributes` no SQS. O compose usa o ready como healthcheck das instâncias.

## Reconciliação

`POST /wallets/{id}/reconciliation` roda num `REPEATABLE READ` read-only: lê a
carteira e `SUM(CASE direction ...)` + `COUNT(*)` do ledger na mesma foto.
`difference = stored - calculated`. Divergência vira log `ERROR` e incrementa a
métrica. Nunca escreve.

## Como verificar na mão

Além da suíte automatizada há um REPL (`make play`, `cmd/playground`) que fala com a
API e com as filas do ambiente local. Ele existe para o avaliador reproduzir os cenários
sem escrever `curl`: replay e conflito de chave, 50 cópias em paralelo, duas apostas
disputando o saldo, reversão antes da referência, envio pela fila e reentrega do mesmo
`messageId`, mensagem inválida indo para a DLQ, eventos publicados, rate limit, bateria
de autorização, health e métricas por instância. O README tem a referência completa.

`make demo` sobe tudo de um checkout limpo e `make verify` roda todas as camadas de
teste, incluindo a execução contra as três instâncias.

A migration `000002` cria `playground_wallets`, que liga cada carteira aberta pelo
playground ao nome de quem a abriu, para retomar uma sessão depois. É uma tabela de apoio
da ferramenta: o domínio, os casos de uso e a API não a conhecem; só o `cmd/playground`
lê e escreve nela, direto no Postgres.

## Limitações e o que ficou de fora

- `FAILED` não é atingido por nenhum caminho (explicado acima).
- Rate limit é por instância.
- Sem tracing OpenTelemetry, sem dashboards, sem teste de carga.
- Sem partidas dobradas.
- O nginx faz round-robin simples; não há sticky session (nem precisa).
- O healthcheck do Keycloak no compose é um check TCP + string no `/health/ready` da
  porta de management, porque a imagem não tem `curl`. A API tem seu próprio retry
  no boot, então um falso positivo ali só atrasa.
- Policies IAM do SQS estão descritas, não aplicadas (LocalStack não as impõe).
- O cursor do ledger é o `seq` (identity) em base64; estável e opaco, mas não
  criptografado — quem quiser pode adivinhar o próximo. Como só `internal` acessa o
  ledger, achei aceitável.
- `WIN` aceita referência opcional e, se informada e ausente, espera como uma reversão.
  Se isso não for desejado, é uma linha no `apply`.
