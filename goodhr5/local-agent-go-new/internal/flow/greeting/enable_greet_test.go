// Package greeting 文件作用：验证关闭发送开关后只读取并关闭详情，任何招呼、索要和消息动作都不执行。
package greeting

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// greetingSettingRuntime 记录详情和外发动作，错误用于验证只检查仍保留原失败路径。
type greetingSettingRuntime struct {
	imageDetailRuntime
	opens, conversations, requests, messages int
	readErr, closeErr                        error
}

// OpenCandidateDetail 记录正常详情打开。
func (r *greetingSettingRuntime) OpenCandidateDetail(context.Context, model.Browser, model.Config, model.Candidate) error {
	r.opens++
	return nil
}

// ExtractCandidateDetail 提供能够通过关键词过滤的详情，或指定的读取失败。
func (r *greetingSettingRuntime) ExtractCandidateDetail(context.Context, model.Browser, model.Config, model.Candidate) (model.CandidateDetail, error) {
	r.domReads++
	return model.CandidateDetail{Text: "符合测试岗位要求"}, r.readErr
}

// CloseCandidateDetail 记录详情清理，并按测试要求返回失败。
func (r *greetingSettingRuntime) CloseCandidateDetail(context.Context, model.Browser, model.Config, model.Candidate) error {
	r.closes++
	return r.closeErr
}

// GreetCandidate 记录招呼调用并验证发送前已经关闭详情。
func (r *greetingSettingRuntime) GreetCandidate(context.Context, model.Browser, model.Config, model.Candidate, model.GreetRequest) error {
	r.greetings++
	if r.closes != 1 {
		return errors.New("打招呼前必须已经关闭详情")
	}
	return nil
}

// EnsureCandidateConversation 记录聊天入口调用。
func (r *greetingSettingRuntime) EnsureCandidateConversation(context.Context, model.Browser, model.Config, model.Candidate) error {
	r.conversations++
	return nil
}

// RequestCandidateInfo 记录资料索要调用。
func (r *greetingSettingRuntime) RequestCandidateInfo(context.Context, model.Browser, model.Config, model.Candidate, model.CandidateInfoRequest) error {
	r.requests++
	return nil
}

// SendCandidateMessage 记录消息发送调用。
func (r *greetingSettingRuntime) SendCandidateMessage(context.Context, model.Browser, model.Config, model.Candidate, string) error {
	r.messages++
	return nil
}

// TestGreetingSettingControlsAllOutboundActions 验证默认和开启保留原发送流程，关闭时在发送等待前结束且详情仍正常关闭。
func TestGreetingSettingControlsAllOutboundActions(t *testing.T) {
	enabled, disabled := true, false
	for _, test := range []struct {
		name    string
		setting *bool
		send    int
	}{
		{name: "default", send: 1},
		{name: "enabled", setting: &enabled, send: 1},
		{name: "disabled", setting: &disabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"score\":99,\"reason\":\"符合测试要求\"}"}}]}`))
			}))
			defer server.Close()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			prepared := shared.PreparedTask{
				Request: shared.StartRequest{TaskID: "task-1", TaskType: "greeting", EnableGreet: test.setting},
				Position: cloud.PositionSnapshot{
					RequiresAI: true, Keywords: []string{"符合"}, GreetMessage: "测试消息",
					RequestPhone: true, RequestWechat: true, RequestResume: true,
					CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword", DetailMode: "dom"},
					AI:           cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture", ScoreThreshold: 1},
					AIOptions:    cloud.PositionAIOptions{RequestScoreThreshold: 1},
				},
			}
			if test.send == 0 {
				prepared.Preferences.GreetBeforeDelayMin = 1
				prepared.Preferences.GreetBeforeDelayMax = 1
			}
			flow := Flow{Browser: client.New(server.URL), AI: ai.New(), Store: store}
			runtime := &greetingSettingRuntime{}
			stats := shared.Stats{}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			if err := flow.processCandidate(ctx, prepared, runtime, model.Candidate{Fingerprint: "fixture"}, &stats); err != nil {
				t.Fatal(err)
			}
			if runtime.opens != 1 || runtime.domReads != 1 || runtime.closes != 1 {
				t.Fatalf("详情读取和关闭应照常执行：%+v", runtime)
			}
			if runtime.greetings != test.send || runtime.conversations != test.send || runtime.requests != test.send || runtime.messages != test.send {
				t.Fatalf("发送开关未覆盖全部外发入口：招呼=%d 聊天=%d 索要=%d 消息=%d", runtime.greetings, runtime.conversations, runtime.requests, runtime.messages)
			}
			if stats.Processed != 1 || stats.Succeeded != test.send || stats.Skipped != 1-test.send || stats.Failed != 0 {
				t.Fatalf("只检查不能计为招呼成功或失败：%+v", stats)
			}
		})
	}
}

// TestDisabledGreetingKeepsDetailAndAIFailures 验证禁止发送不会把详情读取、关闭或 AI 判断失败伪装成成功。
func TestDisabledGreetingKeepsDetailAndAIFailures(t *testing.T) {
	for _, stage := range []string{"read", "close", "ai"} {
		t.Run(stage, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(request.URL.Path, "/page/") {
					_, _ = w.Write([]byte(`{"ok":true,"data":{"pages":[]}}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"invalid-fixture"}}]}`))
			}))
			defer server.Close()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			disabled := false
			prepared := shared.PreparedTask{
				Request: shared.StartRequest{TaskID: "task-1", TaskType: "greeting", EnableGreet: &disabled},
				Position: cloud.PositionSnapshot{
					Keywords: []string{"符合"}, GreetMessage: "测试消息", RequiresAI: stage == "ai",
					CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword", DetailMode: "dom"},
					AI:           cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture"},
				},
			}
			runtime := &greetingSettingRuntime{}
			if stage == "read" {
				runtime.readErr = errors.New("fixture read failed")
			}
			if stage == "close" {
				runtime.closeErr = errors.New("fixture close failed")
			}
			flow := Flow{Browser: client.New(server.URL), AI: ai.New(), Store: store}
			stats := shared.Stats{}
			if err := flow.processCandidate(context.Background(), prepared, runtime, model.Candidate{Fingerprint: "fixture"}, &stats); err == nil {
				t.Fatal("只检查仍必须返回原阶段错误")
			}
			if runtime.closes == 0 || runtime.greetings+runtime.conversations+runtime.requests+runtime.messages != 0 || stats.Succeeded != 0 || stats.Failed != 1 {
				t.Fatalf("失败路径必须关闭详情且不外发：运行=%+v 统计=%+v", runtime, stats)
			}
		})
	}
}
