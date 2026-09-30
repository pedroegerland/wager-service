package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/adapter/sqs"
)

func (s *session) viaSQS(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: sqs <BET|WIN|LOSS|REFUND|ROLLBACK> <valor> [ref]")
	}
	if err := s.requireWallet(); err != nil {
		return err
	}
	kind := strings.ToUpper(args[0])
	op := operation{kind: kind, amount: args[1], ext: s.newExt(kind)}
	if len(args) > 2 {
		op.ref = args[2]
	}
	op.key = s.providerForOps() + ":" + op.ext
	s.remember(op)

	data := s.submitBody(op)
	data["idempotencyKey"] = op.key
	messageID := "msg-" + uuid.NewString()[:8]
	body, _ := json.Marshal(map[string]any{
		"messageId": messageID, "type": "WagerTransactionRequested",
		"occurredAt": time.Now().UTC().Format(time.RFC3339Nano), "data": data,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := sqs.NewClient(ctx, sqs.Config{Endpoint: s.awsEndpoint, Region: "us-east-1", AccessKeyID: "test", SecretAccessKey: "test"})
	if err != nil {
		return err
	}
	queue := s.awsEndpoint + "/000000000000/wager-transactions.fifo"
	if _, err := client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl: aws.String(queue), MessageBody: aws.String(string(body)),
		MessageGroupId: aws.String(s.walletID), MessageDeduplicationId: aws.String(uuid.NewString()),
	}); err != nil {
		return err
	}
	fmt.Printf("mensagem %s enviada (externalTransactionId %s). Aguardando o consumidor...\n", messageID, op.ext)

	for i := 0; i < 40; i++ {
		time.Sleep(500 * time.Millisecond)
		r, err := s.call("GET", "/providers/"+s.providerForOps()+"/wagering/transactions/"+op.ext, s.provider, nil, nil)
		if err != nil {
			return err
		}
		if r.status == 200 {
			s.show(r)
			return s.wallet(nil)
		}
	}
	fmt.Println("ainda não processada; consulte depois com: tx", op.ext)
	return nil
}
