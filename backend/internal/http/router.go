package http

import (
	"context"
	"net/http"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/agent"
	"github.com/chenluuo/smart-project/backend/internal/alert"
	"github.com/chenluuo/smart-project/backend/internal/control"
	"github.com/chenluuo/smart-project/backend/internal/device"
	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/chenluuo/smart-project/backend/internal/knowledge"
	"github.com/chenluuo/smart-project/backend/internal/plot"
	"github.com/chenluuo/smart-project/backend/internal/telemetry"
	"github.com/gin-gonic/gin"
)

type databasePinger interface {
	PingContext(context.Context) error
}

func NewRouter(mode string, db databasePinger, authServices ...authService) *gin.Engine {
	gin.SetMode(mode)
	router := gin.New()
	router.Use(observabilityMiddleware(), gin.Recovery())

	health := healthHandler{db: db}
	actuator := router.Group("/actuator/health")
	actuator.GET("", health.readiness)
	actuator.GET("/liveness", health.liveness)
	actuator.GET("/readiness", health.readiness)
	if len(authServices) > 0 && authServices[0] != nil {
		registerAuthRoutes(router, authServices[0])
	}

	return router
}

func NewRouterWithPlotService(mode string, db databasePinger, auth authService, plots plotService) *gin.Engine {
	router := NewRouter(mode, db, auth)
	registerPlotRoutes(router, auth, plots)
	return router
}

func NewRouterWithServices(mode string, db databasePinger, auth authService, plots plotService, devices deviceService, controls ...controlService) *gin.Engine {
	router := NewRouter(mode, db, auth)
	if plots != nil {
		registerPlotRoutes(router, auth, plots)
	}
	if devices != nil {
		registerDeviceRoutes(router, auth, devices)
	}
	if len(controls) > 0 && controls[0] != nil {
		registerControlRoutes(router, auth, controls[0])
	}
	return router
}

func NewRouterWithAllServices(mode string, db databasePinger, auth authService, plots plotService, devices deviceService, controls controlService, alerts alertService) *gin.Engine {
	router := NewRouterWithServices(mode, db, auth, plots, devices, controls)
	if alerts != nil {
		registerAlertRoutes(router, auth, alerts)
	}
	return router
}

func NewRouterWithBackendServices(
	mode string,
	db databasePinger,
	auth authService,
	plots plotService,
	devices deviceService,
	controls controlService,
	alerts alertService,
	agents agentService,
	knowledgeDocuments knowledgeService,
	telemetry telemetryService,
	internalServiceKey string,
	eventSubscribers ...eventSubscriber,
) *gin.Engine {
	router := NewRouterWithAllServices(mode, db, auth, plots, devices, controls, alerts)
	if alerts != nil {
		registerInternalAlertRoutes(router, alerts, internalServiceKey)
	}
	if agents != nil {
		registerAgentRoutes(router, auth, agents, internalServiceKey)
	}
	if knowledgeDocuments != nil {
		registerKnowledgeRoutes(router, auth, knowledgeDocuments)
	}
	if telemetry != nil {
		registerDashboardRoutes(router, auth, plots, devices, alerts, telemetry)
		registerTelemetryRoutes(router, auth, plots, devices, telemetry)
		registerTelemetryListRoutes(router, auth, plots, alerts, telemetry)
		registerTelemetryHistoryRoutes(router, auth, plots, telemetry)
	}
	if len(eventSubscribers) > 0 && eventSubscribers[0] != nil {
		registerEventRoutes(router, auth, eventSubscribers[0])
	}
	return router
}

type authService interface {
	Register(context.Context, identity.RegisterInput) (*identity.User, error)
	Login(context.Context, string, string) (*identity.LoginResult, error)
	CurrentUser(context.Context, uint64) (*identity.CurrentUserResult, error)
	Authenticate(context.Context, string) (identity.Claims, error)
}

type plotService interface {
	List(context.Context, uint64) ([]plot.Plot, error)
	Get(context.Context, uint64, uint64) (*plot.Plot, error)
}

type deviceService interface {
	List(context.Context, uint64, device.ListFilter) (device.ListResult, error)
	Bind(context.Context, uint64, device.BindInput) (*device.Device, error)
	Unbind(context.Context, uint64, uint64) error
	Status(context.Context, uint64, uint64) (*device.Device, error)
}

type controlService interface {
	Issue(context.Context, uint64, uint64, control.IssueInput) (*control.IssueResult, error)
	IrrigationStatus(context.Context, uint64, uint64) (*control.IrrigationStatus, error)
	Command(context.Context, uint64, string) (*control.CommandResult, error)
	List(context.Context, uint64, control.ListFilter) (control.ListResult, error)
}

type alertService interface {
	ListRules(context.Context, uint64, uint64) ([]alert.RuleView, error)
	UpsertRule(context.Context, uint64, uint64, uint64, alert.RuleInput) (*alert.RuleUpdateResult, error)
	List(context.Context, uint64, alert.ListFilter) (alert.ListResult, error)
	Confirm(context.Context, uint64, uint64, string) (*alert.ConfirmResult, error)
	Trigger(context.Context, alert.TriggerInput) (*alert.TriggerResult, error)
}

type agentService interface {
	CreateSession(context.Context, uint64, *uint64) (*agent.Session, error)
	AppendMessage(context.Context, string, agent.MessageInput) (*agent.Message, error)
	AppendMessageByOwner(context.Context, uint64, string, agent.MessageInput) (*agent.Message, error)
	ListMessages(context.Context, uint64, string, int, int) (agent.MessageList, error)
	CloseSession(context.Context, uint64, string) (*agent.Session, error)
}

type knowledgeService interface {
	ListActive(context.Context, string) ([]knowledge.DocumentView, error)
	MaxUploadBytes() int64
	Upload(context.Context, uint64, knowledge.UploadInput) (*knowledge.Document, error)
	Approve(context.Context, uint64, uint64, string) (*knowledge.Document, error)
	Publish(context.Context, uint64, uint64, string) (*knowledge.Document, error)
	Archive(context.Context, uint64, uint64, string) (*knowledge.Document, error)
}

type telemetryService interface {
	LatestByPlot(context.Context, uint64) (*telemetry.Latest, error)
	LatestByPlots(context.Context, []uint64) ([]telemetry.Latest, error)
	History(context.Context, telemetry.HistoryQuery) (*telemetry.History, error)
}

type healthHandler struct {
	db databasePinger
}

func (h healthHandler) liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "UP"})
}

func (h healthHandler) readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	if h.db == nil || h.db.PingContext(ctx) != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "DOWN"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "UP"})
}
