package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/agent"
	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/chenluuo/smart-project/backend/internal/config"
	"github.com/chenluuo/smart-project/backend/internal/control"
	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/events"
	httpserver "github.com/chenluuo/smart-project/backend/internal/http"
	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/chenluuo/smart-project/backend/internal/knowledge"
	"github.com/chenluuo/smart-project/backend/internal/mqttclient"
	"github.com/chenluuo/smart-project/backend/internal/outbox"
	"github.com/chenluuo/smart-project/backend/internal/platform/database"
	"github.com/chenluuo/smart-project/backend/internal/platform/objectstore"
	"github.com/chenluuo/smart-project/backend/internal/platform/redisstore"
	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/platform/tdengine"
	"github.com/chenluuo/smart-project/backend/internal/telemetry"
)

func main() {
	configureLogging()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}

	db, err := database.Open(cfg.Database)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	sqlDB, err := db.DB()
	if err != nil {
		slog.Error("get database connection", "error", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	var redisClient *redisstore.Client
	if cfg.Redis.Enabled {
		redisContext, redisCancel := context.WithTimeout(context.Background(), cfg.Redis.DialTimeout)
		redisClient, err = redisstore.Open(redisContext, cfg.Redis)
		redisCancel()
		if err != nil {
			slog.Error("connect to Redis", "error", err)
			os.Exit(1)
		}
		defer redisClient.Close()
	}

	if cfg.Database.Migrate {
		if err := database.Migrate(context.Background(), sqlDB); err != nil {
			slog.Error("run database migrations", "error", err)
			os.Exit(1)
		}
	}
	tokenManager, err := identity.NewTokenManager(cfg.Auth.JWTSecret, cfg.Auth.Issuer, cfg.Auth.TokenTTL)
	if err != nil {
		slog.Error("configure JWT", "error", err)
		os.Exit(1)
	}
	authService := identity.NewAuthService(identity.NewRepositories(db), tokenManager)
	var plotStore plot.Store = plot.NewRepositories(db)
	var deviceStore device.Store = device.NewRepositories(db)
	var alertStore alert.Store = alert.NewRepositories(db)
	if redisClient != nil {
		plotStore = plot.NewCachedStore(plotStore, redisClient, cfg.Redis.QueryCacheTTL)
		deviceStore = device.NewCachedStore(deviceStore, redisClient, cfg.Redis.QueryCacheTTL)
		alertStore = alert.NewCachedStore(alertStore, redisClient, cfg.Redis.AlertCacheTTL)
	}
	plotService := plot.NewService(plotStore)
	var activityStore device.ActivityStore
	if redisClient != nil {
		activityStore = device.NewRedisActivityStore(redisClient.Client, cfg.Redis.DeviceActivityTTL)
	}
	deviceService := device.NewService(deviceStore)
	if activityStore != nil {
		deviceService = device.NewService(deviceStore, activityStore)
		deviceService.ConfigureActivityTimeout(cfg.Redis.DeviceOfflineAfter)
	}
	eventBroker := events.NewBroker(512)
	controlService := control.NewService(control.NewRepository(db), eventBroker)
	alertService := alert.NewService(alertStore, eventBroker)
	var latestStore telemetry.LatestStore = telemetry.NullStore{}
	if redisClient != nil {
		latestStore = telemetry.NewRedisStore(redisClient.Client, cfg.Redis.TelemetryTTL)
		controlService.ConfigureSnapshotStore(control.NewRedisIrrigationStore(redisClient.Client, cfg.Redis.IrrigationTTL))
	}
	telemetryService := telemetry.NewService(latestStore, telemetry.NullStore{})
	telemetryIngestService := telemetry.NewIngestService(latestStore, activityStore, alertService, eventBroker)
	// TDengine：高频遥测历史（写入 + 聚合查询）
	var tdClient *tdengine.Client
	var tdWriter *telemetry.TDengineWriter
	if cfg.TDengine.Enabled {
		tdClient = tdengine.NewClient(cfg.TDengine.RESTURL, cfg.TDengine.Username, cfg.TDengine.Password, cfg.TDengine.Database)
		tdWriter = telemetry.NewTDengineWriter(tdClient, cfg.TDengine.Database, cfg.TDengine.BatchSize, cfg.TDengine.FlushPeriod)
		defer tdWriter.Close()
		telemetryService = telemetry.NewService(latestStore, telemetry.NewTDengineStore(tdClient, cfg.TDengine.Database))
		telemetryIngestService.ConfigureHistory(tdWriter)
	}
	var mqttClient *mqttclient.Client
	if cfg.MQTT.Enabled {
		mqttHandler := mqttclient.NewHandler(
			cfg.MQTT.TopicPrefix,
			mqttclient.NewGormSourceResolver(db),
			telemetryIngestService,
		)
		mqttClient = mqttclient.New(cfg.MQTT, mqttHandler)
		mqttClient.Start()
		defer mqttClient.Close()
	}
	agentService := agent.NewService(agent.NewRepository(db))
	var knowledgeObjectStore knowledge.ObjectStore
	if cfg.ObjectStorage.Enabled {
		minioStore, err := objectstore.NewMinIO(objectstore.Config{
			Endpoint: cfg.ObjectStorage.Endpoint, AccessKey: cfg.ObjectStorage.AccessKey,
			SecretKey: cfg.ObjectStorage.SecretKey, Bucket: cfg.ObjectStorage.Bucket,
			Region: cfg.ObjectStorage.Region, Secure: cfg.ObjectStorage.Secure,
		})
		if err != nil {
			slog.Error("configure object storage", "error", err)
			os.Exit(1)
		}
		bucketCtx, bucketCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := minioStore.EnsureBucket(bucketCtx); err != nil {
			bucketCancel()
			slog.Error("prepare object storage", "error", err)
			os.Exit(1)
		}
		bucketCancel()
		knowledgeObjectStore = minioStore
	}
	knowledgeService := knowledge.NewService(knowledge.NewRepository(db), knowledgeObjectStore)
	knowledgeService.ConfigureObjectAccess(cfg.ObjectStorage.MaxUploadBytes, cfg.ObjectStorage.SignedURLTimeout)
	workerContext, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	if cfg.Internal.KnowledgeNotifyURL != "" {
		dispatcher, err := knowledge.NewDispatcher(
			outbox.NewRepository(db),
			&http.Client{Timeout: cfg.Internal.AgentHTTPTimeout},
			cfg.Internal.KnowledgeNotifyURL,
			cfg.Internal.ServiceKey,
		)
		if err != nil {
			slog.Error("configure knowledge outbox dispatcher", "error", err)
			os.Exit(1)
		}
		go dispatcher.Run(workerContext, cfg.Internal.OutboxDispatchInterval, cfg.Internal.OutboxBatchSize)
	}
	if cfg.Internal.AgentAlertURL != "" {
		dispatcher, err := alert.NewDispatcher(
			outbox.NewRepository(db),
			&http.Client{Timeout: cfg.Internal.AgentHTTPTimeout},
			cfg.Internal.AgentAlertURL,
			cfg.Internal.ServiceKey,
		)
		if err != nil {
			slog.Error("configure alert outbox dispatcher", "error", err)
			os.Exit(1)
		}
		go dispatcher.Run(workerContext, cfg.Internal.OutboxDispatchInterval, cfg.Internal.OutboxBatchSize)
	}

	healthPinger := &combinedPinger{mysql: sqlDB, redis: redisClient}
	server := &http.Server{
		Addr: ":" + cfg.Server.Port,
		Handler: httpserver.NewRouterWithBackendServices(
			cfg.Server.Mode, healthPinger, authService, plotService, deviceService, controlService, alertService,
			agentService, knowledgeService, telemetryService, cfg.Internal.ServiceKey, eventBroker,
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// SSE responses are intentionally long-lived; handlers still bound their
		// own downstream calls and ReadHeaderTimeout protects request intake.
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("smart agriculture API started", "address", server.Addr)
		serverErr <- server.ListenAndServe()
	}()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-shutdownSignal.Done():
		slog.Info("shutting down API")
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve API", "error", err)
			os.Exit(1)
		}
	}
	stopWorkers()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown", "error", err)
		os.Exit(1)
	}
}

func configureLogging() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler.WithAttrs([]slog.Attr{
		slog.String("service", "smart-agriculture-api"),
	})))
}

type combinedPinger struct {
	mysql *sql.DB
	redis *redisstore.Client
}

func (p *combinedPinger) PingContext(ctx context.Context) error {
	if p.mysql == nil {
		return errors.New("MySQL is not configured")
	}
	if err := p.mysql.PingContext(ctx); err != nil {
		return err
	}
	if p.redis != nil {
		return p.redis.PingContext(ctx)
	}
	return nil
}
