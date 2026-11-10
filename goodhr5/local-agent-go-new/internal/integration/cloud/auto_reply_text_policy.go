// Package cloud 本文件冻结文字草稿采用的岗位配置，并在设备领取前核对话术空值与旧稿版本；原生资料请求不使用此限制。
package cloud

import (
	"context"
	"errors"
	"strings"

	"goodhr5/local-agent-go-new/internal/storage"
)

// ErrAutoReplyTextPolicyChanged 表示设备尚未领取的文字草稿已过期，调用方可以放下本稿，不能据此重发未知结果。
var ErrAutoReplyTextPolicyChanged = errors.New("岗位设置或话术已变化，这份旧回复先放下")

type autoReplyTextPolicyContextKey struct{}

// autoReplyTextPolicy 仅供服务器冻结当前岗位与指定话术，不把业务版本传给旧版设备。
type autoReplyTextPolicy struct {
	PositionID      string
	ContextRevision string
	PhraseID        string
}

// WithAutoReplyTextPolicy 在登记文字意图前绑定真实岗位快照；空话术编号表示普通答复，仍须核对完整配置版本。
func WithAutoReplyTextPolicy(ctx context.Context, snapshot AutoReplyPositionSnapshot, phraseID string) context.Context {
	policy := autoReplyTextPolicy{PositionID: snapshot.Position.ID, ContextRevision: AutoReplyGoalContextRevision(snapshot), PhraseID: strings.TrimSpace(phraseID)}
	return context.WithValue(ctx, autoReplyTextPolicyContextKey{}, policy)
}

// ValidateAutoReplyQueuedText 只核对尚未执行的文字意图；旧记录缺版本时保守取消，不替未知或已发送的意图解除防重。
func ValidateAutoReplyQueuedText(tx *storage.AutoReplyTx, id, conversationID, content string, snapshot AutoReplyPositionSnapshot) error {
	intent, found, err := getAutoReplyEntity[localAutoReplySend](tx, "sends", id)
	if err != nil {
		return err
	}
	if !found || intent.ConversationID != conversationID || intent.Content != content || intent.Status != "sending" {
		return ErrAutoReplyGoalChanged
	}
	conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
	if err != nil {
		return err
	}
	if !found || intent.ContextRevision == "" || snapshot.Position.ID != conversation.PositionID || snapshot.Config.PositionID != conversation.PositionID || snapshot.Position.PlatformID != conversation.PlatformID || !snapshot.Config.Enabled || !snapshot.Subscription.Active || !snapshot.Subscription.AllowAutoReply || intent.ContextRevision != AutoReplyGoalContextRevision(snapshot) || !autoReplyTextPhraseEnabled(snapshot.Config, intent.PhraseID) {
		return ErrAutoReplyTextPolicyChanged
	}
	return nil
}

// autoReplyTextPhraseEnabled 将明确清空、删除或缺失的话术视为停发；仅旧服务尚无话术库时保留原有简历兼容路径。
func autoReplyTextPhraseEnabled(config PositionAutoReplyConfig, phraseID string) bool {
	if phraseID == "" {
		return true
	}
	for _, phrase := range config.Phrases {
		if phrase.ID == phraseID {
			return !phrase.Deleted && strings.TrimSpace(phrase.Content) != ""
		}
	}
	return config.Phrases == nil && config.PhrasesVersion == "" && (phraseID == "receive_resume" || phraseID == "request_resume")
}
