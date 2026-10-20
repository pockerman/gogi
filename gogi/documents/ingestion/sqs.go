package ingestion

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSQueue sends the messages to an SQS queue, which triggers the ingestion Lambda
type SQSQueue struct {
	client   *sqs.Client
	queueURL string
}

// NewSQSQueue sends to the queue at queueURL, with the credentials of the environment, e.g. the
// server's instance role
func NewSQSQueue(ctx context.Context, queueURL string) (*SQSQueue, error) {
	if queueURL == "" {
		return nil, errors.New("GOGI_SQS_QUEUE_URL names the ingestion queue")
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &SQSQueue{client: sqs.NewFromConfig(cfg), queueURL: queueURL}, nil
}

func (q *SQSQueue) Enqueue(ctx context.Context, message Message) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(q.queueURL),
		MessageBody: aws.String(string(encoded)),
	})
	return err
}
