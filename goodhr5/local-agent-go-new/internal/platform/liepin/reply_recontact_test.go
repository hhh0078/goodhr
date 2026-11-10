// Package liepin 本文件验证真实列表分页中的删除时间边界、新附件来源和同分钟同文去重。
package liepin

import (
	"context"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"testing"
	"time"
)

// TestRecontactWindowStopsAtDeleteMinute 验证已加载边界时零次上翻，既不重读旧附件，也不丢两条同分钟、同文的新消息。
func TestRecontactWindowStopsAtDeleteMinute(t *testing.T) {
	items := make([]contract.FindAllItem, 0, 100)
	for i := 0; i < 97; i++ {
		items = append(items, recontactTestMessage("昨天 12:00", "旧聊天", true))
	}
	items = append(items, recontactTestMessage("12:00", "同分钟旧附件", true), recontactTestMessage("12:01", "收到", false), recontactTestMessage("12:01", "收到", false))
	browser := &countingHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
	after := parseLiepinMessageTime("12:00", time.Now()).Add(25 * time.Second)
	history, err := readLiepinHistoryWindow(context.Background(), browser, historyAuditConfig(), nil, 5000, &after)
	if err != nil || !history.HistoryComplete || history.ResumeCardAvailable || len(history.Messages) != 2 || history.Messages[0].Key == history.Messages[1].Key {
		t.Fatalf("首轮边界错误 %+v %v", history, err)
	}
	for _, scroll := range browser.historyScrolls {
		if scroll.Distance < 0 {
			t.Fatal("边界已可见，仍然上翻")
		}
	}
	// 已知上一条后读取新卡片，返回增量标志，防止被本机当成无共同锚点的旧历史。
	known := []string{history.Messages[1].Key}
	items = append(items, recontactTestMessage("12:02", "新简历", true))
	browser.historyBatches = [][]contract.FindAllItem{items}
	history, err = readLiepinHistoryWindow(context.Background(), browser, historyAuditConfig(), known, 5000, &after)
	if err != nil || history.HistoryComplete || !history.ResumeCardAvailable || history.Messages[len(history.Messages)-1].TextContent != "新简历" {
		t.Fatalf("增量边界错误 %+v %v", history, err)
	}
}

// recontactTestMessage 生成只含方向、时间和可选附件卡片的合成招聘消息。
func recontactTestMessage(clock, text string, resume bool) contract.FindAllItem {
	f := map[string]string{"body_class": "im-ui-message-item-receive", "message_text": text, "message_time": clock}
	if resume {
		f["resume_card"] = "附件简历"
	}
	return contract.FindAllItem{Text: text, Fields: f}
}

// TestRecontactWindowLoadsMissingPage 验证新来信跨过首屏时只上翻到删除分钟，未知时间的旧卡片不能被带入。
func TestRecontactWindowLoadsMissingPage(t *testing.T) {
	unseen := recontactTestMessage("12:01", "新来信", false)
	more := []contract.FindAllItem{recontactTestMessage("11:59", "旧消息", false), recontactTestMessage("", "无时间附件", true), unseen}
	browser := &liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{unseen}, more}}
	after := parseLiepinMessageTime("12:00", time.Now()).Add(22 * time.Second)
	result, err := readLiepinHistoryWindow(context.Background(), browser, historyAuditConfig(), nil, 5000, &after)
	upward := 0
	for _, scroll := range browser.historyScrolls {
		if scroll.Distance < 0 {
			upward++
		}
	}
	if err != nil || !result.HistoryComplete || len(result.Messages) != 1 || result.ResumeCardAvailable || upward != 1 {
		t.Fatalf("新会话分页错误 %+v 上翻%d %v", result, upward, err)
	}
}
