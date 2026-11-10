// Package httpapi 本文件验证空话术、显式覆盖及删除场景在管理员、岗位编辑和Agent快照之间保持静默语义。
package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// emptyPhraseLibrary 构造与真实场景相同的合成默认库，不读取用户或数据库配置。
func emptyPhraseLibrary() autoReplyLibrary {
	return autoReplyLibrary{Phrases: []ReplyPhrase{
		{ID: "request_resume", Category: "简历", Title: "索要简历", Scene: "需要附件时", Content: "原默认索要简历文字"},
		{ID: "receive_resume", Category: "简历", Title: "收到简历", Scene: "刚收到附件时", Content: "原默认收到简历文字"},
		{ID: "custom_notice", Category: "沟通", Title: "自定义提醒", Scene: "候选人询问进度时", Content: "原默认自定义提醒文字"},
	}, ConditionOptions: []string{}}
}

// TestReplyLibraryEmptyContentPreservesInheritance 验证管理员空默认、岗位空覆盖及旧端保存不会复活旧默认，标题和长度边界仍受校验。
func TestReplyLibraryEmptyContentPreservesInheritance(t *testing.T) {
	lib := emptyPhraseLibrary()
	lib.Phrases[0].Content = ""
	raw, _ := json.Marshal(lib)
	parsed, err := parseAutoReplyLibrary(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	item := PositionAutoReplyConfig{ResumeRequestMessage: AutoReplyDefaultResumeRequestMessage, PhraseOverrides: []ReplyPhrase{}}
	if err = prepareReplyOverrides(&item, nil, parsed); err != nil || item.ResumeRequestMessage != "" || item.Phrases[0].Content != "" || len(item.PhraseOverrides) != 0 {
		t.Fatal("没有岗位覆盖时未继承管理员的空内容", err)
	}
	override := parsed.Phrases[1]
	override.Content = " \t\n"
	item.PhraseOverrides = []ReplyPhrase{override}
	if err = prepareReplyOverrides(&item, nil, parsed); err != nil || len(item.Phrases) != 3 || strings.TrimSpace(item.Phrases[1].Content) != "" {
		t.Fatal("显式空覆盖被当成缺失或替换为默认", err)
	}
	agent := item.forAgent()
	encoded, _ := json.Marshal(agent)
	if !strings.Contains(string(encoded), `"content":""`) || strings.Contains(string(encoded), "原默认收到简历文字") {
		t.Fatal("Agent快照丢失空条目或恢复旧正文")
	}
	legacy := PositionAutoReplyConfig{}
	if err = prepareReplyOverrides(&legacy, item.PhraseOverrides, parsed); err != nil || strings.TrimSpace(legacy.Phrases[1].Content) != "" {
		t.Fatal("旧客户端修改其它字段覆盖了静默设置", err)
	}
	bad := slices.Clone(parsed.Phrases)
	bad[0].Title = " "
	if validateReplyPhrases(bad, true, "") == nil {
		t.Fatal("空标题被接受")
	}
	bad = slices.Clone(parsed.Phrases)
	bad[0].Content = strings.Repeat("字", 201)
	if validateReplyPhrases(bad, true, "") == nil {
		t.Fatal("超长内容绕过原长度限制")
	}
}

// TestReplyLibraryDeletedScenesReachOnlyAgent 验证默认及自建话术删除后正文不泄回模型，编辑器不重新出现，恢复默认仍按原规则继承。
func TestReplyLibraryDeletedScenesReachOnlyAgent(t *testing.T) {
	lib := emptyPhraseLibrary()
	custom := ReplyPhrase{ID: "job_private", Category: "沟通", Title: "岗位自建", Scene: "候选人询问自建事项", Content: "不得恢复的自建正文"}
	previous := []ReplyPhrase{custom}
	item := PositionAutoReplyConfig{PhraseOverrides: []ReplyPhrase{{ID: "receive_resume", Deleted: true}}}
	if err := prepareReplyOverrides(&item, previous, lib); err != nil {
		t.Fatal(err)
	}
	if len(item.Phrases) != 2 || slices.ContainsFunc(item.Phrases, func(p ReplyPhrase) bool { return p.ID == custom.ID || p.ID == "receive_resume" }) {
		t.Fatal("删除项重新出现在编辑器有效列表")
	}
	agent := item.forAgent()
	for _, id := range []string{"receive_resume", custom.ID} {
		index := slices.IndexFunc(agent.Phrases, func(p ReplyPhrase) bool { return p.ID == id })
		if index < 0 || !agent.Phrases[index].Deleted || agent.Phrases[index].Content != "" || agent.Phrases[index].Title == "" || agent.Phrases[index].Scene == "" {
			t.Fatal("Agent缺少已移除场景的完整静默标记")
		}
	}
	raw, _ := json.Marshal(agent)
	if strings.Contains(string(raw), custom.Content) || strings.Contains(string(raw), lib.Phrases[1].Content) {
		t.Fatal("已移除的正文仍被下发")
	}
	restore := PositionAutoReplyConfig{PhraseOverrides: []ReplyPhrase{}}
	if err := prepareReplyOverrides(&restore, item.PhraseOverrides, lib); err != nil || !slices.Contains(restore.Phrases, lib.Phrases[1]) || slices.ContainsFunc(restore.Phrases, func(p ReplyPhrase) bool { return p.ID == custom.ID }) {
		t.Fatal("恢复默认失败，或重新出现已删除自建话术", err)
	}
	unsaved := PositionAutoReplyConfig{PhraseOverrides: []ReplyPhrase{deletedReplyPhrase(custom, ReplyPhrase{})}}
	if err := prepareReplyOverrides(&unsaved, nil, lib); err != nil || slices.ContainsFunc(unsaved.Phrases, func(p ReplyPhrase) bool { return p.ID == custom.ID }) {
		t.Fatal("同一草稿中新建后移除的自建场景不能保存", err)
	}
}

// TestAdminLibraryDeletePreservesCustomScene 验证管理员删除自定义默认项后只保留场景，未覆盖的岗位和Agent继承删除决定。
func TestAdminLibraryDeletePreservesCustomScene(t *testing.T) {
	old := emptyPhraseLibrary()
	next := old
	next.Phrases = slices.Clone(old.Phrases[:2])
	next.Phrases[0].Content = ""
	b, _ := json.Marshal(old)
	n, _ := json.Marshal(next)
	merged, err := preserveDeletedDefaultPhrases(string(b), string(n))
	if err != nil {
		t.Fatal(err)
	}
	lib, err := parseAutoReplyLibrary(merged)
	if err != nil || len(lib.Phrases) != 3 || !lib.Phrases[2].Deleted || lib.Phrases[2].Content != "" || lib.Phrases[2].Scene != old.Phrases[2].Scene {
		t.Fatal("管理员删除的自定义场景丢失或正文被保留", err)
	}
	config := PositionAutoReplyConfig{PhraseOverrides: []ReplyPhrase{}}
	applyReplyLibrary(&config, lib)
	if len(config.Phrases) != 2 || config.ResumeRequestMessage != "" || len(config.forAgent().Phrases) != 3 {
		t.Fatal("空默认或删除场景未传到岗位/Agent")
	}
	if again, err := preserveDeletedDefaultPhrases(merged, merged); err != nil || again != merged {
		t.Fatal("反复保存重复增加删除标记", err)
	}
}

// TestAdminLibraryDeletionPreservesUnknownTopLevelFields 验证删除兼容只改话术列表，未知对象、数组、布尔值和大整数不会被丢弃或转换精度。
func TestAdminLibraryDeletionPreservesUnknownTopLevelFields(t *testing.T) {
	previous, _ := json.Marshal(emptyPhraseLibrary())
	incoming := `{"phrases":[],"condition_options":["合成条件"],"future_setting":{"keep":true,"revision":9007199254740993},"future_list":["保留",null],"future_enabled":false}`
	merged, err := preserveDeletedDefaultPhrases(string(previous), incoming)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]json.RawMessage
	if err = json.Unmarshal([]byte(incoming), &before); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(merged), &after); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"condition_options", "future_setting", "future_list", "future_enabled"} {
		if string(before[key]) != string(after[key]) {
			t.Fatalf("话术删除改变了其它配置：%s", key)
		}
	}
	lib, err := parseAutoReplyLibrary(merged)
	if err != nil || len(lib.Phrases) != 3 || slices.ContainsFunc(lib.Phrases, func(p ReplyPhrase) bool { return !p.Deleted || p.Content != "" }) {
		t.Fatal("保留扩展字段时丢失了静默删除语义", err)
	}
}

// TestAdminEmptyLibraryHTTPRoundtrip 验证真实管理员保存入口接受空内容、保留删除场景且下一次保存仍满足并发版本合同。
func TestAdminEmptyLibraryHTTPRoundtrip(t *testing.T) {
	t.Setenv("GOODHR_PG_DSN", "")
	t.Setenv("GOODHR_REDIS_ADDR", "")
	t.Setenv("GOODHR_SMTP_HOST", "")
	t.Setenv("GOODHR_SUPER_ADMINS", "empty-phrases@example.com")
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "empty-phrases@example.com")
	previous, err := server.systemConfigs.Get(autoReplyLibraryKey)
	if err != nil {
		t.Fatal(err)
	}
	lib := emptyPhraseLibrary()
	lib.Phrases = []ReplyPhrase{lib.Phrases[0]}
	lib.Phrases[0].Content = ""
	raw, _ := json.Marshal(lib)
	body, _ := json.Marshal(map[string]string{"config_value": string(raw), "expected_config_value": previous.ConfigValue})
	response := rechargeTestRequest(routes, token, http.MethodPut, "/api/admin/system/configs/"+autoReplyLibraryKey, string(body))
	if response.Code != http.StatusOK {
		t.Fatalf("空内容未通过管理员真实入口：%d", response.Code)
	}
	saved, err := server.systemConfigs.Get(autoReplyLibraryKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseAutoReplyLibrary(saved.ConfigValue)
	if err != nil {
		t.Fatal(err)
	}
	config := PositionAutoReplyConfig{PhraseOverrides: []ReplyPhrase{}}
	applyReplyLibrary(&config, parsed)
	if len(config.Phrases) != 1 || config.Phrases[0].Content != "" || config.ResumeRequestMessage != "" || len(config.forAgent().Phrases) <= 1 {
		t.Fatal("HTTP保存后空内容或删除场景没有保持")
	}
	body, _ = json.Marshal(map[string]string{"config_value": saved.ConfigValue, "expected_config_value": saved.ConfigValue})
	if next := rechargeTestRequest(routes, token, http.MethodPut, "/api/admin/system/configs/"+autoReplyLibraryKey, string(body)); next.Code != http.StatusOK {
		t.Fatal("保存返回后的真实配置不能继续编辑")
	}
}
