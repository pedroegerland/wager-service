package sqs

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/pedroegerland/wager-service/internal/app/port"
)

type EventPublisher struct {
	client   *awssqs.Client
	queueURL string
}

func NewEventPublisher(client *awssqs.Client, queueURL string) *EventPublisher {
	return &EventPublisher{client: client, queueURL: queueURL}
}

func (p *EventPublisher) Publish(ctx context.Context, rec port.OutboxRecord) error {
	_, err := p.client.SendMessage(ctx, &awssqs.SendMessageInput{
		QueueUrl:               aws.String(p.queueURL),
		MessageBody:            aws.String(string(rec.Payload)),
		MessageGroupId:         aws.String(rec.AggregateID.String()),
		MessageDeduplicationId: aws.String(rec.ID.String()),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"eventType": {DataType: aws.String("String"), StringValue: aws.String(rec.EventType)},
			"eventId":   {DataType: aws.String("String"), StringValue: aws.String(rec.ID.String())},
		},
	})
	return err
}
