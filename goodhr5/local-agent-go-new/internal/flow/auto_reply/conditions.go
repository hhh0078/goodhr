// Package auto_reply 本文件负责把岗位条件确定性同步为候选人长期确认项，并保留 AI 已有判断。
package auto_reply

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// syncPositionConfirmationItems 补齐岗位确认项；正文或类型改变时清空旧判断，删除或停用时归档。
func (f *Flow) syncPositionConfirmationItems(ctx context.Context, credentials cloud.AgentCredentials, position cloud.AutoReplyPositionSnapshot, conversation cloud.AutoReplyConversation, existing []cloud.CandidateConfirmationItem) ([]cloud.CandidateConfirmationItem, error) {
	if strings.TrimSpace(conversation.ID) == "" || strings.TrimSpace(conversation.CandidateID) == "" || strings.TrimSpace(position.Position.ID) == "" {
		return nil, fmt.Errorf("同步岗位条件需要完整的会话、候选人和岗位编号")
	}
	activeIDs := make([]string, 0, len(position.Config.Conditions))
	for _, condition := range position.Config.Conditions {
		if condition.Enabled {
			activeIDs = append(activeIDs, condition.ID)
		}
	}
	if err := f.Cloud.ArchiveAutoReplyPositionConfirmations(ctx, credentials, conversation.CandidateID, position.Position.ID, activeIDs); err != nil {
		return nil, err
	}
	active := make(map[string]bool, len(activeIDs))
	for _, id := range activeIDs {
		active[id] = true
	}
	filtered := make([]cloud.CandidateConfirmationItem, 0, len(existing))
	for _, item := range existing {
		if item.SourceType != "position" || active[item.PositionConditionID] {
			filtered = append(filtered, item)
		}
	}
	existing = filtered
	for _, condition := range position.Config.Conditions {
		if !condition.Enabled || strings.TrimSpace(condition.Content) == "" {
			continue
		}
		if index, found := findPositionConfirmation(existing, condition); found {
			current := existing[index]
			if current.PositionConditionID == condition.ID && current.ItemType == condition.Type && current.SourceType == "position" && strings.TrimSpace(current.Content) == strings.TrimSpace(condition.Content) {
				continue
			}
			current.ConversationID = conversation.ID
			current.CandidateID = conversation.CandidateID
			current.PositionID = position.Position.ID
			current.PositionConditionID = condition.ID
			if current.ItemType != condition.Type || strings.TrimSpace(current.Content) != strings.TrimSpace(condition.Content) {
				current.Status = "pending"
				current.StatusReason = "岗位条件已更新，需要重新确认"
				current.EvidenceText = ""
				current.SourceRef = condition.ID
				current.AskCount = 0
				current.LastAskedAt = nil
				current.LastAnsweredAt = nil
			}
			current.Content = strings.TrimSpace(condition.Content)
			current.ItemType = condition.Type
			current.SourceType = "position"
			if current.SourceRef == "" {
				current.SourceRef = condition.ID
			}
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
	activeItems, _ := configuredConfirmations(position, existing)
	return activeItems, nil
}

// configuredConfirmations 按 HR 当前启用的配置筛选可询问、可评估的确认项，并检查每个必要条件都有唯一对应记录。
func configuredConfirmations(position cloud.AutoReplyPositionSnapshot, items []cloud.CandidateConfirmationItem) ([]cloud.CandidateConfirmationItem, bool) {
	byID := make(map[string][]cloud.CandidateConfirmationItem)
	for _, item := range items {
		if item.ArchivedAt == nil && item.ID != "" && item.PositionID == position.Position.ID && item.SourceType == "position" && item.PositionConditionID != "" {
			byID[item.PositionConditionID] = append(byID[item.PositionConditionID], item)
		}
	}
	active := make([]cloud.CandidateConfirmationItem, 0, len(position.Config.Conditions))
	complete := true
	for _, condition := range position.Config.Conditions {
		if !condition.Enabled {
			continue
		}
		rows := byID[condition.ID]
		if len(rows) != 1 || rows[0].ItemType != condition.Type || strings.TrimSpace(rows[0].Content) != strings.TrimSpace(condition.Content) {
			if condition.Type == "required" || condition.Type == "confirm" {
				complete = false
			}
			continue
		}
		active = append(active, rows[0])
	}
	return active, complete
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
	if strings.TrimSpace(itemID) == "" {
		return fmt.Errorf("确认问题缺少条件编号")
	}
	return f.Cloud.IncrementAutoReplyConfirmationAsked(ctx, credentials, itemID)
}
