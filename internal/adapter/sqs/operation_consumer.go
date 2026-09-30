package sqs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
)

const OperationConsumerName = "wager-transactions-consumer"

type OperationConsumerConfig struct {
	QueueURL          string
	DLQURL            string
	Workers           int
	WaitTimeSeconds   int32
	VisibilityTimeout int32
	MaxReceiveCount   int
	DrainTimeout      time.Duration
}

type OperationConsumer struct {
	client  *awssqs.Client
	cfg     OperationConsumerConfig
	proc    *wagering.Processor
	log     *slog.Logger
	metrics port.Metrics

	cancel context.CancelFunc
	wg     sync.WaitGroup

	beforeDeleteHook func(messageID string) bool
}

func NewOperationConsumer(client *awssqs.Client, cfg OperationConsumerConfig, proc *wagering.Processor, log *slog.Logger, m port.Metrics) *OperationConsumer {
	if cfg.Workers <= 0 {
		cfg.Workers = 2
	}
	if cfg.WaitTimeSeconds <= 0 {
		cfg.WaitTimeSeconds = 10
	}
	if cfg.VisibilityTimeout <= 0 {
		cfg.VisibilityTimeout = 30
	}
	if cfg.MaxReceiveCount <= 0 {
		cfg.MaxReceiveCount = 5
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 20 * time.Second
	}
	if m == nil {
		m = port.NopMetrics{}
	}
	return &OperationConsumer{client: client, cfg: cfg, proc: proc, log: log, metrics: m}
}

func (c *OperationConsumer) Start(ctx context.Context) error {
	if _, err := c.client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{QueueUrl: aws.String(c.cfg.QueueURL)}); err != nil {
		return fmt.Errorf("sqs queue not reachable: %w", err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	for i := 0; i < c.cfg.Workers; i++ {
		c.wg.Add(1)
		go c.pollLoop(runCtx, i)
	}
	c.log.Info("sqs consumer started", "workers", c.cfg.Workers, "queue", c.cfg.QueueURL)
	return nil
}

func (c *OperationConsumer) Stop(ctx context.Context) error {
	if c.cancel == nil {
		return nil
	}
	c.cancel()
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
		c.log.Info("sqs consumer stopped")
		return nil
	case <-time.After(c.cfg.DrainTimeout):
		return errors.New("sqs consumer: drain timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *OperationConsumer) pollLoop(ctx context.Context, id int) {
	defer c.wg.Done()
	log := c.log.With("worker", id)
	for ctx.Err() == nil {
		out, err := c.client.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
			QueueUrl:              aws.String(c.cfg.QueueURL),
			MaxNumberOfMessages:   10,
			WaitTimeSeconds:       c.cfg.WaitTimeSeconds,
			VisibilityTimeout:     c.cfg.VisibilityTimeout,
			AttributeNames:        []types.QueueAttributeName{"ApproximateReceiveCount", "MessageGroupId"},
			MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Warn("receive failed", "err", err)
			sleepUnlessCancelled(ctx, 2*time.Second)
			continue
		}

		for _, msg := range out.Messages {
			if ctx.Err() != nil {
				c.makeVisibleAfter(msg, 0)
				continue
			}
			c.handleMessage(context.WithoutCancel(ctx), msg, log)
		}
	}
}

func (c *OperationConsumer) handleMessage(ctx context.Context, msg types.Message, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.VisibilityTimeout)*time.Second)
	defer cancel()

	receives, _ := strconv.Atoi(msg.Attributes["ApproximateReceiveCount"])
	body := aws.ToString(msg.Body)
	sqsID := aws.ToString(msg.MessageId)

	op, err := c.operationFromMessage(body, sqsID)
	if err != nil {
		log.Warn("invalid message", "sqsMessageId", sqsID, "err", err)
		c.moveToDeadLetterQueue(ctx, msg, "invalid: "+err.Error())
		return
	}
	log = log.With("messageId", op.Inbox.MessageID, "providerId", op.ProviderID, "walletId", op.WalletID, "correlationId", op.CorrelationID)

	res, err := c.proc.Process(ctx, op)
	switch {
	case err == nil:
		log.Info("message processed", "transactionId", res.TransactionID, "status", res.Status, "replay", res.IdempotentReplay)
	case isPermanentError(err):
		log.Warn("message rejected permanently", "err", err)
		c.moveToDeadLetterQueue(ctx, msg, err.Error())
		return
	default:

		c.metrics.ConsumerRetry()
		delay := redeliveryDelay(receives, time.Duration(c.cfg.VisibilityTimeout)*time.Second)
		log.Warn("message deferred", "err", err, "receiveCount", receives, "retryIn", delay)
		c.makeVisibleAfter(msg, int32(delay.Seconds()))
		return
	}

	if c.beforeDeleteHook != nil && !c.beforeDeleteHook(op.Inbox.MessageID) {
		return
	}
	c.deleteMessage(ctx, msg)
}

func (c *OperationConsumer) deleteMessage(ctx context.Context, msg types.Message) {
	_, err := c.client.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl: aws.String(c.cfg.QueueURL), ReceiptHandle: msg.ReceiptHandle,
	})
	if err != nil {
		c.log.Warn("delete failed, message will be redelivered", "sqsMessageId", aws.ToString(msg.MessageId), "err", err)
	}
}

func (c *OperationConsumer) makeVisibleAfter(msg types.Message, seconds int32) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.client.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
		QueueUrl: aws.String(c.cfg.QueueURL), ReceiptHandle: msg.ReceiptHandle, VisibilityTimeout: seconds,
	})
	if err != nil {
		c.log.Warn("change visibility failed", "sqsMessageId", aws.ToString(msg.MessageId), "err", err)
	}
}

func (c *OperationConsumer) moveToDeadLetterQueue(ctx context.Context, msg types.Message, reason string) {
	c.metrics.DLQ()
	group := msg.Attributes["MessageGroupId"]
	if group == "" {
		group = "invalid"
	}
	_, err := c.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:               aws.String(c.cfg.DLQURL),
		MessageBody:            msg.Body,
		MessageGroupId:         aws.String(group),
		MessageDeduplicationId: aws.String(aws.ToString(msg.MessageId)),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"reason": {DataType: aws.String("String"), StringValue: aws.String(truncateString(reason, 256))},
		},
	})
	if err != nil {
		c.log.Error("dlq send failed; leaving message for redrive", "sqsMessageId", aws.ToString(msg.MessageId), "err", err)
		return
	}
	c.deleteMessage(ctx, msg)
}

func redeliveryDelay(receives int, max time.Duration) time.Duration {
	if receives < 1 {
		receives = 1
	}
	d := 2 * time.Second
	for i := 1; i < receives && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func sleepUnlessCancelled(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
