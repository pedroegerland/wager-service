//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
)

func sendMessage(t testing.TB, queue, body, group, dedup string) {
	t.Helper()
	_, err := sqsClient.SendMessage(context.Background(), &awssqs.SendMessageInput{
		QueueUrl: aws.String(queue), MessageBody: aws.String(body),
		MessageGroupId: aws.String(group), MessageDeduplicationId: aws.String(dedup),
	})
	must(t, err)
}

func transactionStatus(t testing.TB, provider, ext string) (string, bool) {
	var s string
	err := pool.QueryRow(context.Background(), `SELECT status::text FROM wager_transactions WHERE provider_id = $1 AND external_transaction_id = $2`, provider, ext).Scan(&s)
	if err != nil {
		return "", false
	}
	return s, true
}

func TestSQSProcessAndRedeliver(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	bet := operation{provider: "provider-a", round: "round-1", ext: "sqs-bet-" + uid(), kind: "BET", amount: "15.00", walletID: walletID, playerID: playerID}
	msgID := "msg-" + uid()
	body := operationMessage(bet, msgID)

	sendMessage(t, inboundQueue, body, walletID, "d-"+uid())
	waitUntil(t, 60*time.Second, "sqs bet to be processed", func() bool {
		s, ok := transactionStatus(t, bet.provider, bet.ext)
		return ok && s == "PROCESSED"
	})
	if storedBalance(t, walletID) != 8500 {
		t.Fatalf("balance %d", storedBalance(t, walletID))
	}
	waitUntil(t, 15*time.Second, "inbox completion", func() bool {
		return queryInt64(t, `SELECT COUNT(*) FROM inbox_messages WHERE message_id = $1 AND completed_at IS NOT NULL`, msgID) == 1
	})

	sendMessage(t, inboundQueue, body, walletID, "d-"+uid())

	r := submitAPI(t, bet)
	if r.Status != 200 || !r.bool("idempotentReplay") || r.money("balance") != "85.00" {
		t.Errorf("http after sqs: %d %s", r.Status, r.Raw)
	}
	time.Sleep(3 * time.Second)
	if storedBalance(t, walletID) != 8500 || ledgerCount(t, walletID) != 2 {
		t.Errorf("redelivery moved money: balance %d ledger %d", storedBalance(t, walletID), ledgerCount(t, walletID))
	}

	bet2 := operation{provider: "provider-a", round: "round-1", ext: "sqs-bet-" + uid(), kind: "BET", amount: "5.00", walletID: walletID, playerID: playerID}
	if r := submitAPI(t, bet2); r.Status != 200 {
		t.Fatalf("http bet2: %s", r.Raw)
	}
	m2 := "msg-" + uid()
	sendMessage(t, inboundQueue, operationMessage(bet2, m2), walletID, "d-"+uid())
	waitUntil(t, 20*time.Second, "sqs copy of http bet to be inboxed", func() bool {
		return queryInt64(t, `SELECT COUNT(*) FROM inbox_messages WHERE message_id = $1 AND completed_at IS NOT NULL`, m2) == 1
	})
	if storedBalance(t, walletID) != 8000 || ledgerCount(t, walletID) != 3 {
		t.Errorf("sqs after http: balance %d ledger %d", storedBalance(t, walletID), ledgerCount(t, walletID))
	}
	assertReconciled(t, walletID)
}

func TestSQSInvalidGoesToDLQ(t *testing.T) {
	marker := "broken-" + uid()
	body := `{"messageId":"` + marker + `","type":"WagerTransactionRequested","data":{"kind":"BET","money":{"amount":"abc","currency":"BRL"}}}`
	sendMessage(t, inboundQueue, body, "invalid-"+uid(), "d-"+uid())

	found := false
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && !found {
		out, err := sqsClient.ReceiveMessage(context.Background(), &awssqs.ReceiveMessageInput{
			QueueUrl: aws.String(dlqQueue), MaxNumberOfMessages: 10, WaitTimeSeconds: 2,
			MessageAttributeNames: []string{"All"},
		})
		must(t, err)
		for _, m := range out.Messages {
			if strings.Contains(aws.ToString(m.Body), marker) {
				found = true
				if r := m.MessageAttributes["reason"]; r.StringValue == nil || *r.StringValue == "" {
					t.Error("dlq message without reason attribute")
				}
			}
			_, _ = sqsClient.DeleteMessage(context.Background(), &awssqs.DeleteMessageInput{QueueUrl: aws.String(dlqQueue), ReceiptHandle: m.ReceiptHandle})
		}
	}
	if !found {
		t.Fatal("invalid message did not reach the DLQ")
	}
	if queryInt64(t, `SELECT COUNT(*) FROM inbox_messages WHERE message_id = $1`, marker) != 0 {
		t.Error("invalid message must not be inboxed")
	}
}

func TestSQSAndHTTPRace(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	a := operation{provider: "provider-a", round: "round-1", ext: "race-a-" + uid(), kind: "BET", amount: "80.00", walletID: walletID, playerID: playerID}
	b := operation{provider: "provider-a", round: "round-1", ext: "race-b-" + uid(), kind: "BET", amount: "80.00", walletID: walletID, playerID: playerID}
	sendMessage(t, inboundQueue, operationMessage(a, "msg-"+uid()), walletID, "d-"+uid())
	rb := submitAPI(t, b)
	waitUntil(t, 30*time.Second, "sqs bet to settle", func() bool {
		s, ok := transactionStatus(t, a.provider, a.ext)
		return ok && (s == "PROCESSED" || s == "REJECTED")
	})
	sa, _ := transactionStatus(t, a.provider, a.ext)
	sb := rb.str("status")
	if !((sa == "PROCESSED" && sb == "REJECTED") || (sa == "REJECTED" && sb == "PROCESSED")) {
		t.Errorf("sqs=%s http=%s", sa, sb)
	}
	if storedBalance(t, walletID) != 2000 || ledgerCount(t, walletID) != 2 {
		t.Errorf("balance %d ledger %d", storedBalance(t, walletID), ledgerCount(t, walletID))
	}
	assertReconciled(t, walletID)
}
