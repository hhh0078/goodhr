// Package model 文件作用：验证平台 URL 的旧格式兼容和数组格式边界。
package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestConfigURLFormats 验证字符串、单对象和混合数组保留默认地址及其他平台字段。
func TestConfigURLFormats(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		first string
		urls  []string
	}{
		{name: "旧字符串", value: `"https://example.com/recommend"`, first: "https://example.com/recommend"},
		{name: "单对象", value: `{"url":"https://example.com/recommend"}`, first: "https://example.com/recommend"},
		{name: "字符串数组", value: `["https://example.com/recommend","https://example.com/search"]`, first: "https://example.com/recommend", urls: []string{"https://example.com/recommend", "https://example.com/search"}},
		{name: "对象数组", value: `[{"url":"https://example.com/recommend"},{"url":"https://example.com/search"}]`, first: "https://example.com/recommend", urls: []string{"https://example.com/recommend", "https://example.com/search"}},
		{name: "混合数组", value: `["https://example.com/recommend",{"url":" https://example.com/search "}]`, first: "https://example.com/recommend", urls: []string{"https://example.com/recommend", "https://example.com/search"}},
		{name: "单元素数组", value: `[{"url":"https://example.com/recommend"}]`, first: "https://example.com/recommend", urls: []string{"https://example.com/recommend"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cfg Config
			data := `{"id":"test","name":"测试平台","max_items":17,"behavior":{"supports_paging":true},"login_url":` + test.value + `,"entry_url":` + test.value + `,"messages_url":` + test.value + `,"selectors":{"candidate.item":{"target":{"selectors":[{"type":"css","value":".card"}]},"description":"候选人"}}}`
			if err := json.Unmarshal([]byte(data), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.LoginURL != test.first || cfg.EntryURL != test.first || cfg.MessagesURL != test.first {
				t.Fatalf("原单地址字段没有兼容：%+v", cfg)
			}
			if !reflect.DeepEqual(cfg.LoginURLs, test.urls) || !reflect.DeepEqual(cfg.EntryURLs, test.urls) || !reflect.DeepEqual(cfg.MessagesURLs, test.urls) {
				t.Fatalf("只有数组应启用多页面复用：login=%v entry=%v messages=%v", cfg.LoginURLs, cfg.EntryURLs, cfg.MessagesURLs)
			}
			if cfg.ID != "test" || cfg.Name != "测试平台" || cfg.MaxItems != 17 || !cfg.Behavior.SupportsPaging || cfg.Selectors["candidate.item"].Target.Selectors[0].Value != ".card" {
				t.Fatalf("URL 解析影响了原平台配置：%+v", cfg)
			}
		})
	}
}

// TestConfigURLRejectsInvalidValues 验证不正确的新格式不会悄悄忽略地址或改写已有配置。
func TestConfigURLRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{`42`, `true`, `{}`, `{"url":null}`, `{"url":42}`, `[]`, `[""]`, `[null]`, `[["https://example.com"]]`, `["https://example.com",{}]`} {
		t.Run(raw, func(t *testing.T) {
			cfg := Config{ID: "original", EntryURL: "https://example.com/original"}
			if err := json.Unmarshal([]byte(`{"id":"changed","entry_url":`+raw+`}`), &cfg); err == nil || !strings.Contains(err.Error(), "entry_url") {
				t.Fatalf("应明确拒绝错误地址，得到 %v", err)
			}
			if cfg.ID != "original" || cfg.EntryURL != "https://example.com/original" {
				t.Fatalf("错误配置不应部分覆盖已有值：%+v", cfg)
			}
		})
	}
}

// TestConfigURLPreservesLegacyNullAndMissing 验证缺失字段、null 和显式空字符串沿用旧 JSON 行为。
func TestConfigURLPreservesLegacyNullAndMissing(t *testing.T) {
	cfg := Config{ID: "original", EntryURL: "https://example.com/original"}
	if err := json.Unmarshal([]byte(`{"login_url":null,"entry_url":null}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ID != "original" || cfg.EntryURL != "https://example.com/original" || cfg.LoginURL != "" {
		t.Fatalf("缺失值或 null 改变了旧字段：%+v", cfg)
	}
	if err := json.Unmarshal([]byte(`{"entry_url":["https://example.com/recommend","https://example.com/search"]}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"entry_url":""}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.EntryURL != "" || cfg.EntryURLs != nil {
		t.Fatalf("切回旧字符串时应清除数组复用：%+v", cfg)
	}
}
