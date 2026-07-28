// Package zhaopin 文件作用：实现智联打招呼后的电话、微信、简历和追加消息动作。
package zhaopin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// RequestCandidateInfo 打开智联当前候选人聊天框，索要信息、发送消息并保证关闭聊天框。
func (r *Runtime) RequestCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate, request model.CandidateInfoRequest) (resultErr error) {
	if !request.RequestPhone && !request.RequestWechat && !request.RequestResume && strings.TrimSpace(request.Message) == "" {
		return nil
	}
	if err := common.CandidateAction(ctx, browser, cfg, candidate, "candidate.continue"); err != nil {
		return fmt.Errorf("点击智联“继续沟通”失败：%w", err)
	}
	opened, err := common.SelectorExists(ctx, browser, cfg, "candidate.chat_modal")
	if err != nil {
		return err
	}
	if !opened {
		return fmt.Errorf("智联继续沟通聊天框没有打开")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		if err := common.ClickOptional(cleanupCtx, browser, cfg, "candidate.chat_close"); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("关闭智联聊天框失败：%w", err)
		}
	}()
	if err = common.RequestCandidateInfo(ctx, browser, cfg, request); err != nil {
		return fmt.Errorf("智联索要候选人信息失败：%w", err)
	}
	return nil
}
