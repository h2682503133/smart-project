package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chenluuo/smart-project/backend/internal/identity"
)

type authServiceStub struct{}

func (authServiceStub) Register(_ context.Context, input identity.RegisterInput) (*identity.User, error) {
	if input.Mobile == "invalid" {
		return nil, identity.ErrInvalidMobile
	}
	return &identity.User{ID: 7, AccountName: input.AccountName, Mobile: input.Mobile, PasswordHash: "must-not-leak", Status: identity.UserStatusActive}, nil
}

func (authServiceStub) Login(_ context.Context, accountName, password string) (*identity.LoginResult, error) {
	if password != "strong-password" {
		return nil, identity.ErrInvalidCredentials
	}
	return &identity.LoginResult{
		AccessToken: "signed-token", ExpiresIn: 3600,
		User: &identity.User{ID: 7, AccountName: accountName, Mobile: "13812345678", PasswordHash: "must-not-leak", Status: identity.UserStatusActive}, Role: "FARMER",
	}, nil
}

func (authServiceStub) Authenticate(_ context.Context, token string) (identity.Claims, error) {
	if token != "signed-token" {
		return identity.Claims{}, identity.ErrInvalidToken
	}
	return identity.Claims{UserID: 7, AccountName: "grower", Role: "FARMER"}, nil
}

func (authServiceStub) CurrentUser(_ context.Context, userID uint64) (*identity.CurrentUserResult, error) {
	style := "plain"
	reliance := "data"
	return &identity.CurrentUserResult{User: &identity.User{
		ID: userID, AccountName: "grower", Status: identity.UserStatusActive,
		InteractionStyle: &style, KnowledgeReliance: &reliance,
	}, Role: "FARMER"}, nil
}

func TestAuthEndpoints(t *testing.T) {
	router := NewRouter("test", pingerStub{}, authServiceStub{})
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		token      string
		wantStatus int
		wantBody   string
	}{
		{name: "register", method: http.MethodPost, path: "/api/v1/auth/register", body: `{"mobile":"13812345678","username":"grower","password":"strong-password"}`, wantStatus: http.StatusCreated, wantBody: `"username":"grower"`},
		{name: "login", method: http.MethodPost, path: "/api/v1/auth/login", body: `{"username":"grower","password":"strong-password"}`, wantStatus: http.StatusOK, wantBody: `"accessToken":"signed-token"`},
		{name: "login rejects wrong password", method: http.MethodPost, path: "/api/v1/auth/login", body: `{"username":"grower","password":"wrong"}`, wantStatus: http.StatusUnauthorized, wantBody: `"code":40101`},
		{name: "me requires token", method: http.MethodGet, path: "/api/v1/users/me", wantStatus: http.StatusUnauthorized, wantBody: `"code":40101`},
		{name: "me returns stored profile", method: http.MethodGet, path: "/api/v1/users/me", token: "signed-token", wantStatus: http.StatusOK, wantBody: `"interactionStyle":"plain"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", response.Body.String(), tt.wantBody)
			}
			if strings.Contains(response.Body.String(), "must-not-leak") {
				t.Fatalf("response leaked password hash: %s", response.Body.String())
			}
		})
	}
}
