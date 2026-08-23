package objectstore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	Secure    bool
}

type MinIO struct {
	client *minio.Client
	bucket string
	region string
}

func NewMinIO(config Config) (*MinIO, error) {
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.Secure,
		Region: config.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create MinIO client: %w", err)
	}
	return &MinIO{client: client, bucket: config.Bucket, region: config.Region}, nil
}

func (m *MinIO) EnsureBucket(ctx context.Context) error {
	exists, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return fmt.Errorf("check MinIO bucket %s: %w", m.bucket, err)
	}
	if exists {
		return nil
	}
	if err := m.client.MakeBucket(ctx, m.bucket, minio.MakeBucketOptions{Region: m.region}); err != nil {
		// Multiple stateless API instances may race to create the same bucket.
		// Recheck before failing startup so bucket creation remains idempotent.
		exists, checkErr := m.client.BucketExists(ctx, m.bucket)
		if checkErr == nil && exists {
			return nil
		}
		return fmt.Errorf("create MinIO bucket %s: %w", m.bucket, err)
	}
	return nil
}

func (m *MinIO) Put(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, m.bucket, objectKey, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put MinIO object %s: %w", objectKey, err)
	}
	return nil
}

func (m *MinIO) Remove(ctx context.Context, objectKey string) error {
	if err := m.client.RemoveObject(ctx, m.bucket, objectKey, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove MinIO object %s: %w", objectKey, err)
	}
	return nil
}

func (m *MinIO) PresignedGet(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	result, err := m.client.PresignedGetObject(ctx, m.bucket, objectKey, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign MinIO object %s: %w", objectKey, err)
	}
	return result.String(), nil
}
