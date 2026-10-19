package objects

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

// AzureBlobStore keeps objects in Azure Blob Storage: a bucket is a container of the storage account, an object a
// blob in it
type AzureBlobStore struct {
	client *azblob.Client
	// the containers known to be there
	containers sync.Map
}

func NewAzureBlobStore(client *azblob.Client) *AzureBlobStore {
	return &AzureBlobStore{client: client}
}

// NewAzureBlobStoreFromDefaults creates a store for the storage account at accountURL, e.g.
// https://gogi.blob.core.windows.net/, signed in as Azure's tools are: on an Azure VM, its managed identity
// (AZURE_CLIENT_ID chooses one of several); elsewhere, e.g. AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_CLIENT_SECRET
func NewAzureBlobStoreFromDefaults(accountURL string) (*AzureBlobStore, error) {
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to find Azure credentials: %w", err)
	}
	client, err := azblob.NewClient(accountURL, credential, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create the Azure Blob Storage client for %s: %w", accountURL, err)
	}
	return NewAzureBlobStore(client), nil
}

// ensureContainer makes the container if it isn't there
func (s *AzureBlobStore) ensureContainer(ctx context.Context, container string) error {
	if _, ok := s.containers.Load(container); ok {
		return nil
	}
	_, err := s.client.CreateContainer(ctx, container, nil)
	if err != nil && !bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
		return fmt.Errorf("failed to make container %s: %w", container, err)
	}
	s.containers.Store(container, true)
	return nil
}

// counter counts what's read through it
type counter struct {
	io.Reader
	n int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.n += int64(n)
	return n, err
}

func (s *AzureBlobStore) Put(
	ctx context.Context, container, key string, content io.Reader, _ int64, contentType string,
) (int64, error) {
	if err := s.ensureContainer(ctx, container); err != nil {
		return 0, err
	}
	counted := &counter{Reader: content}
	_, err := s.client.UploadStream(ctx, container, key, counted, &azblob.UploadStreamOptions{
		BlockSize:   streamPartSize,
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: to.Ptr(contentType)},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to put %s/%s: %w", container, key, err)
	}
	return counted.n, nil
}

func (s *AzureBlobStore) Get(ctx context.Context, container, key string, offset, length int64) (io.ReadCloser, error) {
	response, err := s.client.DownloadStream(ctx, container, key, &azblob.DownloadStreamOptions{
		Range: blob.HTTPRange{Offset: offset, Count: length},
	})
	if bloberror.HasCode(err, bloberror.BlobNotFound, bloberror.ContainerNotFound) {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, container, key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get %s/%s: %w", container, key, err)
	}
	return response.Body, nil
}

func (s *AzureBlobStore) Remove(ctx context.Context, container, key string) error {
	_, err := s.client.DeleteBlob(ctx, container, key, &azblob.DeleteBlobOptions{
		DeleteSnapshots: to.Ptr(blob.DeleteSnapshotsOptionTypeInclude),
	})
	if err != nil && !bloberror.HasCode(err, bloberror.BlobNotFound, bloberror.ContainerNotFound) {
		return fmt.Errorf("failed to remove %s/%s: %w", container, key, err)
	}
	return nil
}

func (s *AzureBlobStore) RemovePrefix(ctx context.Context, container, prefix string) error {
	pager := s.client.NewListBlobsFlatPager(container, &azblob.ListBlobsFlatOptions{Prefix: &prefix})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if bloberror.HasCode(err, bloberror.ContainerNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to list %s/%s: %w", container, prefix, err)
		}
		for _, item := range page.Segment.BlobItems {
			if err := s.Remove(ctx, container, *item.Name); err != nil {
				return err
			}
		}
	}
	return nil
}
