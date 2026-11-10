// Package auto_reply 本文件回归真实模型拼造来源编号及错引旧消息的情况，只允许本轮唯一候选原文绑定真实来源。
package auto_reply

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

const run10CandidateRef = "f039954c7d4560f8d19975e862c1af39"
const run10CandidateQuote = "我可以到成都上班，学历是大专。"

// confirmationSourceState 使用run10真实非敏感原话和run11旧消息编号构造同一会话的模型可见上下文。
func confirmationSourceState() *toolExecutionState {
	s := proposalState()
	current := cloud.AutoReplyMessage{ID: "server-current", Fingerprint: run10CandidateRef, ConversationID: s.input.Conversation.ID, Direction: "candidate", TextContent: run10CandidateQuote + "我们可以先交换电话和微信，附件简历我稍后再发。"}
	old := cloud.AutoReplyMessage{ID: "server-old", Fingerprint: "6ce2096499d4cd68e629db9820d2f120", ConversationID: s.input.Conversation.ID, Direction: "candidate", TextContent: "请问具体工作内容？"}
	s.input.Messages = []cloud.AutoReplyMessage{old, current}
	s.input.PendingMessages = []cloud.AutoReplyMessage{current}
	s.input.BasedOnMessageKey = run10CandidateRef
	return s
}

// TestRun10UnknownSourceBindsCanonicalGoalEvidence 验证run10三种错误引用可唯一绑定真实消息，新目标与已有状态更新和工具回执均保存真实来源。
func TestRun10UnknownSourceBindsCanonicalGoalEvidence(t *testing.T) {
	for _, ref := range []string{"candidate-b8b02d35337d8fd776e7a8efb16c299a:" + run10CandidateRef, "candidate_message:" + run10CandidateRef, "candidate_message"} {
		for _, fresh := range []bool{false, true} {
			s := confirmationSourceState()
			item := confirmationToolItem{ID: "location", ItemType: "required", Content: "可到成都到岗", Status: "matched", StatusReason: "候选人已明确说明", SourceRef: ref, EvidenceText: run10CandidateQuote}
			if fresh {
				item.ID, item.ItemType, item.Status = "", "confirm", "pending"
			} else {
				s.confirmations = []cloud.CandidateConfirmationItem{{ID: item.ID, ConversationID: s.input.Conversation.ID, CandidateID: s.input.Conversation.CandidateID, PositionID: s.input.Position.Position.ID, SourceType: "position", ItemType: item.ItemType, Content: item.Content, Status: "pending"}}
			}
			result, err := s.upsertConfirmations(context.Background(), proposalCall(t, []confirmationToolItem{item}))
			if err != nil || len(s.confirmationUpdates) != 1 || s.confirmationUpdates[0].SourceRef != run10CandidateRef || s.confirmationUpdates[0].EvidenceText != run10CandidateQuote {
				t.Fatalf("未保存本轮唯一真实来源：fresh=%t ref=%s updates=%+v err=%v", fresh, ref, s.confirmationUpdates, err)
			}
			var receipt confirmationToolArgs
			if err = json.Unmarshal(result, &receipt); err != nil || len(receipt.Items) != 1 || receipt.Items[0].SourceRef != run10CandidateRef {
				t.Fatal("工具回执没有返回真实来源", string(result), err)
			}
			if item.SourceRef != ref || s.input.PendingMessages[0].Fingerprint != run10CandidateRef {
				t.Fatal("来源解析修改了原模型参数或原始消息")
			}
		}
	}
}

// TestSourceBindingRejectsConflictAndAmbiguity 验证有效旧引用与错误资料类别不能被偷偷改换，只在唯一真实原文存在时提供精确纠错编号。
func TestSourceBindingRejectsConflictAndAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name, ref, quote string
		hint             bool
		change           func(*toolExecutionState)
	}{
		{"run11有效旧消息错原文", "6ce2096499d4cd68e629db9820d2f120", run10CandidateQuote, true, nil},
		{"run11错用简历", "resume", run10CandidateQuote, true, nil},
		{"错用在线资料", "online_profile", run10CandidateQuote, true, nil},
		{"错用岗位资料", "position", run10CandidateQuote, true, nil},
		{"伪造原文", "candidate_message", "我可以在北京上班", false, nil},
		{"空原文", "candidate_message", "", false, nil},
		{"缺来源仍需纠错", "", run10CandidateQuote, true, nil},
		{"不同消息重复原文", "candidate_message", run10CandidateQuote, false, func(s *toolExecutionState) {
			s.input.Messages = append(s.input.Messages, cloud.AutoReplyMessage{Fingerprint: "second-candidate-message", ConversationID: s.input.Conversation.ID, Direction: "candidate", TextContent: run10CandidateQuote})
		}},
		{"其他候选人会话", "candidate_message", run10CandidateQuote, false, func(s *toolExecutionState) {
			s.input.PendingMessages[0].ConversationID = "other-conversation"
		}},
		{"己方原文不能兜底", "candidate_message", run10CandidateQuote, false, func(s *toolExecutionState) {
			s.input.PendingMessages[0].Direction = "self"
		}},
		{"有效己方引用不能改绑", "6ce2096499d4cd68e629db9820d2f120", run10CandidateQuote, true, func(s *toolExecutionState) {
			s.input.Messages[0].Direction = "self"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := confirmationSourceState()
			if tc.change != nil {
				tc.change(s)
			}
			for _, motivation := range []bool{false, true} {
				ref, err := s.normalizeConfirmationSource(tc.ref, tc.quote, motivation)
				if err == nil || ref != "" {
					t.Fatal("冲突或不唯一证据被自动接受", ref, err)
				}
				hint := "该原文唯一对应的source_ref=" + run10CandidateRef + "，请原样使用"
				if strings.Contains(err.Error(), hint) != tc.hint {
					t.Fatal("纠错提示没有精确对应本轮唯一真实原文", err)
				}
				if strings.Contains(err.Error(), run10CandidateQuote) || strings.Contains(err.Error(), "other-conversation") {
					t.Fatal("纠错提示回显了原文或其他会话信息", err)
				}
			}
		})
	}
}

// TestSourceAliasesKeepPromptCanonicalReference 验证既有ID及fingerprint均仍可用，输出采用与模型消息视图相同的来源优先级。
func TestSourceAliasesKeepPromptCanonicalReference(t *testing.T) {
	s := confirmationSourceState()
	for _, ref := range []string{"server-current", run10CandidateRef} {
		got, err := s.normalizeConfirmationSource(ref, run10CandidateQuote, false)
		if err != nil || got != run10CandidateRef {
			t.Fatal("兼容的真实来源被拒绝", got, err)
		}
	}
	s.input.PendingMessages[0].PlatformMessageID = "native-current"
	for _, ref := range []string{"server-current", run10CandidateRef, "native-current"} {
		got, err := s.normalizeConfirmationSource(ref, run10CandidateQuote, false)
		if err != nil || got != "native-current" {
			t.Fatal("来源优先级与prompt不一致", got, err)
		}
	}
}
