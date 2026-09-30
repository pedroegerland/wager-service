package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *session) viaSQS(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: sqs <BET|WIN|LOSS|REFUND|ROLLBACK> <valor> [ref]; ref é o externalTransactionId referenciado (obrigatório em REFUND/ROLLBACK)")
	}
	kindArg := strings.ToUpper(args[0])
	if (kindArg == "REFUND" || kindArg == "ROLLBACK") && len(args) < 3 {
		return fmt.Errorf("%s precisa do ref: sqs %s <valor> <externalTransactionId da operação referenciada>", kindArg, kindArg)
	}
	if err := s.requireWallet(); err != nil {
		return err
	}
	kind := strings.ToUpper(args[0])
	op := operation{kind: kind, amount: normalizeAmount(args[1]), ext: s.newExt(kind)}
	if len(args) > 2 {
		op.ref = args[2]
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.sendToQueue(ctx, inboundQueuePath, op.sqsBody, s.walletID); err != nil {
		return err
	}
	fmt.Printf("mensagem %s enviada (externalTransactionId %s). Aguardando o consumidor...\n", op.messageID, op.ext)

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
	fmt.Println(red("ainda não processada; consulte depois com: tx " + op.ext))
	return nil
}
