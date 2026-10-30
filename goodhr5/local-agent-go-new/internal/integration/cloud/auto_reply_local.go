// Package cloud 本文件把自动回复的会话、简历、确认项和AI全文保存在本机，云端只负责配置与授权结构化结果。
package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// localAutoReplyData 按已认证账号隔离本地业务数据，登录凭证不进入持久化正文。
type localAutoReplyData struct {
	Identities           map[string]CandidatePlatformIdentity   `json:"identities"`
	Conversations        map[string]AutoReplyConversation       `json:"conversations"`
	Messages             map[string][]AutoReplyMessage          `json:"messages"`
	Candidates           map[string]AutoReplyStoredCandidate    `json:"candidates"`
	Confirmations        map[string]CandidateConfirmationItem   `json:"confirmations"`
	Attachments          map[string]StoredResumeAttachment      `json:"attachments"`
	AttachmentPaths      map[string]string                      `json:"attachment_paths"`
	Runs                 map[string]AutoReplyAIRun              `json:"runs"`
	Tools                map[string]AutoReplyToolCall           `json:"tools"`
	Suggestions          map[string]AutoReplyConfigSuggestion   `json:"suggestions"`
	Notifications        map[string]AutoReplyNotificationResult `json:"notifications"`
	CloudCandidates      map[string]string                      `json:"cloud_candidates"`
	Reports              map[string]json.RawMessage             `json:"reports"`
	CloudCandidateHashes map[string]string                      `json:"cloud_candidate_hashes"`
	ReportClaims         map[string]time.Time                   `json:"report_claims"`
	ReportAttempts       map[string]int                         `json:"report_attempts"`
	ReportPublished      map[string]bool                        `json:"report_published"`
}

// EnableLocalAutoReply 在程序组装时启用本机持久化，之后敏感自动回复请求不会发送云端。
func (c *Client) EnableLocalAutoReply(store *storage.Store, directory string) {
	c.localAutoStore, c.localAutoDir, c.localAutoConfigured = store, directory, true
}

// LocalAutoReplyEnabled 返回是否已组装正式本地数据存储。
func (c *Client) LocalAutoReplyEnabled() bool { return c != nil && c.localAutoStore != nil }

// autoReplyDigest 生成不含账号、消息或凭证明文的稳定本地键。
func autoReplyDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// localAutoReplyKey 优先使用启动检查核实的账号，未登录测试仅使用凭证摘要隔离。
func (c *Client) localAutoReplyKey(credentials AgentCredentials) string {
	owner := autoReplyDigest(credentials.Token)
	if authenticated, ok := c.localAutoOwners.Load(owner); ok {
		owner = authenticated.(string)
	}
	return "auto_reply:v1:" + autoReplyDigest(owner)
}

// loadLocalAutoReply 读取已有SQLite设置表，所有字段使用明确业务类型。
func (c *Client) loadLocalAutoReply(ctx context.Context, credentials AgentCredentials) (localAutoReplyData, error) {
	data := localAutoReplyData{Identities: map[string]CandidatePlatformIdentity{}, Conversations: map[string]AutoReplyConversation{}, Messages: map[string][]AutoReplyMessage{}, Candidates: map[string]AutoReplyStoredCandidate{}, Confirmations: map[string]CandidateConfirmationItem{}, Attachments: map[string]StoredResumeAttachment{}, AttachmentPaths: map[string]string{}, Runs: map[string]AutoReplyAIRun{}, Tools: map[string]AutoReplyToolCall{}, Suggestions: map[string]AutoReplyConfigSuggestion{}, Notifications: map[string]AutoReplyNotificationResult{}, CloudCandidates: map[string]string{}, Reports: map[string]json.RawMessage{}}
	encoded, err := c.localAutoStore.GetSetting(ctx, c.localAutoReplyKey(credentials))
	if err != nil {
		return data, err
	}
	if encoded != "" {
		err = json.Unmarshal([]byte(encoded), &data)
	}
	if data.Identities == nil {
		data.Identities = map[string]CandidatePlatformIdentity{}
	}
	if data.Conversations == nil {
		data.Conversations = map[string]AutoReplyConversation{}
	}
	if data.Messages == nil {
		data.Messages = map[string][]AutoReplyMessage{}
	}
	if data.Candidates == nil {
		data.Candidates = map[string]AutoReplyStoredCandidate{}
	}
	if data.Confirmations == nil {
		data.Confirmations = map[string]CandidateConfirmationItem{}
	}
	if data.Attachments == nil {
		data.Attachments = map[string]StoredResumeAttachment{}
	}
	if data.AttachmentPaths == nil {
		data.AttachmentPaths = map[string]string{}
	}
	if data.Runs == nil {
		data.Runs = map[string]AutoReplyAIRun{}
	}
	if data.Tools == nil {
		data.Tools = map[string]AutoReplyToolCall{}
	}
	if data.Suggestions == nil {
		data.Suggestions = map[string]AutoReplyConfigSuggestion{}
	}
	if data.Notifications == nil {
		data.Notifications = map[string]AutoReplyNotificationResult{}
	}
	if data.CloudCandidates == nil {
		data.CloudCandidates = map[string]string{}
	}
	if data.Reports == nil {
		data.Reports = map[string]json.RawMessage{}
	}
	if data.CloudCandidateHashes == nil {
		data.CloudCandidateHashes = map[string]string{}
	}
	if data.ReportClaims == nil {
		data.ReportClaims = map[string]time.Time{}
	}
	if data.ReportAttempts == nil {
		data.ReportAttempts = map[string]int{}
	}
	if data.ReportPublished == nil {
		data.ReportPublished = map[string]bool{}
	}
	return data, err
}

// saveLocalAutoReply 原子替换本地业务快照，原始聊天和AI全文不会进入云端日志。
func (c *Client) saveLocalAutoReply(ctx context.Context, credentials AgentCredentials, data localAutoReplyData) error {
	trimLocalAutoReplyAudits(&data)
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.localAutoStore.SetSetting(ctx, c.localAutoReplyKey(credentials), string(encoded))
}

// handleLocalAutoReply 在既有客户端边界接管敏感数据操作，配置读取仍走原云端接口。
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
	if c.localAutoStore == nil {
		return true, fmt.Errorf("本地自动回复存储尚未准备好，敏感数据不会上传")
	}
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return true, err
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return true, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return true, err
	}
	var response any
	changed := method != http.MethodGet
	now := time.Now().UTC()
	switch route {
	case "/candidate-state":
		lookup := AutoReplyCandidateLookup{PlatformID: parsed.Query().Get("platform_id"), PlatformAccountID: parsed.Query().Get("platform_account_id"), PlatformCandidateID: parsed.Query().Get("platform_candidate_id"), PlatformThreadID: parsed.Query().Get("platform_thread_id"), Phone: parsed.Query().Get("phone")}
		state, stateErr := localAutoReplyCandidateState(data, lookup)
		if stateErr != nil {
			return true, stateErr
		}
		response = state
	case "/identities":
		var item CandidatePlatformIdentity
		if err = json.Unmarshal(encoded, &item); err != nil {
			return true, err
		}
		item.ID = "identity-" + autoReplyDigest(item.PlatformID+"|"+item.PlatformAccountID+"|"+item.PlatformCandidateID)
		if previous, ok := data.Identities[item.ID]; ok {
			if item.CandidateID == "" {
				item.CandidateID = previous.CandidateID
			}
			item.FirstSeenAt = previous.FirstSeenAt
		} else {
			item.FirstSeenAt = now
		}
		item.LastSeenAt = now
		data.Identities[item.ID] = item
		response = struct {
			Identity CandidatePlatformIdentity `json:"identity"`
		}{item}
	case "/conversations":
		var item AutoReplyConversation
		if err = json.Unmarshal(encoded, &item); err != nil {
			return true, err
		}
		if item.ID == "" {
			item.ID = "conversation-" + autoReplyDigest(item.PlatformID+"|"+item.PlatformAccountID+"|"+item.PlatformThreadID)
		}
		if previous, ok := data.Conversations[item.ID]; ok {
			item.CreatedAt = previous.CreatedAt
			item.LastSyncedMessageKey, item.LastCandidateMessageKey = previous.LastSyncedMessageKey, previous.LastCandidateMessageKey
			item.HistoryComplete = item.HistoryComplete || previous.HistoryComplete
			if item.CandidateID == "" {
				item.CandidateID = previous.CandidateID
			}
		} else {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		data.Conversations[item.ID] = item
		if item.CandidateID != "" {
			for id, attachment := range data.Attachments {
				if attachment.ConversationID == item.ID {
					attachment.CandidateID = item.CandidateID
					data.Attachments[id] = attachment
				}
			}
		}
		response = struct {
			Conversation AutoReplyConversation `json:"conversation"`
		}{item}
	case "/messages/sync":
		var request AutoReplyMessageSyncRequest
		if err = json.Unmarshal(encoded, &request); err != nil {
			return true, err
		}
		conversation, ok := data.Conversations[request.ConversationID]
		if !ok {
			return true, fmt.Errorf("本地会话没找到")
		}
		messages := data.Messages[request.ConversationID]
		seen := map[string]bool{}
		for _, item := range messages {
			seen[localAutoReplyMessageKey(item)] = true
		}
		syncResult := AutoReplyMessageSyncResult{}
		for _, item := range request.Messages {
			key := localAutoReplyMessageKey(item)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			item.ConversationID = request.ConversationID
			item.ID = "message-" + autoReplyDigest(request.ConversationID+key)
			item.CreatedAt = now
			item.IngestedAt = now
			messages = append(messages, item)
			syncResult.Inserted++
			conversation.LastSyncedMessageKey = key
			if item.Direction == "candidate" {
				conversation.LastCandidateMessageKey = key
			}
		}
		if len(messages) > AutoReplyMaxHistoryMessages {
			messages = messages[len(messages)-AutoReplyMaxHistoryMessages:]
		}
		data.Messages[request.ConversationID] = messages
		conversation.HistoryComplete = request.HistoryComplete
		conversation.UpdatedAt = now
		data.Conversations[conversation.ID] = conversation
		syncResult.LastSyncedMessageKey = conversation.LastSyncedMessageKey
		syncResult.LastCandidateMessageKey = conversation.LastCandidateMessageKey
		response = struct {
			Sync AutoReplyMessageSyncResult `json:"sync"`
		}{syncResult}
	case "/messages":
		response = struct {
			Messages []AutoReplyMessage `json:"messages"`
		}{data.Messages[parsed.Query().Get("conversation_id")]}
	case "/confirmations":
		if method == http.MethodGet {
			items := []CandidateConfirmationItem{}
			conversation := data.Conversations[parsed.Query().Get("conversation_id")]
			for _, item := range data.Confirmations {
				if item.CandidateID == conversation.CandidateID && item.PositionID == conversation.PositionID && item.ArchivedAt == nil {
					items = append(items, item)
				}
			}
			sort.Slice(items, func(i, j int) bool {
				if items[i].CreatedAt.Equal(items[j].CreatedAt) {
					return items[i].ID < items[j].ID
				}
				return items[i].CreatedAt.Before(items[j].CreatedAt)
			})
			response = struct {
				Items []CandidateConfirmationItem `json:"confirmation_items"`
			}{items}
		} else {
			var item CandidateConfirmationItem
			if err = json.Unmarshal(encoded, &item); err != nil {
				return true, err
			}
			if item.ID == "" {
				item.ID = "condition-" + autoReplyDigest(item.CandidateID+"|"+item.PositionID+"|"+strings.Join(strings.Fields(item.Content), ""))
			}
			if previous, ok := data.Confirmations[item.ID]; ok {
				item.CreatedAt = previous.CreatedAt
			} else {
				item.CreatedAt = now
			}
			item.UpdatedAt = now
			data.Confirmations[item.ID] = item
			response = struct {
				Item CandidateConfirmationItem `json:"confirmation_item"`
			}{item}
		}
	case "/attachments":
		items := []StoredResumeAttachment{}
		for _, item := range data.Attachments {
			if (parsed.Query().Get("candidate_id") != "" && item.CandidateID == parsed.Query().Get("candidate_id")) || (parsed.Query().Get("conversation_id") != "" && item.ConversationID == parsed.Query().Get("conversation_id")) {
				items = append(items, item)
			}
		}
		response = struct {
			Attachments []StoredResumeAttachment `json:"attachments"`
		}{items}
	case "/ai-runs/start", "/ai-runs/finish":
		var item AutoReplyAIRun
		if err = json.Unmarshal(encoded, &item); err != nil {
			return true, err
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
		data.Runs[item.ID] = item
		response = struct {
			Run AutoReplyAIRun `json:"ai_run"`
		}{item}
	case "/tool-calls":
		var item AutoReplyToolCall
		if err = json.Unmarshal(encoded, &item); err != nil {
			return true, err
		}
		item.ID = "tool-" + autoReplyDigest(item.AIRunID+item.ToolCallID)
		data.Tools[item.ID] = item
		response = struct {
			ToolCall AutoReplyToolCall `json:"tool_call"`
		}{item}
	case "/suggestions":
		var item AutoReplyConfigSuggestion
		if err = json.Unmarshal(encoded, &item); err != nil {
			return true, err
		}
		item.ID = "suggestion-" + autoReplyDigest(string(encoded))
		item.CreatedAt = now
		item.Status = "pending"
		data.Suggestions[item.ID] = item
		response = struct {
			Suggestion AutoReplyConfigSuggestion `json:"suggestion"`
		}{item}
	default:
		return true, fmt.Errorf("自动回复敏感操作仅允许在本机执行：%s", route)
	}
	if changed {
		if err = c.saveLocalAutoReply(ctx, credentials, data); err != nil {
			return true, err
		}
	}
	if result != nil {
		output, err := json.Marshal(response)
		if err != nil {
			return true, err
		}
		return true, json.Unmarshal(output, result)
	}
	return true, nil
}

// localAutoReplyMessageKey 与既有平台游标使用一致的原消息编号优先规则。
func localAutoReplyMessageKey(item AutoReplyMessage) string {
	if item.PlatformMessageID != "" {
		return item.PlatformMessageID
	}
	return item.Fingerprint
}

// localAutoReplyCandidateState 只接受唯一平台身份或手机号，重名和多条会话时拒绝猜测。
func localAutoReplyCandidateState(data localAutoReplyData, lookup AutoReplyCandidateLookup) (AutoReplyCandidateState, error) {
	state := AutoReplyCandidateState{OK: true, Attachments: []StoredResumeAttachment{}}
	for _, identity := range data.Identities {
		if identity.PlatformID == lookup.PlatformID && identity.PlatformAccountID == lookup.PlatformAccountID && lookup.PlatformCandidateID != "" && identity.PlatformCandidateID == lookup.PlatformCandidateID {
			copy := identity
			state.Identity = &copy
		}
	}
	for _, conversation := range data.Conversations {
		same := conversation.PlatformID == lookup.PlatformID && conversation.PlatformAccountID == lookup.PlatformAccountID && ((lookup.PlatformThreadID != "" && conversation.PlatformThreadID == lookup.PlatformThreadID) || (state.Identity != nil && conversation.PlatformIdentityID == state.Identity.ID))
		if !same {
			continue
		}
		if state.Conversation != nil && state.Conversation.ID != conversation.ID {
			return state, fmt.Errorf("候选人对应多个会话，我先不猜")
		}
		copy := conversation
		state.Conversation = &copy
		state.Found = true
	}
	candidateID := ""
	if state.Conversation != nil {
		candidateID = state.Conversation.CandidateID
	}
	if candidateID == "" && state.Identity != nil {
		candidateID = state.Identity.CandidateID
	}
	if candidateID == "" && lookup.Phone != "" {
		for id, candidate := range data.Candidates {
			if candidate.Phone == lookup.Phone {
				if candidateID != "" && candidateID != id {
					return state, fmt.Errorf("同手机号对应多份本地简历，我先不合并")
				}
				candidateID = id
			}
		}
	}
	if candidate, ok := data.Candidates[candidateID]; ok {
		state.Candidate = &candidate
		state.Found = true
	}
	if state.Conversation != nil {
		for _, item := range data.Messages[state.Conversation.ID] {
			state.RecentMessageKeys = append(state.RecentMessageKeys, localAutoReplyMessageKey(item))
		}
	}
	for _, attachment := range data.Attachments {
		if (state.Conversation != nil && attachment.ConversationID == state.Conversation.ID) || (candidateID != "" && attachment.CandidateID == candidateID) {
			state.Attachments = append(state.Attachments, attachment)
		}
	}
	state.HasResumeAttachment = len(state.Attachments) > 0
	return state, nil
}

// trimLocalAutoReplyAudits 按完整AI运行裁剪最近千条审计，工具记录不会引用已移除的运行。
func trimLocalAutoReplyAudits(data *localAutoReplyData) {
	ids := make([]string, 0, len(data.Runs))
	for id := range data.Runs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return data.Runs[ids[i]].StartedAt.Before(data.Runs[ids[j]].StartedAt) })
	for id, call := range data.Tools {
		if _, ok := data.Runs[call.AIRunID]; !ok {
			delete(data.Tools, id)
		}
	}
	for _, id := range ids {
		if len(data.Runs) <= 1000 && len(data.Tools) <= 1000 {
			break
		}
		delete(data.Runs, id)
		for key, call := range data.Tools {
			if call.AIRunID == id {
				delete(data.Tools, key)
			}
		}
	}
}
