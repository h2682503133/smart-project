package mqttclient

import (
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/config"
)

func TestStartDoesNotWaitForOfflineBrokerOrDevice(t *testing.T) {
	handler := NewHandler("agri", &resolverStub{}, &ingestorStub{})
	client := New(config.MQTTConfig{
		BrokerURL: "tcp://127.0.0.1:1", ClientID: "mqtt-offline-test", TopicPrefix: "agri",
		ConnectTimeout: 50 * time.Millisecond, ReconnectBackoff: 20 * time.Millisecond, MessageTimeout: time.Second,
	}, handler)
	done := make(chan struct{})
	go func() {
		client.Start()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start() blocked while MQTT broker was unavailable")
	}
	client.Close()
}
