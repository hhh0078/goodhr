// Package liepin 本文件从会话末尾按需扩窗核实删除后的消息，避免为一条新留言反复翻阅整段旧聊天。
package liepin

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// readLiepinRecontactTail 从末尾少量消息开始，每次只在时间边界和稳定增量锚点均未出现时扩大窗口。
func readLiepinRecontactTail(ctx context.Context, browser model.Browser, cfg model.Config, after time.Time, known []string, limit int) ([]contract.FindAllItem, liepinConversationHistory, error) {
	for size := min(8, limit); ; size = min(limit, size*2) {
		items, err := common.ReadConfiguredConversationMessageTail(ctx, browser, cfg, size)
		if err != nil {
			return nil, liepinConversationHistory{}, err
		}
		if len(items) == 0 {
			return nil, liepinConversationHistory{}, fmt.Errorf("新会话暂时未读到消息")
		}
		messages, err := liepinMessages(items)
		if err != nil {
			return nil, liepinConversationHistory{}, err
		}
		_, reached := liepinMessagesAfterDeletion(messages, after)
		boundary := liepinKnownMessageBoundary(messages, known)
		stable := boundary >= 0 && (messages[boundary].PlatformMessageID != "" || boundary >= liepinLeadingMessageGroupEnd(messages))
		if reached || stable || items[0].Index == 0 || len(items) < size || size >= limit {
			return items, liepinConversationHistory{Messages: messages}, nil
		}
	}
}
