// Package zhaopin_test 文件作用：验证推荐页同岗位启动不刷新名单，名称省略或不同岗位仍执行原选岗流程。
package zhaopin_test

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/platform/zhaopin"
)

// currentPositionBrowser 只允许读取当前推荐页及岗位，任何后续列表读取都留下明确记录。
type currentPositionBrowser struct {
	model.Browser
	url, name string
	listReads int
}

// ListPages 返回唯一推荐页。
func (b *currentPositionBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: b.url, Current: true}}}, nil
}

// Read 返回完整岗位文本；显示省略仅由CSS实现，不应读取截断值冒充全名。
func (b *currentPositionBrowser) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{Value: b.name}, nil
}

// FindAll 中断真正切岗前的读取，证明不同全名没有被误判为同一岗位。
func (b *currentPositionBrowser) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.listReads++
	return nil, errors.New("测试到达原切岗路径")
}

// TestRecommendationKeepsExactCurrentPosition 验证只有完整名称一致才保留当前名单，括号差异和省略名都不能跳过选岗。
func TestRecommendationKeepsExactCurrentPosition(t *testing.T) {
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	target := "招生教务老师（五险一金）"
	for _, name := range []string{target, " 招生 教务老师（五险一金） ", "招生教务老师（双休）", "招生教务老师…"} {
		b := &currentPositionBrowser{url: cfg.EntryURL, name: name}
		err := zhaopin.NewRuntime().SelectPosition(context.Background(), b, cfg, model.Position{Name: target})
		same := name == target || name == " 招生 教务老师（五险一金） "
		if (err == nil) != same || (b.listReads == 0) != same {
			t.Fatalf("岗位%q判断不正确：%v，列表读取%d", name, err, b.listReads)
		}
	}
}
