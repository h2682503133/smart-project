package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
)

type Config struct {
	Server        ServerConfig
	Database      DatabaseConfig
	Redis         RedisConfig
	MQTT          MQTTConfig
	TDengine      TDengineConfig
	Auth          AuthConfig
	Internal      InternalConfig
	ObjectStorage ObjectStorageConfig
}

type TDengineConfig struct {
	Enabled     bool
	RESTURL     string // taosAdapter REST 地址，如 http://localhost:6041
	Username    string
	Password    string
	Database    string
	BatchSize   int
	FlushPeriod time.Duration
}

type MQTTConfig struct {
	Enabled          bool
	BrokerURL        string
	ClientID         string
	Username         string
	Password         string
	TopicPrefix      string
	ConnectTimeout   time.Duration
	ReconnectBackoff time.Duration
	MessageTimeout   time.Duration
}

type RedisConfig struct {
	Enabled            bool
	URL                string
	PoolSize           int
	DialTimeout        time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	QueryCacheTTL      time.Duration
	AlertCacheTTL      time.Duration
	TelemetryTTL       time.Duration
	IrrigationTTL      time.Duration
	DeviceActivityTTL  time.Duration
	DeviceOfflineAfter time.Duration
}

type ServerConfig struct {
	Port            string
	Mode            string
	ShutdownTimeout time.Duration
}

type DatabaseConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	Migrate         bool
}

type AuthConfig struct {
	JWTSecret string
	Issuer    string
	TokenTTL  time.Duration
}

type InternalConfig struct {
	ServiceKey             string
	KnowledgeNotifyURL     string
	AgentAlertURL          string
	OutboxDispatchInterval time.Duration
	OutboxBatchSize        int
	AgentHTTPTimeout       time.Duration
}

type ObjectStorageConfig struct {
	Enabled          bool
	Endpoint         string
	AccessKey        string
	SecretKey        string
	Bucket           string
	Region           string
	Secure           bool
	MaxUploadBytes   int64
	SignedURLTimeout time.Duration
}

func Load() (Config, error) {
	maxOpen, err := intValue("DB_POOL_MAX_SIZE", 10)
	if err != nil {
		return Config{}, err
	}
	maxIdle, err := intValue("DB_POOL_MIN_IDLE", 2)
	if err != nil {
		return Config{}, err
	}
	migrate, err := boolValue("DB_MIGRATE", true)
	if err != nil {
		return Config{}, err
	}
	redisEnabled, err := boolValue("REDIS_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	redisPoolSize, err := intValue("REDIS_POOL_SIZE", 10)
	if err != nil || redisPoolSize < 1 {
		return Config{}, fmt.Errorf("REDIS_POOL_SIZE must be a positive integer")
	}
	redisDialTimeout, err := durationValue("REDIS_DIAL_TIMEOUT", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	redisReadTimeout, err := durationValue("REDIS_READ_TIMEOUT", time.Second)
	if err != nil {
		return Config{}, err
	}
	redisWriteTimeout, err := durationValue("REDIS_WRITE_TIMEOUT", time.Second)
	if err != nil {
		return Config{}, err
	}
	queryCacheTTL, err := durationValue("REDIS_QUERY_CACHE_TTL", time.Minute)
	if err != nil {
		return Config{}, err
	}
	alertCacheTTL, err := durationValue("REDIS_ALERT_CACHE_TTL", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	telemetryTTL, err := durationValue("REDIS_TELEMETRY_TTL", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	irrigationTTL, err := durationValue("REDIS_IRRIGATION_TTL", 35*time.Minute)
	if err != nil {
		return Config{}, err
	}
	deviceActivityTTL, err := durationValue("REDIS_DEVICE_ACTIVITY_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	deviceOfflineAfter, err := durationValue("DEVICE_OFFLINE_AFTER", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	if deviceActivityTTL <= deviceOfflineAfter {
		return Config{}, fmt.Errorf("REDIS_DEVICE_ACTIVITY_TTL must be greater than DEVICE_OFFLINE_AFTER")
	}
	mqttEnabled, err := boolValue("MQTT_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	mqttBrokerURL := strings.TrimSpace(value("MQTT_BROKER_URL", "tcp://localhost:1883"))
	if err := validateMQTTBrokerURL(mqttBrokerURL); err != nil {
		return Config{}, err
	}
	mqttTopicPrefix := strings.Trim(strings.TrimSpace(value("MQTT_TOPIC_PREFIX", "agri")), "/")
	if mqttTopicPrefix == "" || strings.ContainsAny(mqttTopicPrefix, "+#") {
		return Config{}, fmt.Errorf("MQTT_TOPIC_PREFIX must not be empty or contain MQTT wildcards")
	}
	mqttConnectTimeout, err := durationValue("MQTT_CONNECT_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	mqttReconnectBackoff, err := durationValue("MQTT_RECONNECT_BACKOFF", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	mqttMessageTimeout, err := durationValue("MQTT_MESSAGE_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	tdengineEnabled, err := boolValue("TDENGINE_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	tdengineBatchSize, err := intValue("TDENGINE_BATCH_SIZE", 100)
	if err != nil || tdengineBatchSize < 1 {
		return Config{}, fmt.Errorf("TDENGINE_BATCH_SIZE must be a positive integer")
	}
	tdengineFlushPeriod, err := durationValue("TDENGINE_FLUSH_PERIOD", 2*time.Second)
	if err != nil {
		return Config{}, err
	}

	dsn, err := databaseDSN()
	if err != nil {
		return Config{}, err
	}
	tokenTTL, err := durationValue("JWT_TTL", 2*time.Hour)
	if err != nil {
		return Config{}, err
	}
	jwtSecret := value("JWT_SECRET", "dev-only-jwt-secret-change-me-32-chars")
	if len(jwtSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must contain at least 32 characters")
	}
	internalServiceKey := value("INTERNAL_SERVICE_KEY", "dev-only-internal-service-key-change-me")
	if len(internalServiceKey) < 32 {
		return Config{}, fmt.Errorf("INTERNAL_SERVICE_KEY must contain at least 32 characters")
	}
	outboxDispatchInterval, err := durationValue("OUTBOX_DISPATCH_INTERVAL", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	outboxBatchSize, err := intValue("OUTBOX_BATCH_SIZE", 50)
	if err != nil || outboxBatchSize < 1 || outboxBatchSize > 500 {
		return Config{}, fmt.Errorf("OUTBOX_BATCH_SIZE must be between 1 and 500")
	}
	agentHTTPTimeout, err := durationValue("AGENT_HTTP_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	objectStorageEnabled, err := boolValue("OBJECT_STORAGE_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	objectStorageSecure, err := boolValue("MINIO_SECURE", false)
	if err != nil {
		return Config{}, err
	}
	maxUploadBytes, err := int64Value("KNOWLEDGE_MAX_UPLOAD_BYTES", 20*1024*1024)
	if err != nil || maxUploadBytes < 1 {
		return Config{}, fmt.Errorf("KNOWLEDGE_MAX_UPLOAD_BYTES must be a positive integer")
	}
	signedURLTimeout, err := durationValue("MINIO_SIGNED_URL_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Server: ServerConfig{
			Port:            value("SERVER_PORT", "8080"),
			Mode:            value("GIN_MODE", "debug"),
			ShutdownTimeout: 10 * time.Second,
		},
		Database: DatabaseConfig{
			DSN:             dsn,
			MaxOpenConns:    maxOpen,
			MaxIdleConns:    maxIdle,
			ConnMaxLifetime: 30 * time.Minute,
			Migrate:         migrate,
		},
		Redis: RedisConfig{
			Enabled: redisEnabled, URL: value("REDIS_URL", "redis://localhost:6379/0"), PoolSize: redisPoolSize,
			DialTimeout: redisDialTimeout, ReadTimeout: redisReadTimeout, WriteTimeout: redisWriteTimeout,
			QueryCacheTTL: queryCacheTTL, AlertCacheTTL: alertCacheTTL, TelemetryTTL: telemetryTTL,
			IrrigationTTL: irrigationTTL, DeviceActivityTTL: deviceActivityTTL, DeviceOfflineAfter: deviceOfflineAfter,
		},
		MQTT: MQTTConfig{
			Enabled: mqttEnabled, BrokerURL: mqttBrokerURL,
			ClientID: strings.TrimSpace(value("MQTT_CLIENT_ID", "smart-agriculture-api")),
			Username: strings.TrimSpace(os.Getenv("MQTT_USERNAME")), Password: os.Getenv("MQTT_PASSWORD"),
			TopicPrefix: mqttTopicPrefix, ConnectTimeout: mqttConnectTimeout,
			ReconnectBackoff: mqttReconnectBackoff, MessageTimeout: mqttMessageTimeout,
		},
		TDengine: TDengineConfig{
			Enabled: tdengineEnabled, RESTURL: strings.TrimRight(value("TDENGINE_REST_URL", "http://localhost:6041"), "/"),
			Username: value("TDENGINE_USERNAME", "root"), Password: value("TDENGINE_PASSWORD", "taosdata"),
			Database: value("TDENGINE_DATABASE", "agri_telemetry"),
			BatchSize: tdengineBatchSize, FlushPeriod: tdengineFlushPeriod,
		},
		Auth: AuthConfig{
			JWTSecret: jwtSecret,
			Issuer:    value("JWT_ISSUER", "smart-agriculture-api"),
			TokenTTL:  tokenTTL,
		},
		Internal: InternalConfig{
			ServiceKey: internalServiceKey, KnowledgeNotifyURL: strings.TrimSpace(os.Getenv("KNOWLEDGE_NOTIFY_URL")),
			AgentAlertURL:          strings.TrimSpace(os.Getenv("AGENT_ALERT_URL")),
			OutboxDispatchInterval: outboxDispatchInterval, OutboxBatchSize: outboxBatchSize, AgentHTTPTimeout: agentHTTPTimeout,
		},
		ObjectStorage: ObjectStorageConfig{
			Enabled: objectStorageEnabled, Endpoint: value("MINIO_ENDPOINT", "localhost:9000"),
			AccessKey: value("MINIO_ACCESS_KEY", "minioadmin"), SecretKey: value("MINIO_SECRET_KEY", "minioadmin"),
			Bucket: value("MINIO_BUCKET", "knowledge"), Region: value("MINIO_REGION", "us-east-1"),
			Secure: objectStorageSecure, MaxUploadBytes: maxUploadBytes, SignedURLTimeout: signedURLTimeout,
		},
	}, nil
}

func validateMQTTBrokerURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("MQTT_BROKER_URL must be a valid MQTT URL: %q", raw)
	}
	switch parsed.Scheme {
	case "tcp", "ssl", "tls", "ws", "wss":
		return nil
	default:
		return fmt.Errorf("MQTT_BROKER_URL uses unsupported scheme %q", parsed.Scheme)
	}
}

func databaseDSN() (string, error) {
	if dsn := os.Getenv("DB_DSN"); dsn != "" {
		return normalizeDSN(dsn)
	}

	username := value("DB_USERNAME", "smart_agriculture")
	password := value("DB_PASSWORD", "smart_agriculture")
	databaseURL := value("DB_URL", "jdbc:mysql://localhost:3307/smart_agriculture?useUnicode=true&characterEncoding=utf8&serverTimezone=UTC&allowPublicKeyRetrieval=true")
	databaseURL = strings.TrimPrefix(databaseURL, "jdbc:")
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme != "mysql" || parsed.Host == "" || strings.TrimPrefix(parsed.Path, "/") == "" {
		return "", fmt.Errorf("DB_URL must be a MySQL URL: %q", databaseURL)
	}

	params := parsed.Query()
	params.Del("useUnicode")
	params.Del("characterEncoding")
	params.Del("serverTimezone")
	params.Del("allowPublicKeyRetrieval")
	params.Set("charset", "utf8mb4")
	params.Set("parseTime", "true")
	params.Set("loc", "UTC")
	params.Set("multiStatements", "true")
	return normalizeDSN(fmt.Sprintf("%s:%s@tcp(%s)/%s?%s", username, password, parsed.Host, strings.TrimPrefix(parsed.Path, "/"), params.Encode()))
}

func normalizeDSN(dsn string) (string, error) {
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("invalid DB_DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.MultiStatements = true
	if cfg.Loc == nil {
		cfg.Loc = time.UTC
	}
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	if _, ok := cfg.Params["charset"]; !ok {
		cfg.Params["charset"] = "utf8mb4"
	}
	return cfg.FormatDSN(), nil
}

func value(key, fallback string) string {
	if result := os.Getenv(key); result != "" {
		return result
	}
	return fallback
}

func intValue(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := strconv.Atoi(raw)
	if err != nil || result < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return result, nil
}

func int64Value(key string, fallback int64) (int64, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return result, nil
}

func boolValue(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return result, nil
}

func durationValue(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	result, err := time.ParseDuration(raw)
	if err != nil || result <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return result, nil
}
