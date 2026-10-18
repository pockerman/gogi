package objects

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"gogi/gogi/utils"
)

func read(t *testing.T, store Store, bucket, key string, offset, length int64) string {
	t.Helper()
	file, err := store.Get(context.Background(), bucket, key, offset, length)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return string(content)
}

// testStoreContract checks the behaviour every Store must have
func testStoreContract(t *testing.T, store Store) {
	ctx := context.Background()
	// a bucket of its own, made by the store, so the test can rerun against a persistent store
	bucket := "gogi-test-" + utils.NewUUIDString()[:8]

	if _, err := store.Get(ctx, bucket, "missing", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing bucket: expected ErrNotFound, got %v", err)
	}
	if err := store.RemovePrefix(ctx, bucket, "maria/"); err != nil {
		t.Fatalf("removing from a missing bucket does nothing, got %v", err)
	}
	if err := store.Remove(ctx, bucket, "maria/lift.md"); err != nil {
		t.Fatalf("removing an object of a missing bucket does nothing, got %v", err)
	}

	size, err := store.Put(ctx, bucket, "maria/lift.md", strings.NewReader("lift and drag"), 13, "text/markdown")
	if err != nil || size != 13 {
		t.Fatalf("put: %d, %v", size, err)
	}
	if _, err := store.Get(ctx, bucket, "maria/missing", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing object: expected ErrNotFound, got %v", err)
	}

	if got := read(t, store, bucket, "maria/lift.md", 0, 0); got != "lift and drag" {
		t.Errorf("whole: %q", got)
	}
	if got := read(t, store, bucket, "maria/lift.md", 5, 3); got != "and" {
		t.Errorf("from 5, 3 bytes: %q", got)
	}
	if got := read(t, store, bucket, "maria/lift.md", 9, 0); got != "drag" {
		t.Errorf("from 9 to the end: %q", got)
	}

	// of unknown size, and larger than one part
	large := bytes.Repeat([]byte("0123456789abcdef"), 2*1024*1024)
	size, err = store.Put(ctx, bucket, "maria/large.bin", bytes.NewReader(large), -1, "application/octet-stream")
	if err != nil || size != int64(len(large)) {
		t.Fatalf("put of unknown size: %d, %v", size, err)
	}
	if got := read(t, store, bucket, "maria/large.bin", 0, 0); got != string(large) {
		t.Errorf("large: %d bytes back of %d", len(got), len(large))
	}

	if _, err := store.Put(ctx, bucket, "maria/lift.md", strings.NewReader("thrust"), 6, "text/markdown"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, store, bucket, "maria/lift.md", 0, 0); got != "thrust" {
		t.Errorf("replaced: %q", got)
	}

	if err := store.Remove(ctx, bucket, "maria/lift.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, bucket, "maria/lift.md", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed: expected ErrNotFound, got %v", err)
	}
	if err := store.Remove(ctx, bucket, "maria/lift.md"); err != nil {
		t.Errorf("removing a missing object does nothing, got %v", err)
	}

	for _, key := range []string{"maria/a", "maria/b/c", "mariana/d"} {
		if _, err := store.Put(ctx, bucket, key, strings.NewReader(key), int64(len(key)), "text/plain"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RemovePrefix(ctx, bucket, "maria/"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"maria/a", "maria/b/c", "maria/large.bin"} {
		if _, err := store.Get(ctx, bucket, key, 0, 0); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: expected it removed with its prefix, got %v", key, err)
		}
	}
	if got := read(t, store, bucket, "mariana/d", 0, 0); got != "mariana/d" {
		t.Errorf("another prefix is kept: %q", got)
	}
}

// The test runs against an S3-compatible store, e.g. MinIO:
// GOGI_TEST_S3_ENDPOINT=localhost:9000 GOGI_TEST_S3_ACCESS_KEY=admin GOGI_TEST_S3_SECRET_KEY=password \
// go test ./gogi/storage/objects/
func TestS3Store(t *testing.T) {
	endpoint := os.Getenv("GOGI_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("GOGI_TEST_S3_ENDPOINT is not set")
	}
	store, err := NewS3Store(S3Options{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("GOGI_TEST_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("GOGI_TEST_S3_SECRET_KEY"),
		Secure:    os.Getenv("GOGI_TEST_S3_SECURE") == "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	testStoreContract(t, store)
}

// Azurite's own account, the same for everyone
const azuriteConnectionString = "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;" +
	"AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;" +
	"BlobEndpoint=%s;"

// The test runs against Azure Blob Storage, or Azurite, its emulator:
// docker run -d -p 10000:10000 mcr.microsoft.com/azure-storage/azurite:3.35.0 azurite-blob --blobHost 0.0.0.0 --skipApiVersionCheck
// GOGI_TEST_AZURE_BLOB_URL=http://127.0.0.1:10000/devstoreaccount1 go test ./gogi/storage/objects/
func TestAzureBlobStore(t *testing.T) {
	blobURL := os.Getenv("GOGI_TEST_AZURE_BLOB_URL")
	if blobURL == "" {
		t.Skip("GOGI_TEST_AZURE_BLOB_URL is not set")
	}
	client, err := azblob.NewClientFromConnectionString(strings.Replace(azuriteConnectionString, "%s", blobURL, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	testStoreContract(t, NewAzureBlobStore(client))
}

func TestNewStoreFromEnv(t *testing.T) {
	for _, name := range []string{"GOGI_OBJECT_STORE", "GOGI_S3_ENDPOINT", "GOGI_S3_ACCESS_KEY", "GOGI_S3_SECRET_KEY",
		"GOGI_S3_SECURE", "GOGI_S3_REGION", "GOGI_MINIO_HOST", "GOGI_MINIO_ROOT_USER", "GOGI_MINIO_ROOT_PASSWORD",
		"AWS_REGION", "AZURE_STORAGE_ACCOUNT_URL"} {
		t.Setenv(name, "")
	}

	// as the development compose file sets it: MinIO, over plain HTTP
	t.Setenv("GOGI_MINIO_HOST", "minio:9000")
	t.Setenv("GOGI_MINIO_ROOT_USER", "admin")
	t.Setenv("GOGI_MINIO_ROOT_PASSWORD", "password")
	options := s3OptionsFromEnv()
	if options != (S3Options{Endpoint: "minio:9000", AccessKey: "admin", SecretKey: "password"}) {
		t.Errorf("MinIO: %+v", options)
	}

	// S3 on AWS: over TLS, signed in as the server's role
	t.Setenv("GOGI_MINIO_HOST", "")
	t.Setenv("GOGI_MINIO_ROOT_USER", "")
	t.Setenv("GOGI_MINIO_ROOT_PASSWORD", "")
	t.Setenv("GOGI_S3_ENDPOINT", "s3.eu-west-2.amazonaws.com")
	t.Setenv("AWS_REGION", "eu-west-2")
	options = s3OptionsFromEnv()
	if options != (S3Options{Endpoint: "s3.eu-west-2.amazonaws.com", Region: "eu-west-2", Secure: true}) {
		t.Errorf("S3: %+v", options)
	}
	store, err := NewStoreFromEnv()
	if _, ok := store.(*S3Store); err != nil || !ok {
		t.Errorf("expected an S3 store, got %T, %v", store, err)
	}

	t.Setenv("GOGI_OBJECT_STORE", "azure")
	if _, err := NewStoreFromEnv(); err == nil {
		t.Errorf("expected an error for Azure Blob Storage without its address")
	}
	t.Setenv("AZURE_STORAGE_ACCOUNT_URL", "https://gogi.blob.core.windows.net/")
	store, err = NewStoreFromEnv()
	if _, ok := store.(*AzureBlobStore); err != nil || !ok {
		t.Errorf("expected an Azure Blob store, got %T, %v", store, err)
	}

	t.Setenv("GOGI_OBJECT_STORE", "gcs")
	if _, err := NewStoreFromEnv(); err == nil {
		t.Errorf("expected an error for an unknown store")
	}
}
