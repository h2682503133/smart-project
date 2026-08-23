package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/outbox"
)

type EventStore interface {
	ClaimAvailableByEventPrefix(context.Context, time.Time, string, int, time.Duration) ([]outbox.Event, error)
	MarkPublished(context.Context, uint64, time.Time) error
	MarkFailed(context.Context, uint64, string, time.Time, time.Time) error
}

type Dispatcher struct {
	store      EventStore
	client     *http.Client
	notifyURL  string
	serviceKey string
	now        func() time.Time
}

func NewDispatcher(store EventStore, client *http.Client, notifyURL, serviceKey string) (*Dispatcher, error) {
	if store == nil || client == nil || strings.TrimSpace(notifyURL) == "" || strings.TrimSpace(serviceKey) == "" {
		return nil, errors.New("knowledge dispatcher dependencies are required")
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(notifyURL))
	if err != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("knowledge notify URL must be an absolute HTTP URL")
	}
	return &Dispatcher{
		store: store, client: client, notifyURL: strings.TrimSpace(notifyURL),
		serviceKey: serviceKey, now: time.Now,
	}, nil
}

func (d *Dispatcher) DispatchOnce(ctx context.Context, batchSize int) (int, error) {
	if batchSize < 1 || batchSize > 500 {
		return 0, errors.New("knowledge dispatcher batch size must be between 1 and 500")
	}
	now := d.now().UTC()
	events, err := d.store.ClaimAvailableByEventPrefix(ctx, now, "KNOWLEDGE_DOCUMENT_", batchSize, 30*time.Second)
	if err != nil {
		return 0, fmt.Errorf("find knowledge outbox events: %w", err)
	}
	published := 0
	var dispatchErrors []error
	for index := range events {
		if err := d.deliver(ctx, &events[index]); err != nil {
			message := err.Error()
			if len(message) > 1000 {
				message = message[:1000]
			}
			failureTime := d.now().UTC()
			availableAt := failureTime.Add(retryDelay(events[index].RetryCount + 1))
			if markErr := d.store.MarkFailed(ctx, events[index].ID, message, availableAt, failureTime); markErr != nil {
				dispatchErrors = append(dispatchErrors, fmt.Errorf("mark knowledge event %d failed: %w", events[index].ID, markErr))
			}
			dispatchErrors = append(dispatchErrors, fmt.Errorf("deliver knowledge event %d: %w", events[index].ID, err))
			continue
		}
		if err := d.store.MarkPublished(ctx, events[index].ID, now); err != nil {
			dispatchErrors = append(dispatchErrors, fmt.Errorf("mark knowledge event %d published: %w", events[index].ID, err))
			continue
		}
		published++
	}
	return published, errors.Join(dispatchErrors...)
}

func (d *Dispatcher) Run(ctx context.Context, interval time.Duration, batchSize int) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := d.DispatchOnce(ctx, batchSize); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("dispatch knowledge outbox", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) deliver(ctx context.Context, event *outbox.Event) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.notifyURL, bytes.NewReader(event.Payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Service-Key", d.serviceKey)
	response, err := d.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("agent returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func retryDelay(retryCount int) time.Duration {
	if retryCount < 1 {
		retryCount = 1
	}
	if retryCount > 8 {
		retryCount = 8
	}
	return time.Duration(1<<(retryCount-1)) * time.Second
}
