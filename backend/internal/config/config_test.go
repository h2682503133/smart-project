package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{"DB_DSN", "DB_URL", "DB_USERNAME", "DB_PASSWORD", "DB_POOL_MAX_SIZE", "DB_POOL_MIN_IDLE", "DB_MIGRATE", "SERVER_PORT", "GIN_MODE", "JWT_SECRET", "JWT_ISSUER", "JWT_TTL", "INTERNAL_SERVICE_KEY", "KNOWLEDGE_NOTIFY_URL", "AGENT_ALERT_URL", "OUTBOX_DISPATCH_INTERVAL", "OUTBOX_BATCH_SIZE", "AGENT_HTTP_TIMEOUT", "OBJECT_STORAGE_ENABLED", "MINIO_SECURE", "KNOWLEDGE_MAX_UPLOAD_BYTES", "MINIO_SIGNED_URL_TTL", "REDIS_ENABLED", "REDIS_URL", "REDIS_POOL_SIZE", "REDIS_DIAL_TIMEOUT", "REDIS_READ_TIMEOUT", "REDIS_WRITE_TIMEOUT", "REDIS_QUERY_CACHE_TTL", "REDIS_ALERT_CACHE_TTL", "REDIS_TELEMETRY_TTL", "REDIS_IRRIGATION_TTL", "REDIS_DEVICE_ACTIVITY_TTL", "DEVICE_OFFLINE_AFTER", "MQTT_ENABLED", "MQTT_BROKER_URL", "MQTT_CLIENT_ID", "MQTT_USERNAME", "MQTT_PASSWORD", "MQTT_TOPIC_PREFIX", "MQTT_CONNECT_TIMEOUT", "MQTT_RECONNECT_BACKOFF", "MQTT_MESSAGE_TIMEOUT"} {
		t.Setenv(key, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != "8080" {
		t.Fatalf("server port = %q, want 8080", cfg.Server.Port)
	}
	if !strings.Contains(cfg.Database.DSN, "@tcp(localhost:3307)/smart_agriculture") {
		t.Fatalf("unexpected default DSN %q", cfg.Database.DSN)
	}
	if !strings.Contains(cfg.Database.DSN, "parseTime=true") || !strings.Contains(cfg.Database.DSN, "multiStatements=true") {
		t.Fatalf("default DSN is missing required options: %q", cfg.Database.DSN)
	}
	if strings.Contains(cfg.Database.DSN, "allowPublicKeyRetrieval") {
		t.Fatalf("default DSN contains unsupported JDBC option: %q", cfg.Database.DSN)
	}
	if cfg.Auth.TokenTTL != 2*time.Hour || cfg.Auth.Issuer != "smart-agriculture-api" {
		t.Fatalf("unexpected auth defaults: %+v", cfg.Auth)
	}
	if len(cfg.Internal.ServiceKey) < 32 {
		t.Fatalf("internal service key is too short: %q", cfg.Internal.ServiceKey)
	}
	if cfg.Internal.KnowledgeNotifyURL != "" || cfg.Internal.AgentAlertURL != "" || cfg.Internal.OutboxBatchSize != 50 || cfg.Internal.OutboxDispatchInterval != 2*time.Second {
		t.Fatalf("unexpected internal defaults: %+v", cfg.Internal)
	}
	if cfg.ObjectStorage.Enabled || cfg.ObjectStorage.MaxUploadBytes != 20*1024*1024 || cfg.ObjectStorage.SignedURLTimeout != 15*time.Minute {
		t.Fatalf("unexpected object storage defaults: %+v", cfg.ObjectStorage)
	}
	if !cfg.Redis.Enabled || cfg.Redis.URL != "redis://localhost:6379/0" || cfg.Redis.QueryCacheTTL != time.Minute || cfg.Redis.DeviceOfflineAfter != 2*time.Minute {
		t.Fatalf("unexpected Redis defaults: %+v", cfg.Redis)
	}
	if !cfg.MQTT.Enabled || cfg.MQTT.BrokerURL != "tcp://localhost:1883" || cfg.MQTT.TopicPrefix != "agri" || cfg.MQTT.ClientID != "smart-agriculture-api" {
		t.Fatalf("unexpected MQTT defaults: %+v", cfg.MQTT)
	}
}

func TestInvalidMQTTConfiguration(t *testing.T) {
	t.Setenv("MQTT_BROKER_URL", "http://localhost:1883")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want unsupported MQTT scheme")
	}
	t.Setenv("MQTT_BROKER_URL", "tcp://localhost:1883")
	t.Setenv("MQTT_TOPIC_PREFIX", "agri/+")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want wildcard topic prefix")
	}
}

func TestInvalidRedisConfiguration(t *testing.T) {
	t.Setenv("REDIS_POOL_SIZE", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid Redis pool size")
	}
	t.Setenv("REDIS_POOL_SIZE", "10")
	t.Setenv("REDIS_DEVICE_ACTIVITY_TTL", "1m")
	t.Setenv("DEVICE_OFFLINE_AFTER", "2m")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want activity TTL ordering error")
	}
}

func TestInvalidJWTConfiguration(t *testing.T) {
	t.Setenv("JWT_SECRET", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want short JWT secret error")
	}

	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("JWT_TTL", "never")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid JWT TTL error")
	}
}

func TestInvalidInternalServiceKey(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_KEY", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want short internal service key error")
	}
}

func TestDBDSNTakesPrecedence(t *testing.T) {
	t.Setenv("DB_DSN", "user:secret@tcp(db:3306)/app?parseTime=true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, option := range []string{"parseTime=true", "multiStatements=true", "charset=utf8mb4"} {
		if !strings.Contains(cfg.Database.DSN, option) {
			t.Fatalf("DSN %q is missing %s", cfg.Database.DSN, option)
		}
	}
}

func TestInvalidDBDSN(t *testing.T) {
	t.Setenv("DB_DSN", "://not-a-dsn")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid DSN error")
	}
}
