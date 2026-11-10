// Package cloud 本文件在既有服务器会话实体中保存三类资料索要进度，不增加本机权威数据或独立数据库表。
package cloud

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// PlanAutoReplyInfoRequests 只在本轮入站仍有效时登记新提议，已有请求、拒绝和未知结果不能被重新规划。
func (c *Client) PlanAutoReplyInfoRequests(ctx context.Context, cred AgentCredentials, conversationID, basedOn string, request model.CandidateInfoRequest) error {
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found || conversation.LastCandidateMessageKey != basedOn {
			return fmt.Errorf("资料索要提议的候选人消息已变化")
		}
		if conversation.InfoRequests == nil {
			conversation.InfoRequests = map[string]AutoReplyInfoRequestProgress{}
		}
		for kind, selected := range map[string]bool{"resume": request.RequestResume, "phone": request.RequestPhone, "wechat": request.RequestWechat} {
			if selected && conversation.InfoRequests[kind].Status == "" {
				conversation.InfoRequests[kind] = AutoReplyInfoRequestProgress{Status: "planned", UpdatedAt: time.Now().UTC()}
			}
		}
		return tx.Put("conversations", conversation.ID, conversation)
	})
}

var nativePhoneValue = regexp.MustCompile(`^\+?[0-9]{6,20}$`)
var nativeWechatValue = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_\-]{4,31}$`)

// SaveAutoReplyNativeContact 仅接受已核对为received的原生动作回执，不从旧简历或其他候选人的资料猜测联系方式。
func (c *Client) SaveAutoReplyNativeContact(ctx context.Context, cred AgentCredentials, conversationID, kind string, result model.AutoReplyInfoRequestResult) error {
	if result.Status != model.AutoReplyInfoReceived || kind != "phone" && kind != "wechat" {
		return fmt.Errorf("联系方式缺少当前平台已获得的证明")
	}
	phone := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(result.Phone))
	wechat := strings.TrimSpace(result.Wechat)
	if kind == "phone" && !nativePhoneValue.MatchString(phone) || kind == "wechat" && !nativeWechatValue.MatchString(wechat) {
		return fmt.Errorf("平台联系方式尚未完整展示")
	}
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found || conversation.CandidateID == "" || conversation.InfoRequests[kind].Status != model.AutoReplyInfoReceived {
			return fmt.Errorf("联系方式与当前候选人状态不一致")
		}
		candidate, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", conversation.CandidateID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("当前候选人资料已不存在")
		}
		if kind == "wechat" {
			if candidate.Wechat != "" && candidate.Wechat != wechat {
				return fmt.Errorf("平台微信与已有资料不一致，请人工核对")
			}
			candidate.Wechat = wechat
		} else {
			if candidate.Phone != "" && candidate.Phone != phone {
				return fmt.Errorf("平台手机号与已有资料不一致，请人工核对")
			}
			others, err := scopedAutoReplyPhoneCandidates(tx, conversation.PlatformID, conversation.PlatformAccountID, phone)
			if err != nil {
				return err
			}
			for _, other := range others {
				if other.ID != candidate.ID {
					return fmt.Errorf("平台手机号已关联其他候选人，请人工核对")
				}
			}
			candidate.Phone = phone
			identity, found, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", conversation.PlatformIdentityID)
			if err != nil {
				return err
			}
			if found && identity.CandidateID == candidate.ID && identity.NormalizedPhone == "" {
				identity.NormalizedPhone = phone
				if err = tx.Put("identities", identity.ID, identity); err != nil {
					return err
				}
			}
		}
		return tx.Put("candidates", candidate.ID, candidate)
	})
}

// SetAutoReplyInfoRequestProgress 仅更新指定种类，避免旧会话快照覆盖其它种类或吞掉持久待办。
func (c *Client) SetAutoReplyInfoRequestProgress(ctx context.Context, cred AgentCredentials, conversationID, kind, status, basedOn string) error {
	if kind != "resume" && kind != "phone" && kind != "wechat" {
		return fmt.Errorf("资料索要种类不完整")
	}
	switch status {
	case "planned", "requested", "already_requested", "received", "declined", "deferred", "unavailable", "unknown":
	default:
		return fmt.Errorf("资料索要结果未确认")
	}
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("资料索要会话已不存在")
		}
		if conversation.InfoRequests == nil {
			conversation.InfoRequests = map[string]AutoReplyInfoRequestProgress{}
		}
		old := conversation.InfoRequests[kind]
		// 已取得的资料不能被迟到的失败回调降级；拒绝后只有真实取得才能推进。
		if old.Status == model.AutoReplyInfoReceived && status != model.AutoReplyInfoReceived || old.Status == model.AutoReplyInfoDeclined && status != model.AutoReplyInfoDeclined && status != model.AutoReplyInfoReceived {
			return nil
		}
		if (status == "planned" || status == "deferred" || status == "unavailable") && old.Status != "" && old.Status != "planned" && old.Status != "deferred" && old.Status != "unavailable" {
			return nil
		}
		conversation.InfoRequests[kind] = AutoReplyInfoRequestProgress{Status: status, LastAttemptMessageKey: basedOn, UpdatedAt: time.Now().UTC()}
		return tx.Put("conversations", conversation.ID, conversation)
	})
}
