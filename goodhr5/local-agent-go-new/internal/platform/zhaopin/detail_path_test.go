// Package zhaopin_test 文件作用：复现搜索页先用推荐卡片失败再成功的问题，验证路径筛选、加载等待及身份约束。
package zhaopin_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/platform/zhaopin"
)

// detailPathBrowser 仅模拟当前布局和候选人详情，不连接账号或发送消息。
type detailPathBrowser struct {
	model.Browser
	search, loading, opened, fallback bool
	probeErr                          error
	clicks                            []contract.ElementClickRequest
	cardProbes                        int
}

// ListPages 为搜索页初始化提供准确活动页地址，不控制浏览器标签。
func (b *detailPathBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	url := "https://rd6.zhaopin.com/app/recommend"
	if b.search {
		url = "https://rd6.zhaopin.com/app/search"
	}
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: url, Current: true}}}, nil
}

// FindAll 返回限定身份的卡片和正文状态，卡片预探测必须是即时且允许缺失的标准只读操作。
func (b *detailPathBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.Selector.Description == "智联候选人详情正文" {
		if b.opened {
			return []contract.FindAllItem{{}}, nil
		}
		return nil, nil
	}
	value := req.Selector.Target.Selectors[0].Value
	if strings.Contains(value, "__inner") {
		if len(req.Selector.Target.Texts) == 0 {
			return []contract.FindAllItem{{}}, nil
		}
		b.cardProbes++
		if b.probeErr != nil {
			return nil, b.probeErr
		}
		if req.Selector.TimeoutMS != 1 || !req.ProbeOnly || !req.ExpectedMissing || len(req.Selector.Target.Texts) != 2 {
			return nil, errors.New("卡片探测丢失身份或短探测约束")
		}
		if !b.loading && strings.Contains(value, "search-resume") == b.search {
			return []contract.FindAllItem{{}}, nil
		}
	}
	return nil, nil
}

// Click 复现推荐卡片在搜索页不存在，正确布局才可打开；每次记录完整请求以核对身份和次数。
func (b *detailPathBrowser) Click(_ context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, req)
	card := req.Selector.Parents[len(req.Selector.Parents)-1]
	if strings.Contains(card.Selectors[0].Value, "search-resume") != b.search || (b.fallback && strings.Contains(req.Selector.Target.Selectors[0].Value, "name")) {
		return contract.ClickResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND", Message: "合成错误布局", Details: []byte(`{"click_started":false}`)}}
	}
	b.opened = true
	return contract.ClickResult{Clicked: true}, nil
}

// TestDetailUsesCurrentCandidateLayout 验证搜索页限定卡片路径，推荐页完全跳过新增探测，备用入口和加载等待仍保留。
func TestDetailUsesCurrentCandidateLayout(t *testing.T) {
	for _, test := range []struct {
		name                      string
		search, loading, fallback bool
		failure                   error
		clicks                    int
	}{
		{name: "搜索页", search: true, clicks: 1},
		{name: "推荐页", clicks: 1},
		{name: "搜索页备用入口", search: true, fallback: true, clicks: 2},
		{name: "加载中保留完整等待路径", search: true, loading: true, clicks: 2},
		{name: "探测断开立即停止", search: true, failure: errors.New("合成浏览器断开")},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := platform.LoadConfig("zhaopin")
			if err != nil {
				t.Fatal(err)
			}
			browser := &detailPathBrowser{search: test.search, loading: test.loading, fallback: test.fallback, probeErr: test.failure}
			candidate := model.Candidate{Name: "合成人选", IdentityTexts: []string{"合成人选", "28岁"}, IdentityOccurrence: 1}
			runtime := zhaopin.NewRuntime()
			if test.search {
				if err := runtime.InitializeGreetingPage(context.Background(), browser, cfg); err != nil {
					t.Fatal(err)
				}
			}
			err = runtime.OpenCandidateDetail(context.Background(), browser, cfg, candidate)
			if !errors.Is(err, test.failure) || len(browser.clicks) != test.clicks {
				t.Fatalf("详情路径结果错误：%v clicks=%d", err, len(browser.clicks))
			}
			for _, req := range browser.clicks {
				card := req.Selector.Parents[len(req.Selector.Parents)-1]
				if len(card.Texts) != 2 || card.Index == nil || *card.Index != 1 {
					t.Fatal("卡片身份或同名序号丢失")
				}
				if !test.loading && strings.Contains(card.Selectors[0].Value, "search-resume") != test.search {
					t.Fatal("仍尝试错误页面布局")
				}
			}
			if len(cfg.Selectors["candidate.item"].Variants()) != 2 {
				t.Fatal("本次匹配污染了原始配置")
			}
			if !test.search && browser.cardProbes != 0 {
				t.Fatal("推荐页进入了新增探测逻辑")
			}
			if !test.search {
				baseline := &detailPathBrowser{}
				if err := common.OpenCandidateDetail(context.Background(), baseline, cfg, candidate); err != nil || !reflect.DeepEqual(browser.clicks, baseline.clicks) {
					t.Fatal("推荐页点击请求与原公共流程不一致", err)
				}
			}
		})
	}
}
