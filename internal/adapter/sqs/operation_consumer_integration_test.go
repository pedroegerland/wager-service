//go:build integration

package sqs

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedroegerland/wager-service/internal/adapter/postgres"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestCrashBetweenCommitAndDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, envOr("DATABASE_URL", "postgres://wager:wager@localhost:5432/wager?sslmode=disable"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	client, err := NewClient(ctx, Config{Endpoint: envOr("AWS_ENDPOINT_URL", "http://localhost:4566"), Region: "us-east-1", AccessKeyID: "test", SecretAccessKey: "test"})
	if err != nil {
		t.Fatal(err)
	}

	suffix := uuid.NewString()[:8]
	mk := func(name string) string {
		out, err := client.CreateQueue(ctx, &awssqs.CreateQueueInput{
			QueueName:  aws.String(name),
			Attributes: map[string]string{"FifoQueue": "true", "ContentBasedDeduplication": "false"},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = client.DeleteQueue(context.Background(), &awssqs.DeleteQueueInput{QueueUrl: out.QueueUrl})
		})
		return aws.ToString(out.QueueUrl)
	}
	queue := mk("crash-test-" + suffix + ".fifo")
	dlq := mk("crash-test-dlq-" + suffix + ".fifo")

	uow := postgres.NewUnitOfWork(pool)
	now := time.Now().UTC()
	w, err := wallet.Open(uuid.New(), money.MustFromUnits(10000, "BRL"), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO wallets (id, player_id, currency, balance, version, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		w.ID(), w.PlayerID(), w.Currency(), w.Balance().Units(), w.Version(), now, now); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	proc := wagering.NewProcessor(uow, clock{}, wagering.Config{}, log, nil)

	var deliveries atomic.Int32
	c := NewOperationConsumer(client, OperationConsumerConfig{QueueURL: queue, DLQURL: dlq, Workers: 1, WaitTimeSeconds: 1, VisibilityTimeout: 3, DrainTimeout: 10 * time.Second}, proc, log, nil)
	c.beforeDeleteHook = func(string) bool {
		return deliveries.Add(1) > 1
	}

	ext := "crash-" + suffix
	body, _ := json.Marshal(map[string]any{
		"messageId": "msg-" + suffix, "type": "WagerTransactionRequested", "occurredAt": now.Format(time.RFC3339),
		"data": map[string]any{
			"providerId": "provider-a", "externalTransactionId": ext, "idempotencyKey": "provider-a:" + ext,
			"playerId": w.PlayerID().String(), "walletId": w.ID().String(), "roundId": "r", "gameId": "g",
			"kind": "BET", "money": map[string]string{"amount": "10.00", "currency": "BRL"},
		},
	})
	if _, err := client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl: aws.String(queue), MessageBody: aws.String(string(body)),
		MessageGroupId: aws.String(w.ID().String()), MessageDeduplicationId: aws.String(uuid.NewString()),
	}); err != nil {
		t.Fatal(err)
	}

	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for deliveries.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if deliveries.Load() < 2 {
		t.Fatalf("expected a redelivery, got %d deliveries", deliveries.Load())
	}

	var balance, entries int
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, w.ID()).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1`, w.ID()).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if balance != 9000 || entries != 1 {
		t.Errorf("balance %d entries %d", balance, entries)
	}

	time.Sleep(4 * time.Second)
	attrs, err := client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl: aws.String(queue), AttributeNames: []types.QueueAttributeName{"ApproximateNumberOfMessages", "ApproximateNumberOfMessagesNotVisible"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attrs.Attributes["ApproximateNumberOfMessages"] != "0" || attrs.Attributes["ApproximateNumberOfMessagesNotVisible"] != "0" {
		t.Errorf("queue not drained: %v", attrs.Attributes)
	}
}

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }
