// 本文件验证本地程序打开云端控制台时携带实际监听端口的 URL 处理规则。
package app

import (
	"net/url"
	"testing"
)

// TestWithLocalAgentPort 验证新增端口参数时会保留原有查询参数和锚点。
func TestWithLocalAgentPort(t *testing.T) {
	target := withLocalAgentPort("https://goodhr5.58it.cn/admin?next=%2Fadmin%2Fpositions#section", 55279)
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("解析控制台地址失败: %v", err)
	}
	if got := parsed.Query().Get("local_port"); got != "55279" {
		t.Fatalf("local_port = %q, want 55279", got)
	}
	if got := parsed.Query().Get("next"); got != "/admin/positions" {
		t.Fatalf("next = %q, want /admin/positions", got)
	}
	if parsed.Fragment != "section" {
		t.Fatalf("fragment = %q, want section", parsed.Fragment)
	}
}

// TestWithLocalAgentPortReplacesExistingValue 验证当前实际端口会覆盖地址中的旧端口。
func TestWithLocalAgentPortReplacesExistingValue(t *testing.T) {
	target := withLocalAgentPort("https://goodhr5.58it.cn/admin?local_port=55271", 55273)
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatalf("解析控制台地址失败: %v", err)
	}
	if got := parsed.Query().Get("local_port"); got != "55273" {
		t.Fatalf("local_port = %q, want 55273", got)
	}
}

// TestWithLocalAgentPortIgnoresInvalidInput 验证非法端口或非法地址不会破坏原始地址。
func TestWithLocalAgentPortIgnoresInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		port int
	}{
		{name: "zero port", raw: "https://goodhr5.58it.cn/admin", port: 0},
		{name: "too large port", raw: "https://goodhr5.58it.cn/admin", port: 65536},
		{name: "relative url", raw: "/admin", port: 55271},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := withLocalAgentPort(test.raw, test.port); got != test.raw {
				t.Fatalf("withLocalAgentPort() = %q, want %q", got, test.raw)
			}
		})
	}
}
