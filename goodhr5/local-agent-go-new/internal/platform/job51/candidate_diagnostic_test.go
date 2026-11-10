// Package job51 文件作用：验证候选人读取和滚动失败时仍保留原错误，并只向岗位日志提供已观察到的安全证据。
package job51

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type listDiagnosticBrowser struct {
	positionBrowserStub
	reads, scrolls       int
	failRead, failScroll int
	cause                error
}

// FindAll 返回模拟的列表卡片、读空窗口，或在指定的一次卡片读取时中断。
func (s *listDiagnosticBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if len(req.Fields) == 0 {
		return s.positionBrowserStub.FindAll(ctx, req)
	}
	s.reads++
	if s.reads == s.failRead {
		return nil, s.cause
	}
	if req.StartIndex > 0 {
		return nil, nil
	}
	return []contract.FindAllItem{{Index: 0, TotalMatches: 20 + s.scrolls, Text: "不得写进诊断的测试简历", Fields: map[string]string{"name": "不得写进诊断的姓名", "platform_candidate_id": "RESUME_CARD_9876543210"}}}, nil
}

// Scroll 模拟滚轮有位移，或在指定轮次模拟浏览器失联。
func (s *listDiagnosticBrowser) Scroll(_ context.Context, _ contract.ScrollRequest) (contract.ScrollResult, error) {
	s.scrolls++
	if s.scrolls == s.failScroll {
		return contract.ScrollResult{}, s.cause
	}
	visible, moved := false, true
	return contract.ScrollResult{ProgressTargetVisible: &visible, ProgressTargetMoved: &moved}, nil
}

// listFailureFacts 检查调用方通过错误链取得角色明确的诊断，同时检查隐私和时间线边界。
func listFailureFacts(t *testing.T, err, cause error) model.CandidateListDiagnostic {
	t.Helper()
	var diagnostic interface {
		CandidateListDiagnostic() model.CandidateListDiagnostic
	}
	if !errors.Is(err, cause) || !errors.As(err, &diagnostic) {
		t.Fatalf("错误链漏掉原始错误或列表诊断：%v", err)
	}
	facts := diagnostic.CandidateListDiagnostic()
	if n := len(facts.Timeline); n == 0 || n > 8 || !strings.Contains(facts.Timeline[n-1], "失败") {
		t.Fatalf("缺少最近失败时间线：%v", facts.Timeline)
	}
	b, err := json.Marshal(facts)
	if err != nil || strings.Contains(string(b), "不得写进诊断") || strings.Contains(string(b), "987654321") {
		t.Fatal("诊断含有原始候选资料或序列化失败")
	}
	return facts
}

// TestFindCandidatesReportsActualRead 验证完整卡片读取失败能关联 Worker 并区分已检查为假、未知和零游标。
func TestFindCandidatesReportsActualRead(t *testing.T) {
	cause := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}}
	browser := &listDiagnosticBrowser{failRead: 1, cause: cause}
	_, err := NewRuntime().FindCandidates(context.Background(), browser, loadPositionConfig(t), model.CandidateListOptions{ReadConcurrency: 7})
	facts := listFailureFacts(t, err, cause)
	if facts.Stage != "读取候选人卡片" || facts.LastSuccessStage != "检查列表加载提示" || facts.SelectorKey != "candidate.item" || facts.Operation != "find_candidates" || facts.Platform != "51job" ||
		facts.CardsRead != nil || facts.LoadingVisible == nil || *facts.LoadingVisible || facts.EmptyVisible != nil ||
		facts.BatchStartIndex == nil || *facts.BatchStartIndex != 0 || facts.ReadConcurrency == nil || *facts.ReadConcurrency != 7 {
		t.Fatalf("读取现场不符：%+v", facts)
	}
}

// TestAdvanceCandidatesReportsScrollAndParentCancel 验证多轮滚动后故障与上层提前结束的两种现场可分辨。
func TestAdvanceCandidatesReportsScrollAndParentCancel(t *testing.T) {
	cause := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}}
	cfg := loadPositionConfig(t)
	before := []model.Candidate{{Index: 0, Fields: map[string]string{"platform_candidate_id": "987654321"}}}
	browser := &listDiagnosticBrowser{failScroll: 3, cause: cause}
	_, err := NewRuntime().AdvanceCandidateList(context.Background(), browser, cfg, before, model.CandidateListOptions{})
	facts := listFailureFacts(t, err, cause)
	if facts.Stage != "向列表末尾执行真实滚轮" || facts.ScrollRound == nil || *facts.ScrollRound != 3 || facts.TailMoved != nil ||
		facts.WindowPhase != "before_scroll" || facts.BeforeTotalMatches == nil || *facts.BeforeTotalMatches != 22 || facts.AfterTotalMatches != nil || facts.NextIndex == nil || *facts.NextIndex != 1 || facts.NewCards == nil || *facts.NewCards != 0 || facts.ScopeBudgetMS == nil || *facts.ScopeBudgetMS != 120000 {
		t.Fatalf("滚动失败现场不符：%+v", facts)
	}
	// 第四次读取为滚动后的下一窗口，首卡已返回当前页总量；同轮滚前、滚后不能混淆。
	browser = &listDiagnosticBrowser{failRead: 4, cause: cause}
	_, err = NewRuntime().AdvanceCandidateList(context.Background(), browser, cfg, before, model.CandidateListOptions{})
	facts = listFailureFacts(t, err, cause)
	if facts.WindowPhase != "after_scroll" || facts.Stage != "读取滚动后的下一批卡片" ||
		facts.BeforeTotalMatches == nil || *facts.BeforeTotalMatches != 20 || facts.AfterTotalMatches == nil || *facts.AfterTotalMatches != 21 ||
		facts.NewCards != nil || facts.TailMoved == nil || !*facts.TailMoved {
		t.Fatalf("本轮滚动前后采样不符：%+v", facts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewRuntime().AdvanceCandidateList(ctx, browser, cfg, before, model.CandidateListOptions{})
	facts = listFailureFacts(t, err, context.Canceled)
	if facts.Stage != "检查本轮滚动的时限" || facts.ParentStatus != context.Canceled.Error() || facts.TailMoved != nil {
		t.Fatalf("上层取消来源或最后阶段丢失：%+v", facts)
	}
}

// TestFindCandidatesUsesOnlyObservedPageTotal 验证缺省、相互矛盾或低于行号的页面总量均不进入日志。
func TestFindCandidatesUsesOnlyObservedPageTotal(t *testing.T) {
	cfg := loadPositionConfig(t)
	for _, pair := range [][2]int{{0, 0}, {1, 1}, {21, 22}, {35, 35}} {
		items := []contract.FindAllItem{
			{Index: 0, TotalMatches: pair[0], Fields: map[string]string{"platform_candidate_id": "无效的编号"}},
			{Index: 1, TotalMatches: pair[1]},
		}
		_, err := NewRuntime().FindCandidates(context.Background(), &positionBrowserStub{cards: items}, cfg, model.CandidateListOptions{})
		var diagnostic interface {
			CandidateListDiagnostic() model.CandidateListDiagnostic
		}
		if !errors.As(err, &diagnostic) {
			t.Fatalf("身份校验未触发列表诊断：%v", err)
		}
		facts := diagnostic.CandidateListDiagnostic()
		if pair[0] == 35 {
			if facts.TotalMatches == nil || *facts.TotalMatches != 35 {
				t.Fatalf("未采用实际页面匹配数：%+v", facts)
			}
		} else if facts.TotalMatches != nil {
			t.Fatalf("把未知页面匹配数当成实际结果：%+v", facts)
		}
		if facts.CardsRead == nil || *facts.CardsRead != len(items) {
			t.Fatalf("本次返回卡片数和页面匹配数必须分开记录：%+v", facts)
		}
	}
}
