#!/bin/bash
# runs inside localstack once it's ready (mounted under /etc/localstack/init/ready.d)
set -euo pipefail

awslocal sqs create-queue --queue-name wager-transactions-dlq.fifo \
  --attributes '{"FifoQueue":"true","ContentBasedDeduplication":"false","MessageRetentionPeriod":"1209600"}'

DLQ_ARN=$(awslocal sqs get-queue-attributes \
  --queue-url http://localhost:4566/000000000000/wager-transactions-dlq.fifo \
  --attribute-names QueueArn --query 'Attributes.QueueArn' --output text)

# maxReceiveCount must match SQS_MAX_RECEIVE_COUNT in the api
awslocal sqs create-queue --queue-name wager-transactions.fifo \
  --attributes "{\"FifoQueue\":\"true\",\"ContentBasedDeduplication\":\"false\",\"VisibilityTimeout\":\"30\",\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"${DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"5\\\"}\"}"

# destination for the outbox
awslocal sqs create-queue --queue-name wallet-events.fifo \
  --attributes '{"FifoQueue":"true","ContentBasedDeduplication":"false","MessageRetentionPeriod":"345600"}'

awslocal sqs list-queues
