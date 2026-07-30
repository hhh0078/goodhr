// Package platform 文件作用：验证四个平台本地 JSON 可以解析成统一强类型运行配置。
package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestPlatformConfigTemplates 验证四个平台模板的必需字段和选择器集合。
func TestPlatformConfigTemplates(t *testing.T) {
	for _, platformID := range []string{"boss", "zhaopin", "liepin", "hliepin"} {
		t.Run(platformID, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(platformID, "config.json"))
			if err != nil {
				t.Fatalf("读取平台配置失败：%v", err)
			}
			var cfg model.Config
			if err = json.Unmarshal(content, &cfg); err != nil {
				t.Fatalf("解析平台配置失败：%v", err)
			}
			if cfg.ID != platformID || cfg.EntryURL == "" {
				t.Fatalf("平台基础字段不完整：id=%s entry_url=%s", cfg.ID, cfg.EntryURL)
			}
			if len(cfg.Selectors) == 0 || len(cfg.CandidateFields) == 0 {
				t.Fatalf("平台选择器集合为空")
			}
			if platformID == "zhaopin" &&
				(!cfg.Behavior.DirectPositionSelection || !cfg.Behavior.SelectFirstPositionResult) {
				t.Fatal("智联必须保留每次直接选择第一条岗位结果的旧版规则")
			}
			assertRequiredSelectors(t, platformID, cfg)
			assertLegacyCandidateFields(t, platformID, cfg)
			assertConfigComments(t, content)
		})
	}
}

// TestLoadConfigUsesLocalZhaopinSelectors 验证运行时直接加载旧版已经验证过的智联本地选择器。
func TestLoadConfigUsesLocalZhaopinSelectors(t *testing.T) {
	cfg, err := LoadConfig("zhaopin")
	if err != nil {
		t.Fatalf("读取智联本地配置失败：%v", err)
	}
	expected := map[string]string{
		"position.open":      "a[zp-stat-id='talent_more_jobs']",
		"position.input":     ".job-side-selector__filter input",
		"position.item":      ".job-side-selector__item",
		"position.item_text": ".job-side-selector__title",
		"position.panel":     ".job-side-selector",
	}
	for key, value := range expected {
		selector, ok := cfg.Selectors[key]
		if !ok || len(selector.Target.Selectors) == 0 || selector.Target.Selectors[0].Value != value {
			t.Errorf("智联本地选择器不正确：key=%s selector=%+v", key, selector)
		}
	}
}

// TestLoadConfigUsesSafeBossDetailTarget 验证 Boss 只点击基础信息摘要打开详情，避免随机命中候选人头像。
func TestLoadConfigUsesSafeBossDetailTarget(t *testing.T) {
	cfg, err := LoadConfig("boss")
	if err != nil {
		t.Fatalf("读取 Boss 本地配置失败：%v", err)
	}
	selector := cfg.Selectors["candidate.open_target"]
	if len(selector.Target.Selectors) != 1 ||
		selector.Target.Selectors[0].Value != ".base-info.join-text-wrap" {
		t.Fatalf("Boss 详情入口必须保持为不包含头像的安全小区域：%+v", selector.Target.Selectors)
	}
}

// TestLoadConfigUsesUniqueHLiepinCandidateRows 验证猎聘猎头端直接定位候选人行，不再依赖页面里多个 tbody。
func TestLoadConfigUsesUniqueHLiepinCandidateRows(t *testing.T) {
	cfg, err := LoadConfig("hliepin")
	if err != nil {
		t.Fatalf("读取猎聘猎头端本地配置失败：%v", err)
	}
	selector := cfg.Selectors["candidate.item"]
	if len(selector.Parents) != 0 {
		t.Fatalf("猎聘候选人行不应该依赖不唯一的 tbody 父级：%+v", selector.Parents)
	}
	if len(selector.Target.Selectors) == 0 ||
		selector.Target.Selectors[0].Value != "tr[data-tlg-elem-id='h_pc_search_res_listcard']" {
		t.Fatalf("猎聘候选人行选择器不正确：%+v", selector.Target.Selectors)
	}
}

// TestValidateTaskConfig 验证自动回复缺少真实消息配置时会在启动前明确拦截。
func TestValidateTaskConfig(t *testing.T) {
	cfg, err := LoadConfig("zhaopin")
	if err != nil {
		t.Fatalf("读取智联本地配置失败：%v", err)
	}
	if err = ValidateTaskConfig(cfg, "greeting"); err != nil {
		t.Fatalf("打招呼任务不应要求消息页配置：%v", err)
	}
	if err = ValidateTaskConfig(cfg, "auto_reply"); err == nil {
		t.Fatal("智联自动回复缺少消息页时应返回错误")
	}
	cfg.MessagesURL = "https://example.com/messages"
	for _, key := range []string{"message.unread_item", "message.context", "message.input", "message.send"} {
		cfg.Selectors[key] = contract.SelectorSpec{}
	}
	if err = ValidateTaskConfig(cfg, "auto_reply"); err == nil {
		t.Fatal("自动回复选择器目标为空时应返回错误")
	}
}

// assertRequiredSelectors 验证旧版当前会执行的平台能力都有对应选择器模板。
func assertRequiredSelectors(t *testing.T, platformID string, cfg model.Config) {
	t.Helper()
	required := map[string][]string{
		"boss": {
			"candidate.item", "candidate.list", "candidate.open_target",
			"candidate.detail", "candidate.greet", "candidate.continue",
			"candidate.chat_modal", "candidate.chat_close", "position.open",
			"position.input", "position.item",
		},
		"zhaopin": {
			"candidate.item", "candidate.list", "candidate.open_target",
			"candidate.detail", "candidate.greet", "candidate.continue",
			"candidate.chat_modal", "candidate.chat_name", "candidate.chat_close",
			"candidate.followup_input", "candidate.contact_trigger",
			"candidate.contact_drawer", "candidate.contact_drawer_close",
			"candidate.contact_item", "position.open",
			"position.input", "position.item", "position.item_text", "position.panel",
		},
		"liepin": {
			"candidate.item", "candidate.list", "candidate.open_target",
			"candidate.detail", "candidate.greet", "candidate.continue",
			"candidate.chat_modal", "candidate.chat_name", "candidate.chat_close",
			"candidate.followup_input", "candidate.contact_trigger",
			"candidate.contact_drawer", "candidate.contact_drawer_close",
			"candidate.contact_item", "position.current",
			"position.open", "position.item", "position.item_text",
		},
		"hliepin": {
			"candidate.item", "candidate.list", "candidate.open_target",
			"candidate.detail", "candidate.greet", "candidate.greet_modal",
			"candidate.greet_job_open", "candidate.greet_job_item",
			"candidate.greet_without_job", "candidate.greet_submit",
			"candidate.continue", "candidate.chat_modal", "candidate.chat_name",
			"candidate.chat_close", "candidate.contact_trigger",
			"candidate.contact_drawer", "candidate.contact_drawer_close",
			"candidate.contact_item", "candidate.page_number",
		},
	}
	for _, key := range []string{
		"candidate.request_phone",
		"candidate.request_wechat",
		"candidate.request_resume",
	} {
		selector, ok := cfg.Selectors[key]
		if !ok || len(selector.Target.Selectors) == 0 {
			t.Errorf("平台索要候选人信息能力缺少选择器：%s", key)
		}
	}
	for _, key := range required[platformID] {
		selector, ok := cfg.Selectors[key]
		if !ok || len(selector.Target.Selectors) == 0 {
			t.Errorf("旧版已使用的能力缺少选择器模板：%s", key)
		}
	}
}

// assertLegacyCandidateFields 验证旧版已经读取的候选人字段都保留在本地配置中。
func assertLegacyCandidateFields(t *testing.T, platformID string, cfg model.Config) {
	t.Helper()
	required := map[string][]string{
		"boss":    {"name", "basic_info", "education", "university", "description"},
		"zhaopin": {"name", "basic_info", "education", "description"},
		"liepin":  {"name", "basic_info", "education", "university", "description"},
		"hliepin": {"platform_candidate_id", "name", "basic_info", "education", "university", "description"},
	}
	for _, key := range required[platformID] {
		selector, ok := cfg.CandidateFields[key]
		if !ok || len(selector.Target.Selectors) == 0 {
			t.Errorf("旧版已读取的候选人字段缺少本地选择器：%s", key)
		}
	}
}

// assertConfigComments 验证平台 JSON 顶层、行为、动作和选择器都有中文说明。
func assertConfigComments(t *testing.T, content []byte) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(content, &raw); err != nil {
		t.Fatalf("解析配置注释结构失败：%v", err)
	}
	for key, value := range raw {
		if strings.HasPrefix(key, "_") {
			continue
		}
		if _, ok := raw["_comment_"+key]; ok {
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) == nil && hasComment(object) {
			continue
		}
		t.Errorf("顶层属性 %s 缺少注释", key)
	}
	assertCommentedObject(t, raw["behavior"], "behavior")
	for _, key := range []string{"login_init_actions", "greeting_init_actions", "message_init_actions", "filter_actions"} {
		assertCommentedActions(t, raw[key], key)
	}
	for _, key := range []string{"selectors", "candidate_fields", "conversation_fields"} {
		assertCommentedSelectors(t, raw[key], key)
	}
}

// assertCommentedObject 验证普通 JSON 对象的每个业务属性都有同级注释。
func assertCommentedObject(t *testing.T, raw json.RawMessage, path string) {
	t.Helper()
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		t.Errorf("%s 不是对象", path)
		return
	}
	if !hasComment(object) {
		t.Errorf("%s 缺少对象作用说明", path)
	}
	for key := range object {
		if strings.HasPrefix(key, "_") {
			continue
		}
		if _, ok := object["_comment_"+key]; !ok {
			t.Errorf("%s.%s 缺少注释", path, key)
		}
	}
}

// assertCommentedActions 验证配置动作及其每个属性都有注释。
func assertCommentedActions(t *testing.T, raw json.RawMessage, path string) {
	t.Helper()
	var actions []map[string]json.RawMessage
	if json.Unmarshal(raw, &actions) != nil {
		t.Errorf("%s 不是动作数组", path)
		return
	}
	for index, action := range actions {
		actionPath := fmt.Sprintf("%s[%d]", path, index)
		if !hasComment(action) {
			t.Errorf("%s 缺少动作说明", actionPath)
		}
		for key := range action {
			if strings.HasPrefix(key, "_") {
				continue
			}
			if _, ok := action["_comment_"+key]; !ok {
				t.Errorf("%s.%s 缺少注释", actionPath, key)
			}
		}
	}
}

// assertCommentedSelectors 验证选择器集合中的对象、父级、目标和候选值都有说明。
func assertCommentedSelectors(t *testing.T, raw json.RawMessage, path string) {
	t.Helper()
	var selectors map[string]json.RawMessage
	if json.Unmarshal(raw, &selectors) != nil {
		t.Errorf("%s 不是选择器对象", path)
		return
	}
	for key, value := range selectors {
		var spec struct {
			Comment string                   `json:"_comment"`
			Frames  []commentedSelectorGroup `json:"frames"`
			Parents []commentedSelectorGroup `json:"parents"`
			Target  commentedSelectorGroup   `json:"target"`
		}
		if json.Unmarshal(value, &spec) != nil {
			t.Errorf("%s.%s 不是有效选择器", path, key)
			continue
		}
		if strings.TrimSpace(spec.Comment) == "" {
			t.Errorf("%s.%s 缺少选择器作用说明", path, key)
		}
		for index, group := range append(spec.Frames, spec.Parents...) {
			assertSelectorGroupComments(t, group, fmt.Sprintf("%s.%s.parents[%d]", path, key, index))
		}
		assertSelectorGroupComments(t, spec.Target, path+"."+key+".target")
	}
}

// commentedSelectorGroup 表示平台 JSON 中带说明的选择器组。
type commentedSelectorGroup struct {
	Comment   string                       `json:"_comment"`
	Selectors []commentedSelectorCandidate `json:"selectors"`
}

// commentedSelectorCandidate 表示平台 JSON 中带说明的候选选择器。
type commentedSelectorCandidate struct {
	Comment string `json:"_comment"`
}

// assertSelectorGroupComments 验证一层选择器组和全部候选值都有说明。
func assertSelectorGroupComments(t *testing.T, group commentedSelectorGroup, path string) {
	t.Helper()
	if strings.TrimSpace(group.Comment) == "" {
		t.Errorf("%s 缺少层级说明", path)
	}
	for index, candidate := range group.Selectors {
		if strings.TrimSpace(candidate.Comment) == "" {
			t.Errorf("%s.selectors[%d] 缺少候选选择器说明", path, index)
		}
	}
}

// hasComment 判断 JSON 对象是否包含非空 `_comment`。
func hasComment(object map[string]json.RawMessage) bool {
	var comment string
	return json.Unmarshal(object["_comment"], &comment) == nil && strings.TrimSpace(comment) != ""
}
