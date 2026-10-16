// 本文件验证 BOSS 附件基础识别复用系统提示词管理、旧配置默认值、超管权限及付费任务恢复，不调用真实 AI。
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestBossAttachmentPromptDefaults 验证旧 JSON、空配置和缺失存储都能读取安全默认值，同时保留已有岗位提示词。
func TestBossAttachmentPromptDefaults(t *testing.T) {
	if loadDefaultPrompts(nil).BossAttachmentBasicPrompt != builtinBossAttachmentBasicPrompt {
		t.Fatal("缺失存储未使用安全默认值")
	}
	for _, rule := range []string{"联系电话", "Tel", "Mobile", "国家码", "分段空白", "只标为微信", "不能填入phone", "掩码手机号不得补全"} {
		if !strings.Contains(builtinBossAttachmentBasicPrompt, rule) {
			t.Fatalf("默认识别缺少明确手机号边界: %s", rule)
		}
	}
	for _, test := range []struct{ raw, want string }{
		{`{"filter_prompt":"旧筛选","open_detail_prompt":"旧详情","review_prompt":"旧复核"}`, builtinBossAttachmentBasicPrompt},
		{`{"boss_attachment_basic_prompt":"  "}`, builtinBossAttachmentBasicPrompt},
		{`{"boss_attachment_basic_prompt":"  配置的基础识别  "}`, "配置的基础识别"},
	} {
		store := NewMemorySystemConfigStore()
		if err := store.Save(SystemConfig{ConfigKey: defaultPromptsConfigKey, ConfigValue: test.raw, Enabled: true}); err != nil {
			t.Fatal(err)
		}
		got := loadDefaultPrompts(store)
		if got.BossAttachmentBasicPrompt != test.want {
			t.Fatalf("基础提示词=%q", got.BossAttachmentBasicPrompt)
		}
		if strings.Contains(test.raw, "旧筛选") && (got.FilterPrompt != "旧筛选" || got.OpenDetailPrompt != "旧详情" || got.ReviewPrompt != "旧复核") {
			t.Fatal("新增附件提示词改变了原岗位提示词")
		}
	}
}

// TestBossAttachmentPromptAdminAndRuntime 验证既有管理接口保存后新任务立即采用系统提示词，同键恢复不因配置变化再次付费。
func TestBossAttachmentPromptAdminAndRuntime(t *testing.T) {
	t.Setenv("GOODHR_SUPER_ADMINS", "prompt-admin@example.com")
	server := mustNewServer(t)
	admin := wechatTestSession(t, server, "prompt-admin@example.com")
	user := wechatTestSession(t, server, "prompt-user@example.com")
	if err := server.auth.store.SaveSession("prompt-plugin", Session{Email: "owner@example.com", Kind: extensionSessionKind}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := server.ai.store.SaveUserConfig("owner@example.com", AIConfig{BaseURL: "https://ai.example.com/v1", Model: "test", APIKey: "test-only-prompt-key"}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var expected atomic.Value
	server.ai.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		var body struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[0].Content != expected.Load().(string) || strings.Contains(body.Messages[0].Content, "测试候选人") || body.Messages[1].Role != "user" || !strings.Contains(body.Messages[1].Content, "测试候选人") {
			t.Error("系统配置未生效或动态简历原文进入固定提示词")
		}
		return attachmentAIResponseForTest(`{"is_resume":true,"candidate_name":"测试候选人"}`), nil
	})}
	input := attachmentInputForTest("basic")
	for index, prompt := range []string{"管理员配置的基础识别一", "管理员配置的基础识别二"} {
		expected.Store(prompt)
		config := map[string]any{"filter_prompt": "保留筛选", "open_detail_prompt": "保留详情", "review_prompt": "保留复核", "boss_attachment_basic_prompt": prompt, "future_setting": map[string]bool{"keep": true}}
		value, _ := json.Marshal(config)
		body, _ := json.Marshal(map[string]string{"config_value": string(value)})
		for _, token := range []string{user, "prompt-plugin"} {
			response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/"+defaultPromptsConfigKey, token, string(body))
			if response.Code != http.StatusForbidden {
				t.Fatalf("非超管修改提示词: status=%d", response.Code)
			}
		}
		response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/"+defaultPromptsConfigKey, admin, string(body))
		if response.Code != http.StatusOK {
			t.Fatalf("超管保存失败: %d %s", response.Code, response.Body.String())
		}
		saved, err := server.systemConfigs.Get(defaultPromptsConfigKey)
		if err != nil || saved.ConfigValue != string(value) {
			t.Fatal("保存丢失原岗位提示词或未知字段")
		}
		response = wechatTestRequest(server.Routes(), http.MethodGet, "/api/system/default-prompts", user, "")
		var result struct{ Prompts DefaultPrompts }
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Prompts.BossAttachmentBasicPrompt != prompt {
			t.Fatal("既有默认提示词接口未展示当前配置")
		}
		if index == 1 {
			if code, _, _ := attachmentRequestForTest(t, server.attachmentParses, "POST", attachmentParsePath, "prompt-plugin", input); code != 200 || calls.Load() != 1 {
				t.Fatal("提示词变化造成同键任务再次付费")
			}
			input.RequestKey = "request-prompt-second"
		}
		_, task, _ := attachmentRequestForTest(t, server.attachmentParses, "POST", attachmentParsePath, "prompt-plugin", input)
		if got := waitAttachmentForTest(t, server.attachmentParses, task.ID); got.Status != "completed" {
			t.Fatalf("新识别未完成: %+v", got)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("新任务调用次数=%d", calls.Load())
	}
}

// TestBossAttachmentPromptAdminValidation 验证专用字段类型和长度，拒绝的配置不会覆盖已保存内容。
func TestBossAttachmentPromptAdminValidation(t *testing.T) {
	t.Setenv("GOODHR_SUPER_ADMINS", "prompt-admin@example.com")
	server := mustNewServer(t)
	admin := wechatTestSession(t, server, "prompt-admin@example.com")
	before, err := server.systemConfigs.Get(defaultPromptsConfigKey)
	if err != nil {
		t.Fatal(err)
	}
	tooLong, _ := json.Marshal(map[string]string{"boss_attachment_basic_prompt": strings.Repeat("字", 20001)})
	for _, raw := range []string{`[]`, `null`, `{"boss_attachment_basic_prompt":7}`, `{"boss_attachment_basic_prompt":null}`, string(tooLong)} {
		body, _ := json.Marshal(map[string]string{"config_value": raw})
		response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/"+defaultPromptsConfigKey, admin, string(body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("接受无效提示词: status=%d", response.Code)
		}
		after, _ := server.systemConfigs.Get(defaultPromptsConfigKey)
		if after.ConfigValue != before.ConfigValue {
			t.Fatal("校验失败仍覆盖原配置")
		}
	}
}
