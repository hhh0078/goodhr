// Package auto_reply 本文件校验 AI 动态目标的新增、修改和归档提议，岗位必要条件仍由 HR 控制。
package auto_reply

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

type archiveGoalToolArgs struct {
	IDs    []string `json:"ids"`
	Reason string   `json:"reason"`
}

type goalFollowupToolItem struct {
	Content            string `json:"content"`
	ConfirmationItemID string `json:"confirmation_item_id"`
}

// validateGoalQuestion 统一检查普通回复和 HR 待确认回执后的独立追问，已问未答或拒绝沟通时不再打扰。
func (s *toolExecutionState) validateGoalQuestion(item ReplyMessage) error {
	goal, found := s.confirmationByID(item.ConfirmationItemID)
	if !found || !cloud.AutoReplyGoalCanAsk(goal) {
		return argumentError("确认消息必须关联一条仍未确认且尚未问过的 required 或 confirm 目标")
	}
	if item.Content == "" || len([]rune(item.Content)) > maxAutoReplyMessageRunes || strings.ContainsAny(item.Content, "{}｛｝") {
		return argumentError("追问正文为空、超过200字或还带有未填写变量")
	}
	if err := checkPhoneReply(s.input.PhoneContact, s.input.Position.Config, item.Content); err != nil {
		return err
	}
	if candidateEndedContact(s.input.PendingMessages) {
		return argumentError("候选人已拒绝继续沟通，本轮不要再追问")
	}
	return nil
}

// goalContentExists 在当前活跃目标中合并同正文项目，模型须优先按语义引用已有目标编号。
func (s *toolExecutionState) goalContentExists(content string) (cloud.CandidateConfirmationItem, bool) {
	for _, goal := range s.confirmations {
		if goal.ArchivedAt == nil && normalizeConfirmationContent(goal.Content) == normalizeConfirmationContent(content) {
			return goal, true
		}
	}
	return cloud.CandidateConfirmationItem{}, false
}

// validateGoalMotivation 要求新增或改写的沟通任务有当前岗位、简历或来信依据，不能凭岗位名称造招聘门槛。
func (s *toolExecutionState) validateGoalMotivation(item confirmationToolItem) error {
	ref, quote := strings.TrimSpace(item.SourceRef), strings.TrimSpace(item.EvidenceText)
	if quote == "" {
		return argumentError("新目标需要说明为何要问：source_ref 使用真实消息、resume 或 position，evidence_text 引用依据原文")
	}
	if ref == "position" && strings.Contains(s.input.Position.Config.PositionDescription, quote) {
		return nil
	}
	if ref == "resume" {
		if s.input.Resume != nil && strings.Contains(s.input.Resume.OnlineResumeText, quote) {
			return nil
		}
		if s.input.CandidateState.Candidate != nil && s.input.CandidateState.Candidate.ResumeStructured {
			b, _ := json.Marshal(s.input.CandidateState.Candidate)
			if strings.Contains(string(b), quote) {
				return nil
			}
		}
	}
	for _, m := range s.input.Messages {
		if m.Direction == "candidate" && (ref == m.ID || ref == m.PlatformMessageID || ref == m.Fingerprint) && strings.Contains(m.TextContent, quote) {
			return nil
		}
	}
	return argumentError("新目标依据没有对应到本轮岗位说明、候选人消息或简历，请引用真实原文")
}

// proposeGoalUpdates 整批验证后才合并提议；新项目只作沟通确认，不能变成推荐的硬门槛。
func (s *toolExecutionState) proposeGoalUpdates(call ai.ToolCall) (json.RawMessage, error) {
	var args confirmationToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}
	if len(args.Items) == 0 || len(args.Items) > 50 {
		return nil, argumentError("items 需要包含1到50条确认项")
	}
	saved := make([]cloud.CandidateConfirmationItem, 0, len(args.Items))
	seenIDs, seenContent := map[string]bool{}, map[string]bool{}
	for index, item := range args.Items {
		if err := validateConfirmationToolItem(item); err != nil {
			return nil, argumentError(fmt.Sprintf("第%d条确认项%s", index+1, err.Error()))
		}
		id, body := strings.TrimSpace(item.ID), normalizeConfirmationContent(item.Content)
		if id != "" && seenIDs[id] || seenContent[body] {
			return nil, argumentError("同一批目标出现重复编号或相同正文，请合并后再提交")
		}
		seenIDs[id], seenContent[body] = true, true
		var result cloud.CandidateConfirmationItem
		found := false
		if strings.TrimSpace(item.ID) != "" {
			result, found = s.confirmationByID(item.ID)
			if !found {
				return nil, argumentError("目标编号不存在；新目标的 id 应留空，不能借用旧编号")
			}
		} else {
			result, found = s.goalContentExists(item.Content)
		}
		if found && result.ArchivedAt != nil {
			return nil, argumentError("这条目标已删除，不应重新追问")
		}
		if found && item.PositionConditionID != "" && item.PositionConditionID != result.PositionConditionID {
			return nil, argumentError("岗位条件编号不一致")
		}
		editable := found && result.SourceType == "ai" && result.CreatedByKind == "ai" && result.ConversationID == s.input.Conversation.ID
		changed := found && (result.ItemType != item.ItemType || strings.TrimSpace(result.Content) != strings.TrimSpace(item.Content))
		if found && changed && !editable {
			return nil, argumentError("HR 岗位条件只能更新判断，不能改变正文、类型或放宽条件")
		}
		if !found || changed {
			if item.ItemType != "confirm" {
				return nil, argumentError("AI 新增或修改的沟通目标只能使用 confirm；不能增设 required 门槛")
			}
			if item.Status != "pending" {
				return nil, argumentError("新问题或改写后的目标先保持 pending，不能套用旧问题的结论")
			}
			if err := s.validateGoalMotivation(item); err != nil {
				return nil, err
			}
		}
		if !found {
			count := 0
			for _, g := range s.confirmations {
				if g.SourceType == "ai" && g.ArchivedAt == nil {
					count++
				}
			}
			if count+len(saved) >= 50 {
				return nil, argumentError("当前待沟通目标较多，请先合并或完成已有目标")
			}
			id, err := newAutoReplyTraceID("goal")
			if err != nil {
				return nil, err
			}
			result = cloud.CandidateConfirmationItem{ID: id, ConversationID: s.input.Conversation.ID, CandidateID: s.input.Conversation.CandidateID, PositionID: s.input.Position.Position.ID, SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", RequireAbsent: true}
		} else if !result.RequireAbsent && result.ExpectedUpdatedAt == nil {
			old := result.UpdatedAt
			result.ExpectedUpdatedAt = &old
		}
		result.AIProposal = true
		if changed && result.LastAskedAt != nil && result.LastAskedContentKey == "" {
			result.LastAskedContentKey = cloud.AutoReplyGoalContentKey(result)
		}
		result.Content, result.ItemType = strings.TrimSpace(item.Content), item.ItemType
		result.Status, result.StatusReason = item.Status, strings.TrimSpace(item.StatusReason)
		result.SourceRef, result.EvidenceText = strings.TrimSpace(item.SourceRef), strings.TrimSpace(item.EvidenceText)
		result.Summary = result.StatusReason
		now := time.Now().UTC()
		result.LastReviewedAt = &now
		if result.Status != "pending" && result.SourceRef == s.input.BasedOnMessageKey {
			result.LastAnsweredAt = &now
		}
		if err := s.validateConfirmationEvidence(result); err != nil {
			return nil, err
		}
		saved = append(saved, result)
	}
	for _, goal := range saved {
		s.addConfirmationProposal(goal)
		s.replaceConfirmation(goal)
	}
	return marshalToolResult(confirmationToolArgs{Items: conditionsForTool(saved)})
}

// archiveGoals 仅归档 AI 自建目标；旧发送提议会因目标版本及归档状态失效，历史审计仍保留。
func (s *toolExecutionState) archiveGoals(call ai.ToolCall) (json.RawMessage, error) {
	var args archiveGoalToolArgs
	if err := decodeToolArguments(call, &args); err != nil {
		return nil, err
	}
	if len(args.IDs) == 0 || len(args.IDs) > 50 || strings.TrimSpace(args.Reason) == "" || len([]rune(args.Reason)) > 500 {
		return nil, argumentError("请提供1至50个目标编号及简短归档原因")
	}
	var saved []cloud.CandidateConfirmationItem
	seen := map[string]bool{}
	for _, id := range args.IDs {
		goal, ok := s.confirmationByID(id)
		if !ok || seen[id] || goal.SourceType != "ai" || goal.CreatedByKind != "ai" || goal.ConversationID != s.input.Conversation.ID {
			return nil, argumentError("只能删除本会话 AI 自建的目标，不能删除 HR 岗位必要条件")
		}
		seen[id] = true
		if !goal.RequireAbsent && goal.ExpectedUpdatedAt == nil {
			old := goal.UpdatedAt
			goal.ExpectedUpdatedAt = &old
		}
		now := time.Now().UTC()
		goal.ArchivedAt, goal.AIProposal, goal.StatusReason = &now, true, strings.TrimSpace(args.Reason)
		saved = append(saved, goal)
	}
	for _, goal := range saved {
		s.addConfirmationProposal(goal)
		s.replaceConfirmation(goal)
	}
	return marshalToolResult(struct {
		Archived []string `json:"archived_ids"`
	}{args.IDs})
}
