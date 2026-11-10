// Package platform 文件作用：验证前程无忧复制适配后的配置边界、详情加载条件和未开放自动回复的启动拦截。
package platform

import (
	"os"
	"strings"
	"testing"
)

// TestJob51ConfigUsesConfirmedGreetingPages 验证两页入口、主列表限定和精确招呼文案，防止残留智联配置。
func TestJob51ConfigUsesConfirmedGreetingPages(t *testing.T) {
	content, err := os.ReadFile("job51/config.json")
	if err != nil {
		t.Fatal(err)
	}
	assertConfigComments(t, content)
	cfg, err := LoadConfig("51job")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID != "51job" || len(cfg.EntryURLs) != 2 || cfg.Behavior.CandidateListMode != "infinite_scroll" {
		t.Fatalf("两页配置不完整：%+v", cfg)
	}
	if strings.Contains(string(content), "zhaopin") || strings.Contains(string(content), "智联") || cfg.MessagesURL != "" || cfg.Behavior.AttachmentAllowedHost != "" {
		t.Fatal("不能把其他平台的消息/附件节点当成前程无忧能力")
	}
	if cfg.Behavior.DirectPositionSelection || cfg.Behavior.SelectFirstPositionResult {
		t.Fatal("前程无忧必须先确认当前岗位并按名字匹配")
	}
	if cfg.Behavior.CandidateTopInset != 216 || cfg.Behavior.CandidateBottomInset != 288 {
		t.Fatal("前程无忧按钮必须避让固定页头和底部客服浮层")
	}
	card := cfg.Selectors["candidate.item"]
	if len(card.Parents) != 1 || !strings.Contains(card.Parents[0].Selectors[0].Value, "talent-recommend-container") || !strings.Contains(card.Target.Selectors[0].Value, "RESUME_CARD_") {
		t.Fatal("卡片必须限定在主列表，不能混进详情内相似人选")
	}
	if !strings.Contains(cfg.Selectors["candidate.detail"].Target.Selectors[0].Value, ".user_name:not(:empty)") {
		t.Fatal("详情骨架未加载姓名时不能读取正文")
	}
	if !strings.Contains(cfg.Selectors["candidate.detail"].Target.Selectors[0].Value, "> div:has(.resume_main_item)") {
		t.Fatal("正文必须排除同级水印容器，不能按公司名字全局删除")
	}
	id := cfg.CandidateFields["platform_candidate_id"]
	if id.ReadAttribute != "id" || id.Target.Selectors[0].Value != ":scope" || id.State != "attached" {
		t.Fatal("候选人编号必须从卡片自身公开id读取")
	}
	greet := cfg.Selectors["candidate.greet"].Target
	if greet.Text != "打个招呼" || greet.ExactText == nil || !*greet.ExactText {
		t.Fatal("招呼动作必须精确限定文案")
	}
	if err = ValidateTaskConfig(cfg, "greeting"); err != nil {
		t.Fatal(err)
	}
	if err = ValidateTaskConfig(cfg, "auto_reply"); err == nil || !strings.Contains(err.Error(), "消息页") {
		t.Fatal("尚未适配的自动回复必须在启动前明确拒绝")
	}
}

// TestJob51ButtonsSupportWideAndNarrowWindows 验证宽屏long按钮与窄屏short按钮都保留，且始终用精确文案隔离动作。
func TestJob51ButtonsSupportWideAndNarrowWindows(t *testing.T) {
	cfg, err := LoadConfig("51job")
	if err != nil {
		t.Fatal(err)
	}
	for key, wantText := range map[string]string{"candidate.greet": "打个招呼", "candidate.continue": "继续聊聊"} {
		target := cfg.Selectors[key].Target
		if len(target.Selectors) != 2 || target.Selectors[0].Value != ".right-opt .long-btn:visible" || target.Selectors[1].Value != ".right-opt .short-btn:visible" || target.Text != wantText || target.ExactText == nil || !*target.ExactText {
			t.Fatalf("%s必须同时支持真实宽窄按钮且不误点其它操作", key)
		}
	}
}
