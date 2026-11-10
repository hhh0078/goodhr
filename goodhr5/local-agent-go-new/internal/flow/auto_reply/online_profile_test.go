// Package auto_reply 本文件验证首条来信先准备在线资料、AI模式不偷换DOM、缓存复用、姓名冲突停止和不默认索要附件。
package auto_reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

type onlineProfileRuntime struct {
	*answeringRuntime
	profile model.AutoReplyOnlineProfile
	reads   int
}

// ReadAutoReplyOnlineProfile 返回已核对的合成在线资料，记录是否重复打开页面。
func (r *onlineProfileRuntime) ReadAutoReplyOnlineProfile(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) (model.AutoReplyOnlineProfile, error) {
	r.reads++
	return r.profile, nil
}

type onlineProfileResponder struct {
	visionCalls int
	replyCalls  int
	visionName  string
	visionErr   error
	sawProfile  bool
}

// RecognizeOnlineProfile 模拟服务器图片AI，只在有截图时返回当前候选人的基础资料。
func (r *onlineProfileResponder) RecognizeOnlineProfile(_ context.Context, _ ReplyContext, images [][]byte) (model.AutoReplyOnlineProfile, error) {
	r.visionCalls++
	if r.visionErr != nil {
		return model.AutoReplyOnlineProfile{}, r.visionErr
	}
	if len(images) == 0 {
		return model.AutoReplyOnlineProfile{}, fmt.Errorf("没有真实截图")
	}
	return model.AutoReplyOnlineProfile{CandidateName: r.visionName, RawText: r.visionName + " 本科 28岁", EducationLevel: "本科"}, nil
}

// TestOnlineProfileFailuresHaveSafeStageDiagnostics 验证真实AI解析、姓名冲突及HTTP403保存失败均有安全阶段，原cause保留而原文和密钥不进入诊断。
func TestOnlineProfileFailuresHaveSafeStageDiagnostics(t *testing.T) {
	for _, stage := range []string{"AI识别", "姓名核验", "保存在线资料"} {
		t.Run(stage, func(t *testing.T) {
			f, p, pos, conv, snapshot, state, r, responder, calls, _ := onlineProfileFixture(t, "boss")
			var original error
			switch stage {
			case "AI识别":
				original = &ai.ResponseError{Cause: errors.New("PRIVATE-AI-PARSE token=PRIVATE-KEY"), Diagnostic: ai.ResponseDiagnostic{Response: "PRIVATE-RESUME", ParseError: "PRIVATE-PARSE"}}
				responder.visionErr = original
			case "姓名核验":
				responder.visionName = "PRIVATE-WRONG-NAME"
			case "保存在线资料":
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"code":"RUNTIME_AUTH_REQUIRED","message":"PRIVATE-SERVER https://private.invalid?token=PRIVATE-KEY"}`))
				}))
				defer server.Close()
				f.Cloud = cloud.New(server.URL)
			}
			_, err := f.prepareOnlineProfile(context.Background(), p, r, *pos, conv, snapshot, state)
			if err == nil {
				t.Fatal("已知失败边界被当作成功")
			}
			details := failurediag.Details(err)
			if len(details) == 0 || details[0].Phase != stage || details[0].Source != "在线资料" {
				t.Fatalf("在线失败没有真实阶段：%+v", details)
			}
			encoded, _ := json.Marshal(details)
			if strings.Contains(string(encoded), "PRIVATE-") || strings.Contains(string(encoded), "private.invalid") || strings.Contains(failurediag.Summary(details), "PRIVATE-") {
				t.Fatal("安全诊断泄漏原文、URL或凭据")
			}
			if original != nil && !errors.Is(err, original) {
				t.Fatal("补充诊断丢失原错误链")
			}
			if stage == "保存在线资料" {
				var api *cloud.APIError
				if details[0].HTTPStatus != 403 || !errors.As(err, &api) || api.Code != "RUNTIME_AUTH_REQUIRED" {
					t.Fatal("403或原API分类丢失")
				}
			} else if *calls != 0 {
				t.Fatal("识别或姓名失败后仍提交了在线资料")
			}
		})
	}
}

// Reply 记录普通回答前资料是否已可用，内部目标复核不制造第二份答案或附件提议。
func (r *onlineProfileResponder) Reply(_ context.Context, input ReplyContext) (ReplyDecision, error) {
	if input.ConditionsOnly {
		return ReplyDecision{}, nil
	}
	r.replyCalls++
	r.sawProfile = input.CandidateState.Candidate != nil && input.CandidateState.Candidate.OnlineProfile != nil
	return ReplyDecision{Messages: []ReplyMessage{{Type: "answer", Content: "可以的，我们先聊聊岗位。"}}}, nil
}

// onlineProfileFixture 为每例创建独立状态、配置和匹配API替身，不使用用户数据库或AI配置。
func onlineProfileFixture(t *testing.T, platform string) (*Flow, shared.PreparedTask, *cloud.AutoReplyPositionSnapshot, cloud.AutoReplyConversation, model.AutoReplyConversationSnapshot, cloud.AutoReplyCandidateState, *onlineProfileRuntime, *onlineProfileResponder, *int, *string) {
	t.Helper()
	pos := &cloud.AutoReplyPositionSnapshot{OK: true, Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师", PlatformID: platform}, Config: cloud.PositionAutoReplyConfig{PositionID: "position", Enabled: true, Version: 1, PhrasesVersion: "profile", Phrases: []cloud.AutoReplyPhrase{{ID: "request_resume", Content: "方便发一份简历吗？"}}}, CompanyProfile: cloud.CompanyProfile{ID: "company"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}}
	calls := new(int)
	canonical := new(string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/auto-reply/agent/online-profile" {
			*calls++
			_ = json.NewEncoder(w).Encode(cloud.AutoReplyOnlineProfileResult{CandidateID: *canonical, MatchStatus: "temporary", ProfileKey: "server-profile-key", Candidate: cloud.StructuredCandidate{CandidateName: "测试候选人", EducationLevel: "本科"}})
			return
		}
		_ = json.NewEncoder(w).Encode(pos)
	}))
	t.Cleanup(server.Close)
	f, p := newReliabilityFlow(t, server.URL)
	p.Platform.ID = platform
	ctx := context.Background()
	conv, err := f.Cloud.SaveAutoReplyConversation(ctx, credentials(p), cloud.AutoReplyConversation{PlatformID: platform, PlatformAccountID: p.Request.PlatformAccountID, PlatformThreadID: "thread", PositionID: "position", CandidateName: "测试候选人"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Cloud.EnsureLocalAutoReplyCandidate(ctx, credentials(p), conv); err != nil {
		t.Fatal(err)
	}
	message := model.ConversationMessage{PlatformMessageID: "message-1", Direction: "candidate", MessageType: "text", TextContent: "你好，想了解岗位。"}
	if _, err = f.Cloud.SyncAutoReplyMessages(ctx, credentials(p), cloud.AutoReplyMessageSyncRequest{ConversationID: conv.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: message.PlatformMessageID, Direction: message.Direction, MessageType: message.MessageType, TextContent: message.TextContent}}}); err != nil {
		t.Fatal(err)
	}
	state, err := f.Cloud.AutoReplyCandidateState(ctx, credentials(p), cloud.AutoReplyCandidateLookup{PlatformID: platform, PlatformAccountID: p.Request.PlatformAccountID, PlatformThreadID: "thread"})
	if err != nil {
		t.Fatal(err)
	}
	conv = *state.Conversation
	snapshot := model.AutoReplyConversationSnapshot{CandidateName: "测试候选人", PlatformCandidateID: "page-id", PlatformThreadID: "thread", PlatformAccountID: p.Request.PlatformAccountID, CommunicationPosition: "工程师", Messages: []model.ConversationMessage{message}, HistoryComplete: true}
	profile := model.AutoReplyOnlineProfile{CandidateName: "测试候选人", Mode: "dom", RawText: "测试候选人 本科", SourceKey: "dom-profile"}
	if platform == "boss" {
		profile.Mode = "ai"
		profile.RawText = ""
		profile.SourceKey = "image-profile"
		profile.Screenshots = [][]byte{[]byte("synthetic-image")}
	}
	runtime := &onlineProfileRuntime{answeringRuntime: &answeringRuntime{autoReplyRuntimeStub: &autoReplyRuntimeStub{opened: snapshot, latestBefore: message}}, profile: profile}
	responder := &onlineProfileResponder{visionName: "测试候选人"}
	f.Responder = responder
	return f, p, pos, conv, snapshot, state, runtime, responder, calls, canonical
}

// TestOnlineProfileAIAndCachePromotion 验证Boss确实走服务器图片AI，缓存模式不开正式库，后续开入库只重发文字缓存而不再识别。
func TestOnlineProfileAIAndCachePromotion(t *testing.T) {
	f, p, pos, conv, snapshot, state, r, responder, calls, canonical := onlineProfileFixture(t, "boss")
	ctx := context.Background()
	next, err := f.prepareOnlineProfile(ctx, p, r, *pos, conv, snapshot, state)
	if err != nil {
		t.Fatal(err)
	}
	if r.reads != 1 || responder.visionCalls != 1 || *calls != 1 || next.Candidate.OnlineProfile == nil || next.Candidate.OnlineProfile.CandidateID != "" || next.Candidate.ResumeStructured {
		t.Fatal("在线AI或临时缓存边界不正确")
	}
	if next.Candidate.OnlineProfile.SourceKey != "image-profile" || len(next.Candidate.OnlineProfile.Profile.Screenshots) != 0 {
		t.Fatal("实际图源被替换或截图进入缓存")
	}
	if _, err = f.prepareOnlineProfile(ctx, p, r, *pos, conv, snapshot, next); err != nil || r.reads != 1 || responder.visionCalls != 1 || *calls != 1 {
		t.Fatal("普通轮次重复打开或重复AI", err)
	}
	pos.Position.OutputStructuredResume = true
	*canonical = "new-canonical-id"
	latest, err := f.prepareOnlineProfile(ctx, p, r, *pos, conv, snapshot, next)
	if err != nil || r.reads != 1 || responder.visionCalls != 1 || *calls != 2 || latest.Candidate.OnlineProfile.CandidateID != *canonical {
		t.Fatal("启用入库没有复用服务器缓存", err)
	}
}

// TestOnlineProfileAIRejectsEmptyOrWrongName 验证图中文字必须确认完整姓名，空姓名和错名不保存、不降级用DOM姓名。
func TestOnlineProfileAIRejectsEmptyOrWrongName(t *testing.T) {
	for _, name := range []string{"", "另一位候选人"} {
		t.Run(name, func(t *testing.T) {
			f, p, pos, conv, snapshot, state, r, responder, calls, _ := onlineProfileFixture(t, "boss")
			responder.visionName = name
			if _, err := f.prepareOnlineProfile(context.Background(), p, r, *pos, conv, snapshot, state); err == nil || *calls != 0 {
				t.Fatal("未核实AI姓名仍提交了资料")
			}
		})
	}
}

// TestDOMOnlineProfileComesBeforeReplyWithoutDefaultAttachment 验证其它平台不调图片AI，普通首条问候先获得在线上下文且不会因话术启用就索要附件。
func TestDOMOnlineProfileComesBeforeReplyWithoutDefaultAttachment(t *testing.T) {
	f, p, pos, _, snapshot, _, r, responder, calls, _ := onlineProfileFixture(t, "zhaopin")
	conversation := model.Conversation{Key: "thread", PlatformThreadID: "thread", PlatformCandidateID: "page-id", PlatformAccountID: p.Request.PlatformAccountID, Name: "测试候选人", CommunicationPosition: "工程师"}
	stats := &shared.Stats{}
	if err := f.processConversation(context.Background(), p, r, []cloud.AutoReplyPositionSnapshot{*pos}, conversation, &snapshot, stats); err != nil {
		t.Fatal(err)
	}
	if !responder.sawProfile || responder.visionCalls != 0 || *calls != 1 || r.resumeRequests != 0 || len(r.replies) != 1 {
		t.Fatal("首条问候未使用DOM资料或默认索附件", responder.sawProfile, responder.visionCalls, *calls, r.resumeRequests, r.replies)
	}
}

// TestRequiredMismatchNeedsCurrentExplicitEvidence 验证必要条件有反证才触发，不把未知或AI沟通目标当招聘硬门槛。
func TestRequiredMismatchNeedsCurrentExplicitEvidence(t *testing.T) {
	pos := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "p"}, Config: cloud.PositionAutoReplyConfig{Conditions: []cloud.PositionReplyCondition{{ID: "required", Type: "required", Content: "已持相应岗位资格证", Enabled: true}}}}
	item := cloud.CandidateConfirmationItem{ID: "goal", PositionID: "p", SourceType: "position", PositionConditionID: "required", ItemType: "required", Content: "已持相应岗位资格证", Status: "unmatched", SourceRef: "online_profile", EvidenceText: "尚未取得岗位资格证"}
	if !hasExplicitRequiredMismatch(pos, []cloud.CandidateConfirmationItem{item}) {
		t.Fatal("明确反证未识别")
	}
	item.EvidenceText = ""
	if hasExplicitRequiredMismatch(pos, []cloud.CandidateConfirmationItem{item}) {
		t.Fatal("缺资料被误判不符合")
	}
	item.EvidenceText = "尚未取得岗位资格证"
	item.SourceType = "ai"
	if hasExplicitRequiredMismatch(pos, []cloud.CandidateConfirmationItem{item}) {
		t.Fatal("AI目标被当成HR必要条件")
	}
}

// TestOnlineProfileCanSupportGoalsWithoutInventingEvidence 验证新目标和确定性判断都能引用在线原文，换成不存在的原文仍会拒绝。
func TestOnlineProfileCanSupportGoalsWithoutInventingEvidence(t *testing.T) {
	state := proposalState()
	state.input.CandidateState.Candidate = &cloud.AutoReplyStoredCandidate{OnlineProfile: &cloud.AutoReplyOnlineProfileState{Profile: model.AutoReplyOnlineProfile{RawText: "持有高级电工资格证；希望了解夜班安排"}}}
	motivation := confirmationToolItem{SourceRef: "online_profile", EvidenceText: "希望了解夜班安排"}
	if err := state.validateGoalMotivation(motivation); err != nil {
		t.Fatal("在线原文无法支持新目标", err)
	}
	if err := state.validateConfirmationEvidence(cloud.CandidateConfirmationItem{Status: "matched", SourceRef: "online_profile", EvidenceText: "持有高级电工资格证"}); err != nil {
		t.Fatal("在线原文无法支持判断", err)
	}
	motivation.EvidenceText = "我已同意夜班"
	if err := state.validateGoalMotivation(motivation); err == nil {
		t.Fatal("AI虚构在线证据仍被接受")
	}
}

// TestPersonalAttributeMismatchNeedsHR 验证年龄等个人属性的反证不直接触发自动淘汰话术。
func TestPersonalAttributeMismatchNeedsHR(t *testing.T) {
	pos := cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "p"}, Config: cloud.PositionAutoReplyConfig{Conditions: []cloud.PositionReplyCondition{{ID: "age", Type: "required", Content: "年龄35岁以下", Enabled: true}}}}
	item := cloud.CandidateConfirmationItem{ID: "goal", PositionID: "p", SourceType: "position", PositionConditionID: "age", ItemType: "required", Content: "年龄35岁以下", Status: "unmatched", SourceRef: "online_profile", EvidenceText: "36岁"}
	if hasExplicitRequiredMismatch(pos, []cloud.CandidateConfirmationItem{item}) || !hasPersonalAttributeMismatch(pos, []cloud.CandidateConfirmationItem{item}) {
		t.Fatal("个人属性被当成自动淘汰条件")
	}
}

// TestOnlineProfilePromptBudgetKeepsOneCopy 验证在线原文进入模型时受限且不重复，不会裁剪或清空服务器保存的原件。
func TestOnlineProfilePromptBudgetKeepsOneCopy(t *testing.T) {
	input := proposalState().input
	raw := strings.Repeat("明确在线资料；", 4000)
	input.CandidateState.Candidate = &cloud.AutoReplyStoredCandidate{RawText: raw, BasicInfo: raw, OnlineProfile: &cloud.AutoReplyOnlineProfileState{SourceKey: "proof", Profile: model.AutoReplyOnlineProfile{RawText: raw}}}
	bounded, err := boundedReplyContext(input)
	if err != nil {
		t.Fatal(err)
	}
	candidate := bounded.CandidateState.Candidate
	if len([]rune(candidate.OnlineProfile.Profile.RawText)) != 12000 || candidate.RawText != "" || candidate.BasicInfo != "" {
		t.Fatal("在线正文未压缩或重复送入模型")
	}
	if input.CandidateState.Candidate.RawText != raw || input.CandidateState.Candidate.OnlineProfile.Profile.RawText != raw {
		t.Fatal("模型压缩修改了服务器原件")
	}
}
