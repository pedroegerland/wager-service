package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/adapter/sqs"
)

const (
	inboundQueuePath = "/000000000000/wager-transactions.fifo"
	dlqPath          = "/000000000000/wager-transactions-dlq.fifo"
	eventsQueuePath  = "/000000000000/wallet-events.fifo"
)

func (s *session) sqsClient(ctx context.Context) (*awssqs.Client, error) {
	return sqs.NewClient(ctx, sqs.Config{Endpoint: s.awsEndpoint, Region: "us-east-1", AccessKeyID: "test", SecretAccessKey: "test"})
}

func (s *session) sendToQueue(ctx context.Context, path, body, group string) error {
	client, err := s.sqsClient(ctx)
	if err != nil {
		return err
	}
	_, err = client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl: aws.String(s.awsEndpoint + path), MessageBody: aws.String(body),
		MessageGroupId: aws.String(group), MessageDeduplicationId: aws.String(uuid.NewString()),
	})
	return err
}

func (s *session) drainQueue(ctx context.Context, path string, max int) ([]map[string]any, error) {
	client, err := s.sqsClient(ctx)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for len(out) < max {
		res, err := client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
			QueueUrl: aws.String(s.awsEndpoint + path), MaxNumberOfMessages: 10, WaitTimeSeconds: 1,
			MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			return nil, err
		}
		if len(res.Messages) == 0 {
			break
		}
		for _, m := range res.Messages {
			item := map[string]any{}
			_ = json.Unmarshal([]byte(aws.ToString(m.Body)), &item)
			if reason, ok := m.MessageAttributes["reason"]; ok {
				item["_reason"] = aws.ToString(reason.StringValue)
			}
			if _, ok := item["eventType"]; !ok {
				if _, ok := item["messageId"]; !ok {
					item["_raw"] = aws.ToString(m.Body)
				}
			}
			out = append(out, item)
			_, _ = client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{QueueUrl: aws.String(s.awsEndpoint + path), ReceiptHandle: m.ReceiptHandle})
		}
	}
	return out, nil
}

func (s *session) events(args []string) error {
	max := 50
	if len(args) > 0 {
		if v, err := strconv.Atoi(args[0]); err == nil && v > 0 {
			max = v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	items, err := s.drainQueue(ctx, eventsQueuePath, max)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("fila wallet-events.fifo vazia")
		return nil
	}
	fmt.Printf("%d eventos lidos e removidos de wallet-events.fifo (* = carteira atual)\n", len(items))
	for _, it := range items {
		agg, _ := it["aggregateId"].(string)
		mark := " "
		if agg == s.walletID {
			mark = "*"
		}
		data, _ := it["data"].(map[string]any)
		detail := ""
		switch it["eventType"] {
		case "WalletBalanceChanged":
			detail = fmt.Sprintf("%s %s  %s -> %s  v%v", data["direction"], amountOf(data["money"]), amountOf(data["balanceBefore"]), amountOf(data["balanceAfter"]), data["walletVersion"])
		case "WagerTransactionProcessed":
			detail = fmt.Sprintf("%s %s  saldo %s", data["kind"], amountOf(data["money"]), amountOf(data["balanceAfter"]))
		case "WagerTransactionRejected":
			detail = fmt.Sprintf("%s %s  %s", data["kind"], amountOf(data["money"]), data["failureCode"])
		case "WagerTransactionPendingReference":
			detail = fmt.Sprintf("%s aguardando %s (tentativa %v)", data["kind"], data["referenceExternalTransactionId"], data["attempt"])
		}
		fmt.Printf(" %s %-34s %s  %s\n", mark, it["eventType"], shortID(agg), detail)
	}
	return nil
}

func amountOf(v any) string {
	m, _ := v.(map[string]any)
	a, _ := m["amount"].(string)
	return a
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (s *session) dlq([]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	items, err := s.drainQueue(ctx, dlqPath, 50)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("DLQ vazia")
		return nil
	}
	fmt.Printf("%d mensagens lidas e removidas da DLQ\n", len(items))
	for _, it := range items {
		body := it["_raw"]
		if body == nil {
			b, _ := json.Marshal(it)
			body = string(b)
		}
		fmt.Printf("  motivo: %v\n  corpo:  %s\n", it["_reason"], truncate(fmt.Sprint(body), 160))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (s *session) poison([]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	marker := "poison-" + uuid.NewString()[:6]
	body := `{"messageId":"` + marker + `","type":"WagerTransactionRequested","data":{"kind":"BET","money":{"amount":"abc","currency":"BRL"}}}`
	if err := s.sendToQueue(ctx, inboundQueuePath, body, "poison"); err != nil {
		return err
	}
	fmt.Printf("mensagem inválida %s enviada para wager-transactions.fifo; esperando o consumidor mandar para a DLQ...\n", marker)
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		items, err := s.drainQueue(ctx, dlqPath, 50)
		if err != nil {
			return err
		}
		for _, it := range items {
			if strings.Contains(fmt.Sprint(it["_raw"]), marker) || it["messageId"] == marker {
				fmt.Println(green(fmt.Sprintf("chegou na DLQ com motivo: %v", it["_reason"])))
				return nil
			}
		}
	}
	fmt.Println(red("não apareceu na DLQ em 30s; confira: dlq"))
	return nil
}

func (s *session) redeliver(args []string) error {
	var op operation
	if len(args) == 1 {
		op = s.ops[args[0]]
	} else {
		for i := len(s.order) - 1; i >= 0; i-- {
			if s.ops[s.order[i]].sqsBody != "" {
				op = s.ops[s.order[i]]
				break
			}
		}
	}
	if op.sqsBody == "" {
		return fmt.Errorf("uso: redeliver [id de uma operação enviada com sqs]; nenhuma encontrada")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.sendToQueue(ctx, inboundQueuePath, op.sqsBody, s.walletID); err != nil {
		return err
	}
	fmt.Println(green(fmt.Sprintf("mesmo envelope (messageId %s) enviado de novo; a inbox deve reconhecer e nada muda no saldo", op.messageID)))
	time.Sleep(3 * time.Second)
	return s.wallet(nil)
}
