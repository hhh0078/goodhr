// Package auto_reply 本文件把全部联系人视图与本机按账号保存的水位对照，选出需要打开核实的会话。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// reviewAllContacts 仅记录任务范围内的联系人摘要，待核对项继续经过原有采集、消息去重与发送回执流程。
func (f *Flow) reviewAllContacts(ctx context.Context, prepared shared.PreparedTask, rows []model.Conversation) ([]model.Conversation, map[string]cloud.AutoReplyListReview, error) {
	if f.Cloud == nil || !f.Cloud.LocalAutoReplyEnabled() {
		return nil, nil, fmt.Errorf("本机消息记录还没有准备好，暂不根据联系人列表回复")
	}
	// 没有读到任何实际联系人，可能是抽屉刚打开尚未渲染。该轮保留原水位和已登记待办。
	if len(rows) == 0 {
		return nil, nil, nil
	}
	target := prepared.Request.AutoReplyTarget
	scope := ""
	if target != nil {
		scope = target.PlatformThreadID
	}
	byThread := make(map[string]model.Conversation, len(rows))
	observations := make([]cloud.AutoReplyListObservation, 0, len(rows))
	for _, row := range rows {
		if !targetConversationAllowed(target, row) {
			continue
		}
		thread := persistentConversationThreadID(row.PlatformThreadID, row.Key)
		signature := row.Fields["contact_list_signature"]
		if thread == "" || signature == "" {
			return nil, nil, fmt.Errorf("有一条联系人记录的身份或摘要不完整，稍后重新查看")
		}
		obs := cloud.AutoReplyListObservation{ThreadID: thread, Signature: signature}
		if preview := strings.TrimSpace(row.Summary); preview != "" {
			obs.PreviewDigest = fmt.Sprintf("%x", sha256.Sum256([]byte(preview)))
		}
		if t, err := time.Parse(time.RFC3339, row.Fields["contact_activity_at"]); err == nil {
			obs.ActivityTime = &t
		}
		observations = append(observations, obs)
		byThread[thread] = row
	}
	reviews, err := f.Cloud.ObserveAutoReplyList(ctx, credentials(prepared), prepared.Platform.ID, prepared.Request.PlatformAccountID, scope, observations, time.Now())
	if err != nil {
		return nil, nil, err
	}
	pending := make([]model.Conversation, 0, len(reviews))
	byReview := make(map[string]cloud.AutoReplyListReview, len(reviews))
	for _, review := range reviews {
		if row, found := byThread[review.ThreadID]; found {
			pending = append(pending, row)
			byReview[review.ThreadID] = review
		}
	}
	return pending, byReview, nil
}

// prioritizeDueListReview 在合并恢复任务后保留列表本轮的轮查名额，沿用队列里的完整采集意图。
func prioritizeDueListReview(queue []model.Conversation, reviews map[string]cloud.AutoReplyListReview) {
	if len(reviews) == 0 {
		return
	}
	for i, item := range queue {
		if reviews[persistentConversationThreadID(item.PlatformThreadID, item.Key)].ReserveTurn {
			copy(queue[1:i+1], queue[:i])
			queue[0] = item
			return
		}
	}
}
