// Package boss 本文件验证原生附件邀约纯读、显式单次同意、新文件来源和删除边界；不操作真实招聘账号。
package boss

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossResumeOfferFixture 创建实测结构的合成原生邀约，图标不同于真正PDF卡的resume-icon。
func bossResumeOfferFixture() contract.FindAllItem {
	text := "对方想发送附件简历给您，您是否同意\n拒绝\n同意"
	row := bossTestMessage("05:00", "item-friend", text)
	row.Fields["common_card"], row.Fields["message_meta"] = text, "message-dialog-icon message-dialog-icon-resume"
	return row
}

// bossResumePDFMessage 创建真实附件类型字段，不以邀约标题或普通文字伪造已收到文件。
func bossResumePDFMessage(clock, name string) contract.FindAllItem {
	row := bossTestMessage(clock, "item-friend", name)
	row.Fields["resume_card"] = name
	return row
}

// bossResumeOfferBrowser 只模拟已核实同意后的PDF替换、追加、未知和身份切换，记录实际同意次数。
type bossResumeOfferBrowser struct {
	*bossReplyTestBrowser
	after        []contract.FindAllItem
	accepts      int
	clickErr     error
	changeOnRead func()
	clicked      contract.SelectorSpec
}

// FindAll 在首次正文返回后模拟外部切换，供点击前核身守卫验证。
func (b *bossResumeOfferBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	items, err := b.bossReplyTestBrowser.FindAll(ctx, request)
	if request.Selector.Description == "messages" && len(request.Fields) > 2 && b.changeOnRead != nil {
		change := b.changeOnRead
		b.changeOnRead = nil
		change()
	}
	return items, err
}

// Click 只接受显式附件同意选择器，同意最多一次后才展示合成的新PDF卡。
func (b *bossResumeOfferBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if request.Selector.Description != "accept-offer" {
		return contract.ClickResult{}, errors.New("不允许点普通索要或其他按钮")
	}
	b.accepts++
	b.clicked = request.Selector
	if b.clickErr != nil {
		return contract.ClickResult{}, b.clickErr
	}
	if b.after != nil {
		b.messages = b.after
	}
	return contract.ClickResult{Clicked: true}, nil
}

// bossResumeOfferEnvironment 构造有旧PDF、待接收邀约和较晚HR锚点的当前已核身会话。
func bossResumeOfferEnvironment(t *testing.T) (*bossResumeOfferBrowser, model.Config, model.AutoReplyConversationSnapshot) {
	t.Helper()
	b, cfg, snapshot := bossIdentityFixture()
	cfg.ConversationFields["message_meta"] = bossReplySelector("message_meta")
	root := contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".chat-root"}}}
	cfg.Selectors["message.item"] = contract.SelectorSpec{Parents: []contract.SelectorGroup{root, {Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".message-list"}}}}, Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".message-item"}}}, Description: "messages"}
	for key, css := range map[string]string{"message.current_candidate_id": "span.resume", "message.current_phone_candidate_id": "span.phone", "message.current_wechat_candidate_id": "span.wechat"} {
		spec := cfg.Selectors[key]
		spec.Parents, spec.ReadAttribute = []contract.SelectorGroup{root}, "ka"
		spec.Target.Selectors = []contract.SelectorCandidate{{Type: "css", Value: css}}
		cfg.Selectors[key] = spec
	}
	cfg.Selectors["candidate.accept_resume_offer"] = bossReplySelector("accept-offer")
	b.messages = []contract.FindAllItem{bossTestMessage("04:59", "item-system", "旧消息"), bossResumePDFMessage("", "旧文件.pdf"), bossResumeOfferFixture(), bossTestMessage("05:01", "item-myself", "先前已回复")}
	messages, err := bossMessages(b.messages, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ResumeOfferMessageID = messages[2].Key
	snapshot.ExpectedSendMessageKey = messages[3].Key
	return &bossResumeOfferBrowser{bossReplyTestBrowser: b}, cfg, snapshot
}

// TestBossResumeOfferPureReadKeepsKnownInvitation 验证已知HR锚点前的待接收邀约仍被回传，普通读取不点击也不冒充已收到PDF。
func TestBossResumeOfferPureReadKeepsKnownInvitation(t *testing.T) {
	b, cfg, snapshot := bossResumeOfferEnvironment(t)
	for _, delivery := range []bool{false, true} {
		if delivery {
			snapshot.DeliveryPageURL = cfg.MessagesURL
		}
		messages, _, err := (&Runtime{}).ReadAutoReplyMessages(context.Background(), b, cfg, snapshot, []string{snapshot.ExpectedSendMessageKey}, 5000)
		if err != nil {
			t.Fatal(err)
		}
		if b.accepts != 0 || len(messages) != 2 || !bossResumeOfferMessage(messages[0]) || messages[0].Key != snapshot.ResumeOfferMessageID || messages[0].MessageType != "card" {
			t.Fatal("已知锚点裁掉邀约、读取有副作用或改变旧卡片类型与来源")
		}
	}
	row := bossResumeOfferFixture()
	delete(row.Fields, "message_meta")
	plain, err := bossMessages([]contract.FindAllItem{row}, time.Now())
	if err != nil || bossResumeOfferMessage(plain[0]) {
		t.Fatal("没有原生结构证明的普通卡片被当作可接收邀约")
	}
	for _, side := range []string{"item-myself", "item-system"} {
		row := bossResumeOfferFixture()
		row.Fields["body_class"] = side
		messages, err := bossMessages([]contract.FindAllItem{row}, time.Now())
		if err != nil || bossResumeOfferMessage(messages[0]) {
			t.Fatal("非候选人方向可以冒充主动简历邀约")
		}
	}
}

// TestBossResumeOfferAcceptsOnlyNewPDF 验证新PDF无论替换旧邀约行或追加都保留自己的真实来源，旧文件不能顶替。
func TestBossResumeOfferAcceptsOnlyNewPDF(t *testing.T) {
	for _, replace := range []bool{false, true} {
		b, cfg, snapshot := bossResumeOfferEnvironment(t)
		b.after = append([]contract.FindAllItem(nil), b.messages...)
		pdf := bossResumePDFMessage("05:00", "本轮新文件.pdf")
		if replace {
			b.after[2] = pdf
		} else {
			b.after = append(b.after, pdf)
		}
		result, err := requestBossAutoReplyCandidateInfo(context.Background(), b, cfg, snapshot, model.CandidateInfoRequest{RequestResume: true})
		if err != nil || b.accepts != 1 || result.Status != model.AutoReplyInfoReceived || result.EvidenceMessage == nil || result.EvidenceMessageID == snapshot.ResumeOfferMessageID || result.EvidenceMessage.TextContent != "本轮新文件.pdf" || result.EvidenceMessage.Direction != "candidate" || result.EvidenceMessage.MessageType != "resume" {
			t.Fatalf("本轮新PDF没有独立回执或发生重复同意：replace=%v accepts=%d err=%v", replace, b.accepts, err)
		}
		pdfIndex, anchorIndex := -1, -1
		for index, message := range result.EvidenceMessages {
			if message.Key == result.EvidenceMessageID {
				pdfIndex = index
			}
			if message.Key == snapshot.ExpectedSendMessageKey {
				anchorIndex = index
			}
		}
		if pdfIndex < 0 || anchorIndex < 0 || replace && pdfIndex > anchorIndex || !replace && pdfIndex < anchorIndex {
			t.Fatal("回传窗口丢失真实PDF、已知HR锚点或改变了原生消息顺序")
		}
		if len(b.clicked.Parents) < 2 || !strings.Contains(b.clicked.Parents[1].Selectors[0].Value, "nth-child(4)") || !strings.Contains(b.clicked.Parents[1].Selectors[0].Value, "nth-child(5)") {
			t.Fatal("同意按钮未限定读取时的总行数")
		}
		for _, prefix := range []string{"get_resume_", "get_phone_", "get_wechat_"} {
			if !strings.Contains(b.clicked.Parents[0].Selectors[0].Value, `[ka="`+prefix+snapshot.PlatformCandidateID+`"]`) {
				t.Fatal("同意按钮没有原子限定当前候选的三类原生编号")
			}
		}
	}
}

// TestBossResumeOfferGuardsBeforeClick 验证错人、同名错编号、岗位变化、删除前邀约、新插话和不匹配资料请求都不点击。
func TestBossResumeOfferGuardsBeforeClick(t *testing.T) {
	for _, mode := range []string{"same_name_wrong_id", "other_name", "position", "deleted", "new_candidate", "wrong_kind", "missing_anchor"} {
		t.Run(mode, func(t *testing.T) {
			b, cfg, snapshot := bossResumeOfferEnvironment(t)
			request := model.CandidateInfoRequest{RequestResume: true}
			switch mode {
			case "same_name_wrong_id":
				b.changeOnRead = func() { b.values["candidate"] = "get_resume_999999999" }
			case "other_name":
				b.changeOnRead = func() { b.values["name"] = "另一候选人" }
			case "position":
				b.changeOnRead = func() { b.values["position"] = "另一岗位" }
			case "deleted":
				messages, _ := bossMessages(b.messages, time.Now())
				snapshot.HistoryAfter = messages[2].SentAt
			case "new_candidate":
				b.messages = append(b.messages, bossTestMessage("05:02", "item-friend", "请先不要接收"))
			case "wrong_kind":
				request = model.CandidateInfoRequest{RequestPhone: true}
			case "missing_anchor":
				snapshot.ExpectedSendMessageKey = ""
			}
			result, err := requestBossAutoReplyCandidateInfo(context.Background(), b, cfg, snapshot, request)
			if b.accepts != 0 || err == nil && result.Status != model.AutoReplyInfoDeferred {
				t.Fatalf("不可信邀约仍被接收：accepts=%d status=%s err=%v", b.accepts, result.Status, err)
			}
		})
	}
}

// TestBossResumeOfferUnknownNeverClicksTwice 验证点击错误或始终只有旧PDF时保持未知，不使用旧文件或再次点同意。
func TestBossResumeOfferUnknownNeverClicksTwice(t *testing.T) {
	for _, mode := range []string{"click_unknown", "old_pdf_only"} {
		b, cfg, snapshot := bossResumeOfferEnvironment(t)
		if mode == "click_unknown" {
			b.clickErr = errors.New("测试模拟回执丢失")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		result, err := requestBossAutoReplyCandidateInfo(ctx, b, cfg, snapshot, model.CandidateInfoRequest{RequestResume: true})
		cancel()
		if err == nil || b.accepts != 1 || result.EvidenceMessage != nil {
			t.Fatal("点击未知或旧PDF被误当成功，或者重复点了同意")
		}
	}
}

// TestBossResumeOfferRejectsAmbiguousPDF 验证多份新PDF及同分钟同文重编号均保持未知，不能选择某份旧文件充数。
func TestBossResumeOfferRejectsAmbiguousPDF(t *testing.T) {
	now := time.Now()
	rows := []contract.FindAllItem{bossResumeOfferFixture(), bossResumePDFMessage("05:00", "相同文件.pdf"), bossResumePDFMessage("05:00", "相同文件.pdf")}
	messages, err := bossMessages(rows, now)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{messages[1].Key: true, bossResumeVisibleContentKey(messages[1]): true}
	if result, _, err := bossNewResumeOfferReceipt(messages, messages[0], known, nil); err == nil || result.EvidenceMessage != nil {
		t.Fatal("旧PDF发生重复序号位移后被冒认成本次新附件")
	}
	rows[1], rows[2] = bossResumePDFMessage("05:00", "新文件A.pdf"), bossResumePDFMessage("05:00", "新文件B.pdf")
	messages, err = bossMessages(rows, now)
	if err != nil {
		t.Fatal(err)
	}
	if result, _, err := bossNewResumeOfferReceipt(messages, messages[0], map[string]bool{}, nil); err == nil || result.EvidenceMessage != nil {
		t.Fatal("多份新文件来源不明仍任意选了一份")
	}
}
