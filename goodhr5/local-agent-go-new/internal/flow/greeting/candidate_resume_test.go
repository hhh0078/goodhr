// Package greeting 文件作用：验证本地断点跨进程重开后仍有效，最新位置与未完成者分别保留，账号和岗位配置互相隔离。
package greeting

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// resumeFlowRuntime 使用真实公共断点实现，其余任务动作不参与存储测试。
type resumeFlowRuntime struct {
	model.Runtime
	common.CandidateResumeTracker
}

// resumeProviderRuntime 模拟正式日志封装只显式交付底层断点接口，其他方法继续由原运行时执行。
type resumeProviderRuntime struct {
	model.Runtime
	resumer model.CandidateListResumer
}

// CandidateListResumer 返回已有平台的可选断点能力，不支持时保持 nil。
func (r resumeProviderRuntime) CandidateListResumer() model.CandidateListResumer { return r.resumer }

// FindCandidates 让补处理测试复用真实公共名单与断点实现，不模拟发送或平台脚本。
func (r *resumeFlowRuntime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
	rows, err := common.FindCandidates(ctx, browser, cfg, cfg.ID, options)
	if err == nil {
		r.RememberCandidateResume(cfg, rows)
	}
	return rows, err
}

// resumeFlowBrowser 仅提供活动页和合成名单，不执行鼠标键盘或发送。
type resumeFlowBrowser struct {
	model.Browser
	rows []model.Candidate
	url  string
}

// ListPages 返回唯一活动页面，地址仅用于内存中的作用域摘要。
func (b *resumeFlowBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: b.url, Current: true}}}, nil
}

// FindAll 在重启恢复时返回合成人选，保留全局序号。
func (b *resumeFlowBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	rows := []contract.FindAllItem{}
	for i := req.StartIndex; i < min(len(b.rows), req.StartIndex+req.MaxItems); i++ {
		rows = append(rows, contract.FindAllItem{Index: i, Text: b.rows[i].Fields["name"] + " " + b.rows[i].Fields["basic_info"], Fields: b.rows[i].Fields})
	}
	return rows, nil
}

// resumeFlowFixture 构造最小账号岗位与三人名单，完整隐私原文不得进入断点JSON。
func resumeFlowFixture() (shared.PreparedTask, *resumeFlowBrowser) {
	prepared := shared.PreparedTask{Session: cloud.UserSession{UserID: "user-a"}, ProfilePath: "/fixture/profile-a", Position: cloud.PositionSnapshot{ID: "position-a", Name: "合成岗位"}, Platform: model.Config{ID: "test", Selectors: map[string]contract.SelectorSpec{"candidate.item": {Description: "合成人选", Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".fixture"}}}}}, CandidateFields: map[string]contract.SelectorSpec{"name": {Description: "姓名"}, "basic_info": {Description: "基础信息"}}}}
	rows := []model.Candidate{{Index: 0, Fingerprint: "test_不可落盘的甲_30岁", Fields: map[string]string{"name": "不可落盘的甲", "basic_info": "30岁"}}, {Index: 1, Fingerprint: "test_不可落盘的乙_31岁", Fields: map[string]string{"name": "不可落盘的乙", "basic_info": "31岁"}}, {Index: 2, Fingerprint: "test_不可落盘的丙_32岁", Fields: map[string]string{"name": "不可落盘的丙", "basic_info": "32岁"}}}
	return prepared, &resumeFlowBrowser{rows: rows, url: "https://example.invalid/search?q=不可落盘的查询"}
}

// TestCandidateResumePersistsFailuresAndLatestSuccess 验证最新进度独立推进，失败者单独持久化，重开库先补处理且不倒退最新成功位置。
func TestCandidateResumePersistsFailuresAndLatestSuccess(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "resume.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	prepared, browser := resumeFlowFixture()
	runtime := &resumeFlowRuntime{}
	flow := &Flow{Store: store}
	resume, err := flow.prepareCandidateResume(ctx, browser, prepared, resumeProviderRuntime{runtime, runtime}, &model.CandidateListCursor{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.RememberCandidateResume(prepared.Platform, browser.rows)
	raw, _ := store.GetSetting(ctx, resume.key)
	if raw != "" {
		t.Fatal("预读取不应该推进断点")
	}
	if err := resume.complete(ctx, browser.rows[0], nil); err != nil {
		t.Fatal(err)
	}
	sameFingerprint := browser.rows[1]
	sameFingerprint.Fingerprint = browser.rows[0].Fingerprint
	if resume.completedAnchor(sameFingerprint) {
		t.Fatal("同名指纹被一起跳过")
	}
	if !resume.completedAnchor(browser.rows[0]) || resume.completedAnchor(browser.rows[1]) {
		t.Fatal("已完成锚点与其他人混淆")
	}

	if err := resume.complete(ctx, browser.rows[1], errors.New("未处理完成")); err != nil {
		t.Fatal(err)
	}
	if err := resume.complete(ctx, browser.rows[2], nil); err != nil {
		t.Fatal(err)
	}
	raw, _ = store.GetSetting(ctx, resume.key)
	var saved candidateResumeRecord
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	firstPoint, _ := runtime.CandidateResumePoint(browser.rows[0])
	latestPoint, _ := runtime.CandidateResumePoint(browser.rows[2])
	failedPoint, _ := runtime.CandidateResumePoint(browser.rows[1])
	if strings.Contains(raw, "不可落盘") || strings.Contains(raw, "example.invalid") || saved.Point != latestPoint || len(saved.Pending) != 1 || saved.Pending[0] != failedPoint || saved.Version != 2 {
		t.Fatalf("断点含正文或跳过失败者：%s", raw)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cursor := &model.CandidateListCursor{}
	runtime = &resumeFlowRuntime{}
	restored, err := (&Flow{Store: reopened}).prepareCandidateResume(ctx, browser, prepared, resumeProviderRuntime{runtime, runtime}, cursor)
	if err != nil || restored.record.Point != latestPoint || cursor.StartIndex != 2 {
		t.Fatalf("重启后恢复错误：%+v %v %+v", restored, err, cursor)
	}
	rows, retrying, err := restored.retryCandidates(ctx, browser, prepared.Platform, runtime, model.CandidateListOptions{Cursor: cursor})
	if err != nil || !retrying || len(rows) != 1 || rows[0].Index != 1 || restored.completedAnchor(rows[0]) {
		t.Fatalf("重启没有先补中间失败者：%v %v %+v", retrying, err, rows)
	}
	if err := restored.complete(ctx, rows[0], nil); err != nil {
		t.Fatal(err)
	}
	_, retrying, err = restored.retryCandidates(ctx, browser, prepared.Platform, runtime, model.CandidateListOptions{Cursor: cursor})
	if err != nil || retrying || restored.record.Point != latestPoint || len(restored.record.Pending) != 0 || cursor.StartIndex != 2 || latestPoint == firstPoint {
		t.Fatalf("补处理后没有回到最新进度：%v %v %+v", retrying, err, cursor)
	}
}

// TestCandidateResumeScopeIsolation 验证换账号、Profile、岗位、页面或筛选要求均不复用旧断点，运行计数和TaskID变化不影响恢复。
func TestCandidateResumeScopeIsolation(t *testing.T) {
	p, browser := resumeFlowFixture()
	key, scope, err := candidateResumeScope(p, browser.url)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"用户", "岗位", "Profile", "平台账号", "平台", "名称", "要求", "页面", "计数"} {
		t.Run(kind, func(t *testing.T) {
			changed := p
			url := browser.url
			switch kind {
			case "用户":
				changed.Session.UserID = "other"
			case "岗位":
				changed.Position.ID = "other"
			case "Profile":
				changed.ProfilePath = "other"
			case "平台账号":
				changed.Request.PlatformAccountID = "other"
			case "平台":
				changed.Platform.ID = "other"
			case "名称":
				changed.Position.Name = "other"
			case "要求":
				changed.Position.Description = "other"
			case "页面":
				url += "&page=2"
			case "计数":
				changed.Position.ScannedCount = 999
				changed.Request.TaskID = "new-run"
			}
			k, s, err := candidateResumeScope(changed, url)
			if err != nil {
				t.Fatal(err)
			}
			if (k == key && s == scope) != (kind == "计数") {
				t.Fatalf("作用域不正确：%s", kind)
			}
		})
	}
}

// TestCandidateResumeRetryBoundaries 验证首人、末人或全部失败都能补处理，再失败不在本次运行循环，最新位置不倒退。
func TestCandidateResumeRetryBoundaries(t *testing.T) {
	for _, kind := range []string{"首人失败", "末人失败", "全部失败", "补处理仍失败"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(filepath.Join(t.TempDir(), "resume.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			prepared, browser := resumeFlowFixture()
			runtime := &resumeFlowRuntime{}
			session, err := (&Flow{Store: store}).prepareCandidateResume(ctx, browser, prepared, runtime, &model.CandidateListCursor{})
			if err != nil {
				t.Fatal(err)
			}
			runtime.RememberCandidateResume(prepared.Platform, browser.rows)
			latest, _ := runtime.CandidateResumePoint(browser.rows[2])
			wantRetries := 0
			for _, row := range browser.rows {
				var failure error
				if kind == "全部失败" || (kind == "末人失败" && row.Index == 2) || ((kind == "首人失败" || kind == "补处理仍失败") && row.Index == 0) {
					failure = errors.New("合成失败")
					wantRetries++
				}
				if err := session.complete(ctx, row, failure); err != nil {
					t.Fatal(err)
				}
			}
			cursor := &model.CandidateListCursor{}
			fresh := &resumeFlowRuntime{}
			restored, err := (&Flow{Store: store}).prepareCandidateResume(ctx, browser, prepared, fresh, cursor)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < wantRetries; i++ {
				rows, retrying, err := restored.retryCandidates(ctx, browser, prepared.Platform, fresh, model.CandidateListOptions{Cursor: cursor})
				if err != nil || !retrying || len(rows) != 1 {
					t.Fatalf("没有逐个补处理：%v %v %+v", retrying, err, rows)
				}
				var failure error
				if kind == "补处理仍失败" {
					failure = errors.New("再次失败")
				}
				if err := restored.complete(ctx, rows[0], failure); err != nil {
					t.Fatal(err)
				}
			}
			_, retrying, err := restored.retryCandidates(ctx, browser, prepared.Platform, fresh, model.CandidateListOptions{Cursor: cursor})
			wantPending := 0
			if kind == "补处理仍失败" {
				wantPending = 1
			}
			if err != nil || retrying || restored.record.Point != latest || cursor.StartIndex != 2 || len(restored.record.Pending) != wantPending {
				t.Fatalf("补处理倒退、遗漏或循环：%v %v %+v %+v", retrying, err, cursor, restored.record)
			}
		})
	}
}

// resumeReadErrorBrowser 为恢复读取注入确定错误，验证不得冒充断点丢失而清空记录。
type resumeReadErrorBrowser struct{ model.Browser }

// FindAll 模拟浏览器断线，其他接口沿用嵌入的真实测试接口。
func (resumeReadErrorBrowser) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	return nil, errors.New("合成浏览器断线")
}

// TestCandidateResumeMigrationAndFailedWrites 验证旧记录可迁移、读取错误和取消写入保留已保存断点、名单变化才从头。
func TestCandidateResumeMigrationAndFailedWrites(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(filepath.Join(t.TempDir(), "resume.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prepared, browser := resumeFlowFixture()
	key, scope, _ := candidateResumeScope(prepared, browser.url)
	runtime := &resumeFlowRuntime{}
	if _, err := runtime.RestoreCandidateList(ctx, browser, prepared.Platform, model.CandidateResumePoint{}, &model.CandidateListCursor{}); err != nil {
		t.Fatal(err)
	}
	runtime.RememberCandidateResume(prepared.Platform, browser.rows)
	point, _ := runtime.CandidateResumePoint(browser.rows[1])
	old, _ := json.Marshal(candidateResumeRecord{Version: 1, ScopeHash: scope, Point: point})
	if err := store.SetSetting(ctx, key, string(old)); err != nil {
		t.Fatal(err)
	}
	cursor := &model.CandidateListCursor{}
	fresh := &resumeFlowRuntime{}
	restored, err := (&Flow{Store: store}).prepareCandidateResume(ctx, browser, prepared, fresh, cursor)
	if err != nil || restored.record.Version != 2 || restored.record.Point != point || cursor.StartIndex != 1 {
		t.Fatalf("旧记录无法迁移：%v %+v", err, restored)
	}
	restored.retries = []model.CandidateResumePoint{point}
	_, _, err = restored.retryCandidates(ctx, resumeReadErrorBrowser{Browser: browser}, prepared.Platform, fresh, model.CandidateListOptions{Cursor: cursor})
	if err == nil || restored.record.Point != point {
		t.Fatal("读取错误被当成找不到")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := restored.save(canceled, candidateResumeRecord{Version: 2, ScopeHash: scope}); err == nil || restored.record.Point != point {
		t.Fatal("取消写入仍修改了内存断点")
	}
	raw, _ := store.GetSetting(ctx, key)
	if raw != string(old) {
		t.Fatal("写入取消或读取失败改变了已存进度")
	}
	browser.rows = browser.rows[:1]
	restored, err = (&Flow{Store: store}).prepareCandidateResume(ctx, browser, prepared, &resumeFlowRuntime{}, cursor)
	if err != nil || restored.record.Point.CandidateID != "" || cursor.StartIndex != 0 {
		t.Fatalf("锚点消失没有从头：%v %+v", err, cursor)
	}
}
