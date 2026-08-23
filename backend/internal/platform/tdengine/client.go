// Package tdengine 提供 TDengine REST SQL 客户端（taosAdapter）。
package tdengine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 是 TDengine REST 接口（/rest/sql）的轻量封装。
type Client struct {
	restURL  string
	username string
	password string
	db       string
	http     *http.Client
}

type restResponse struct {
	Code       int             `json:"code"`
	Desc       string          `json:"desc"`
	ColumnMeta [][]interface{} `json:"column_meta"`
	Data       [][]interface{} `json:"data"`
	Rows       int             `json:"rows"`
}

// NewClient 创建 TDengine REST 客户端。
func NewClient(restURL, username, password, db string) *Client {
	return &Client{
		restURL:  strings.TrimRight(restURL, "/"),
		username: username,
		password: password,
		db:       db,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Exec 执行 SQL 并返回行数据（不校验列结构）。
func (c *Client) Exec(ctx context.Context, sql string) ([][]interface{}, error) {
	return c.query(ctx, sql)
}

// Query 执行查询，返回行数据。
func (c *Client) Query(ctx context.Context, sql string) ([][]interface{}, error) {
	return c.query(ctx, sql)
}

func (c *Client) query(ctx context.Context, sql string) ([][]interface{}, error) {
	endpoint := c.restURL + "/rest/sql"
	if c.db != "" {
		endpoint = c.restURL + "/rest/sql/" + url.PathEscape(c.db)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(sql))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(
		[]byte(c.username+":"+c.password),
	))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tdengine request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tdengine read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tdengine http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed restResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("tdengine decode: %w", err)
	}
	if parsed.Code != 0 {
		return nil, fmt.Errorf("tdengine sql error %d: %s", parsed.Code, parsed.Desc)
	}
	return parsed.Data, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
