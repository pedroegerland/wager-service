package fxapp

import (
	"testing"

	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/config"
)

func TestGraphIsComplete(t *testing.T) {
	cfg := config.Config{
		DatabaseURL:        "postgres://x:y@localhost:5432/db",
		OIDCIssuer:         "http://localhost:8180/realms/wager",
		OIDCJWKSURL:        "http://localhost:8180/realms/wager/protocol/openid-connect/certs",
		OIDCAudience:       "wager-api",
		SQSInboundQueueURL: "http://localhost:4566/000000000000/wager-transactions.fifo",
		SQSDLQURL:          "http://localhost:4566/000000000000/wager-transactions-dlq.fifo",
		SQSEventsQueueURL:  "http://localhost:4566/000000000000/wallet-events.fifo",
		RateLimitRPS:       10, RateLimitBurst: 10, ReferenceMaxAttempts: 3, SQSVisibilityTimeout: 30, DBMaxConns: 4,
	}
	if err := fx.ValidateApp(Options(cfg)); err != nil {
		t.Fatal(err)
	}
}
