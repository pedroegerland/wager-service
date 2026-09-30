package port

import (
	"context"
	"time"
)

type InboxMessage struct {
	ConsumerName string
	MessageID    string
	PayloadHash  string
}

type InboxState int

const (
	InboxNew InboxState = iota
	InboxDuplicate
	InboxHashMismatch
)

type InboxRepository interface {
	Register(ctx context.Context, m InboxMessage, now time.Time) (InboxState, error)
	Complete(ctx context.Context, m InboxMessage, now time.Time) error
}
