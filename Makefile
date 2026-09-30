export CGO_ENABLED=0
GOFLAGS ?=

.PHONY: build test test-race vet fmt lint up down logs migrate-up migrate-down migrate-version \
        deps-up deps-down infra test-integration test-multi token demo verify play clean

build:
	go build $(GOFLAGS) -o bin/wager-service ./cmd/wager-service
	go build $(GOFLAGS) -o bin/migrate ./cmd/migrate

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint: fmt vet

## full stack: postgres, keycloak, localstack, migrations, 3 api instances, nginx
up:
	docker compose up --build -d
	@echo "api (load balanced): http://localhost:8080  instances: :8081 :8082 :8083  keycloak: http://localhost:8180"

down:
	docker compose down -v

logs:
	docker compose logs -f api-1 api-2 api-3

## only the dependencies, for running the api or the integration tests from the host
deps-up:
	docker compose up -d postgres keycloak localstack
	docker compose run --rm migrate

deps-down:
	docker compose down -v

migrate-up:
	docker compose run --rm migrate up

migrate-down:
	docker compose run --rm --entrypoint migrate migrate down

migrate-version:
	docker compose run --rm --entrypoint migrate migrate version

## detecta o sistema, inicia o docker se preciso e sobe o que não estiver respondendo
infra:
	@scripts/ensure-infra.sh

## integration tests with the app booted in-process against the real containers. Build tag keeps them out of `go test ./...`
test-integration: infra
	go test -race -tags integration -count=1 -timeout 15m ./test/integration/... ./internal/adapter/sqs/...

## same suite, but targeting the three running instances directly
test-multi: infra
	API_URL=http://localhost:8080 API_INSTANCES=http://localhost:8081,http://localhost:8082,http://localhost:8083 \
	go test -race -tags integration -count=1 -timeout 15m ./test/integration/...

token:
	@scripts/token.sh $(or $(CLIENT),provider-a)

## sobe tudo do zero e roda um fluxo de exemplo
demo:
	@scripts/local-up.sh

## gofmt + vet + unitarios + integracao (precisa do ambiente no ar)
verify:
	@scripts/verify.sh

## REPL interativo: abra carteira, aposte, replay, conflito, corrida, sqs. Sobe a infra se ela não estiver no ar
play:
	@scripts/play.sh

clean:
	rm -rf bin
