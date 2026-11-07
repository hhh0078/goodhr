// Package auto_reply 本文件在原聊天框内短暂等候送达证据，只读回执，不追加按键、点击或重发。
package auto_reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// confirmOpenReplyDelivery 正常气泡一次回读即可通过；原消息未变时短等，新来信抢到末尾时只在真实锚点之后寻找己方新气泡。
func (f *Flow) confirmOpenReplyDelivery(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, snapshot model.AutoReplyConversationSnapshot, content string) (model.ConversationMessage, error) {
	anchor := snapshot.ExpectedSendMessageKey
	if anchor == "" {
		return model.ConversationMessage{}, fmt.Errorf("缺少发送前的消息编号，暂无法核对送达")
	}
	key := func(row model.ConversationMessage) string { return firstNonEmpty(row.PlatformMessageID, row.Key) }
	matches := func(row model.ConversationMessage) bool {
		id := key(row)
		return id != "" && id != anchor && strings.EqualFold(row.Direction, "self") && strings.TrimSpace(row.TextContent) == content
	}
	readCtx := ctx
	pollStarted := false
	for {
		latest, opened, err := runtime.ReadOpenAutoReplyLatestMessage(readCtx, f.Browser, prepared.Platform, snapshot)
		if err != nil {
			return model.ConversationMessage{}, err
		}
		if !opened || key(latest) == "" {
			return model.ConversationMessage{}, fmt.Errorf("原聊天框或消息编号无法确认")
		}
		if matches(latest) {
			return latest, nil
		}
		if !pollStarted {
			var cancel context.CancelFunc
			readCtx, cancel = context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()
			pollStarted = true
		}
		if key(latest) != anchor {
			if !strings.EqualFold(latest.Direction, "candidate") {
				return model.ConversationMessage{}, fmt.Errorf("出现了其他己方消息，暂无法确认这次送达")
			}
			rows, _, err := runtime.ReadAutoReplyMessages(readCtx, f.Browser, prepared.Platform, snapshot, nil, 8)
			if err != nil {
				return model.ConversationMessage{}, err
			}
			seenAnchor := false
			var receipt model.ConversationMessage
			for _, row := range rows {
				if key(row) == anchor {
					seenAnchor = true
					receipt = model.ConversationMessage{}
					continue
				}
				if seenAnchor && matches(row) {
					receipt = row
				}
			}
			if key(receipt) != "" {
				return receipt, nil
			}
		}
		select {
		case <-readCtx.Done():
			return model.ConversationMessage{}, readCtx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
