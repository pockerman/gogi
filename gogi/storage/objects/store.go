// Package objects keeps files, e.g. documents' uploads and prompts' texts, in the object storage of where gogi runs:
// MinIO in development, S3 on AWS, Blob Storage on Azure
package objects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"gogi/gogi/utils"
)

// ErrNotFound is returned when reading an object, or from a bucket, that isn't there
var ErrNotFound = errors.New("object not found")

// Store is gogi's object storage. A bucket is made when an object is first put in it, where the store may; in the
// cloud, the deployment makes them
type Store interface {
	// Put stores the content, replacing any object of the key, and returns its size; a size of -1 means unknown,
	// and the content is read to its end
	Put(ctx context.Context, bucket, key string, content io.Reader, size int64, contentType string) (int64, error)
	// Get opens the object for reading from offset, length bytes of it, or to its end if length is 0
	Get(ctx context.Context, bucket, key string, offset, length int64) (io.ReadCloser, error)
	// Remove deletes the object; removing one that isn't there does nothing
	Remove(ctx context.Context, bucket, key string) error
	// RemovePrefix deletes every object whose key starts with the prefix
	RemovePrefix(ctx context.Context, bucket, prefix string) error
}

const (
	// StoreS3 selects S3, or an S3-compatible store such as MinIO
	StoreS3 = "s3"
	// StoreAzure selects Azure Blob Storage
	StoreAzure = "azure"
)

// NewStoreFromEnv creates the store selected by GOGI_OBJECT_STORE: "s3" (the default) or "azure"
//
// Other settings:
//   - s3: GOGI_S3_ENDPOINT, e.g. s3.eu-west-2.amazonaws.com or minio:9000; GOGI_S3_REGION, AWS_REGION by default;
//     GOGI_S3_SECURE, true by default; GOGI_S3_ACCESS_KEY and GOGI_S3_SECRET_KEY, or else AWS' own variables or the
//     server's role. Without GOGI_S3_ENDPOINT, the development settings, GOGI_MINIO_HOST, GOGI_MINIO_ROOT_USER and
//     GOGI_MINIO_ROOT_PASSWORD, are MinIO's, over plain HTTP
//   - azure: AZURE_STORAGE_ACCOUNT_URL, e.g. https://gogi.blob.core.windows.net/, signed in as the VM's managed
//     identity (AZURE_CLIENT_ID chooses one of several) or with Azure's own variables
func NewStoreFromEnv() (Store, error) {
	switch backend := utils.GetEnv("GOGI_OBJECT_STORE", StoreS3); backend {
	case StoreS3:
		return NewS3Store(s3OptionsFromEnv())
	case StoreAzure:
		accountURL := utils.GetEnv("AZURE_STORAGE_ACCOUNT_URL", "")
		if accountURL == "" {
			return nil, fmt.Errorf("GOGI_OBJECT_STORE is %q, but AZURE_STORAGE_ACCOUNT_URL is not set", backend)
		}
		return NewAzureBlobStoreFromDefaults(accountURL)
	default:
		return nil, fmt.Errorf("unknown GOGI_OBJECT_STORE %q, expected %q or %q", backend, StoreS3, StoreAzure)
	}
}

func s3OptionsFromEnv() S3Options {
	endpoint := utils.GetEnv("GOGI_S3_ENDPOINT", "")
	if endpoint == "" {
		// development's MinIO
		return S3Options{
			Endpoint:  utils.GetEnv("GOGI_MINIO_HOST", "minio:9000"),
			AccessKey: utils.GetEnv("GOGI_MINIO_ROOT_USER", "admin"),
			SecretKey: utils.GetEnv("GOGI_MINIO_ROOT_PASSWORD", "password"),
		}
	}
	secure, err := strconv.ParseBool(utils.GetEnv("GOGI_S3_SECURE", "true"))
	return S3Options{
		Endpoint:  endpoint,
		Region:    utils.GetEnv("GOGI_S3_REGION", utils.GetEnv("AWS_REGION", "")),
		AccessKey: utils.GetEnv("GOGI_S3_ACCESS_KEY", ""),
		SecretKey: utils.GetEnv("GOGI_S3_SECRET_KEY", ""),
		// anything but a clear false keeps TLS
		Secure: secure || err != nil,
	}
}

// DocumentsBucket keeps documents' uploaded files; GOGI_DOCUMENTS_BUCKET, as S3's names are shared by everyone
func DocumentsBucket() string {
	return utils.GetEnv("GOGI_DOCUMENTS_BUCKET", "gogi-documents")
}

// PromptsBucket keeps prompts' texts; GOGI_PROMPTS_BUCKET
func PromptsBucket() string {
	return utils.GetEnv("GOGI_PROMPTS_BUCKET", "gogi-prompts")
}
