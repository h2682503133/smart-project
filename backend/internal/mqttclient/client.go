package mqttclient

import (
	"context"
	"log/slog"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/config"
	paho "github.com/eclipse/paho.mqtt.golang"
)

type Client struct {
	client         paho.Client
	topic          string
	handler        *Handler
	messageTimeout time.Duration
}

func New(cfg config.MQTTConfig, handler *Handler) *Client {
	topic := cfg.TopicPrefix + "/+/+/telemetry"
	result := &Client{topic: topic, handler: handler, messageTimeout: cfg.MessageTimeout}
	options := paho.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetCleanSession(false).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(cfg.ReconnectBackoff).
		SetConnectTimeout(cfg.ConnectTimeout).
		SetKeepAlive(30 * time.Second).
		SetPingTimeout(10 * time.Second)
	options.SetConnectionLostHandler(func(_ paho.Client, err error) {
		slog.Warn("MQTT connection lost; reconnecting", "error", err)
	})
	options.SetOnConnectHandler(func(client paho.Client) {
		slog.Info("MQTT broker connected", "topic", topic)
		token := client.Subscribe(topic, 1, result.onMessage)
		if !token.WaitTimeout(cfg.ConnectTimeout) {
			slog.Error("MQTT subscription timed out", "topic", topic)
			return
		}
		if err := token.Error(); err != nil {
			slog.Error("subscribe to MQTT telemetry", "topic", topic, "error", err)
		}
	})
	result.client = paho.NewClient(options)
	return result
}

// Start begins connecting in the background. With ConnectRetry enabled, an
// unavailable broker or an offline device never prevents the HTTP API starting.
func (c *Client) Start() {
	token := c.client.Connect()
	go func() {
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Warn("MQTT initial connection failed; retrying", "error", err)
		}
	}()
}

func (c *Client) Close() {
	c.client.Disconnect(250)
}

func (c *Client) onMessage(_ paho.Client, message paho.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), c.messageTimeout)
	defer cancel()
	if err := c.handler.Handle(ctx, message.Topic(), message.Payload()); err != nil {
		slog.Warn("reject MQTT telemetry", "topic", message.Topic(), "messageId", message.MessageID(), "error", err)
	}
}
