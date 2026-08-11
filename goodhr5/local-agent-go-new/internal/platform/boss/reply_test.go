// Package boss 文件作用：验证 Boss 自动回复始终留在推荐页并正确区分消息方向。
package boss

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBossAutoReplyStaysOnRecommendPage 验证 Boss 自动回复不会导航到独立沟通页。
func TestBossAutoReplyStaysOnRecommendPage(t *testing.T) {
	cfg := loadBossAutoReplyTestConfig(t)
	if cfg.MessagesURL != cfg.EntryURL || cfg.MessagesURL != "https://www.zhipin.com/web/chat/recommend" {
		t.Fatalf("Boss 自动回复必须留在推荐页：entry=%s messages=%s", cfg.EntryURL, cfg.MessagesURL)
	}
}

// TestBossMessageMarkersSeparateCandidateAndHR 验证 Boss 候选人和 HR 消息方向不会混淆。
func TestBossMessageMarkersSeparateCandidateAndHR(t *testing.T) {
	items := []contract.FindAllItem{
		{Index: 0, Fields: map[string]string{"body_class": "item-friend", "message_text": "候选人消息"}},
		{Index: 1, Fields: map[string]string{"body_class": "item-myself", "message_text": "HR 消息"}},
	}
	messages, err := common.ParseConfiguredAutoReplyMessages(items, bossAutoReplyAdapter.MessageRules, time.Date(2026, 8, 12, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	if err != nil {
		t.Fatalf("解析 Boss 消息方向失败：%v", err)
	}
	if len(messages) != 2 || messages[0].Direction != "candidate" || messages[1].Direction != "self" {
		t.Fatalf("Boss 候选人和 HR 消息方向不正确：%+v", messages)
	}
}

// loadBossAutoReplyTestConfig 读取并解析当前目录中的 Boss 本地配置。
func loadBossAutoReplyTestConfig(t *testing.T) model.Config {
	t.Helper()
	content, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatalf("读取 Boss 配置失败：%v", err)
	}
	var cfg model.Config
	if err = json.Unmarshal(content, &cfg); err != nil {
		t.Fatalf("解析 Boss 配置失败：%v", err)
	}
	return cfg
}
