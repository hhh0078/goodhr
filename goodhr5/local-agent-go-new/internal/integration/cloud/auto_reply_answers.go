// Package cloud 本文件读取已核实的候选人补答排队记录，与服务器确认领取及实际送达。
package cloud

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"net/http"
	"net/url"
)

// AutoReplyAnswer 是已归档公开事实对原会话的一次补答；ID在重启、断线和重试间保持不变。
type AutoReplyAnswer struct {
	ID                string `json:"id"`
	ConversationID    string `json:"conversation_id"`
	PositionID        string `json:"position_id"`
	PlatformID        string `json:"platform_id"`
	PlatformAccountID string `json:"platform_account_id"`
	PlatformThreadID  string `json:"platform_thread_id"`
	SourceMessageKey  string `json:"source_message_key"`
	Answer            string `json:"answer"`
}

// PendingAutoReplyAnswers 仅读取本次设备授权对应的招聘平台账号及已归档、待送达的事实。
func (c *Client) PendingAutoReplyAnswers(ctx context.Context, cred AgentCredentials, platform, account string) ([]AutoReplyAnswer, error) {
	var resp struct {
		Answers []AutoReplyAnswer `json:"answers"`
	}
	q := url.Values{"platform_id": {platform}, "platform_account_id": {account}}
	err := c.doWithMachineID(ctx, http.MethodGet, autoReplyAgentBasePath+"/questions/pending-answers?"+q.Encode(), cred.Token, cred.MachineID, nil, &resp)
	return resp.Answers, err
}

// ClaimAutoReplyAnswer 在操作页面前重新确认答复版本、待办和来源仍然有效。
func (c *Client) ClaimAutoReplyAnswer(ctx context.Context, cred AgentCredentials, a AutoReplyAnswer) (bool, error) {
	var resp struct {
		Claimed bool `json:"claimed"`
	}
	err := c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/questions/answer-claim", cred.Token, cred.MachineID, map[string]string{"id": a.ID, "conversation_id": a.ConversationID}, &resp)
	return resp.Claimed, err
}

// FinishAutoReplyAnswer 在实际外发键通过核验后才把补答置为送达；结果不明转入人工处理。
func (c *Client) FinishAutoReplyAnswer(ctx context.Context, cred AgentCredentials, a AutoReplyAnswer, status, outbound string) error {
	if status != "sent" && status != "manual" || status == "sent" && outbound == "" {
		return fmt.Errorf("补答缺少有效的发送凭据")
	}
	return c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/questions/answer-receipt", cred.Token, cred.MachineID, map[string]string{"id": a.ID, "conversation_id": a.ConversationID, "status": status, "outbound_message_key": outbound}, nil)
}

// AutoReplySendReceipt 仅按此会话独立发送账本的稳定键读取既有回执，不根据消息正文猜测。
func (c *Client) AutoReplySendReceipt(ctx context.Context, cred AgentCredentials, conversation, key string, sequence int) (status, outbound string, err error) {
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		row, ok, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", autoReplySendID(conversation, key, sequence))
		if err == nil && ok {
			status, outbound = row.Status, row.OutboundMessageKey
		}
		return err
	})
	return
}

// AutoReplyAnswerConversation 从权威账号范围按精确会话编号读取，无记录表示原候选人已清理。
func (c *Client) AutoReplyAnswerConversation(ctx context.Context, cred AgentCredentials, id string) (conv AutoReplyConversation, found bool, err error) {
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		var e error
		conv, found, e = getAutoReplyEntity[AutoReplyConversation](tx, "conversations", id)
		return e
	})
	return
}
