// Package greeting 文件作用：验证本地断点跨进程重开后仍有效，未完成者不被后续成功越过，账号和岗位配置互相隔离。
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
		rows = append(rows, contract.FindAllItem{Index: i, Fields: b.rows[i].Fields})
	}
	return rows, nil
}

// resumeFlowFixture 构造最小账号岗位与三人名单，完整隐私原文不得进入断点JSON。
func resumeFlowFixture() (shared.PreparedTask, *resumeFlowBrowser) {
	prepared := shared.PreparedTask{Session: cloud.UserSession{UserID: "user-a"}, ProfilePath: "/fixture/profile-a", Position: cloud.PositionSnapshot{ID: "position-a", Name: "合成岗位"}, Platform: model.Config{ID: "test", Selectors: map[string]contract.SelectorSpec{"candidate.item": {Description: "合成人选", Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".fixture"}}}}}, CandidateFields: map[string]contract.SelectorSpec{"name": {Description: "姓名"}, "basic_info": {Description: "基础信息"}}}}
	rows := []model.Candidate{{Index: 0, Fingerprint: "test_不可落盘的甲_30岁", Fields: map[string]string{"name": "不可落盘的甲", "basic_info": "30岁"}}, {Index: 1, Fingerprint: "test_不可落盘的乙_31岁", Fields: map[string]string{"name": "不可落盘的乙", "basic_info": "31岁"}}, {Index: 2, Fingerprint: "test_不可落盘的丙_32岁", Fields: map[string]string{"name": "不可落盘的丙", "basic_info": "32岁"}}}
	return prepared, &resumeFlowBrowser{rows: rows, url: "https://example.invalid/search?q=不可落盘的查询"}
}

// TestCandidateResumePersistsAndNeverSkipsFailure 验证只读名单不推进；中途失败后后续成功不覆盖最后连续完成者；重开数据库仍能定位。
func TestCandidateResumePersistsAndNeverSkipsFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "resume.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	prepared, browser := resumeFlowFixture()
	runtime := &resumeFlowRuntime{}
	flow := &Flow{Store: store}
	resume, err := flow.prepareCandidateResume(ctx, browser, prepared, runtime, &model.CandidateListCursor{})
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

	_ = resume.complete(ctx, browser.rows[1], errors.New("未处理完成"))
	_ = resume.complete(ctx, browser.rows[2], nil)
	raw, _ = store.GetSetting(ctx, resume.key)
	var saved candidateResumeRecord
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	firstPoint, _ := runtime.CandidateResumePoint(browser.rows[0])
	if strings.Contains(raw, "不可落盘") || strings.Contains(raw, "example.invalid") || saved.Point != firstPoint {
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
	restored, err := (&Flow{Store: reopened}).prepareCandidateResume(ctx, browser, prepared, &resumeFlowRuntime{}, cursor)
	if err != nil || restored.record.Point != firstPoint || cursor.StartIndex != 0 {
		t.Fatalf("重启后恢复错误：%+v %v %+v", restored, err, cursor)
	}
	// 正常完成第二人后，下一任务只需跳过这一锚点，不能把尚未完成的第三人当成已完成。
	runtime = &resumeFlowRuntime{}
	restored, _ = (&Flow{Store: reopened}).prepareCandidateResume(ctx, browser, prepared, runtime, cursor)
	runtime.RememberCandidateResume(prepared.Platform, browser.rows)
	if err := restored.complete(ctx, browser.rows[1], nil); err != nil {
		t.Fatal(err)
	}
	again, err := (&Flow{Store: reopened}).prepareCandidateResume(ctx, browser, prepared, &resumeFlowRuntime{}, cursor)
	if err != nil || again.record.Point == firstPoint || cursor.StartIndex != 1 {
		t.Fatalf("完成后未推进：%+v %v %+v", again, err, cursor)
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
