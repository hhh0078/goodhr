// 本文件验证 BOSS 附件基础识别复用系统提示词管理、旧配置默认值、超管权限及付费任务恢复，不调用真实 AI。
package httpapi

import (
	"context"
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
	for _, rule := range []string{"实习", "兼职", "受聘机构岗位", "不能仅凭机构名称", "纯志愿服务", "班委", "团委", "社团", "课程实践", "竞赛", "个人项目", "不得放入work_experiences", "开始时间最早", "work_experiences:[]", "完整阶段保留全部", "project_experiences", "实际任职或实习雇主", "发布平台、渠道、软件工具、服务客户", "不能仅据名称当作雇主", "company_name留空", "不编造或拿渠道凑数"} {
		if !strings.Contains(bossAttachmentWorkClassification, rule) || strings.Count(builtinBossAttachmentBasicPrompt, bossAttachmentWorkClassification) != 1 {
			t.Fatalf("默认识别缺少统一经历分类规则: %s", rule)
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

// TestBossAttachmentExperienceRulesBothPhases 验证默认与系统配置经过真实请求构造后，两阶段复用同一经历分类规则且完整 JSON 合同不变。
func TestBossAttachmentExperienceRulesBothPhases(t *testing.T) {
	for _, test := range []struct{ name, config, prefix string }{
		{"missing-config", "", builtinBossAttachmentBasicPrompt},
		{"legacy-config", `{"filter_prompt":"保留旧筛选"}`, builtinBossAttachmentBasicPrompt},
		{"explicit-config", "configured", "系统管理配置的基础识别\n" + bossAttachmentWorkClassification},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, phase := range []string{"basic", "full"} {
				t.Run(phase, func(t *testing.T) {
					var system string
					s := attachmentServiceForTest(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
						var body struct {
							Messages []struct{ Role, Content string }
						}
						if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
							t.Fatalf("unexpected message contract: %+v", body)
						}
						system = body.Messages[0].Content
						return attachmentAIResponseForTest(`{"is_resume":true,"candidate_name":"测试候选人","work_experiences":[{"company_name":"测试镇政府","position_name":"办公室文员","start_ym":"2025-07","end_ym":"2025-08"},{"company_name":"测试统计局","position_name":"文员","start_ym":"2026-07","end_ym":"2026-08"}]}`), nil
					}))
					if test.config != "" {
						store := NewMemorySystemConfigStore()
						raw := test.config
						if raw == "configured" {
							data, _ := json.Marshal(map[string]string{"boss_attachment_basic_prompt": test.prefix})
							raw = string(data)
						}
						if err := store.Save(SystemConfig{ConfigKey: defaultPromptsConfigKey, ConfigValue: raw, Enabled: true}); err != nil {
							t.Fatal(err)
						}
						s.ai.systemConfigs = store
					}
					input := attachmentInputForTest(phase)
					input.Text = "测试候选人\n实践经历\n2023-09至2024-09：班级团委。2025-01至2025-02：测试博物馆纯志愿者。2025-07至2025-08：测试镇政府办公室文员，负责归档和日常接待。2026-07至2026-08：测试统计局文员。"
					cfg, err := s.ai.store.UserConfig("owner@example.com")
					if err != nil {
						t.Fatal(err)
					}
					resume, code, message, _ := s.parseAI(context.Background(), cfg, input)
					if code != "" {
						t.Fatalf("request failed: %s %s", code, message)
					}
					if strings.Count(system, bossAttachmentWorkClassification) != 1 {
						t.Fatalf("classification rule missing or duplicated in %s: %s", phase, system)
					}
					if phase == "basic" {
						if system != test.prefix || len(attachmentRows(resume["work_experiences"])) != 1 {
							t.Fatal("basic config override or one-work contract changed")
						}
					} else if !strings.HasPrefix(system, attachmentFullSystem+"\n") || len(attachmentRows(resume["work_experiences"])) != 2 {
						t.Fatal("full schema or all-work contract changed")
					}
					if strings.Contains(system, "测试博物馆") {
						t.Fatal("candidate content entered fixed classification prompt")
					}
				})
			}
		})
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
