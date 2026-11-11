// Package platform_test 文件作用：验证BOSS、智联和无忧仅无限推荐页按完整卡片起跑，分页搜索页及暂缓的猎聘配置不受影响。
package platform_test

import (
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"testing"
)

// TestViewportModeOnlyOnRecommendation 验证配置与真实页面类型共同决定起点方式，不把翻页搜索或其他平台误当成推荐页。
func TestViewportModeOnlyOnRecommendation(t *testing.T) {
	for _, id := range []string{"boss", "zhaopin", "51job", "liepin"} {
		cfg, err := platform.LoadConfig(id)
		if err != nil {
			t.Fatal(err)
		}
		if got := common.CandidatePageUsesViewport(cfg, cfg.EntryURL); got != (id != "liepin") {
			t.Fatalf("%s推荐页起点方式错误", id)
		}
		if cfg.ManualSearchURL != "" && common.CandidatePageUsesViewport(cfg, cfg.ManualSearchURL) {
			t.Fatalf("%s搜索页不应改起点", id)
		}
		cfg.Behavior.CandidateListMode = "next_page"
		if common.CandidatePageUsesViewport(cfg, cfg.EntryURL) {
			t.Fatal("翻页模式不应按视口重置")
		}
	}
}
