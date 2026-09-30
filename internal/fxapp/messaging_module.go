package fxapp

import (
	"context"
	"log/slog"
	"time"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/adapter/sqs"
	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/config"
)

var MessagingModule = fx.Module("messaging",
	fx.Provide(
		func(cfg config.Config) (*awssqs.Client, error) {
			return sqs.NewClient(context.Background(), sqs.Config{
				Endpoint: cfg.AWSEndpoint, Region: cfg.AWSRegion,
				AccessKeyID: cfg.AWSAccessKeyID, SecretAccessKey: cfg.AWSSecretAccessKey,
			})
		},
		func(c *awssqs.Client, cfg config.Config) port.EventPublisher {
			return sqs.NewEventPublisher(c, cfg.SQSEventsQueueURL)
		},
	),
	fx.Invoke(func(lc fx.Lifecycle, c *awssqs.Client, cfg config.Config, log *slog.Logger) {
		lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
			return waitForDependency(ctx, 30, time.Second, func() error { return sqs.CheckQueue(ctx, c, cfg.SQSInboundQueueURL) }, log, "sqs")
		}})
	}),
)
