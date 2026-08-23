package http

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenluuo/smart-project/backend/internal/agent"
	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/chenluuo/smart-project/backend/internal/knowledge"
)

type agentServiceStub struct{}

func (agentServiceStub) CreateSession(_ context.Context, userID uint64, plotID *uint64) (*agent.Session, error) {
	return &agent.Session{ID: "chat_1", UserID: userID, PlotID: plotID, Status: agent.SessionStatusActive}, nil
}

func (agentServiceStub) AppendMessage(_ context.Context, sessionID string, input agent.MessageInput) (*agent.Message, error) {
	return &agent.Message{ID: 9, SessionID: sessionID, Role: agent.MessageRole(input.Role), Content: input.Content, CreatedAt: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)}, nil
}

func (agentServiceStub) AppendMessageByOwner(_ context.Context, _ uint64, sessionID string, input agent.MessageInput) (*agent.Message, error) {
	return &agent.Message{ID: 10, SessionID: sessionID, Role: agent.MessageRole(input.Role), Content: input.Content, PlotID: input.PlotID, ModelVersion: stringPointer(input.ModelVersion), TraceID: stringPointer(input.TraceID), CreatedAt: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)}, nil
}

func stringPointer(value string) *string { return &value }

func (agentServiceStub) ListMessages(_ context.Context, _ uint64, _ string, page, pageSize int) (agent.MessageList, error) {
	return agent.MessageList{Items: []agent.Message{{ID: 9, Content: "answer"}}, Page: page, PageSize: pageSize, Total: 1}, nil
}

func (agentServiceStub) CloseSession(_ context.Context, userID uint64, sessionID string) (*agent.Session, error) {
	return &agent.Session{ID: sessionID, UserID: userID, Status: agent.SessionStatusClosed}, nil
}

type knowledgeServiceStub struct{}

func (knowledgeServiceStub) MaxUploadBytes() int64 { return 20 * 1024 * 1024 }

func (knowledgeServiceStub) ListActive(_ context.Context, _ string) ([]knowledge.DocumentView, error) {
	return []knowledge.DocumentView{{ID: 1, Title: "指南"}}, nil
}

func (knowledgeServiceStub) Upload(_ context.Context, actorID uint64, input knowledge.UploadInput) (*knowledge.Document, error) {
	return &knowledge.Document{ID: 1, Title: input.Title, UploadedBy: actorID, Status: knowledge.StatusDraft}, nil
}

func (knowledgeServiceStub) Approve(_ context.Context, actorID, documentID uint64, _ string) (*knowledge.Document, error) {
	return &knowledge.Document{ID: documentID, ApprovedBy: &actorID, Status: knowledge.StatusApproved}, nil
}

func (knowledgeServiceStub) Publish(_ context.Context, _, documentID uint64, _ string) (*knowledge.Document, error) {
	return &knowledge.Document{ID: documentID, Status: knowledge.StatusActive}, nil
}

func (knowledgeServiceStub) Archive(_ context.Context, _, documentID uint64, _ string) (*knowledge.Document, error) {
	return &knowledge.Document{ID: documentID, Status: knowledge.StatusArchived}, nil
}

type adminAuthServiceStub struct{ authServiceStub }

func (adminAuthServiceStub) Authenticate(ctx context.Context, token string) (identity.Claims, error) {
	claims, err := (authServiceStub{}).Authenticate(ctx, token)
	claims.Role = "SYSTEM_ADMIN"
	return claims, err
}

func TestAgentRoutesAndInternalAuthentication(t *testing.T) {
	const key = "test-internal-service-key-32-characters"
	router := NewRouterWithBackendServices("test", pingerStub{}, authServiceStub{}, nil, nil, nil, nil, agentServiceStub{}, nil, nil, key)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		token      string
		serviceKey string
		wantStatus int
		wantBody   string
	}{
		{name: "create session", method: http.MethodPost, path: "/api/v1/ai/sessions", body: `{}`, token: "signed-token", wantStatus: http.StatusCreated, wantBody: `"id":"chat_1"`},
		{name: "list messages", method: http.MethodGet, path: "/api/v1/ai/sessions/chat_1/messages", token: "signed-token", wantStatus: http.StatusOK, wantBody: `"total":1`},
		{name: "python appends", method: http.MethodPost, path: "/api/v1/agent/sessions/chat_1/messages", body: `{"role":"assistant","content":"answer","plot_id":"11","model_version":"model-v1"}`, token: "signed-token", wantStatus: http.StatusCreated, wantBody: `"messageId":10`},
		{name: "python append rejects missing JWT", method: http.MethodPost, path: "/api/v1/agent/sessions/chat_1/messages", body: `{"role":"assistant","content":"answer"}`, wantStatus: http.StatusUnauthorized, wantBody: `"code":40101`},
		{name: "python append rejects invalid plot ID", method: http.MethodPost, path: "/api/v1/agent/sessions/chat_1/messages", body: `{"role":"assistant","content":"answer","plot_id":"plot_a3"}`, token: "signed-token", wantStatus: http.StatusBadRequest, wantBody: `"code":40001`},
		{name: "internal rejects missing key", method: http.MethodPost, path: "/internal/agent/sessions/chat_1/messages", body: `{"role":"ASSISTANT","content":"answer"}`, wantStatus: http.StatusUnauthorized, wantBody: `"code":40102`},
		{name: "internal appends", method: http.MethodPost, path: "/internal/agent/sessions/chat_1/messages", body: `{"role":"ASSISTANT","content":"answer"}`, serviceKey: key, wantStatus: http.StatusCreated, wantBody: `"messageId":9`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.serviceKey != "" {
				request.Header.Set("X-Internal-Service-Key", tt.serviceKey)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantBody) {
				t.Fatalf("status=%d body=%s, want status=%d body containing %s", response.Code, response.Body.String(), tt.wantStatus, tt.wantBody)
			}
		})
	}
}

func TestKnowledgeRoutesRequireSystemAdmin(t *testing.T) {
	farmerRouter := NewRouterWithBackendServices("test", pingerStub{}, authServiceStub{}, nil, nil, nil, nil, nil, knowledgeServiceStub{}, nil, "unused-internal-service-key-32-chars")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/docs", strings.NewReader(`{"title":"指南","category":"general","objectKey":"knowledge/guide.pdf","fileHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	farmerRouter.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("farmer status=%d body=%s, want 403", response.Code, response.Body.String())
	}

	adminRouter := NewRouterWithBackendServices("test", pingerStub{}, adminAuthServiceStub{}, nil, nil, nil, nil, nil, knowledgeServiceStub{}, nil, "unused-internal-service-key-32-chars")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("title", "指南")
	_ = writer.WriteField("category", "general")
	part, err := writer.CreateFormFile("file", "guide.pdf")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	_, _ = part.Write([]byte("document"))
	_ = writer.Close()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/docs", body)
	request.Header.Set("Authorization", "Bearer signed-token")
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response = httptest.NewRecorder()
	adminRouter.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"status":"DRAFT"`) {
		t.Fatalf("admin status=%d body=%s, want 201 draft", response.Code, response.Body.String())
	}
}
