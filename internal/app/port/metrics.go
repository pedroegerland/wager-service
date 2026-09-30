package port

import (
	"time"

	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type Metrics interface {
	TransactionResult(kind wager.Kind, status wager.Status)
	IdempotentReplay()
	IdempotencyConflict()
	ConcurrencyConflict()
	ProcessingLatency(kind wager.Kind, d time.Duration)
	ReferenceRetry()
	ReconciliationDivergence()
	OutboxPublished()
	OutboxRetry()
	OutboxLag(d time.Duration)
	InboxDuplicate()
	ConsumerRetry()
	DLQ()
}

type NopMetrics struct{}

func (NopMetrics) TransactionResult(wager.Kind, wager.Status)  {}
func (NopMetrics) IdempotentReplay()                           {}
func (NopMetrics) IdempotencyConflict()                        {}
func (NopMetrics) ConcurrencyConflict()                        {}
func (NopMetrics) ProcessingLatency(wager.Kind, time.Duration) {}
func (NopMetrics) ReferenceRetry()                             {}
func (NopMetrics) ReconciliationDivergence()                   {}
func (NopMetrics) OutboxPublished()                            {}
func (NopMetrics) OutboxRetry()                                {}
func (NopMetrics) OutboxLag(time.Duration)                     {}
func (NopMetrics) InboxDuplicate()                             {}
func (NopMetrics) ConsumerRetry()                              {}
func (NopMetrics) DLQ()                                        {}
