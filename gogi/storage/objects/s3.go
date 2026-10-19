package objects

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// uploads larger than this are sent in parts of this size. Without it, an upload of unknown size buffers parts sized
// for the largest possible object
const streamPartSize = 16 * 1024 * 1024

// S3Options says where an S3-compatible store is, and how to sign in to it
type S3Options struct {
	// e.g. s3.eu-west-2.amazonaws.com, or minio:9000
	Endpoint string
	Region   string
	// without them, AWS' own variables, e.g. AWS_ACCESS_KEY_ID, or else the server's role, e.g. an EC2 instance's
	AccessKey string
	SecretKey string
	Secure    bool
}

// S3Store keeps objects in S3, or in an S3-compatible store such as MinIO
type S3Store struct {
	client *minio.Client
	// the buckets known to be there
	buckets sync.Map
}

func NewS3Store(options S3Options) (*S3Store, error) {
	creds := credentials.NewStaticV4(options.AccessKey, options.SecretKey, "")
	if options.AccessKey == "" {
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{},
			&credentials.IAM{},
		})
	}
	client, err := minio.New(options.Endpoint, &minio.Options{
		Creds:  creds,
		Secure: options.Secure,
		Region: options.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create the S3 client for %s: %w", options.Endpoint, err)
	}
	return &S3Store{client: client}, nil
}

func notFound(err error) bool {
	switch minio.ToErrorResponse(err).Code {
	case "NoSuchKey", "NoSuchBucket":
		return true
	}
	return false
}

// ensureBucket makes the bucket if it isn't there, e.g. in development's MinIO
func (s *S3Store) ensureBucket(ctx context.Context, bucket string) error {
	if _, ok := s.buckets.Load(bucket); ok {
		return nil
	}
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to look up bucket %s: %w", bucket, err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil &&
			minio.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
			return fmt.Errorf("failed to make bucket %s: %w", bucket, err)
		}
	}
	s.buckets.Store(bucket, true)
	return nil
}

func (s *S3Store) Put(
	ctx context.Context, bucket, key string, content io.Reader, size int64, contentType string,
) (int64, error) {
	if err := s.ensureBucket(ctx, bucket); err != nil {
		return 0, err
	}
	info, err := s.client.PutObject(ctx, bucket, key, content, size, minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    streamPartSize,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to put %s/%s: %w", bucket, key, err)
	}
	return info.Size, nil
}

func (s *S3Store) Get(ctx context.Context, bucket, key string, offset, length int64) (io.ReadCloser, error) {
	options := minio.GetObjectOptions{}
	if offset > 0 || length > 0 {
		end := int64(0)
		if length > 0 {
			end = offset + length - 1
		}
		if err := options.SetRange(offset, end); err != nil {
			return nil, err
		}
	}
	// GetObject only fetches the object when it's first read, so whether it's there is asked first; its own Stat
	// would drop the range
	_, err := s.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	var object *minio.Object
	if err == nil {
		object, err = s.client.GetObject(ctx, bucket, key, options)
	}
	if notFound(err) {
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, bucket, key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get %s/%s: %w", bucket, key, err)
	}
	return object, nil
}

func (s *S3Store) Remove(ctx context.Context, bucket, key string) error {
	err := s.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !notFound(err) {
		return fmt.Errorf("failed to remove %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (s *S3Store) RemovePrefix(ctx context.Context, bucket, prefix string) error {
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to look up bucket %s: %w", bucket, err)
	}
	if !exists {
		return nil
	}

	listed := s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	// the listing's errors come in the channel too: they stop the removal
	var listErr error
	keys := make(chan minio.ObjectInfo)
	go func() {
		defer close(keys)
		for object := range listed {
			if object.Err != nil {
				listErr = object.Err
				return
			}
			keys <- object
		}
	}()
	var removeErr error
	// drain the channel, so the removal finishes
	for result := range s.client.RemoveObjects(ctx, bucket, keys, minio.RemoveObjectsOptions{}) {
		if removeErr == nil {
			removeErr = result.Err
		}
	}
	if listErr != nil {
		return fmt.Errorf("failed to list %s/%s: %w", bucket, prefix, listErr)
	}
	if removeErr != nil {
		return fmt.Errorf("failed to remove %s/%s: %w", bucket, prefix, removeErr)
	}
	return nil
}
