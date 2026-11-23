// Package platform_test 文件作用：验证推荐页、无忧搜索及猎聘猎头端从完整卡片起跑，其他平台配置不受影响。
package platform_test

import (
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"testing"
)

// TestViewportModeMatchesPageConfiguration 验证显式开启的分页与滚动页面均可从视口起跑，其他平台沿用原配置。
func TestViewportModeMatchesPageConfiguration(t *testing.T) {
	for _, id := range []string{"boss", "zhaopin", "51job", "liepin", "hliepin"} {
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
		if got := common.CandidatePageUsesViewport(cfg, cfg.EntryURL); got != (id == "hliepin") {
			t.Fatalf("%s分页页起点不符合显式配置", id)
		}
		cfg.Behavior.CandidateStartFromViewport = false
		if common.CandidatePageUsesViewport(cfg, cfg.EntryURL) {
			t.Fatalf("%s关闭视口起点后仍然启用", id)
		}
	}
}

// TestDetailReadBudgetsOnlyApplyToBody 验证五个平台仅正文取值有5秒预算，不改变其他节点、定位等待和整次任务保护。
func TestDetailReadBudgetsOnlyApplyToBody(t *testing.T) {
	for _, id := range []string{"boss", "zhaopin", "51job", "liepin", "hliepin"} {
		cfg, err := platform.LoadConfig(id)
		if err != nil {
			t.Fatal(err)
		}
		for key, spec := range cfg.Selectors {
			for _, variant := range spec.Variants() {
				want := 0
				if key == "candidate.detail" {
					want = 5000
				}
				if variant.ReadTimeoutMS != want {
					t.Fatalf("%s %s预算%d，预期%d", id, key, variant.ReadTimeoutMS, want)
				}
			}
		}
	}
}
