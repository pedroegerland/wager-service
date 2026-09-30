//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/adapter/postgres"
	"github.com/pedroegerland/wager-service/internal/adapter/sqs"
	"github.com/pedroegerland/wager-service/internal/worker"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func insertOutbox(t testing.TB, ids []uuid.UUID, eventType string) {
	t.Helper()
	for _, id := range ids {
		payload, _ := json.Marshal(map[string]any{"eventId": id, "eventType": eventType, "aggregateId": uuid.New(), "data": map[string]any{}})
		_, err := pool.Exec(context.Background(), `INSERT INTO outbox_events (id, aggregate_id, event_type, payload, occurred_at, next_attempt_at)
			VALUES ($1, gen_random_uuid(), $2, $3, now(), now())`, id, eventType, payload)
		must(t, err)
	}
}

func TestOutboxCompetingPublishers(t *testing.T) {
	const n = 60
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.Must(uuid.NewV7())
	}
	insertOutbox(t, ids, "IntegrationTestEvent")

	uow := postgres.NewUnitOfWork(pool)
	pub := sqs.NewEventPublisher(sqsClient, eventsQueue)
	cfg := worker.OutboxConfig{BatchSize: 7, Lease: 10 * time.Second}
	p1 := worker.NewOutboxPublisher(uow, pub, cfg, quietLog(), nil)
	p2 := worker.NewOutboxPublisher(uow, pub, cfg, quietLog(), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for _, p := range []*worker.OutboxPublisher{p1, p2} {
		wg.Add(1)
		go func(p *worker.OutboxPublisher) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				if _, err := p.PublishDueEvents(ctx); err != nil {
					t.Error(err)
					return
				}
			}
		}(p)
	}
	wg.Wait()

	waitUntil(t, 20*time.Second, "all records published", func() bool {
		return queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE id = ANY($1) AND published_at IS NOT NULL`, ids) == n
	})
	if bad := queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE id = ANY($1) AND attempts <> 1`, ids); bad != 0 {
		t.Errorf("%d records were claimed more than once", bad)
	}
}

func TestOutboxAbandonedLeaseRecovered(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	insertOutbox(t, []uuid.UUID{id}, "IntegrationTestAbandoned")
	_, err := pool.Exec(context.Background(), `UPDATE outbox_events SET locked_by = 'dead-instance', locked_until = now() - interval '1 minute' WHERE id = $1`, id)
	must(t, err)

	p := worker.NewOutboxPublisher(postgres.NewUnitOfWork(pool), sqs.NewEventPublisher(sqsClient, eventsQueue), worker.OutboxConfig{BatchSize: 100}, quietLog(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := 0; i < 10; i++ {
		if _, err := p.PublishDueEvents(ctx); err != nil {
			t.Fatal(err)
		}
		if queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE id = $1 AND published_at IS NOT NULL`, id) == 1 {
			break
		}
	}
	waitUntil(t, 15*time.Second, "abandoned record to be published", func() bool {
		return queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE id = $1 AND published_at IS NOT NULL AND locked_by IS NULL`, id) == 1
	})
	if owner := queryString(t, `SELECT COALESCE(locked_by, '') FROM outbox_events WHERE id = $1`, id); owner == "dead-instance" {
		t.Error("record still owned by the dead instance")
	}
}

func TestOutboxEventReachesQueue(t *testing.T) {
	walletID, playerID := openWallet(t, "12.00")
	if r := submitAPI(t, operation{ext: "bet-" + shortID(), kind: "BET", amount: "2.00", walletID: walletID, playerID: playerID}); r.Status != 200 {
		t.Fatal(string(r.Raw))
	}
	waitUntil(t, 20*time.Second, "outbox to drain for the wallet", func() bool {
		return queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1 AND published_at IS NULL`, walletID) == 0
	})

	want := map[string]bool{}
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) && len(want) < 2 {
		out, err := sqsClient.ReceiveMessage(context.Background(), &awssqs.ReceiveMessageInput{
			QueueUrl: aws.String(eventsQueue), MaxNumberOfMessages: 10, WaitTimeSeconds: 2, MessageAttributeNames: []string{"All"},
		})
		must(t, err)
		for _, m := range out.Messages {
			var env struct {
				EventID     string `json:"eventId"`
				EventType   string `json:"eventType"`
				AggregateID string `json:"aggregateId"`
				OccurredAt  string `json:"occurredAt"`
				Version     int    `json:"version"`
				Data        struct {
					Direction     string `json:"direction"`
					WalletVersion int64  `json:"walletVersion"`
					BalanceAfter  struct {
						Amount string `json:"amount"`
					} `json:"balanceAfter"`
				} `json:"data"`
			}
			_ = json.Unmarshal([]byte(aws.ToString(m.Body)), &env)
			if env.AggregateID == walletID && env.EventType == "WalletBalanceChanged" {
				if env.EventID == "" || env.Version != 1 || env.OccurredAt == "" {
					t.Errorf("bad envelope: %s", aws.ToString(m.Body))
				}
				if attr := m.MessageAttributes["eventId"]; attr.StringValue == nil || *attr.StringValue != env.EventID {
					t.Error("eventId attribute missing or different")
				}
				want[env.Data.Direction] = true
				if env.Data.Direction == "DEBIT" && (env.Data.BalanceAfter.Amount != "10.00" || env.Data.WalletVersion != 2) {
					t.Errorf("debit event payload: %s", aws.ToString(m.Body))
				}
			}
			_, _ = sqsClient.DeleteMessage(context.Background(), &awssqs.DeleteMessageInput{QueueUrl: aws.String(eventsQueue), ReceiptHandle: m.ReceiptHandle})
		}
	}
	if !want["CREDIT"] || !want["DEBIT"] {
		t.Errorf("did not see both balance events on the queue: %v", want)
	}
}
