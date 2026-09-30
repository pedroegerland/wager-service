package fxapp

import (
	"log/slog"
	"time"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/adapter/sqs"
	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/config"
	"github.com/pedroegerland/wager-service/internal/worker"
)

var WorkerModule = fx.Module("workers",
	fx.Provide(
		func(uow port.UnitOfWork, pub port.EventPublisher, cfg config.Config, log *slog.Logger, m port.Metrics) *worker.OutboxPublisher {
			return worker.NewOutboxPublisher(uow, pub, worker.OutboxConfig{
				PollInterval: cfg.OutboxPollInterval, BatchSize: cfg.OutboxBatchSize, Lease: cfg.OutboxLease,
			}, log.With("component", "outbox"), m)
		},
		func(p *wagering.Processor, cfg config.Config, log *slog.Logger) *worker.PendingReferenceRetrier {
			return worker.NewPendingReferenceRetrier(p, worker.PendingReferenceConfig{
				PollInterval: cfg.ReferencePollInterval,
			}, log.With("component", "pending-reference"))
		},
		func(c *awssqs.Client, cfg config.Config, p *wagering.Processor, log *slog.Logger, m port.Metrics) *sqs.OperationConsumer {
			return sqs.NewOperationConsumer(c, sqs.OperationConsumerConfig{
				QueueURL: cfg.SQSInboundQueueURL, DLQURL: cfg.SQSDLQURL, Workers: cfg.SQSWorkers,
				VisibilityTimeout: cfg.SQSVisibilityTimeout, MaxReceiveCount: cfg.SQSMaxReceiveCount,
				DrainTimeout: cfg.ShutdownTimeout - 5*time.Second,
			}, p, log.With("component", "consumer"), m)
		},
	),
	fx.Invoke(func(lc fx.Lifecycle, cfg config.Config, ob *worker.OutboxPublisher, pr *worker.PendingReferenceRetrier, c *sqs.OperationConsumer) {
		if cfg.OutboxEnabled {
			lc.Append(fx.Hook{OnStart: ob.Start, OnStop: ob.Stop})
		}
		lc.Append(fx.Hook{OnStart: pr.Start, OnStop: pr.Stop})
		if cfg.ConsumerEnabled {
			lc.Append(fx.Hook{OnStart: c.Start, OnStop: c.Stop})
		}
	}),
)
