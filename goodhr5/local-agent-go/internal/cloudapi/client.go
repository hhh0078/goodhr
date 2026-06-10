// Package cloudapi 负责 Go 版本本地程序访问云端公开接口和会员接口。
package cloudapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"bytes"
	"log"
)

// Client 是云端接口客户端。
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// PlatformConfig 表示从云端读取到的平台配置。
type PlatformConfig map[string]any

// New 创建云端接口客户端。
// baseURL 为云端 HTTP API 基础地址。
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimSpace(baseURL),
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// FetchPlatformConfig 从云端公开接口读取指定平台配置。
// ctx 为请求上下文，platformID 为平台 ID。
func (c *Client) FetchPlatformConfig(ctx context.Context, platformID string) (PlatformConfig, error) {
	baseURL, err := c.safeBaseURL()
	if err != nil {
		return nil, err
	}
	safePlatform := strings.ToLower(strings.TrimSpace(platformID))
	if safePlatform == "" {
		return nil, fmt.Errorf("平台 ID 不能为空，无法读取平台配置")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/platforms/config/", nil)
	if err != nil {
		return nil, fmt.Errorf("创建平台配置请求失败：%w", err)
	}
	payload, status, err := c.doJSON(req)
	if err != nil {
		return nil, fmt.Errorf("读取云端平台配置失败：%w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("%s", cloudMessage(payload, "读取云端平台配置失败"))
	}
	configs, ok := configList(payload["configs"])
	if !ok {
		if data, ok := payload["data"].(map[string]any); ok {
			configs, ok = configList(data["configs"])
		}
	}
	if !ok {
		return nil, fmt.Errorf("云端平台配置返回格式不正确")
	}
	targetKey := "platform." + safePlatform
	for _, item := range configs {
		key := strings.ToLower(strings.TrimSpace(stringFromMap(item, "config_key")))
		if key != targetKey {
			continue
		}
		config, err := decodeConfigValue(item["config_value"])
		if err != nil {
			return nil, err
		}
		if _, ok := config["id"]; !ok {
			config["id"] = safePlatform
		}
		return config, nil
	}
	return nil, fmt.Errorf("云端没有找到平台配置：%s", safePlatform)
}

// FetchSubscription 读取云端会员状态。
// ctx 为请求上下文，token 为登录令牌。
func (c *Client) FetchSubscription(ctx context.Context, token string) (map[string]any, error) {
	baseURL, err := c.safeBaseURL()
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("请先登录后再校验会员")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/subscription/status", nil)
	if err != nil {
		return nil, fmt.Errorf("创建会员校验请求失败：%w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	payload, status, err := c.doJSON(req)
	if err != nil {
		return nil, fmt.Errorf("会员校验失败：%w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("%s", cloudMessage(payload, "会员校验失败"))
	}
	subscription, ok := payload["subscription"].(map[string]any)
	if !ok {
		if data, ok := payload["data"].(map[string]any); ok {
			subscription, ok = data["subscription"].(map[string]any)
		}
	}
	if !ok {
		return nil, fmt.Errorf("会员校验返回格式错误")
	}
	return subscription, nil
}

// safeBaseURL 校验并规范化云端接口地址。
// 返回值不包含末尾斜杠。
func (c *Client) safeBaseURL() (string, error) {
	raw := strings.TrimSpace(c.BaseURL)
	if raw == "" {
		return "", fmt.Errorf("云端接口地址不能为空")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("云端接口地址格式不正确")
	}
	return strings.TrimRight(raw, "/"), nil
}

// doJSON 执行请求并解析 JSON 响应。
// req 为 HTTP 请求，返回响应体和状态码。
func (c *Client) doJSON(req *http.Request) (map[string]any, int, error) {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(body) == 0 {
		return map[string]any{}, resp.StatusCode, nil
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("云端返回格式不是 JSON")
	}
	return payload, resp.StatusCode, nil
}

// configList 将原始值转换为平台配置列表。
// value 为响应里的 configs 字段。
func configList(value any) ([]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if config, ok := item.(map[string]any); ok {
			result = append(result, config)
		}
	}
	return result, true
}

// decodeConfigValue 解码 system_configs.config_value。
// value 可以是 JSON 字符串，也可以是对象。
func decodeConfigValue(value any) (PlatformConfig, error) {
	if config, ok := value.(map[string]any); ok {
		return config, nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("云端平台配置内容不是有效对象")
	}
	config := map[string]any{}
	if err := json.Unmarshal([]byte(text), &config); err != nil {
		return nil, fmt.Errorf("云端平台配置 JSON 格式不正确")
	}
	return config, nil
}

// cloudMessage 提取云端错误消息。
// payload 为云端返回体，fallback 为默认中文错误。
func cloudMessage(payload map[string]any, fallback string) string {
	for _, key := range []string{"msg", "message", "error"} {
		if text := stringFromMap(payload, key); text != "" {
			return translateKnownMessage(text)
		}
	}
	return fallback
}

// stringFromMap 从 map 中读取字符串字段。
// item 为原始字典，key 为字段名。
func stringFromMap(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	if value, ok := item[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// translateKnownMessage 把常见英文错误改成中文。
// text 为云端或底层返回的原始错误。
func translateKnownMessage(text string) string {
	switch strings.TrimSpace(text) {
	case "session is invalid or expired":
		return "登录已过期，请重新登录"
	case "subscription_expired":
		return "会员已过期，请先续费"
	case "failed to load subscription":
		return "读取会员状态失败"
	case "failed to load system configs":
		return "读取平台配置失败"
	default:
		return text
	}
}

// SendTaskFailNotice 通知云端发送任务失败邮件。
// ctx 为请求上下文，taskID 为本地任务 ID，email 为接收通知的邮箱，errorMsg 为失败原因。
func (c *Client) SendTaskFailNotice(ctx context.Context, taskID string, email string, errorMsg string) error {
	baseURL, err := c.safeBaseURL()
	if err != nil {
		log.Printf("[失败邮件] 获取云端地址失败：%v", err)
		return err
	}
	apiURL := strings.TrimSuffix(baseURL, "/") + "/api/fail-notice"
	body := map[string]any{
		"task_id":       taskID,
		"email":         email,
		"error_message": errorMsg,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		log.Printf("[失败邮件] JSON 序列化失败：%v", err)
		return err
	}
	log.Printf("[失败邮件] 请求地址：%s", apiURL)
	log.Printf("[失败邮件] 请求参数：%s", string(payload))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		log.Printf("[失败邮件] 创建请求失败：%v", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, code, err := c.doJSON(req)
	if err != nil {
		log.Printf("[失败邮件] 请求失败：%v", err)
		return err
	}
	log.Printf("[失败邮件] 响应状态码：%d", code)
	log.Printf("[失败邮件] 响应数据：%v", resp)
	if code != http.StatusOK && code != http.StatusAccepted {
		return fmt.Errorf("云端返回非预期状态码：%d", code)
	}
	return nil
}
