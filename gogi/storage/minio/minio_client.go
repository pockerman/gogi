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
