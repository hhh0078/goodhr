// Package boss 本文件验证附件采集只省同一窗口的重复读，变化见证、首次核身和文件来源校验不能省略。
package boss

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossAttachmentPathBrowser 记录纯读次数，并可在指定正文快照返回后模拟新消息以验证窗口变化守卫。
type bossAttachmentPathBrowser struct {
	*bossReplyTestBrowser
	nameReads   int
	bodyReads   int
	changeAfter int
}

// Read 仅统计现有核身调用，其他字段仍由明确的合成页面提供。
func (b *bossAttachmentPathBrowser) Read(ctx context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Selector.Description == "name" {
		b.nameReads++
	}
	return b.bossReplyTestBrowser.Read(ctx, request)
}

// FindAll 在正文读取后模拟候选插话；首尾见证必须发现变化，不能为了省调用沿用旧快照。
func (b *bossAttachmentPathBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	items, err := b.bossReplyTestBrowser.FindAll(ctx, request)
	if request.Selector.Description == "messages" && len(request.Fields) > 2 {
		b.bodyReads++
		if b.bodyReads == b.changeAfter {
			b.messages = append(b.messages, bossTestMessage("06:00", "item-friend", "新消息"))
		}
	}
	return items, err
}

// bossAttachmentPathFixture 构造已有真实PDF类型来源的核身环境，不提供下载能力以阻止测试实际保存文件。
func bossAttachmentPathFixture(t *testing.T, history bool) (*bossAttachmentPathBrowser, model.Config, model.AutoReplyConversationSnapshot) {
	t.Helper()
	base, cfg, snapshot := bossResumeOfferEnvironment(t)
	cfg.Selectors["message.resume_attachment_entry"] = bossReplySelector("pdf-entry")
	base.messages = []contract.FindAllItem{bossTestMessage("04:59", "item-system", "之前消息"), bossResumePDFMessage("05:00", "本轮文件.pdf"), bossTestMessage("05:01", "item-myself", "已有回复")}
	if history {
		base.messages = []contract.FindAllItem{bossTestMessage("04:40", "item-system", "历史起点"), bossResumePDFMessage("04:41", "本轮文件.pdf")}
		for i := 2; i < 12; i++ {
			base.messages = append(base.messages, bossTestMessage(fmt.Sprintf("04:%02d", 40+i), "item-friend", fmt.Sprintf("普通消息%d", i)))
		}
	}
	messages, err := bossMessages(base.messages, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ResumeOfferMessageID = ""
	snapshot.ResumeCardAvailable, snapshot.ResumeSourceMessageID = true, messages[1].Key
	return &bossAttachmentPathBrowser{bossReplyTestBrowser: base.bossReplyTestBrowser}, cfg, snapshot
}

// TestBossAttachmentReusesVerifiedRecentWindow 验证正常近期来源仅核身一次、读正文一次及必要首尾一次，不重复完整扫描。
func TestBossAttachmentReusesVerifiedRecentWindow(t *testing.T) {
	b, cfg, snapshot := bossAttachmentPathFixture(t, false)
	_, err := collectBossAutoReplyResume(context.Background(), b, cfg, snapshot)
	if err == nil || !strings.Contains(err.Error(), "下载监听") {
		t.Fatal("合成环境应停在下载能力边界，不能触发真实页面")
	}
	if b.nameReads != 1 || b.bodyReads != 1 || len(b.requests) != 3 || b.clicks != 0 {
		t.Fatalf("同一近期窗口仍有重复核身/首尾读或越过下载边界：names=%d body=%d requests=%d clicks=%d", b.nameReads, b.bodyReads, len(b.requests), b.clicks)
	}
}

// TestBossAttachmentKeepsChangedWindowWitness 验证首次正文读取后有新消息时仍重读并获取新首尾，不能使用旧总行数。
func TestBossAttachmentKeepsChangedWindowWitness(t *testing.T) {
	b, cfg, snapshot := bossAttachmentPathFixture(t, false)
	b.changeAfter = 1
	pinned, err := pinBossResumeSource(context.Background(), b, cfg, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	entry := pinned.Selectors["message.resume_attachment_entry"]
	if b.bodyReads != 2 || len(b.requests) != 6 || len(entry.Parents) < 2 || !strings.Contains(entry.Parents[1].Selectors[0].Value, "nth-child(4)") {
		t.Fatal("正文期间插话后没有重读、首尾见证或固定新的总行数")
	}
}

// TestBossAttachmentRechecksFreshHistoryRead 验证补历史后重新读正文仍必须核首尾，第二份窗口变化时拒绝旧文件定位。
func TestBossAttachmentRechecksFreshHistoryRead(t *testing.T) {
	b, cfg, snapshot := bossAttachmentPathFixture(t, true)
	b.changeAfter = 3 // 尾窗、完整历史及最后一份新正文；只在最后重读后插入消息。
	_, err := pinBossResumeSource(context.Background(), b, cfg, snapshot)
	if err == nil || !strings.Contains(err.Error(), "聊天已变化") || b.bodyReads != 3 || b.clicks != 0 {
		t.Fatalf("补历史后的新窗口未验证变化：body=%d clicks=%d err=%v", b.bodyReads, b.clicks, err)
	}
}

// bossAttachmentSourceBrowser 只给出显式iframe或href结果，记录是否错误读取没有href的下载div。
type bossAttachmentSourceBrowser struct {
	model.Browser
	values map[string]string
	errors map[string]error
	reads  []string
}

// Read 返回合成来源并记录属性类型，不允许额外浏览器能力。
func (b *bossAttachmentSourceBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	b.reads = append(b.reads, request.Selector.Description+":"+request.Attribute)
	if err := b.errors[request.Selector.Description]; err != nil {
		return contract.ReadResult{}, err
	}
	return contract.ReadResult{Value: b.values[request.Selector.Description]}, nil
}

// TestBossAttachmentPrefersVerifiedFrameSource 验证优先iframe、缺入口兼容href，但已读到的无效来源或真正读取错误不能被备用链接掩盖。
func TestBossAttachmentPrefersVerifiedFrameSource(t *testing.T) {
	for _, mode := range []string{"frame", "href_legacy", "frame_missing", "frame_invalid", "frame_error"} {
		t.Run(mode, func(t *testing.T) {
			cfg := model.Config{Selectors: map[string]contract.SelectorSpec{}}
			source := bossReplySelector("frame")
			source.ReadAttribute = "src"
			cfg.Selectors["message.attachment_file_source"] = source
			b := &bossAttachmentSourceBrowser{values: map[string]string{"frame": "https://files.example.test/frame.pdf", "download": "https://files.example.test/href.pdf"}, errors: map[string]error{}}
			want, wantReads := "https://files.example.test/frame.pdf", []string{"frame:src"}
			switch mode {
			case "href_legacy":
				delete(cfg.Selectors, "message.attachment_file_source")
				want, wantReads = b.values["download"], []string{"download:href"}
			case "frame_missing":
				b.errors["frame"] = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
				want, wantReads = b.values["download"], []string{"frame:src", "download:href"}
			case "frame_invalid":
				b.values["frame"], want = "javascript:void(0)", ""
			case "frame_error":
				b.errors["frame"] = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ACTION_TIMEOUT"}}
				want = ""
			}
			file, err := readBossAttachmentFileSource(context.Background(), b, cfg, "https://example.test/chat", bossReplySelector("download"))
			if (err != nil) != (want == "") || file != want || !reflect.DeepEqual(b.reads, wantReads) {
				t.Fatalf("来源优先级、备用边界或校验失败处理不正确：mode=%s reads=%v err=%v", mode, b.reads, err)
			}
		})
	}
}
