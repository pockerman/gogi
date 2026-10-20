package ingestion

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azqueue"
)

// AzureQueue sends the messages to an Azure Storage queue, which triggers the ingestion Function
type AzureQueue struct {
	client *azqueue.QueueClient
}

// NewAzureQueue sends to the queue at queueURL, e.g. https://<account>.queue.core.windows.net/ingestion,
// with the credentials of the environment, e.g. the server's managed identity
func NewAzureQueue(queueURL string) (*AzureQueue, error) {
	if queueURL == "" {
		return nil, errors.New("GOGI_AZURE_QUEUE_URL names the ingestion queue")
	}
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}
	client, err := azqueue.NewQueueClient(queueURL, credential, nil)
	if err != nil {
		return nil, err
	}
	return &AzureQueue{client: client}, nil
}

func (q *AzureQueue) Enqueue(ctx context.Context, message Message) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	// a Function's queue trigger reads messages as base64
	_, err = q.client.EnqueueMessage(ctx, base64.StdEncoding.EncodeToString(encoded), nil)
	return err
}
