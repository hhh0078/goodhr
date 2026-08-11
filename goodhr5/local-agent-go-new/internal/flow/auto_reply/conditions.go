// Package auto_reply 本文件负责把岗位条件确定性同步为候选人长期确认项，并保留 AI 已有判断。
package auto_reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// syncPositionConfirmationItems 补齐岗位配置中的确认项，已有状态不回退为未确认。
func (f *Flow) syncPositionConfirmationItems(ctx context.Context, credentials cloud.AgentCredentials, position cloud.AutoReplyPositionSnapshot, conversation cloud.AutoReplyConversation, existing []cloud.CandidateConfirmationItem) ([]cloud.CandidateConfirmationItem, error) {
	if strings.TrimSpace(conversation.ID) == "" || strings.TrimSpace(conversation.CandidateID) == "" || strings.TrimSpace(position.Position.ID) == "" {
		return nil, fmt.Errorf("同步岗位条件需要完整的会话、候选人和岗位编号")
	}
	for _, condition := range position.Config.Conditions {
		if !condition.Enabled || strings.TrimSpace(condition.Content) == "" {
			continue
		}
		if index, found := findPositionConfirmation(existing, condition); found {
			current := existing[index]
			if current.PositionConditionID == condition.ID && current.ItemType == condition.Type && current.SourceType == "position" {
				continue
			}
			current.ConversationID = conversation.ID
			current.CandidateID = conversation.CandidateID
			current.PositionID = position.Position.ID
			current.PositionConditionID = condition.ID
			current.ItemType = condition.Type
			current.SourceType = "position"
			current.SourceRef = condition.ID
			saved, err := f.Cloud.SaveAutoReplyConfirmationItem(ctx, credentials, current)
			if err != nil {
				return nil, fmt.Errorf("关联岗位条件“%s”失败：%w", condition.Content, err)
			}
			existing[index] = saved
			continue
		}
		saved, err := f.Cloud.SaveAutoReplyConfirmationItem(ctx, credentials, cloud.CandidateConfirmationItem{
			ConversationID: conversation.ID, CandidateID: conversation.CandidateID,
			PositionID: position.Position.ID, PositionConditionID: condition.ID,
			ItemType: condition.Type, Content: strings.TrimSpace(condition.Content), Status: "pending",
			StatusReason: "简历和聊天暂时没有足够证据，需要继续确认",
			SourceType:   "position", SourceRef: condition.ID,
			EvidenceText: "暂未发现可直接判断的简历或聊天证据", Summary: "等待候选人确认",
			CreatedByKind: "system",
		})
		if err != nil {
			return nil, fmt.Errorf("同步岗位条件“%s”失败：%w", condition.Content, err)
		}
		existing = append(existing, saved)
	}
	return existing, nil
}

// findPositionConfirmation 按岗位条件编号、去重键或正文找到已经存在的长期确认项。
func findPositionConfirmation(items []cloud.CandidateConfirmationItem, condition cloud.PositionReplyCondition) (int, bool) {
	for index, item := range items {
		if condition.ID != "" && item.PositionConditionID == condition.ID {
			return index, true
		}
		if condition.DedupeKey != "" && item.DedupeKey == condition.DedupeKey {
			return index, true
		}
		if normalizeConfirmationContent(item.Content) == normalizeConfirmationContent(condition.Content) {
			return index, true
		}
	}
	return -1, false
}

// markConfirmationAsked 保存确认问题已经发送的次数和时间，供后续避免机械重复询问。
func (f *Flow) markConfirmationAsked(ctx context.Context, credentials cloud.AgentCredentials, conversation cloud.AutoReplyConversation, itemID string, items []cloud.CandidateConfirmationItem) error {
	for _, item := range items {
		if item.ID != strings.TrimSpace(itemID) {
			continue
		}
		now := time.Now().UTC()
		item.ConversationID = conversation.ID
		item.CandidateID = conversation.CandidateID
		item.AskCount++
		item.LastAskedAt = &now
		item.LastReviewedAt = &now
		if _, err := f.Cloud.SaveAutoReplyConfirmationItem(ctx, credentials, item); err != nil {
			return fmt.Errorf("确认问题已经发出，但询问次数没保存成功：%w", err)
		}
		return nil
	}
	return fmt.Errorf("确认问题对应的条件没有找到")
}
