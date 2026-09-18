// Package common 文件作用：验证岗位搜索刷新产生的短暂空列表会被重读，会话故障不会重试。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type positionRefreshBrowser struct {
	model.Browser
	calls int
	err   error
}

// FindAll 模拟岗位搜索结果先刷新为空、随后返回匹配岗位。
func (b *positionRefreshBrowser) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.calls++
	if b.err != nil {
		return nil, b.err
	}
	if b.calls == 1 {
		return nil, nil
	}
	return []contract.FindAllItem{{Index: 0, Text: "销售管培生"}}, nil
}

// TestPositionSearchRefresh 验证刷新期间空列表不会提前结束选择。
func TestPositionSearchRefresh(t *testing.T) {
	b := &positionRefreshBrowser{}
	items, err := readPositionSearchResults(context.Background(), b, contract.ElementFindAllRequest{})
	if err != nil || len(items) != 1 || b.calls != 2 {
		t.Fatalf("items=%v calls=%d err=%v", items, b.calls, err)
	}
}

// TestPositionSearchFatalError 验证浏览器故障立即返回而不是反复读取。
func TestPositionSearchFatalError(t *testing.T) {
	original := errors.New("WORKER_UNRESPONSIVE")
	b := &positionRefreshBrowser{err: original}
	_, err := readPositionSearchResults(context.Background(), b, contract.ElementFindAllRequest{})
	if !errors.Is(err, original) || b.calls != 1 {
		t.Fatalf("calls=%d err=%v", b.calls, err)
	}
}
