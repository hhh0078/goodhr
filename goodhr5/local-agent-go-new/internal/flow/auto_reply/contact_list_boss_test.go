// Package auto_reply 本文件使用真实BOSS平台解析器和服务器列表水位核验51行含11个置顶联系人的接口合同及失败审计阶段。
package auto_reply

import (
	"context"
	"fmt"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/platform/boss"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// bossContractListBrowser 只返回合成可见联系人，复用真实BOSS扫描，不连接Worker或招聘网站。
type bossContractListBrowser struct {
	autoReplyBrowserStub
	rows []contract.FindAllItem
}

// FindAll 按正式Worker合同返回每行位置、总数和明确请求的字段，抽屉探测不产生任何点击。
func (b *bossContractListBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.ProbeOnly {
		return []contract.FindAllItem{{}}, nil
	}
	var rows []contract.FindAllItem
	for i := req.StartIndex; i < min(len(b.rows), req.StartIndex+req.MaxItems); i++ {
		row := contract.FindAllItem{Index: i, TotalMatches: len(b.rows), Text: b.rows[i].Text, Fields: map[string]string{}}
		for key := range req.Fields {
			row.Fields[key] = b.rows[i].Fields[key]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// bossContractFixture 创建51行11个无key置顶人的快照，全部字段均来自合成数据。
func bossContractFixture() (*bossContractListBrowser, model.Config) {
	selector := contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "synthetic"}}}}
	cfg := model.Config{ID: "boss", MessagesURL: "https://example.test/chat", MaxItems: 100, Selectors: map[string]contract.SelectorSpec{"message.drawer": selector, "message.contact_item": selector}, ConversationFields: map[string]contract.SelectorSpec{}}
	cfg.Behavior.AutoReplyAllContactInbox = true
	for _, key := range []string{"thread_id", "name", "avatar_url", "last_message"} {
		cfg.ConversationFields[key] = selector
	}
	b := &bossContractListBrowser{autoReplyBrowserStub: autoReplyBrowserStub{pages: []contract.PageInfo{{URL: cfg.MessagesURL, Current: true}}}}
	for i := 0; i < 51; i++ {
		fields := map[string]string{"name": fmt.Sprintf("合成人选%d", i), "avatar_url": fmt.Sprintf("https://image.test/%d.png", i), "last_message": "原消息"}
		if i >= 11 {
			fields["thread_id"] = fmt.Sprintf("%d-0", 1000+i)
		}
		b.rows = append(b.rows, contract.FindAllItem{Text: fields["name"] + " 原消息 昨天", Fields: fields})
	}
	return b, cfg
}

// TestBossAllContactsScanToFlowContract 验证真实BOSS扫描产生完整持久摘要，换位不误判、新来信在Flow中能被准确发现。
func TestBossAllContactsScanToFlowContract(t *testing.T) {
	f, prepared := newReliabilityFlow(t, "")
	b, cfg := bossContractFixture()
	f.Browser, prepared.Platform = b, cfg
	runtime := boss.NewRuntime()
	for _, phase := range []string{"baseline", "reorder", "message"} {
		if phase == "reorder" {
			b.rows[0], b.rows[50] = b.rows[50], b.rows[0]
		}
		if phase == "message" {
			b.rows[20].Text = "合成人选20 新来信 今天"
			b.rows[20].Fields["last_message"] = "新来信"
		}
		rows, err := runtime.ScanUnreadConversations(context.Background(), b, cfg)
		if err != nil || len(rows) != 51 {
			t.Fatalf("BOSS扫描不完整：%d %v", len(rows), err)
		}
		for _, row := range rows {
			if len(row.Fields["contact_list_signature"]) != 64 {
				t.Fatalf("BOSS扫描违反全部列表合同：%s", row.Key)
			}
		}
		pending, _, err := f.reviewAllContacts(context.Background(), prepared, rows)
		if err != nil {
			t.Fatalf("成功扫描后在Flow列表核对阶段失败：%v", err)
		}
		if phase != "message" && len(pending) != 0 {
			t.Fatalf("基线或换位被当成新消息：%+v", pending)
		}
		if phase == "message" && (len(pending) != 1 || pending[0].PlatformThreadID != "1020-0") {
			t.Fatalf("新来信未准确进入Flow待办：%+v", pending)
		}
	}
}

// contactReviewTestLogger 记录失败步骤及可信诊断，不通过错误文案反推阶段。
type contactReviewTestLogger struct {
	step string
	err  error
}

// Step 保存本次流程实际报告的步骤与错误。
func (l *contactReviewTestLogger) Step(_, _, step, _ string, _ time.Time, err error) {
	l.step, l.err = step, err
}

// TestAllContactBusinessFailureLogsItsOwnStage 验证扫描成功后的列表合同失败会准确记为核对列表，并有不含招聘原文的安全原因。
func TestAllContactBusinessFailureLogsItsOwnStage(t *testing.T) {
	f, prepared := newReliabilityFlow(t, "")
	b, cfg := bossContractFixture()
	f.Browser, prepared.Platform = b, cfg
	logger := &contactReviewTestLogger{}
	f.Logger = logger
	// 扫描成功返回了行，但缺少必须的变化摘要，复现旧BOSS与Flow的接口断点。
	scanner := captureUnreadScanner{items: []model.Conversation{{Key: "synthetic-thread", PlatformThreadID: "synthetic-thread", Name: "合成人选"}}}
	_, _, err := f.processCheckpoint(context.Background(), prepared, scanner, &autoReplyRuntimeStub{}, nil, 1, &shared.Stats{}, &shared.ConsecutiveErrorPolicy{}, nil)
	if err == nil || logger.step != "review_contact_list" || len(failurediag.Details(logger.err)) != 1 {
		t.Fatalf("扫描业务失败错报页面初始化：step=%s error=%v", logger.step, err)
	}
}
