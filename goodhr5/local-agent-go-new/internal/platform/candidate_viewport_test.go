// Package platform_test 文件作用：验证推荐页与无忧搜索从完整卡片起跑，智联搜索和暂缓的猎聘配置不受影响。
package platform_test

import (
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"testing"
)

// TestViewportModeMatchesPageConfiguration 验证配置与真实页面类型共同决定起点方式，不把翻页搜索或其他平台误当成推荐页。
func TestViewportModeMatchesPageConfiguration(t *testing.T) {
	for _, id := range []string{"boss", "zhaopin", "51job", "liepin"} {
		cfg, err := platform.LoadConfig(id)
		if err != nil {
			t.Fatal(err)
		}
		if got := common.CandidatePageUsesViewport(cfg, cfg.EntryURL); got != (id != "liepin") {
			t.Fatalf("%s推荐页起点方式错误", id)
		}
		if cfg.ManualSearchURL != "" && common.CandidatePageUsesViewport(cfg, cfg.ManualSearchURL) != (id == "51job") {
			t.Fatalf("%s搜索页起点不符合配置", id)
		}
		cfg.Behavior.CandidateListMode = "next_page"
		if common.CandidatePageUsesViewport(cfg, cfg.EntryURL) {
			t.Fatal("翻页模式不应按视口重置")
		}
	}
}
