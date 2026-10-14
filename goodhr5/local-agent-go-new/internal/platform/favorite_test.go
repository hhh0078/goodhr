// Package platform_test 文件作用：验证两个平台收藏动作、唯一定位、结果确认和不支持平台的空操作。
package platform_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// favoriteBrowser 在 Worker 封装能力边界模拟收藏状态，记录实际点击与失败后的调用次数。
type favoriteBrowser struct {
	model.Browser
	savedMatches int
	probeErr     error
	scrollErr    error
	clickErr     error
	failAt       int
	clicked      int
	probes       []contract.ElementFindAllRequest
	clicks       []contract.ElementClickRequest
	scrolls      []contract.ScrollRequest
	order        []string
}

// FindAll 返回已收藏状态匹配数量，模拟唯一、缺失或同名元素。
func (b *favoriteBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.probes = append(b.probes, request)
	if b.probeErr != nil {
		return nil, b.probeErr
	}
	items := make([]contract.FindAllItem, b.savedMatches)
	return items, nil
}

// Click 在指定步骤失败，否则模拟 Worker 完成唯一定位、真实点击和结果确认。
func (b *favoriteBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, request)
	b.order = append(b.order, "click")
	if b.failAt == len(b.clicks) {
		return contract.ClickResult{}, b.clickErr
	}
	b.clicked++
	return contract.ClickResult{Clicked: true, Verified: true}, nil
}

// Scroll 记录收藏前通过真实滚轮回到详情顶部的请求，并支持模拟滚动失败。
func (b *favoriteBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls = append(b.scrolls, request)
	b.order = append(b.order, "scroll")
	return contract.ScrollResult{}, b.scrollErr
}

// TestFavoriteCandidatePreservesSavedState 验证已收藏不点击，不唯一或探测失败时也不猜测。
func TestFavoriteCandidatePreservesSavedState(t *testing.T) {
	for _, platformID := range []string{"boss", "hliepin"} {
		for _, item := range []struct {
			name     string
			count    int
			probeErr error
			wantErr  bool
		}{
			{name: "已经收藏", count: 1},
			{name: "两个同名已收藏状态", count: 2, wantErr: true},
			{name: "浏览器状态读取失败", probeErr: errors.New("探测失败"), wantErr: true},
		} {
			t.Run(platformID+"/"+item.name, func(t *testing.T) {
				runtime, cfg := favoriteRuntimeForTest(t, platformID)
				browser := &favoriteBrowser{savedMatches: item.count, probeErr: item.probeErr}
				err := runtime.FavoriteCandidate(context.Background(), browser, cfg, model.Candidate{})
				if (err != nil) != item.wantErr || browser.clicked != 0 || len(browser.clicks) != 0 || len(browser.scrolls) != 0 {
					t.Fatalf("状态保护错误：err=%v clicks=%d requests=%d scrolls=%d", err, browser.clicked, len(browser.clicks), len(browser.scrolls))
				}
				if len(browser.probes) != 1 || browser.probes[0].MaxItems != 2 || !browser.probes[0].ProbeOnly || !browser.probes[0].ExpectedMissing || browser.probes[0].Selector.Target.Index != nil {
					t.Fatalf("已收藏探测没有保留两个元素以识别歧义：%+v", browser.probes)
				}
			})
		}
	}
}

// TestFavoriteCandidateClickSequenceAndVerification 验证 Boss 一次点击及猎聘三次点击都携带结果确认。
func TestFavoriteCandidateClickSequenceAndVerification(t *testing.T) {
	for _, platformID := range []string{"boss", "hliepin"} {
		t.Run(platformID, func(t *testing.T) {
			runtime, cfg := favoriteRuntimeForTest(t, platformID)
			browser := &favoriteBrowser{}
			if err := runtime.FavoriteCandidate(context.Background(), browser, cfg, model.Candidate{}); err != nil {
				t.Fatal(err)
			}
			keys := []string{"candidate.favorite"}
			visibleKeys := []string{"candidate.favorite_saved"}
			if platformID == "hliepin" {
				keys = []string{"candidate.favorite", "candidate.favorite_group", "candidate.favorite_save"}
				visibleKeys = []string{"candidate.favorite_panel", "candidate.favorite_group_selected", "candidate.favorite_saved"}
				if len(browser.scrolls) != 1 || browser.order[0] != "scroll" {
					t.Fatalf("猎聘没有先滚回收藏按钮：%+v", browser.order)
				}
				scroll := browser.scrolls[0]
				// 置顶按钮固定在窗口边缘；只要求安全落点，完整可见加边距会让真实滚轮永远无法满足。
				if scroll.Target == nil || !reflect.DeepEqual(*scroll.Target, cfg.Selectors["candidate.favorite"]) || scroll.WheelAnchor == nil || !reflect.DeepEqual(*scroll.WheelAnchor, cfg.Selectors["candidate.detail"]) || scroll.RequireFull == nil || *scroll.RequireFull || scroll.ViewportMargin != 0 || scroll.MaxAttempts != 40 || scroll.Distance != 520 {
					t.Fatalf("猎聘滚轮目标或约束不正确：%+v", scroll)
				}
				if browser.clicks[0].ViewportMargin != 0 {
					t.Fatal("置顶收藏按钮不能被要求离开固定窗口边缘后才能点击")
				}
			}
			if len(browser.clicks) != len(keys) || browser.clicked != len(keys) {
				t.Fatalf("收藏点击次数不正确：%d", len(browser.clicks))
			}
			for index, key := range keys {
				request := browser.clicks[index]
				if !reflect.DeepEqual(request.Selector, cfg.Selectors[key]) || request.Selector.Target.Index != nil || request.Verify == nil || request.Verify.TargetVisible == nil || !reflect.DeepEqual(*request.Verify.TargetVisible, cfg.Selectors[visibleKeys[index]]) {
					t.Fatalf("第 %d 次点击缺少唯一定位或结果确认：%+v", index, request)
				}
			}
			if platformID == "hliepin" && (browser.clicks[2].Verify.TargetHidden == nil || !reflect.DeepEqual(*browser.clicks[2].Verify.TargetHidden, cfg.Selectors["candidate.favorite_panel"])) {
				t.Fatal("猎聘保存收藏必须确认分组弹层消失")
			}
			browser.savedMatches = 1
			if err := runtime.FavoriteCandidate(context.Background(), browser, cfg, model.Candidate{}); err != nil || len(browser.clicks) != len(keys) {
				t.Fatalf("重复收藏取消了原收藏或产生重复点击：%v", err)
			}
		})
	}
}

// TestFavoriteCandidateFailureDoesNotRetry 验证收藏任何步骤失败后直接返回，包括同名动作和滚动失败。
func TestFavoriteCandidateFailureDoesNotRetry(t *testing.T) {
	for _, platformID := range []string{"boss", "hliepin"} {
		steps := 1
		if platformID == "hliepin" {
			steps = 3
		}
		for failAt := 1; failAt <= steps; failAt++ {
			for _, failure := range []error{errors.New("收藏确认失败"), &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_AMBIGUOUS"}}} {
				t.Run(platformID+"/"+failure.Error(), func(t *testing.T) {
					runtime, cfg := favoriteRuntimeForTest(t, platformID)
					browser := &favoriteBrowser{failAt: failAt, clickErr: failure}
					err := runtime.FavoriteCandidate(context.Background(), browser, cfg, model.Candidate{})
					if !errors.Is(err, failure) || len(browser.clicks) != failAt || browser.clicked != failAt-1 {
						t.Fatalf("失败后继续或重复点击：err=%v requests=%d clicks=%d", err, len(browser.clicks), browser.clicked)
					}
				})
			}
		}
	}
	runtime, cfg := favoriteRuntimeForTest(t, "hliepin")
	failure := errors.New("滚轮没有把收藏按钮带回屏幕")
	browser := &favoriteBrowser{scrollErr: failure}
	if err := runtime.FavoriteCandidate(context.Background(), browser, cfg, model.Candidate{}); !errors.Is(err, failure) || len(browser.clicks) != 0 {
		t.Fatalf("猎聘滚动失败后仍然点击：%v clicks=%d", err, len(browser.clicks))
	}
}

// TestFavoriteCandidateUnsupportedPlatformsAreNoop 验证智联及猎聘企业端不需要浏览器或配置即可安全跳过。
func TestFavoriteCandidateUnsupportedPlatformsAreNoop(t *testing.T) {
	for _, platformID := range []string{"zhaopin", "liepin"} {
		runtime, _ := favoriteRuntimeForTest(t, platformID)
		if err := runtime.FavoriteCandidate(context.Background(), nil, model.Config{}, model.Candidate{}); err != nil {
			t.Fatalf("%s 未支持收藏却返回错误：%v", platformID, err)
		}
	}
}

// favoriteRuntimeForTest 读取正式内嵌配置和对应平台实现，避免测试替代真实动作配置。
func favoriteRuntimeForTest(t *testing.T, platformID string) (model.Runtime, model.Config) {
	t.Helper()
	runtime, err := platform.RuntimeFor(platformID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := platform.LoadConfig(platformID)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, cfg
}
