// Package ai 本文件用隔离HTTP替身验证在线图片真进入多模态请求、动态管理员提示词生效与明确字段证据，绝不调用付费服务。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// onlineVisionTestPNG 生成不含实际候选人信息的真实PNG，避免非图片测试掩盖传输错误。
func onlineVisionTestPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestOnlineProfileVisionUsesImageAndEditablePrompt 验证固定system后传动态图，并清除模型推断的年龄、学校和伪造来源编号。
func TestOnlineProfileVisionUsesImageAndEditablePrompt(t *testing.T) {
	var calls atomic.Int32
	var captured struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, chatEnvelopeForTest(`{"candidate_name":"测试甲","age":40,"education_level":"本科","earliest_company_name":"示例公司","earliest_company_year":2011,"earliest_school_name":"不存在学校","earliest_school_year":2007,"raw_text":"测试甲 本科\n工作经历\n示例公司 2011.07-2015.01","source_key":"forged"}`))
	}))
	defer server.Close()
	profile, err := New().ReadAutoReplyOnlineProfileVision(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "synthetic", Model: "vision"}, [][]byte{onlineVisionTestPNG(t)}, "管理员当前在线资料规则")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(captured.Messages) != 2 || captured.Messages[0].Role != "system" || !strings.Contains(string(captured.Messages[0].Content), "管理员当前在线资料规则") || !strings.Contains(string(captured.Messages[1].Content), "data:image/png;base64,") {
		t.Fatal("图片或动态提示词没有真正进入模型")
	}
	if profile.Age != nil || profile.EarliestSchoolName != "" || profile.EarliestSchoolYear != nil || profile.SourceKey != "" {
		t.Fatal("模型推断字段或伪造编号未清理")
	}
	if profile.EarliestCompanyName != "示例公司" || profile.EarliestCompanyYear == nil || *profile.EarliestCompanyYear != 2011 || profile.Mode != "ai" {
		t.Fatal("明确字段丢失")
	}
}

// TestOnlineProfileVisionDoesNotRetryUnknownCharges 验证未知AI结果不自动重复扣费或把空识别当成成功。
func TestOnlineProfileVisionDoesNotRetryUnknownCharges(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); http.Error(w, "unavailable", 503) }))
	defer server.Close()
	if _, err := New().ReadAutoReplyOnlineProfileVision(context.Background(), cloud.AIConfig{BaseURL: server.URL, APIKey: "synthetic", Model: "vision"}, [][]byte{onlineVisionTestPNG(t)}, ""); err == nil || calls.Load() != 1 {
		t.Fatal("识别失败自动重试或误报成功", calls.Load(), err)
	}
}
