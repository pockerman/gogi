package minio

import (
	"bytes"
	"context"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type GogiMinIOClient struct {
	client *minio.Client
}

func NewGogiMinIOClient(host, user, password string) *GogiMinIOClient {

	client, _ := minio.New(host, &minio.Options{
		Creds: credentials.NewStaticV4(
			user,
			password,
			"",
		),
		Secure: false, // true if using HTTPS
	})

	return &GogiMinIOClient{client: client}
}

// func NewClient() (*minio.Client, error) {
// // 	return minio.New("localhost:9000", &minio.Options{
// // 		Creds: credentials.NewStaticV4(
// // 			"minioadmin",
// // 			"minioadmin",
// // 			"",
// // 		),
// // 		Secure: false, // true if using HTTPS
// // 	})
// // }

func (minioClient *GogiMinIOClient) CreateBucket(bucket string) error {
	ctx := context.Background()

	exists, err := minioClient.client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	return minioClient.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
}

func (minioClient *GogiMinIOClient) UploadFile(bucket, object, filename string) error {

	ctx := context.Background()

	_, err := minioClient.client.FPutObject(
		ctx,
		bucket,
		object,
		filename,
		minio.PutObjectOptions{},
	)

	return err
}

func (minioClient *GogiMinIOClient) UploadBytes(

	bucket,
	object string,
	data []byte,
	contentType string,
) error {

	reader := bytes.NewReader(data)

	_, err := minioClient.client.PutObject(
		context.Background(),
		bucket,
		object,
		reader,
		int64(len(data)),
		minio.PutObjectOptions{
			ContentType: contentType,
		},
	)

	return err
}

func (minioClient *GogiMinIOClient) Download(
	bucket,
	object string,
) ([]byte, error) {

	obj, err := minioClient.client.GetObject(
		context.Background(),
		bucket,
		object,
		minio.GetObjectOptions{},
	)
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	return io.ReadAll(obj)
}

func (minioClient *GogiMinIOClient) Delete(
	bucket,
	object string,
) error {

	return minioClient.client.RemoveObject(
		context.Background(),
		bucket,
		object,
		minio.RemoveObjectOptions{},
	)
}

// uploads larger than this are sent in parts of this size. Without it, an upload of unknown
// size buffers parts sized for the largest possible object
const streamPartSize = 16 * 1024 * 1024

// Put stores the reader's content; a size of -1 means unknown, and it's read until EOF
func (minioClient *GogiMinIOClient) Put(
	ctx context.Context,
	bucket,
	object string,
	content io.Reader,
	size int64,
	contentType string,
) (int64, error) {

	if err := minioClient.CreateBucket(bucket); err != nil {
		return 0, err
	}

	info, err := minioClient.client.PutObject(ctx, bucket, object, content, size, minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    streamPartSize,
	})
	return info.Size, err
}

// Remove deletes the object; removing one that doesn't exist does nothing
func (minioClient *GogiMinIOClient) Remove(ctx context.Context, bucket, object string) error {
	return minioClient.client.RemoveObject(ctx, bucket, object, minio.RemoveObjectOptions{})
}

// RemovePrefix deletes every object whose key starts with the prefix
func (minioClient *GogiMinIOClient) RemovePrefix(ctx context.Context, bucket, prefix string) error {

	exists, err := minioClient.client.BucketExists(ctx, bucket)
	if err != nil || !exists {
		return err
	}

	objects := minioClient.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	var firstErr error
	// drain the channel, so the removal finishes
	for removeErr := range minioClient.client.RemoveObjects(ctx, bucket, objects, minio.RemoveObjectsOptions{}) {
		if firstErr == nil {
			firstErr = removeErr.Err
		}
	}
	return firstErr
}
