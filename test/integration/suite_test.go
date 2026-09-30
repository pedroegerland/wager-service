//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/adapter/sqs"
	"github.com/pedroegerland/wager-service/internal/config"
	"github.com/pedroegerland/wager-service/internal/fxapp"
)

var (
	databaseURL = envOr("DATABASE_URL", "postgres://wager:wager@localhost:5432/wager?sslmode=disable")
	keycloakURL = envOr("KEYCLOAK_URL", "http://localhost:8180")
	awsEndpoint = envOr("AWS_ENDPOINT_URL", "http://localhost:4566")
	apiURL      = os.Getenv("API_URL")

	pool      *pgxpool.Pool
	sqsClient *awssqs.Client
	inProcess bool
)

const (
	inboundQueue = "http://localhost:4566/000000000000/wager-transactions.fifo"
	dlqQueue     = "http://localhost:4566/000000000000/wager-transactions-dlq.fifo"
	eventsQueue  = "http://localhost:4566/000000000000/wallet-events.fifo"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func testConfig(port int) config.Config {
	return config.Config{
		HTTPAddr:              fmt.Sprintf("127.0.0.1:%d", port),
		LogLevel:              envOr("TEST_LOG_LEVEL", "warn"),
		ShutdownTimeout:       15 * time.Second,
		RateLimitRPS:          1000,
		RateLimitBurst:        2000,
		DatabaseURL:           databaseURL,
		DBMaxConns:            20,
		DBStatementTimeout:    10 * time.Second,
		OIDCIssuer:            keycloakURL + "/realms/wager",
		OIDCJWKSURL:           keycloakURL + "/realms/wager/protocol/openid-connect/certs",
		OIDCAudience:          "wager-api",
		AWSRegion:             "us-east-1",
		AWSEndpoint:           awsEndpoint,
		AWSAccessKeyID:        "test",
		AWSSecretAccessKey:    "test",
		SQSInboundQueueURL:    inboundQueue,
		SQSDLQURL:             dlqQueue,
		SQSEventsQueueURL:     eventsQueue,
		SQSWorkers:            2,
		SQSVisibilityTimeout:  10,
		SQSMaxReceiveCount:    5,
		ConsumerEnabled:       true,
		OutboxEnabled:         true,
		OutboxPollInterval:    200 * time.Millisecond,
		OutboxBatchSize:       50,
		OutboxLease:           10 * time.Second,
		ReferenceMaxAttempts:  2,
		ReferenceBaseBackoff:  time.Second,
		ReferenceMaxBackoff:   2 * time.Second,
		ReferencePollInterval: 300 * time.Millisecond,
	}
}

func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func newApp(port int, mutate func(*config.Config)) *fx.App {
	cfg := testConfig(port)
	if mutate != nil {
		mutate(&cfg)
	}
	return fx.New(fxapp.Options(cfg), fx.NopLogger)
}

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	var err error
	pool, err = pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db:", err)
		os.Exit(1)
	}
	sqsClient, err = sqs.NewClient(ctx, sqs.Config{Endpoint: awsEndpoint, Region: "us-east-1", AccessKeyID: "test", SecretAccessKey: "test"})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sqs:", err)
		os.Exit(1)
	}
	if _, err := sqsClient.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{QueueUrl: aws.String(inboundQueue)}); err != nil {
		fmt.Fprintln(os.Stderr, "sqs queues not provisioned (is localstack up?):", err)
		os.Exit(1)
	}

	var app *fx.App
	if apiURL == "" {
		inProcess = true
		port := freePort()
		apiURL = fmt.Sprintf("http://127.0.0.1:%d", port)
		app = newApp(port, nil)
		if err := app.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "app start:", err)
			os.Exit(1)
		}
	}
	cancel()

	code := m.Run()

	if app != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := app.Stop(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "app stop:", err)
			code = 1
		}
		cancel()
	}
	pool.Close()
	os.Exit(code)
}
