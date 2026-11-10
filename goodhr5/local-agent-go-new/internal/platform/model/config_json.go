// Package model 文件作用：兼容平台 URL 的旧字符串、单对象和多页面数组配置。
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// UnmarshalJSON 读取不同 URL 格式并保留原单地址字段，只有数组格式启用多页面复用。
func (c *Config) UnmarshalJSON(data []byte) error {
	type plainConfig Config
	next := *c
	decoded := struct {
		*plainConfig
		LoginURL    json.RawMessage `json:"login_url"`
		EntryURL    json.RawMessage `json:"entry_url"`
		MessagesURL json.RawMessage `json:"messages_url"`
	}{plainConfig: (*plainConfig)(&next)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	for _, field := range []struct {
		name     string
		raw      json.RawMessage
		fallback *string
		urls     *[]string
	}{
		{name: "login_url", raw: decoded.LoginURL, fallback: &next.LoginURL, urls: &next.LoginURLs},
		{name: "entry_url", raw: decoded.EntryURL, fallback: &next.EntryURL, urls: &next.EntryURLs},
		{name: "messages_url", raw: decoded.MessagesURL, fallback: &next.MessagesURL, urls: &next.MessagesURLs},
	} {
		raw := bytes.TrimSpace(field.raw)
		// 原字符串字段遇到缺失值或 null 时保留原值，继续沿用标准 JSON 解码行为。
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		fallback, urls, err := decodePageURLs(raw)
		if err != nil {
			return fmt.Errorf("%s 配置不正确：%w", field.name, err)
		}
		*field.fallback = fallback
		*field.urls = urls
	}
	*c = next
	return nil
}

// decodePageURLs 把单页面配置或数组转换为默认地址和可选的多页面列表。
func decodePageURLs(raw json.RawMessage) (string, []string, error) {
	if raw[0] != '[' {
		value, err := decodePageURL(raw)
		return value, nil, err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return "", nil, err
	}
	if len(entries) == 0 {
		return "", nil, fmt.Errorf("页面地址数组不能为空")
	}
	urls := make([]string, 0, len(entries))
	for index, entry := range entries {
		value, err := decodePageURL(bytes.TrimSpace(entry))
		if err != nil {
			return "", nil, fmt.Errorf("第 %d 个页面地址不正确：%w", index+1, err)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return "", nil, fmt.Errorf("第 %d 个页面地址为空", index+1)
		}
		urls = append(urls, value)
	}
	return urls[0], urls, nil
}

// decodePageURL 读取旧字符串或含 url 字段的单个页面对象，不接受嵌套数组。
func decodePageURL(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("页面地址为空")
	}
	if raw[0] == '"' {
		var value string
		err := json.Unmarshal(raw, &value)
		return value, err
	}
	if raw[0] == '{' {
		var page struct {
			URL *string `json:"url"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return "", err
		}
		if page.URL == nil {
			return "", fmt.Errorf("页面对象缺少 url 字符串")
		}
		return *page.URL, nil
	}
	return "", fmt.Errorf("页面地址必须是字符串或含 url 的对象")
}
