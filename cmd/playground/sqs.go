package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func sqsOperation(args []string) (operation, error) {
	usage := "uso: sqs <BET|WIN|LOSS|REFUND|ROLLBACK> <valor> [ref]; ref é o externalTransactionId referenciado"
	if len(args) < 2 {
		return operation{}, fmt.Errorf("%s", usage)
	}
	kind := strings.ToUpper(args[0])
	op := operation{kind: kind, amount: normalizeAmount(args[1])}
	if len(args) > 2 {
		op.ref = args[2]
	}
	switch kind {
	case "BET", "LOSS":
		if op.ref != "" {
			return operation{}, fmt.Errorf("%s não aceita ref (você passou %q); use: sqs %s <valor>", kind, op.ref, kind)
		}
	case "WIN":
	case "REFUND", "ROLLBACK":
		if op.ref == "" {
			return operation{}, fmt.Errorf("%s precisa do ref: sqs %s <valor> <externalTransactionId da operação referenciada>", kind, kind)
		}
	default:
		return operation{}, fmt.Errorf("kind %q desconhecido; %s", args[0], usage)
	}
	if kind == "LOSS" && op.amount != "0.00" {
		return operation{}, fmt.Errorf("LOSS só aceita 0.00 (você passou %s)", op.amount)
	}
	return op, nil
}

func (s *session) viaSQS(args []string) error {
	op, err := sqsOperation(args)
	if err != nil {
		return err
	}
	if err := s.requireWallet(); err != nil {
		return err
	}
	op.ext = s.newExt(op.kind)
	op.key = s.providerForOps() + ":" + op.ext

	data := s.submitBody(op)
	data["idempotencyKey"] = op.key
	op.messageID = "msg-" + uuid.NewString()[:8]
	body, _ := json.Marshal(map[string]any{
		"messageId": op.messageID, "type": "WagerTransactionRequested",
		"occurredAt": time.Now().UTC().Format(time.RFC3339Nano), "data": data,
	})
	op.sqsBody = string(body)
	s.remember(op)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.sendToQueue(ctx, inboundQueuePath, op.sqsBody, s.walletID); err != nil {
		return err
	}
	fmt.Printf("mensagem %s enviada (externalTransactionId %s). Aguardando o consumidor...\n", op.messageID, op.ext)

	for i := range 40 {
		time.Sleep(500 * time.Millisecond)
		r, err := s.call("GET", "/providers/"+s.providerForOps()+"/wagering/transactions/"+op.ext, s.provider, nil, nil)
		if err != nil {
			return err
		}
		if r.status == 200 {
			s.show(r)
			return s.wallet(nil)
		}
		if i%4 == 3 {
			if reason, found := s.findInDLQ(ctx, op.messageID); found {
				fmt.Println(red("o consumidor recusou a mensagem e ela foi para a DLQ. motivo: " + reason))
				return nil
			}
		}
	}
	fmt.Println(red("não processada em 20s e não está na DLQ; consulte depois com: tx " + op.ext + " ou dlq"))
	return nil
}

func (s *session) findInDLQ(ctx context.Context, messageID string) (string, bool) {
	items, err := s.drainQueue(ctx, dlqPath, 50)
	if err != nil {
		return "", false
	}
	var reason string
	found := false
	for _, it := range items {
		raw := fmt.Sprint(it["_raw"])
		if it["messageId"] == messageID || strings.Contains(raw, messageID) {
			reason = fmt.Sprint(it["_reason"])
			found = true
			continue
		}
		body := raw
		if it["_raw"] == nil {
			b, _ := json.Marshal(it)
			body = string(b)
		}
		if err := s.sendToQueue(ctx, dlqPath, body, "requeued"); err != nil {
			fmt.Println(yellow("aviso: uma mensagem alheia da DLQ foi lida e não pôde ser devolvida: " + truncate(body, 80)))
		}
	}
	return reason, found
}
