package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestMinIOIntegration(t *testing.T) {
	if os.Getenv("TEST_MINIO_INTEGRATION") != "1" {
		t.Skip("set TEST_MINIO_INTEGRATION=1 to run against local MinIO")
	}
	store, err := NewMinIO(Config{
		Endpoint: "localhost:9000", AccessKey: "minioadmin", SecretKey: "minioadmin",
		Bucket: "knowledge-integration", Region: "us-east-1",
	})
	if err != nil {
		t.Fatalf("NewMinIO() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket() error = %v", err)
	}
	defer func() {
		if err := store.client.RemoveBucket(context.Background(), store.bucket); err != nil {
			t.Errorf("RemoveBucket() cleanup error = %v", err)
		}
	}()
	key := fmt.Sprintf("test/%d.txt", time.Now().UnixNano())
	defer func() {
		if err := store.Remove(context.Background(), key); err != nil {
			t.Errorf("Remove() cleanup error = %v", err)
		}
	}()
	contents := []byte("knowledge integration test")
	if err := store.Put(ctx, key, bytes.NewReader(contents), int64(len(contents)), "text/plain"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	downloadURL, err := store.PresignedGet(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("PresignedGet() error = %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("download signed URL error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read signed URL response error = %v", err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Equal(body, contents) {
		t.Fatalf("download status=%d body=%q", response.StatusCode, body)
	}
}
