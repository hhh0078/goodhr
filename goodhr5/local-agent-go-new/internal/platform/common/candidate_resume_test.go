// Package common 文件作用：验证各平台共享断点分批查找、联合身份唯一性、前缀变化保护和错误传播。
package common

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// resumeIdentityBrowser 只返回已加载行的最小字段，记录每次读取量，拒绝本测试未授权的页面操作。
type resumeIdentityBrowser struct {
	model.Browser
	rows     []model.Candidate
	requests []contract.ElementFindAllRequest
	cause    error
}

// FindAll 模拟按真实全局行号分批读取，支持取消和断线。
func (b *resumeIdentityBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.cause != nil {
		return nil, b.cause
	}
	b.requests = append(b.requests, req)
	items := []contract.FindAllItem{}
	for i := req.StartIndex; i < min(len(b.rows), req.StartIndex+req.MaxItems); i++ {
		items = append(items, contract.FindAllItem{Index: i, Fields: b.rows[i].Fields})
	}
	return items, nil
}

// resumeCandidateRows 提供互不重名的合成候选人，用于重启和批次边界测试。
func resumeCandidateRows(n int) []model.Candidate {
	rows := make([]model.Candidate, n)
	for i := range rows {
		rows[i] = model.Candidate{Index: i, Fingerprint: fmt.Sprint("test_", i), Fields: map[string]string{"name": fmt.Sprint("合成人选", i), "basic_info": "30岁 本科", "education": "测试学校"}}
	}
	return rows
}

// resumeCandidateConfig 用现有字段构成联合身份，不复制平台选择器。
func resumeCandidateConfig() model.Config {
	cfg := scrollCandidateConfig()
	cfg.CandidateFields = map[string]contract.SelectorSpec{"name": selector("姓名"), "basic_info": selector("基本信息"), "education": selector("经历")}
	return cfg
}

// TestSharedResumeSearchesSmallWindows 验证恢复位置超过一批时，每次最多只读20个联合身份，且不重新从头处理。
func TestSharedResumeSearchesSmallWindows(t *testing.T) {
	cfg := resumeCandidateConfig()
	rows := resumeCandidateRows(65)
	tracker := &CandidateResumeTracker{}
	_, _ = tracker.RestoreCandidateList(context.Background(), nil, cfg, model.CandidateResumePoint{}, &model.CandidateListCursor{})
	tracker.RememberCandidateResume(cfg, rows)
	point, ok := tracker.CandidateResumePoint(rows[49])
	if !ok {
		t.Fatal("没有断点")
	}
	next := &CandidateResumeTracker{}
	browser := &resumeIdentityBrowser{rows: rows}
	cursor := &model.CandidateListCursor{}
	found, err := next.RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
	if err != nil || !found || cursor.StartIndex != 49 || len(browser.requests) != 4 {
		t.Fatalf("恢复错误：found=%v err=%v cursor=%+v requests=%d", found, err, cursor, len(browser.requests))
	}
	for _, req := range browser.requests {
		if req.MaxItems != 20 || len(req.Fields) != 3 {
			t.Fatalf("不是最小分批读取：%+v", req)
		}
	}
}

// TestSharedResumeDoesNotGuess 验证重复联合身份、换序、缺失及读取错误都不能静默接错人。
func TestSharedResumeDoesNotGuess(t *testing.T) {
	cfg := resumeCandidateConfig()
	rows := resumeCandidateRows(4)
	tracker := &CandidateResumeTracker{enabled: true}
	tracker.RememberCandidateResume(cfg, rows)
	point, _ := tracker.CandidateResumePoint(rows[1])
	for _, kind := range []string{"缺失", "换序", "重复", "断线"} {
		t.Run(kind, func(t *testing.T) {
			changed := append([]model.Candidate(nil), rows...)
			browser := &resumeIdentityBrowser{rows: changed}
			switch kind {
			case "缺失":
				browser.rows = append(changed[:1], changed[2:]...)
			case "换序":
				changed[0], changed[2] = changed[2], changed[0]
			case "重复":
				changed[3] = changed[1]
			case "断线":
				browser.cause = errors.New("connection closed")
			}
			cursor := &model.CandidateListCursor{StartIndex: 99}
			found, err := (&CandidateResumeTracker{}).RestoreCandidateList(context.Background(), browser, cfg, point, cursor)
			if found || cursor.StartIndex != 0 || (err != nil) != (kind == "断线") {
				t.Fatalf("错误被当成成功或缺失：%v %v %+v", found, err, cursor)
			}
		})
	}
}

// TestCandidateBatchSizePreservesHeadProbe 验证候选人只读20人、轻量头探测仍只读1人；不降低联系人原有100人上限。
func TestCandidateBatchSizePreservesHeadProbe(t *testing.T) {
	cfg := scrollCandidateConfig()
	cfg.CandidateBatchSize = 20
	browser := &scrollingCandidateBrowser{count: 100}
	rows, err := FindCandidates(context.Background(), browser, cfg, "test", model.CandidateListOptions{})
	if err != nil || len(rows) != 20 || cfg.MaxItems != 100 {
		t.Fatalf("小批读取错误：%d %v", len(rows), err)
	}
	head := cfg
	head.MaxItems = 1
	rows, err = FindCandidates(context.Background(), browser, head, "test", model.CandidateListOptions{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("头探测被扩大：%d %v", len(rows), err)
	}
}

// TestResumeUsesConfiguredStableFields 验证平台可排除活跃时间等动态文字，稳定年龄学历不变时断点仍然一致。
func TestResumeUsesConfiguredStableFields(t *testing.T) {
	cfg := resumeCandidateConfig()
	cfg.CandidateResumeFields = []string{"name", "education"}
	first := map[string]string{"name": "合成人选", "education": "30岁 5年 本科", "basic_info": "刚刚活跃"}
	later := map[string]string{"name": "合成人选", "education": "30岁 5年 本科", "basic_info": "1小时前活跃"}
	if candidateResumeIdentity(cfg, first) != candidateResumeIdentity(cfg, later) {
		t.Fatal("活跃状态改变了稳定断点")
	}
	later["education"] = "31岁 10年 硕士"
	if candidateResumeIdentity(cfg, first) == candidateResumeIdentity(cfg, later) {
		t.Fatal("不同候选人的联合身份混淆")
	}
}
