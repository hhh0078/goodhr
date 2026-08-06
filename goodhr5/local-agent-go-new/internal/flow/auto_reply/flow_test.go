// Package auto_reply 验证多岗位归属、页面选择、消息同步和安全发送核心规则。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// autoReplyBrowserStub 提供自动回复流程测试所需的浏览器最小实现。
type autoReplyBrowserStub struct {
	pages      []contract.PageInfo
	usedPageID string
}

// StartBrowser 模拟复用已经打开的浏览器。
func (b *autoReplyBrowserStub) StartBrowser(context.Context, contract.BrowserStartRequest) (contract.BrowserStatus, error) {
	return contract.BrowserStatus{Running: true, Reused: true}, nil
}

// OpenPage 模拟打开页面。
func (b *autoReplyBrowserStub) OpenPage(context.Context, contract.PageOpenRequest) (contract.PageInfo, error) {
	return contract.PageInfo{}, nil
}

// ListPages 返回测试配置的标签页。
func (b *autoReplyBrowserStub) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: b.pages, Count: len(b.pages)}, nil
}

// UsePage 记录流程选择的标签页。
func (b *autoReplyBrowserStub) UsePage(_ context.Context, request contract.PageUseRequest) (contract.PageInfo, error) {
	b.usedPageID = request.PageID
	return contract.PageInfo{PageID: request.PageID, Current: true}, nil
}

// FindAll 返回空列表。
func (b *autoReplyBrowserStub) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	return nil, nil
}

// Read 返回空读取结果。
func (b *autoReplyBrowserStub) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{}, nil
}

// Click 返回成功点击结果。
func (b *autoReplyBrowserStub) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, nil
}

// Input 返回成功输入结果。
func (b *autoReplyBrowserStub) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, nil
}

// Scroll 返回成功滚动结果。
func (b *autoReplyBrowserStub) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{}, nil
}

// PressKey 返回成功按键结果。
func (b *autoReplyBrowserStub) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, nil
}

// ClosePage 模拟关闭页面。
func (b *autoReplyBrowserStub) ClosePage(context.Context) error {
	return nil
}

// autoReplyRuntimeStub 模拟平台自动回复能力和发送前后的最新消息。
type autoReplyRuntimeStub struct {
	latestBefore model.ConversationMessage
	latestAfter  model.ConversationMessage
	opened       model.AutoReplyConversationSnapshot
	resumeBundle model.AutoReplyResumeBundle
	resumeErr    error
	sent         bool
	sentText     string
	closeCount   int
}

// InitializeAutoReplyPage 模拟页面初始化。
func (r *autoReplyRuntimeStub) InitializeAutoReplyPage(context.Context, model.Browser, model.Config) error {
	return nil
}

// OpenAutoReplyConversation 返回空会话快照。
func (r *autoReplyRuntimeStub) OpenAutoReplyConversation(context.Context, model.Browser, model.Config, model.Conversation, string, int) (model.AutoReplyConversationSnapshot, error) {
	return r.opened, nil
}

// RequestAutoReplyResume 模拟索要简历。
func (r *autoReplyRuntimeStub) RequestAutoReplyResume(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) error {
	return nil
}

// CollectAutoReplyResume 返回空简历结果。
func (r *autoReplyRuntimeStub) CollectAutoReplyResume(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	return r.resumeBundle, r.resumeErr
}

// SendAutoReplyMessage 记录实际发送的消息。
func (r *autoReplyRuntimeStub) SendAutoReplyMessage(_ context.Context, _ model.Browser, _ model.Config, _ model.AutoReplyConversationSnapshot, message string) error {
	r.sent = true
	r.sentText = message
	return nil
}

// ReadLatestAutoReplyMessage 按发送前后返回不同的页面最新消息。
func (r *autoReplyRuntimeStub) ReadLatestAutoReplyMessage(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	if r.sent {
		return r.latestAfter, nil
	}
	return r.latestBefore, nil
}

// CloseAutoReplyConversation 模拟关闭自动回复候选人会话。
func (r *autoReplyRuntimeStub) CloseAutoReplyConversation(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) error {
	r.closeCount++
	return nil
}

type unreadScannerStub struct {
	err error
}

// resumeResponderStub 模拟简历结构化成功或失败，并满足自动回复处理器接口。
type resumeResponderStub struct {
	structured StructuredResume
	err        error
}

// Reply 返回空决策，本测试只验证简历结构化边界。
func (r resumeResponderStub) Reply(context.Context, ReplyContext) (ReplyDecision, error) {
	return ReplyDecision{}, nil
}

// StructureResume 返回测试指定的结构化结果或错误。
func (r resumeResponderStub) StructureResume(context.Context, ResumeStructureContext) (StructuredResume, error) {
	return r.structured, r.err
}

// ScanUnreadConversations 返回检查点错误策略测试配置的固定错误。
func (s unreadScannerStub) ScanUnreadConversations(context.Context, model.Browser, model.Config) ([]model.Conversation, error) {
	return nil, s.err
}

// TestResolvePositionRequiresUniqueMatch 验证完整岗位、截断岗位和同前缀冲突的安全规则。
func TestResolvePositionRequiresUniqueMatch(t *testing.T) {
	items := []cloud.AutoReplyPositionSnapshot{
		{Position: cloud.AutoReplyPosition{ID: "p1", Name: "AI应用开发工程师初级"}},
		{Position: cloud.AutoReplyPosition{ID: "p2", Name: "高中数学老师"}},
	}
	resolved, err := resolvePosition(items, "高中数学老师")
	if err != nil || resolved.Position.ID != "p2" {
		t.Fatalf("完整岗位没有唯一匹配：result=%+v err=%v", resolved, err)
	}
	resolved, err = resolvePosition(items, "AI应用开发工程师初...")
	if err != nil || resolved.Position.ID != "p1" {
		t.Fatalf("截断岗位没有唯一匹配：result=%+v err=%v", resolved, err)
	}
	items = append(items, cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "p3", Name: "AI应用开发工程师初中级"}})
	if _, err = resolvePosition(items, "AI应用开发工程师初..."); err == nil || !strings.Contains(err.Error(), "匹配到2个岗位") {
		t.Fatalf("同前缀岗位应该转人工：%v", err)
	}
}

// TestMergeStructuredCandidatePreservesExistingCollections 验证 AI 空数组不会清掉云端已有经历，非空字段可以增量补齐。
func TestMergeStructuredCandidatePreservesExistingCollections(t *testing.T) {
	target := cloud.StructuredCandidate{
		Phone:           "13800138000",
		WorkExperiences: []cloud.CandidateWorkExperience{{CompanyName: "已有公司"}},
		Educations:      []cloud.CandidateEducation{{SchoolName: "已有学校"}},
	}
	mergeStructuredCandidate(&target, cloud.StructuredCandidate{
		Email:      "candidate@example.com",
		Educations: []cloud.CandidateEducation{{SchoolName: "新学校"}},
	})
	if target.Phone != "13800138000" || target.Email != "candidate@example.com" {
		t.Fatalf("基础字段没有正确增量合并：%+v", target)
	}
	if len(target.WorkExperiences) != 1 || target.WorkExperiences[0].CompanyName != "已有公司" {
		t.Fatalf("AI 空工作经历清掉了已有数据：%+v", target.WorkExperiences)
	}
	if len(target.Educations) != 1 || target.Educations[0].SchoolName != "新学校" {
		t.Fatalf("AI 非空教育经历没有更新：%+v", target.Educations)
	}
}

// TestNormalizeAutoReplyContacts 验证国际手机号、错误短号和邮箱统一清洗。
func TestNormalizeAutoReplyContacts(t *testing.T) {
	if actual := normalizeAutoReplyPhone("+86 136-3281-3031"); actual != "+8613632813031" {
		t.Fatalf("国际手机号清洗不正确：%q", actual)
	}
	if actual := normalizeAutoReplyPhone("123"); actual != "" {
		t.Fatalf("错误短号不应进入正式简历：%q", actual)
	}
	if actual := normalizeAutoReplyEmail(" User@Example.COM "); actual != "user@example.com" {
		t.Fatalf("邮箱清洗不正确：%q", actual)
	}
}

// TestStoredAttachmentTextDeduplicatesContent 验证临时附件正文可在下一轮继续使用且不会重复拼接。
func TestStoredAttachmentTextDeduplicatesContent(t *testing.T) {
	actual := storedAttachmentText([]cloud.StoredResumeAttachment{
		{ExtractedText: "附件正文甲"},
		{ExtractedText: " 附件正文甲 "},
		{ExtractedText: "附件正文乙"},
		{},
	})
	if actual != "附件正文甲\n\n附件正文乙" {
		t.Fatalf("附件正文合并不正确：%q", actual)
	}
}

// TestEnsureResumeDoesNotUploadWithoutValidatedCandidate 验证结构化失败或缺少手机号时不会拿空候选人编号上传附件。
func TestEnsureResumeDoesNotUploadWithoutValidatedCandidate(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "不应该请求云端", http.StatusInternalServerError)
	}))
	defer server.Close()
	prepared := shared.PreparedTask{
		Request:   shared.StartRequest{TaskID: "task-resume-guard", Token: "test-token"},
		MachineID: "test-machine",
		Platform:  model.Config{ID: "liepin", Name: "猎聘企业端"},
	}
	position := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position-1"}}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "邓云川", ResumeCardAvailable: true}
	conversation := cloud.AutoReplyConversation{ID: "conversation-1"}
	runtime := &autoReplyRuntimeStub{resumeBundle: model.AutoReplyResumeBundle{
		CandidateName: "邓云川", OnlineResumeText: "7年工作经验", AttachmentPaths: []string{"/tmp/test-resume.pdf"},
	}}
	cases := []struct {
		name      string
		responder resumeResponderStub
		wantError string
	}{
		{name: "结构化失败", responder: resumeResponderStub{err: fmt.Errorf("AI 返回格式不正确")}, wantError: "整理候选人正式简历失败"},
		{name: "缺少手机号", responder: resumeResponderStub{structured: StructuredResume{Candidate: cloud.StructuredCandidate{WorkYears: "7"}}}, wantError: "没找到可用手机号"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stats := shared.Stats{}
			flow := &Flow{Cloud: cloud.New(server.URL), Responder: testCase.responder}
			_, _, err := flow.ensureResume(
				context.Background(), prepared, runtime, position, &conversation,
				snapshot, cloud.AutoReplyCandidateState{}, "message-1", &stats,
			)
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("没有返回真正的简历入库错误：%v", err)
			}
			if stats.Failed != 1 {
				t.Fatalf("失败统计不正确：%+v", stats)
			}
		})
	}
	if requestCount != 0 {
		t.Fatalf("候选人未通过校验时仍请求了云端：count=%d", requestCount)
	}
}

// TestEnsureResumeDoesNotUsePlatformMessageKeyAsCloudMessageID 验证平台消息键不会误传给云端 UUID 外键。
func TestEnsureResumeDoesNotUsePlatformMessageKeyAsCloudMessageID(t *testing.T) {
	resumePath := filepath.Join(t.TempDir(), "resume.pdf")
	if err := os.WriteFile(resumePath, []byte("%PDF-test-resume"), 0o600); err != nil {
		t.Fatalf("准备测试简历失败：%v", err)
	}
	uploaded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auto-reply/agent/candidates":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "candidate_id": "candidate-1",
				"platform_identity": map[string]any{"id": "identity-1"},
			})
		case "/api/auto-reply/agent/conversations":
			var conversation cloud.AutoReplyConversation
			if err := json.NewDecoder(r.Body).Decode(&conversation); err != nil {
				t.Fatalf("读取会话请求失败：%v", err)
			}
			conversation.ID = "conversation-1"
			conversation.CandidateID = "candidate-1"
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "conversation": conversation})
		case "/api/auto-reply/agent/attachments":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("读取附件请求失败：%v", err)
			}
			if source := r.FormValue("source_message_id"); source != "" {
				t.Fatalf("平台消息键被误当成云端消息 UUID：%q", source)
			}
			if r.FormValue("candidate_id") != "candidate-1" || r.FormValue("conversation_id") != "conversation-1" {
				t.Fatalf("附件归属不正确：candidate=%q conversation=%q", r.FormValue("candidate_id"), r.FormValue("conversation_id"))
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("读取上传文件失败：%v", err)
			}
			defer file.Close()
			hash := sha256.New()
			size, err := io.Copy(hash, file)
			if err != nil {
				t.Fatalf("读取上传内容失败：%v", err)
			}
			uploaded = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "attachment": map[string]any{
					"id": "attachment-1", "candidate_id": "candidate-1", "conversation_id": "conversation-1",
					"sha256": hex.EncodeToString(hash.Sum(nil)), "size_bytes": size, "created_at": "2026-08-06T00:00:00Z",
				},
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()
	prepared := shared.PreparedTask{
		Request: shared.StartRequest{TaskID: "task-resume-upload", Token: "test-token"}, MachineID: "test-machine",
		Platform: model.Config{ID: "liepin", Name: "猎聘企业端"},
	}
	conversation := cloud.AutoReplyConversation{
		ID: "temporary-conversation", PlatformID: "liepin", PlatformThreadID: "thread-1", CandidateName: "邓云川",
	}
	runtime := &autoReplyRuntimeStub{resumeBundle: model.AutoReplyResumeBundle{
		CandidateName: "邓云川", Phone: "17607080935", OnlineResumeText: "7年工作经验",
		AttachmentPaths: []string{resumePath}, ResumeSourceMessageID: "platform-message-1",
	}}
	flow := &Flow{
		Cloud: cloud.New(server.URL),
		Responder: resumeResponderStub{structured: StructuredResume{Candidate: cloud.StructuredCandidate{
			CandidateName: "邓云川", Phone: "17607080935", WorkYears: "7",
		}}},
	}
	stats := shared.Stats{}
	resume, handled, err := flow.ensureResume(
		context.Background(), prepared, runtime,
		cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "position-1"}},
		&conversation, model.AutoReplyConversationSnapshot{
			CandidateName: "邓云川", ResumeCardAvailable: true, ResumeSourceMessageID: "platform-message-1",
		}, cloud.AutoReplyCandidateState{}, "message-1", &stats,
	)
	if err != nil || handled || resume == nil || !uploaded {
		t.Fatalf("简历附件没有正常保存：resume=%+v handled=%t uploaded=%t err=%v", resume, handled, uploaded, err)
	}
}

// TestSelectCurrentPlatformPagePrefersCurrentAndRejectsAmbiguous 验证当前页优先以及多个后台标签页不猜测。
func TestSelectCurrentPlatformPagePrefersCurrentAndRejectsAmbiguous(t *testing.T) {
	config := model.Config{Name: "猎聘企业端", EntryURL: "https://lpt.liepin.com/recommend", LoginURL: "https://lpt.liepin.com/login"}
	browser := &autoReplyBrowserStub{pages: []contract.PageInfo{
		{PageID: "other", URL: "https://example.com"},
		{PageID: "liepin-current", URL: "https://lpt.liepin.com/recommend", Current: true},
		{PageID: "liepin-other", URL: "https://lpt.liepin.com/chat"},
	}}
	flow := &Flow{Browser: browser}
	if err := flow.selectCurrentPlatformPage(context.Background(), config); err != nil {
		t.Fatalf("当前猎聘页应该直接使用：%v", err)
	}
	if browser.usedPageID != "" {
		t.Fatalf("当前页已匹配时不应切换标签：%s", browser.usedPageID)
	}

	browser.pages[1].Current = false
	if err := flow.selectCurrentPlatformPage(context.Background(), config); err == nil || !strings.Contains(err.Error(), "同时打开了2个") {
		t.Fatalf("多个非当前猎聘页应该报错：%v", err)
	}
}

// TestConvertMessagesKeepsLatestCandidateKey 验证消息方向校验和候选人最新消息键。
func TestConvertMessagesKeepsLatestCandidateKey(t *testing.T) {
	messages, key, err := convertMessages([]model.ConversationMessage{
		{PlatformMessageID: "in-1", Direction: "candidate", MessageType: "text", TextContent: "你好"},
		{PlatformMessageID: "out-1", Direction: "self", MessageType: "text", TextContent: "你好"},
		{Key: "in-2-fingerprint", Direction: "candidate", MessageType: "text", TextContent: "薪资是多少"},
	})
	if err != nil || len(messages) != 3 || key != "in-2-fingerprint" {
		t.Fatalf("消息转换不正确：messages=%+v key=%s err=%v", messages, key, err)
	}
	if _, _, err = convertMessages([]model.ConversationMessage{{Direction: "unknown"}}); err == nil {
		t.Fatal("无法确认消息方向时不应继续")
	}
}

// TestSendVerifiedMessageChecksLatestAndAvoidsDuplicate 验证发送前消息复核、发送后回读和重复保护。
func TestSendVerifiedMessageChecksLatestAndAvoidsDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/messages/sync" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var payload cloud.AutoReplyMessageSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Messages) != 1 || payload.Messages[0].Direction != "self" || payload.Messages[0].TextContent != "薪资面议" {
			t.Fatalf("sent message sync=%+v", payload.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"sync":{"inserted":1,"last_synced_message_key":"out-1"}}`))
	}))
	defer server.Close()
	store, err := storage.Open(filepath.Join(t.TempDir(), "auto-reply.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	flow := &Flow{Browser: &autoReplyBrowserStub{}, Cloud: cloud.New(server.URL), Store: store}
	prepared := shared.PreparedTask{
		Request: shared.StartRequest{TaskID: "task-1", Token: "token"}, MachineID: "goodhr-device-v1-test",
		Platform: model.Config{ID: "liepin", Name: "猎聘企业端"},
	}
	runtime := &autoReplyRuntimeStub{
		latestBefore: model.ConversationMessage{PlatformMessageID: "in-1", Direction: "candidate", TextContent: "薪资是多少"},
		latestAfter:  model.ConversationMessage{PlatformMessageID: "out-1", Direction: "self", TextContent: "薪资面议"},
	}
	conversation := cloud.AutoReplyConversation{ID: "conversation-1", PlatformThreadID: "thread-1"}
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "李女士"}
	sent, err := flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, snapshot, "in-1", "薪资面议", true)
	if err != nil || !sent || runtime.sentText != "薪资面议" {
		t.Fatalf("安全发送失败：sent=%t text=%q err=%v", sent, runtime.sentText, err)
	}

	runtime.sent = false
	sent, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, snapshot, "in-1", "薪资面议", true)
	if err != nil || sent || runtime.sent {
		t.Fatalf("重复回复应该在点击前跳过：sent=%t runtime.sent=%t err=%v", sent, runtime.sent, err)
	}

	runtime.latestBefore = model.ConversationMessage{PlatformMessageID: "in-2", Direction: "candidate", TextContent: "又发了一条"}
	sent, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, snapshot, "in-1", "另一条回复", true)
	if err != nil || sent || runtime.sent {
		t.Fatalf("候选人新消息出现后旧回复应该废弃：sent=%t runtime.sent=%t err=%v", sent, runtime.sent, err)
	}

	runtime.sent = false
	sent, err = flow.sendVerifiedMessage(context.Background(), prepared, runtime, conversation, snapshot, "in-2", "薪资面议", true)
	if err != nil || !sent || !runtime.sent {
		t.Fatalf("同一候选人的新消息应该允许复用相同回复：sent=%t runtime.sent=%t err=%v", sent, runtime.sent, err)
	}
}

// TestCheckpointSettingsCapsThree 验证单轮配置永远不会超过三个候选人。
func TestCheckpointSettingsCapsThree(t *testing.T) {
	items := []cloud.AutoReplyPositionSnapshot{{
		Position: cloud.AutoReplyPosition{ID: "p1"},
		Config:   cloud.PositionAutoReplyConfig{MaxThreadsPerCheckpoint: 99, PollIntervalSeconds: 5},
	}}
	limit, wait := checkpointSettings(items, "p1")
	if limit != 3 || wait != 5 {
		t.Fatalf("checkpoint settings limit=%d wait=%d", limit, wait)
	}
}

// TestRunCheckpointSkipsWithoutAutoReplyAccess 验证 Plus 或过期会员的打招呼任务不会误调自动回复接口。
func TestRunCheckpointSkipsWithoutAutoReplyAccess(t *testing.T) {
	result, err := (&Flow{}).RunCheckpoint(
		context.Background(), shared.PreparedTask{}, nil, &CheckpointSession{},
	)
	if err != nil || result.Enabled || result.TouchedPage || result.Processed != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

// TestProcessCheckpointStopsAfterThreeScanErrors 验证未读列表同类错误前两次跳过、第三次停止。
func TestProcessCheckpointStopsAfterThreeScanErrors(t *testing.T) {
	flow := &Flow{}
	policy := &shared.ConsecutiveErrorPolicy{}
	stats := &shared.Stats{}
	prepared := shared.PreparedTask{Request: shared.StartRequest{TaskID: "task-1"}}
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := flow.processCheckpoint(
			context.Background(), prepared, unreadScannerStub{err: fmt.Errorf("侧边栏暂时没读到")},
			&autoReplyRuntimeStub{}, nil, 3, stats, policy,
		)
		if attempt < 3 && err != nil {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
		if attempt == 3 && (err == nil || !strings.Contains(err.Error(), "连续 3 次")) {
			t.Fatalf("attempt 3 error = %v", err)
		}
	}
}

// TestProcessConversationKeepsConversationOpen 验证消息格式报错时也保留自动回复侧边会话，方便批次内继续处理下一人。
func TestProcessConversationKeepsConversationOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/candidate-state" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"found":false,"attachments":[]}`))
	}))
	defer server.Close()
	runtime := &autoReplyRuntimeStub{opened: model.AutoReplyConversationSnapshot{
		Messages: []model.ConversationMessage{{Direction: "unknown", TextContent: "测试"}},
	}}
	flow := &Flow{Cloud: cloud.New(server.URL)}
	stats := &shared.Stats{}
	err := flow.processConversation(context.Background(), shared.PreparedTask{
		Request: shared.StartRequest{TaskID: "task-1", Token: "token"}, MachineID: "machine",
		Platform: model.Config{ID: "liepin"},
	}, runtime, nil, model.Conversation{Key: "thread-1"}, stats)
	if err == nil || runtime.closeCount != 0 {
		t.Fatalf("error=%v close_count=%d", err, runtime.closeCount)
	}
}

// TestMessageFingerprintIsStable 验证没有平台消息编号时同一消息会生成相同指纹。
func TestMessageFingerprintIsStable(t *testing.T) {
	sentAt := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	message := model.ConversationMessage{Direction: "candidate", MessageType: "text", TextContent: "你好", SentAt: &sentAt}
	if messageFingerprint(message, 0) != messageFingerprint(message, 0) {
		t.Fatal("同一条消息的指纹必须稳定")
	}
}
