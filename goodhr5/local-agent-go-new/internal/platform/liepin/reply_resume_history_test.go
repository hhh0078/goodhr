// Package liepin 本文件验证简历来源按卡片出现时机读取，避免为每页旧消息重复读取正文或无谓返回聊天底部。
package liepin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// resumeHistoryBrowser 只模拟已加载DOM与标准卡片探测，记录完整字段读取次数。
type resumeHistoryBrowser struct {
	liepinReplyBrowserStub
	fullReads int
	probes    int
}

// TestLiepinRecentResumePinsOnlyProvenSource 验证仅取尾部八条即可确认新附件；同分钟截断、删除时间、发送人和同文新卡都不能误放行。
func TestLiepinRecentResumePinsOnlyProvenSource(t *testing.T) {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	row := cfg.Selectors["message.item"]
	row.Description = "测试聊天消息"
	cfg.Selectors["message.item"] = row
	cutoff := parseLiepinMessageTime("10:01", time.Now()).Add(20 * time.Second)
	for _, tc := range []struct {
		label                                            string
		clock                                            string
		platformID, firstSource, wrongCID, self, clipped bool
		wantFast, wantErr                                bool
	}{
		{label: "同分钟两张选最后一张", clock: "10:02", wantFast: true},
		{label: "平台编号不需覆盖分钟", clock: "10:02", platformID: true, clipped: true, wantFast: true},
		{label: "第一张不能冒充最新", clock: "10:02", firstSource: true, wantErr: true},
		{label: "同分钟开始不完整回退", clock: "10:02", clipped: true},
		{label: "删除同一分钟即使有ID也拒绝", clock: "10:01", platformID: true, wantErr: true},
		{label: "未知时间不能用于删后建档", clock: "", platformID: true, wantErr: true},
		{label: "卡片其他候选人", clock: "10:02", wrongCID: true, wantErr: true},
		{label: "最后为HR发的卡片", clock: "10:02", self: true, wantErr: true},
	} {
		t.Run(tc.label, func(t *testing.T) {
			items := make([]contract.FindAllItem, 16)
			for i := range items {
				clock := "10:00"
				if tc.clipped && i >= 8 {
					clock = tc.clock
				}
				items[i] = recontactTestMessage(clock, "普通消息", false)
			}
			for _, i := range []int{13, 14} {
				items[i] = recontactTestMessage(tc.clock, "附件简历", true)
				items[i].Fields["candidate_meta"] = "cid=correct"
			}
			if tc.platformID {
				items[14].Fields["message_meta"] = `{"message_id":"resume-identity"}`
			}
			if tc.wrongCID {
				items[14].Fields["candidate_meta"] = "cid=other"
			}
			if tc.self {
				items[14].Fields["body_class"] = "im-ui-message-item-send"
			}
			msgs, err := liepinMessages(items)
			if err != nil {
				t.Fatal(err)
			}
			source := msgs[14].Key
			if tc.firstSource {
				source = msgs[13].Key
			}
			b := &resumeHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
			pinned, fast, err := liepinRecentResumeSource(context.Background(), b, cfg, model.AutoReplyConversationSnapshot{HistoryAfter: &cutoff, PlatformCandidateID: "correct", ResumeSourceMessageID: source})
			if fast != tc.wantFast || (err != nil) != tc.wantErr || b.fullReads != 1 || len(b.historyScrolls) != 0 || len(b.clicks) != 0 {
				t.Fatalf("核验结果错误：fast=%t err=%v reads=%d", fast, err, b.fullReads)
			}
			parents := pinned.Selectors["message.resume_attachment_entry"].Parents
			lastCard := parents[len(parents)-1]
			if fast && (!strings.Contains(lastCard.Selectors[0].Value, ":not(:has(~") || !strings.Contains(lastCard.Selectors[0].Value, ":nth-match("+row.Target.Selectors[0].Value+", 15)") || len(lastCard.Texts) != 1 || lastCard.Texts[0] != tc.clock) {
				t.Fatal("点击未绑定最后一张附件、原始消息行与发送时间", lastCard)
			}
			if strings.Contains(cfg.Selectors["message.resume_attachment_entry"].Parents[1].Selectors[0].Value, ":nth-match(") {
				t.Fatal("本次测试更改了共享平台配置")
			}
		})
	}
}

// FindAll 根据当前已加载批次返回附件入口存在性，不在测试中执行真实浏览器操作。
func (b *resumeHistoryBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description == "测试附件入口" {
		b.probes++
		if !request.ProbeOnly || !request.ExpectedMissing || request.Selector.State != "attached" {
			return nil, fmt.Errorf("简历探测没有使用只读attached契约")
		}
		for _, item := range b.historyBatches[min(b.historyBatch, len(b.historyBatches)-1)] {
			if item.Fields["resume_card"] != "" {
				return []contract.FindAllItem{{Index: 0}}, nil
			}
		}
		return nil, nil
	}
	if !request.ProbeOnly && request.MaxItems > 1 {
		b.fullReads++
	}
	return b.liepinReplyBrowserStub.FindAll(ctx, request)
}

// TestPrepareLiepinResumeReadsOnlyAfterCardAppears 验证20→40→60→80只完整读一次，且不再回到底部后重复上翻。
func TestPrepareLiepinResumeReadsOnlyAfterCardAppears(t *testing.T) {
	items := make([]contract.FindAllItem, 80)
	for i := range items {
		items[i] = identityTestMessage(fmt.Sprintf("合成消息%d", i), fmt.Sprintf("8月6日 %02d:%02d", 10+i/60, i%60))
	}
	items[10].Fields["resume_card"] = "在线简历 附件简历"
	parsed, err := liepinMessages(items)
	if err != nil {
		t.Fatal(err)
	}
	browser := &resumeHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items[60:], items[40:], items[20:], items}}}
	cfg := historyAuditConfig()
	cfg.Selectors["message.resume_attachment_entry"] = testLiepinSelector("测试附件入口")
	if err = prepareLiepinResumeSource(context.Background(), browser, cfg, parsed[10].Key); err != nil {
		t.Fatal(err)
	}
	if browser.fullReads != 1 || browser.probes != 4 || len(browser.historyScrolls) != 3 {
		t.Fatalf("重复读正文或没有逐页探测: full=%d probes=%d scrolls=%d", browser.fullReads, browser.probes, len(browser.historyScrolls))
	}
	for _, scroll := range browser.historyScrolls {
		if scroll.Distance != -1600 || scroll.ProgressTarget == nil || scroll.MaxAttempts != 1 {
			t.Fatalf("来源准备改变了真实滚轮约束或先回底部: %+v", scroll)
		}
	}
}

// TestPrepareLiepinResumeRejectsWrongSourceAndCanceledTask 验证错源与取消都不会继续滚动或进入附件点击。
func TestPrepareLiepinResumeRejectsWrongSourceAndCanceledTask(t *testing.T) {
	items := []contract.FindAllItem{identityTestMessage("较早锚点", "10:00"), identityTestMessage("合成简历", "10:01")}
	items[1].Fields["resume_card"] = "在线简历 附件简历"
	cfg := historyAuditConfig()
	cfg.Selectors["message.resume_attachment_entry"] = testLiepinSelector("测试附件入口")
	for _, cancelled := range []bool{false, true} {
		browser := &resumeHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		err := prepareLiepinResumeSource(ctx, browser, cfg, "wrong-source")
		cancel()
		if err == nil || len(browser.clicks) != 0 || len(browser.historyScrolls) != 0 {
			t.Fatal("不可靠来源仍继续操作", err)
		}
		if cancelled && (browser.fullReads != 0 || browser.probes != 0) {
			t.Fatal("取消后仍读取页面")
		}
	}
}

// TestPrepareLiepinResumeStopsAtProvenTopWithoutCard 验证没有附件时只读取首尾并保留三次真实顶部判断。
func TestPrepareLiepinResumeStopsAtProvenTopWithoutCard(t *testing.T) {
	browser := &resumeHistoryBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{{identityTestMessage("合成消息", "10:00")}}}}
	cfg := historyAuditConfig()
	cfg.Selectors["message.resume_attachment_entry"] = testLiepinSelector("测试附件入口")
	if err := prepareLiepinResumeSource(context.Background(), browser, cfg, "missing-source"); err == nil {
		t.Fatal("未找到附件却成功")
	}
	if browser.fullReads != 0 || len(browser.historyScrolls) < 3 {
		t.Fatalf("缺卡片重复读全文或顶部未确认: full=%d scrolls=%d", browser.fullReads, len(browser.historyScrolls))
	}
}
