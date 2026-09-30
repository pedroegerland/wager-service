package wager

import "fmt"

type Kind string

const (
	KindOpening  Kind = "OPENING"
	KindBet      Kind = "BET"
	KindWin      Kind = "WIN"
	KindLoss     Kind = "LOSS"
	KindRefund   Kind = "REFUND"
	KindRollback Kind = "ROLLBACK"
)

func ParseExternalKind(s string) (Kind, error) {
	switch Kind(s) {
	case KindBet, KindWin, KindLoss, KindRefund, KindRollback:
		return Kind(s), nil
	}
	return "", fmt.Errorf("unknown or not allowed kind %q", s)
}

func (k Kind) IsReversal() bool { return k == KindRefund || k == KindRollback }

func (k Kind) RequiresReference() bool { return k.IsReversal() }
