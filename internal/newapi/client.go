package newapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxPerCall = 100

var (
	ErrCompliance   = errors.New("newapi: payment compliance not enabled")
	ErrUnauthorized = errors.New("newapi: access token invalid or not admin")
	ErrUpstream     = errors.New("newapi: upstream error")
)

type Config struct {
	BaseURL     string
	AccessToken string // 系统访问令牌(非 sk- relay 令牌)
	AdminUserID string // 管理员数字 user id
}

// Issuer 是发码抽象,便于 service 层注入假实现做单测。
type Issuer interface {
	GenerateCodes(ctx context.Context, name string, quota int64, count int) ([]string, error)
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 120 * time.Second}}
}

type redemptionReq struct {
	Name        string `json:"name"`
	Quota       int64  `json:"quota"`
	Count       int    `json:"count"`
	ExpiredTime int64  `json:"expired_time"`
}

type apiResp struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    []string `json:"data"`
}

func trimName(s string) string {
	r := []rune(s)
	if len(r) > 20 {
		return string(r[:20])
	}
	return s
}

// GenerateCodes 批量发码;单次 ≤100 自动分批。返回已成功生成的码;
// 中途某批失败时返回已成功的码 + 非 nil 错误(partial)。
func (c *Client) GenerateCodes(ctx context.Context, name string, quota int64, count int) ([]string, error) {
	var all []string
	var firstErr error
	remaining := count
	for remaining > 0 {
		n := remaining
		if n > maxPerCall {
			n = maxPerCall
		}
		body, _ := json.Marshal(redemptionReq{Name: trimName(name), Quota: quota, Count: n, ExpiredTime: 0})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/redemption/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", c.cfg.AccessToken)
		req.Header.Set("New-Api-User", c.cfg.AdminUserID)

		resp, err := c.http.Do(req)
		if err != nil {
			if firstErr == nil {
				firstErr = ErrUpstream
			}
			break
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var ar apiResp
		_ = json.Unmarshal(raw, &ar)
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return all, ErrUnauthorized
		case strings.Contains(strings.ToLower(string(raw)), "compliance"):
			return all, ErrCompliance
		case resp.StatusCode >= 500:
			if firstErr == nil {
				firstErr = ErrUpstream
			}
			break
		case !ar.Success:
			if firstErr == nil {
				firstErr = fmt.Errorf("%w: %s", ErrUpstream, ar.Message)
			}
			break
		}
		all = append(all, ar.Data...)
		remaining -= n
	}
	return all, firstErr
}

// TestConnection 用 /api/user/self 验证 网址/令牌/用户ID 有效且为管理员。
func (c *Client) TestConnection(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+"/api/user/self", nil)
	req.Header.Set("Authorization", c.cfg.AccessToken)
	req.Header.Set("New-Api-User", c.cfg.AdminUserID)
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrUnauthorized
	}
	var self struct {
		Success bool `json:"success"`
		Data    struct {
			Role int `json:"role"`
		} `json:"data"`
	}
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &self)
	if !self.Success {
		return ErrUnauthorized
	}
	if self.Data.Role < 10 { // NewAPI: RoleAdminUser=10
		return errors.New("newapi: token owner is not an admin")
	}
	return nil
}
