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

func NewClient() (*minio.Client, error) {
	return minio.New("localhost:9000", &minio.Options{
		Creds: credentials.NewStaticV4(
			"minioadmin",
			"minioadmin",
			"",
		),
		Secure: false, // true if using HTTPS
	})
}

func CreateBucket(client *minio.Client, bucket string) error {
	ctx := context.Background()

	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	return client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
}

func UploadFile(client *minio.Client, bucket, object, filename string) error {

	ctx := context.Background()

	_, err := client.FPutObject(
		ctx,
		bucket,
		object,
		filename,
		minio.PutObjectOptions{},
	)

	return err
}

func UploadBytes(
	client *minio.Client,
	bucket,
	object string,
	data []byte,
	contentType string,
) error {

	reader := bytes.NewReader(data)

	_, err := client.PutObject(
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

func Download(
	client *minio.Client,
	bucket,
	object string,
) ([]byte, error) {

	obj, err := client.GetObject(
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

func Delete(
	client *minio.Client,
	bucket,
	object string,
) error {

	return client.RemoveObject(
		context.Background(),
		bucket,
		object,
		minio.RemoveObjectOptions{},
	)
}
