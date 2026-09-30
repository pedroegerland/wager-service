package main

import (
	"strings"
	"testing"
)

func TestSQSOperationArguments(t *testing.T) {
	ok := []string{"BET 15", "bet 15", "WIN 10", "WIN 10 bet-1", "LOSS 0", "REFUND 10 bet-1", "ROLLBACK 10 bet-1"}
	for _, line := range ok {
		if _, err := sqsOperation(strings.Fields(line)); err != nil {
			t.Errorf("%q: %v", line, err)
		}
	}
	bad := map[string]string{
		"BET":             "uso:",
		"BET 15 ROLLBACK": "não aceita ref",
		"LOSS 0 bet-1":    "não aceita ref",
		"LOSS 5":          "só aceita 0.00",
		"REFUND 10":       "precisa do ref",
		"ROLLBACK 10":     "precisa do ref",
		"DEPOSIT 10":      "desconhecido",
		"OPENING 10":      "desconhecido",
	}
	for line, want := range bad {
		_, err := sqsOperation(strings.Fields(line))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want message containing %q", line, err, want)
		}
	}
	op, _ := sqsOperation(strings.Fields("rollback 12.5 bet-1"))
	if op.kind != "ROLLBACK" || op.amount != "12.50" || op.ref != "bet-1" {
		t.Errorf("parsed %+v", op)
	}
}
