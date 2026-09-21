// 文件作用：验证猎聘从实际页面继续翻页、末页识别及翻页失败保护，防止重复处理旧页。
package hliepin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// pagingBrowser 模拟有独立当前页码、下一页状态和候选人列表的分页页面。
type pagingBrowser struct {
	model.Browser
	page                                            string
	lastPage, nextMissing, numberMissing, staleList bool
	readErr, nextErr, clickErr                      error
	clicks                                          []contract.ElementClickRequest
	scrolls                                         []contract.ScrollRequest
	readConcurrency                                 int
}

// PressKey 模拟长列表通过真实快捷键定位到底部分页区域。
func (b *pagingBrowser) PressKey(_ context.Context, r contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if r.Key != "Control+End" {
		return contract.KeyboardPressResult{}, errors.New("未使用预期的分页定位按键")
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// Read 每次返回页面此刻的页码，允许模拟用户手动翻页和读取失败。
func (b *pagingBrowser) Read(_ context.Context, _ contract.ElementReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{Value: b.page}, b.readErr
}

// FindAll 模拟实际配置中的分页控件，缺失时故意返回空数组以验证末页不会被误判。
func (b *pagingBrowser) FindAll(_ context.Context, r contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	switch r.Selector.Description {
	case "猎聘猎头端已到最后一页":
		if b.lastPage {
			return []contract.FindAllItem{{Text: "下一页"}}, nil
		}
		return nil, nil
	case "猎聘猎头端下一页":
		if b.nextErr != nil {
			return nil, b.nextErr
		}
		if b.nextMissing {
			return nil, nil
		}
		return []contract.FindAllItem{{Text: "下一页"}}, nil
	}
	if r.Selector.Target.Text != "" {
		n, _ := strconv.Atoi(b.page)
		if b.numberMissing || r.Selector.Target.Text != strconv.Itoa(n+1) {
			return nil, nil
		}
		return []contract.FindAllItem{{Text: strconv.Itoa(n + 1)}}, nil
	}
	if len(r.Fields) > 0 {
		b.readConcurrency = r.ReadConcurrency
		id := "page-" + b.page
		if b.staleList {
			id = "old"
		}
		return []contract.FindAllItem{{Text: "测试候选人 立即沟通", Fields: map[string]string{"name": "测试候选人", "platform_candidate_id": id}}}, nil
	}
	return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
}

// Scroll 记录真实滚轮定位请求，点击测试将检查滚轮必须先执行。
func (b *pagingBrowser) Scroll(_ context.Context, r contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls = append(b.scrolls, r)
	return contract.ScrollResult{Scrolled: true}, nil
}

// Click 模拟实际翻页并核对目标页码验证，失败后不允许再点击其他翻页入口。
func (b *pagingBrowser) Click(_ context.Context, r contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, r)
	if len(b.scrolls) != len(b.clicks) {
		return contract.ClickResult{}, errors.New("分页未先滚动到可点击位置")
	}
	n, _ := strconv.Atoi(b.page)
	if r.Verify == nil || r.Verify.TargetVisible == nil || r.Verify.TargetVisible.Target.Text != strconv.Itoa(n+1) {
		return contract.ClickResult{}, errors.New("未验证实际下一页页码")
	}
	b.page = strconv.Itoa(n + 1)
	return contract.ClickResult{Clicked: b.clickErr == nil}, b.clickErr
}

// pagingConfig 读取实际打包配置，避免测试选择器与发布配置脱节。
func pagingConfig(t *testing.T) model.Config {
	t.Helper()
	data, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err = json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestAdvanceCandidatesUsesLivePage 验证无快捷搜索启动和运行中手动换页都按实际页码继续。
func TestAdvanceCandidatesUsesLivePage(t *testing.T) {
	for _, numericOnly := range []bool{false, true} {
		t.Run(strconv.FormatBool(numericOnly), func(t *testing.T) {
			b := &pagingBrowser{page: "2", nextMissing: numericOnly}
			r := NewRuntime()
			cfg := pagingConfig(t)
			if err := r.SelectPosition(context.Background(), b, cfg, model.Position{Name: "测试岗位"}); err != nil {
				t.Fatal(err)
			}
			for _, page := range []string{"2", "7"} {
				b.page = page
				ok, err := r.AdvanceCandidateList(context.Background(), b, cfg, nil, model.CandidateListOptions{ReadConcurrency: 10})
				if err != nil || !ok {
					t.Fatalf("从实际第%s页翻页失败: %v", page, err)
				}
				click := b.clicks[len(b.clicks)-1]
				if !numericOnly && click.Selector.Description != "猎聘猎头端下一页" {
					t.Fatal("没有优先点击下一页")
				}
				if b.readConcurrency != 10 {
					t.Fatal("翻页未沿用读取并发配置")
				}
			}
		})
	}
}

// TestAdvanceCandidatesStopsOnlyAtConfirmedLastPage 验证禁用下一页才结束，缺少分页或未知页码必须报错。
func TestAdvanceCandidatesStopsOnlyAtConfirmedLastPage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		b       pagingBrowser
		wantErr bool
	}{
		{"末页", pagingBrowser{page: "23", lastPage: true}, false},
		{"单页", pagingBrowser{page: "1", lastPage: true}, false},
		{"缺少页码", pagingBrowser{page: ""}, true},
		{"页码无效", pagingBrowser{page: "加载中"}, true},
		{"分页入口缺失", pagingBrowser{page: "2", nextMissing: true, numberMissing: true}, true},
		{"读取超时", pagingBrowser{page: "2", readErr: context.DeadlineExceeded}, true},
		{"下一页探测超时", pagingBrowser{page: "2", nextErr: context.DeadlineExceeded}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := NewRuntime().AdvanceCandidateList(context.Background(), &tc.b, pagingConfig(t), nil, model.CandidateListOptions{})
			if ok || (err != nil) != tc.wantErr || len(tc.b.clicks) != 0 {
				t.Fatalf("ok=%v err=%v clicks=%d", ok, err, len(tc.b.clicks))
			}
		})
	}
}

// TestAdvanceCandidatesDoesNotDoubleClickAfterTimeout 验证翻页已经触发但验证超时时不会补点数字页码。
func TestAdvanceCandidatesDoesNotDoubleClickAfterTimeout(t *testing.T) {
	b := &pagingBrowser{page: "2", clickErr: context.DeadlineExceeded}
	ok, err := NewRuntime().AdvanceCandidateList(context.Background(), b, pagingConfig(t), nil, model.CandidateListOptions{})
	if ok || !errors.Is(err, context.DeadlineExceeded) || len(b.clicks) != 1 {
		t.Fatalf("ok=%v err=%v clicks=%d", ok, err, len(b.clicks))
	}
}

// TestAdvanceCandidatesRequiresFreshList 验证页码变化但列表尚未更新时不得报告翻页成功或全部完成。
func TestAdvanceCandidatesRequiresFreshList(t *testing.T) {
	b := &pagingBrowser{page: "2", staleList: true}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	before := []model.Candidate{{Fields: map[string]string{"platform_candidate_id": "old"}}}
	ok, err := NewRuntime().AdvanceCandidateList(ctx, b, pagingConfig(t), before, model.CandidateListOptions{})
	if ok || err == nil {
		t.Fatalf("旧候选人列表被当作成功: ok=%v err=%v", ok, err)
	}
}
