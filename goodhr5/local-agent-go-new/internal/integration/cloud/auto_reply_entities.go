// Package cloud 本文件以强类型协议读写单条SQLite实体，旧HTTP方法名仅作为内部兼容分发。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// getAutoReplyEntity 读取并解码一条实体，缺失时返回零值与false。
func getAutoReplyEntity[T any](tx *storage.AutoReplyTx, kind, id string) (item T, found bool, err error) {
	row, found, err := tx.Get(kind, id)
	if err != nil || !found {
		return item, found, err
	}
	err = json.Unmarshal(row.Data, &item)
	return item, found, err
}

// listAutoReplyEntities 使用索引条件读取实体，避免加载账号其他会话。
func listAutoReplyEntities[T any](tx *storage.AutoReplyTx, kind string, filters ...storage.AutoReplyFilter) ([]T, error) {
	rows, err := tx.List(kind, filters...)
	if err != nil {
		return nil, err
	}
	items := make([]T, 0, len(rows))
	for _, row := range rows {
		var item T
		if err = json.Unmarshal(row.Data, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// autoReplyFilter 创建明确的索引筛选条件。
func autoReplyFilter(field, value string) storage.AutoReplyFilter {
	return storage.AutoReplyFilter{Field: field, Value: value}
}

// handleLocalAutoReply 将敏感协议限制在本机，事务只读写当前业务实体。
func (c *Client) handleLocalAutoReply(ctx context.Context, method, path string, credentials AgentCredentials, payload, result any) (bool, error) {
	if !c.localAutoConfigured {
		return false, nil
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return true, err
	}
	route := strings.TrimPrefix(parsed.Path, autoReplyAgentBasePath)
	if !strings.HasPrefix(parsed.Path, autoReplyAgentBasePath+"/") || strings.HasPrefix(route, "/positions") || route == "/recommendations/completed" {
		return false, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return true, err
	}
	var response any
	var deferredError error
	now := time.Now().UTC()
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		switch route {
		case "/candidate-state":
			state, err := readLocalAutoReplyCandidateState(tx, AutoReplyCandidateLookup{PlatformID: parsed.Query().Get("platform_id"), PlatformAccountID: parsed.Query().Get("platform_account_id"), PlatformCandidateID: parsed.Query().Get("platform_candidate_id"), PlatformThreadID: parsed.Query().Get("platform_thread_id"), Phone: parsed.Query().Get("phone")})
			response = state
			return err
		case "/identities":
			var item CandidatePlatformIdentity
			if err := json.Unmarshal(encoded, &item); err != nil {
				return err
			}
			item.ID = "identity-" + autoReplyDigest(item.PlatformID+"|"+item.PlatformAccountID+"|"+item.PlatformCandidateID)
			old, exists, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", item.ID)
			if err != nil {
				return err
			}
			if exists {
				if item.CandidateID == "" {
					item.CandidateID = old.CandidateID
				}
				item.FirstSeenAt = old.FirstSeenAt
			} else {
				item.FirstSeenAt = now
			}
			item.LastSeenAt = now
			response = struct {
				Identity CandidatePlatformIdentity `json:"identity"`
			}{item}
			return tx.Put("identities", item.ID, item)
		case "/conversations":
			var item AutoReplyConversation
			if err := json.Unmarshal(encoded, &item); err != nil {
				return err
			}
			if item.ID == "" {
				item.ID = "conversation-" + autoReplyDigest(item.PlatformID+"|"+item.PlatformAccountID+"|"+item.PlatformThreadID)
			}
			old, exists, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", item.ID)
			if err != nil {
				return err
			}
			if exists {
				if old.PlatformID != item.PlatformID || old.PlatformAccountID != item.PlatformAccountID || old.PlatformThreadID != item.PlatformThreadID {
					return fmt.Errorf("会话账号归属发生变化，请重新识别")
				}
				item.CreatedAt = old.CreatedAt
				item.LastSyncedMessageKey = old.LastSyncedMessageKey
				item.LastCandidateMessageKey = old.LastCandidateMessageKey
				item.HistoryComplete = item.HistoryComplete || old.HistoryComplete
				item.MessageIdentityVersion = old.MessageIdentityVersion
				// 只有通过身份核验后的消息同步事务可以结束采集，保存页面元数据不能提前清除恢复意图。
				item.CaptureIntent = old.CaptureIntent
				// 简历待办只由领取、完成和补审事务改变，普通页面保存不能抹掉待办。
				item.ResumeRetry = old.ResumeRetry
				if item.CandidateID == "" {
					item.CandidateID = old.CandidateID
				}
			} else {
				item.CreatedAt = now
				item.MessageIdentityVersion = 0
			}
			item.UpdatedAt = now
			if err = tx.Put("conversations", item.ID, item); err != nil {
				return err
			}
			if item.CandidateID != "" {
				attachments, err := listAutoReplyEntities[localAttachment](tx, "attachments", autoReplyFilter("conversation_id", item.ID))
				if err != nil {
					return err
				}
				for _, a := range attachments {
					if a.CandidateID != item.CandidateID {
						a.CandidateID = item.CandidateID
						if err = tx.Put("attachments", a.ID, a); err != nil {
							return err
						}
					}
				}
			}
			response = struct {
				Conversation AutoReplyConversation `json:"conversation"`
			}{item}
			return nil

		case "/messages/sync":
			var input AutoReplyMessageSyncRequest
			if err := json.Unmarshal(encoded, &input); err != nil {
				return err
			}
			saved, isolated, err := syncLocalAutoReplyMessages(tx, input)
			if isolated {
				deferredError = fmt.Errorf("历史消息没有对上原游标，已保留本机，请人工核对后继续")
			}
			response = struct {
				Sync AutoReplyMessageSyncResult `json:"sync"`
			}{saved}
			return err

		case "/messages":
			items, err := listAutoReplyEntities[AutoReplyMessage](tx, "messages", autoReplyFilter("conversation_id", parsed.Query().Get("conversation_id")))
			response = struct {
				Messages []AutoReplyMessage `json:"messages"`
			}{items}
			return err
		case "/confirmations":
			if method == http.MethodGet {
				conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", parsed.Query().Get("conversation_id"))
				if err != nil {
					return err
				}
				items := []CandidateConfirmationItem{}
				if found && conversation.CandidateID != "" {
					all, err := listAutoReplyEntities[CandidateConfirmationItem](tx, "confirmations", autoReplyFilter("candidate_id", conversation.CandidateID), autoReplyFilter("position_id", conversation.PositionID))
					if err != nil {
						return err
					}
					for _, item := range all {
						if item.ArchivedAt == nil {
							items = append(items, item)
						}
					}
				}
				response = struct {
					Items []CandidateConfirmationItem `json:"confirmation_items"`
				}{items}
				return nil
			}
			var item CandidateConfirmationItem
			if err := json.Unmarshal(encoded, &item); err != nil {
				return err
			}
			saved, err := putLocalAutoReplyConfirmation(tx, item)
			response = struct {
				Item CandidateConfirmationItem `json:"confirmation_item"`
			}{saved}
			return err
		case "/attachments":
			items, err := readLocalAutoReplyAttachments(tx, parsed.Query().Get("candidate_id"), parsed.Query().Get("conversation_id"))
			response = struct {
				Attachments []StoredResumeAttachment `json:"attachments"`
			}{items}
			return err
		case "/ai-runs/start", "/ai-runs/finish":
			var item AutoReplyAIRun
			if err := json.Unmarshal(encoded, &item); err != nil {
				return err
			}
			if item.ID == "" {
				item.ID = "run-" + autoReplyDigest(item.TraceID)
				item.StartedAt = now
				item.CreatedAt = now
				item.Status = "running"
			}
			if route == "/ai-runs/finish" {
				item.CompletedAt = &now
			}
			if err = tx.Put("runs", item.ID, item); err != nil {
				return err
			}
			response = struct {
				Run AutoReplyAIRun `json:"ai_run"`
			}{item}
			return tx.PruneAudits()
		case "/tool-calls":
			var item AutoReplyToolCall
			if err := json.Unmarshal(encoded, &item); err != nil {
				return err
			}
			item.ID = "tool-" + autoReplyDigest(item.AIRunID+item.ToolCallID)
			response = struct {
				ToolCall AutoReplyToolCall `json:"tool_call"`
			}{item}
			return tx.Put("tools", item.ID, item)
		case "/suggestions":
			var item AutoReplyConfigSuggestion
			if err := json.Unmarshal(encoded, &item); err != nil {
				return err
			}
			if item.ID == "" {
				item.ID = "suggestion-" + autoReplyDigest(string(encoded))
			}
			item.CreatedAt = now
			item.Status = "pending"
			response = struct {
				Suggestion AutoReplyConfigSuggestion `json:"suggestion"`
			}{item}
			return tx.Put("suggestions", item.ID, item)
		default:
			return fmt.Errorf("自动回复敏感操作仅允许在本机执行：%s", route)
		}
	})
	if err != nil {
		return true, err
	}
	if deferredError != nil {
		return true, deferredError
	}
	if result != nil {
		raw, err := json.Marshal(response)
		if err != nil {
			return true, err
		}
		return true, json.Unmarshal(raw, result)
	}
	return true, nil
}

// readLocalAutoReplyCandidateState 优先使用精确线程和平台身份查询，手机号只在没有已绑定身份时使用。
func readLocalAutoReplyCandidateState(tx *storage.AutoReplyTx, lookup AutoReplyCandidateLookup) (AutoReplyCandidateState, error) {
	state := AutoReplyCandidateState{OK: true, Attachments: []StoredResumeAttachment{}}
	scope := []storage.AutoReplyFilter{autoReplyFilter("platform_id", lookup.PlatformID), autoReplyFilter("account_id", lookup.PlatformAccountID)}
	if lookup.PlatformCandidateID != "" {
		items, err := listAutoReplyEntities[CandidatePlatformIdentity](tx, "identities", append(scope, autoReplyFilter("platform_candidate_id", lookup.PlatformCandidateID))...)
		if err != nil {
			return state, err
		}
		if len(items) == 1 {
			state.Identity = &items[0]
		}
	}
	var conversations []AutoReplyConversation
	var err error
	if lookup.PlatformThreadID != "" {
		conversations, err = listAutoReplyEntities[AutoReplyConversation](tx, "conversations", append(scope, autoReplyFilter("thread_id", lookup.PlatformThreadID))...)
	} else if state.Identity != nil {
		conversations, err = listAutoReplyEntities[AutoReplyConversation](tx, "conversations", append(scope, autoReplyFilter("identity_id", state.Identity.ID))...)
	}
	if err != nil {
		return state, err
	}
	if len(conversations) > 1 {
		return state, fmt.Errorf("候选人对应多个会话，请先确认当前会话")
	}
	candidateID := ""
	if len(conversations) == 1 {
		state.Conversation = &conversations[0]
		state.Found = true
		candidateID = conversations[0].CandidateID
	}
	if candidateID == "" && state.Identity != nil {
		candidateID = state.Identity.CandidateID
	}
	if candidateID == "" && lookup.Phone != "" {
		items, err := scopedAutoReplyPhoneCandidates(tx, lookup.PlatformID, lookup.PlatformAccountID, lookup.Phone)
		if err != nil {
			return state, err
		}
		if len(items) > 1 {
			return state, fmt.Errorf("同手机号对应多份本地资料，请人工确认")
		}
		if len(items) == 1 {
			candidateID = items[0].ID
		}
	}
	if candidateID != "" {
		item, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", candidateID)
		if err != nil {
			return state, err
		}
		if found {
			state.Candidate = &item
			state.Found = true
		}
	}
	conversationID := ""
	if state.Conversation != nil {
		conversationID = state.Conversation.ID
		state.RecentMessageKeys = []string{state.Conversation.LastSyncedMessageKey}
	}
	state.Attachments, err = readLocalAutoReplyAttachments(tx, candidateID, conversationID)
	state.HasResumeAttachment = len(state.Attachments) > 0
	return state, err
}

// readLocalAutoReplyAttachments 合并当前候选人与会话的附件索引，始终去重且不扫描其他候选人。
func readLocalAutoReplyAttachments(tx *storage.AutoReplyTx, candidateID, conversationID string) ([]StoredResumeAttachment, error) {
	items := []StoredResumeAttachment{}
	seen := map[string]bool{}
	filters := []storage.AutoReplyFilter{}
	if conversationID != "" {
		filters = append(filters, autoReplyFilter("conversation_id", conversationID))
	}
	if candidateID != "" {
		filters = append(filters, autoReplyFilter("candidate_id", candidateID))
	}
	for _, filter := range filters {
		rows, err := listAutoReplyEntities[localAttachment](tx, "attachments", filter)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !seen[row.ID] {
				seen[row.ID] = true
				items = append(items, row.StoredResumeAttachment)
			}
		}
	}
	return items, nil
}

// putLocalAutoReplyConfirmation 保留询问次数等运行字段，防止旧AI快照覆盖新发送记录。
func putLocalAutoReplyConfirmation(tx *storage.AutoReplyTx, item CandidateConfirmationItem) (CandidateConfirmationItem, error) {
	if item.CandidateID == "" || item.PositionID == "" {
		return item, fmt.Errorf("确认项缺少候选人或岗位归属")
	}
	if item.ID == "" {
		item.ID = "condition-" + autoReplyDigest(item.CandidateID+"|"+item.PositionID+"|"+strings.Join(strings.Fields(item.Content), ""))
	}
	old, exists, err := getAutoReplyEntity[CandidateConfirmationItem](tx, "confirmations", item.ID)
	if err != nil {
		return item, err
	}
	now := time.Now().UTC()
	if exists {
		if old.CandidateID != item.CandidateID || old.PositionID != item.PositionID {
			return item, fmt.Errorf("确认项归属不一致")
		}
		item.CreatedAt = old.CreatedAt
		changedPositionCondition := old.SourceType == "position" && item.SourceType == "position" && old.PositionConditionID != "" && item.PositionConditionID == old.PositionConditionID && (old.Content != item.Content || old.ItemType != item.ItemType)
		if !changedPositionCondition {
			item.AskCount = old.AskCount
			item.LastAskedAt = old.LastAskedAt
		}
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return item, tx.Put("confirmations", item.ID, item)
}

// scopedAutoReplyPhoneCandidates 只在同招聘平台账号已关联的候选人中匹配手机号，禁止跨账号继承附件或确认。
func scopedAutoReplyPhoneCandidates(tx *storage.AutoReplyTx, platformID, accountID, phone string) ([]AutoReplyStoredCandidate, error) {
	matches, err := listAutoReplyEntities[AutoReplyStoredCandidate](tx, "candidates", autoReplyFilter("phone", phone))
	if err != nil {
		return nil, err
	}
	result := []AutoReplyStoredCandidate{}
	for _, candidate := range matches {
		filters := []storage.AutoReplyFilter{autoReplyFilter("platform_id", platformID), autoReplyFilter("account_id", accountID), autoReplyFilter("candidate_id", candidate.ID)}
		identities, err := tx.List("identities", filters...)
		if err != nil {
			return nil, err
		}
		if len(identities) > 0 {
			result = append(result, candidate)
			continue
		}
		conversations, err := tx.List("conversations", filters...)
		if err != nil {
			return nil, err
		}
		if len(conversations) > 0 {
			result = append(result, candidate)
		}
	}
	return result, nil
}
