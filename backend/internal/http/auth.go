package http

import (
	"errors"
	"net/http"
	"strings"

	"github.com/chenluuo/smart-project/backend/internal/identity"
	"github.com/gin-gonic/gin"
)

const claimsContextKey = "auth.claims"

type registerRequest struct {
	Mobile   string `json:"mobile" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type errorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type successResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type authHandler struct {
	service authService
}

func registerAuthRoutes(router *gin.Engine, service authService) {
	handler := authHandler{service: service}
	auth := router.Group("/api/v1/auth")
	auth.POST("/register", handler.register)
	auth.POST("/login", handler.login)
	users := router.Group("/api/v1/users")
	users.GET("/me", jwtAuthentication(service), handler.me)
}

func (h authHandler) register(c *gin.Context) {
	var request registerRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：mobile、username 和 password 不能为空")
		return
	}
	user, err := h.service.Register(c.Request.Context(), identity.RegisterInput{
		Mobile: request.Mobile, AccountName: request.Username, Password: request.Password,
	})
	if err != nil {
		switch {
		case errors.Is(err, identity.ErrUserConflict):
			respondError(c, http.StatusConflict, 40901, err.Error())
		case errors.Is(err, identity.ErrInvalidAccountName), errors.Is(err, identity.ErrInvalidMobile), errors.Is(err, identity.ErrInvalidPassword):
			respondError(c, http.StatusBadRequest, 40001, err.Error())
		default:
			respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		}
		return
	}
	respondSuccess(c, http.StatusCreated, gin.H{"user": user})
}

func (h authHandler) login(c *gin.Context) {
	var request loginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondError(c, http.StatusBadRequest, 40001, "参数错误：username 和 password 不能为空")
		return
	}
	result, err := h.service.Login(c.Request.Context(), request.Username, request.Password)
	if err != nil {
		switch {
		case errors.Is(err, identity.ErrInvalidCredentials):
			respondError(c, http.StatusUnauthorized, 40101, err.Error())
		case errors.Is(err, identity.ErrUserDisabled):
			respondError(c, http.StatusForbidden, 40301, err.Error())
		default:
			respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		}
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{
		"accessToken": result.AccessToken,
		"expiresIn":   result.ExpiresIn,
		"user": gin.H{
			"id":   result.User.ID,
			"name": result.User.AccountName,
			"role": result.Role,
		},
	})
}

func (h authHandler) me(c *gin.Context) {
	value, exists := c.Get(claimsContextKey)
	claims, ok := value.(identity.Claims)
	if !exists || !ok {
		respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
		return
	}
	result, err := h.service.CurrentUser(c.Request.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			respondError(c, http.StatusNotFound, 40401, "用户不存在")
		} else {
			respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
		}
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{
		"id": result.User.ID, "name": result.User.AccountName, "role": result.Role,
		"interactionStyle": result.User.InteractionStyle, "knowledgeReliance": result.User.KnowledgeReliance,
	})
}

func jwtAuthentication(service authService) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
			c.Header("WWW-Authenticate", "Bearer")
			respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
			c.Abort()
			return
		}
		claims, err := service.Authenticate(c.Request.Context(), parts[1])
		if err != nil {
			if !errors.Is(err, identity.ErrInvalidToken) {
				respondError(c, http.StatusInternalServerError, 50000, "服务器内部错误")
				c.Abort()
				return
			}
			c.Header("WWW-Authenticate", "Bearer")
			respondError(c, http.StatusUnauthorized, 40101, "未登录或访问令牌无效")
			c.Abort()
			return
		}
		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

func respondSuccess(c *gin.Context, status int, data any) {
	c.JSON(status, successResponse{Code: 0, Message: "OK", Data: data})
}

func respondError(c *gin.Context, status, code int, message string) {
	c.JSON(status, errorResponse{Code: code, Message: message, Data: nil})
}
