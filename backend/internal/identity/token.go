package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid or expired token")

type Claims struct {
	UserID      uint64 `json:"uid"`
	AccountName string `json:"accountName"`
	Role        string `json:"role"`
	Issuer      string `json:"iss"`
	Subject     string `json:"sub"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

type TokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenManager(secret, issuer string, ttl time.Duration) (*TokenManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT secret must contain at least 32 characters")
	}
	if strings.TrimSpace(issuer) == "" {
		return nil, errors.New("JWT issuer is required")
	}
	if ttl < time.Second {
		return nil, errors.New("JWT TTL must be at least one second")
	}
	return &TokenManager{secret: []byte(secret), issuer: issuer, ttl: ttl, now: time.Now}, nil
}

func (m *TokenManager) Issue(userID uint64, accountName, role string) (string, int64, error) {
	if userID == 0 || accountName == "" || role == "" {
		return "", 0, errors.New("JWT subject is required")
	}
	now := m.now().UTC()
	claims := Claims{
		UserID:      userID,
		AccountName: accountName,
		Role:        role,
		Issuer:      m.issuer,
		Subject:     strconv.FormatUint(userID, 10),
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(m.ttl).Unix(),
	}
	header, err := encodeJSON(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", 0, err
	}
	payload, err := encodeJSON(claims)
	if err != nil {
		return "", 0, err
	}
	signed := header + "." + payload
	return signed + "." + m.signature(signed), int64(m.ttl / time.Second), nil
}

func (m *TokenManager) Parse(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Claims{}, ErrInvalidToken
	}
	signed := parts[0] + "." + parts[1]
	expected, err := base64.RawURLEncoding.Strict().DecodeString(m.signature(signed))
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	provided, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, provided) {
		return Claims{}, ErrInvalidToken
	}

	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if err := decodeJSON(parts[0], &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := decodeJSON(parts[1], &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	now := m.now().UTC().Unix()
	if claims.Issuer != m.issuer || claims.UserID == 0 || claims.Subject != strconv.FormatUint(claims.UserID, 10) || claims.AccountName == "" || claims.Role == "" || claims.IssuedAt > now+60 || claims.ExpiresAt <= now || claims.ExpiresAt <= claims.IssuedAt {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func (m *TokenManager) signature(value string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode JWT: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeJSON(value string, target any) error {
	data, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
