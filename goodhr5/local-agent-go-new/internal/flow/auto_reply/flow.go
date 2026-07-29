// Package auto_reply 实现自动回复独立主流程，并提供重复发送和空回复保护。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// Flow 组装自动回复主流程依赖。
type Flow struct {
	Browser      *client.Client
	AI           *ai.Client
	Store        *storage.Store
	Cloud        *cloud.Client
	Logger       shared.Logger
	DownloadsDir string
}

type flowStep struct {
	name     string
	optional bool
	run      func(context.Context) error
}

// Run 按平铺步骤启动浏览器、准备消息页、处理未读会话并同步摘要。
func (f *Flow) Run(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime) (shared.Stats, error) {
	stats := shared.Stats{}
	steps := []flowStep{
		{name: "start_browser", run: func(ctx context.Context) error { return f.startBrowser(ctx, prepared) }},
		{name: "open_message_page", run: func(ctx context.Context) error {
			return runtime.OpenMessagesPage(ctx, f.Browser, prepared.Platform)
		}},
		{name: "initialize_message_page", run: func(ctx context.Context) error {
			return runtime.InitializeMessagesPage(ctx, f.Browser, prepared.Platform)
		}},
		{name: "scan_generate_and_reply", run: func(ctx context.Context) error {
			return f.processConversations(ctx, prepared, runtime, &stats)
		}},
	}
	for _, step := range steps {
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, step.name, "start", startedAt, nil)
		if err := step.run(ctx); err != nil {
			if step.optional {
				f.log(prepared.Request.TaskID, step.name, "warning", startedAt, err)
				continue
			}
			f.log(prepared.Request.TaskID, step.name, "failed", startedAt, err)
			return stats, fmt.Errorf("%s 失败：%w", step.name, err)
		}
		f.log(prepared.Request.TaskID, step.name, "success", startedAt, nil)
	}
	return stats, nil
}

// startBrowser 使用当前 Profile 启动或复用 CloakBrowser。
func (f *Flow) startBrowser(ctx context.Context, prepared shared.PreparedTask) error {
	headless := prepared.Request.Headless
	humanize := true
	_, err := f.Browser.StartBrowser(ctx, contract.BrowserStartRequest{
		UserDataDir: prepared.ProfilePath, DownloadsPath: f.DownloadsDir,
		Headless: &headless, Humanize: &humanize, Locale: "zh-CN", Timezone: "Asia/Shanghai",
	})
	return err
}

// processConversations 读取未读会话并逐条执行生成、安全检查、发送和保存。
func (f *Flow) processConversations(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, stats *shared.Stats) error {
	maxRounds := prepared.Position.MaxBatches
	if maxRounds <= 0 {
		maxRounds = 1
	}
	errorPolicy := &shared.ConsecutiveErrorPolicy{}
	for round := 0; round < maxRounds; round++ {
		if err := shared.EnsureCloudSession(ctx, f.Cloud, prepared.Request.Token, prepared.Request.TaskID, "auto_reply", f.Logger); err != nil {
			return err
		}
		conversations, err := runtime.ScanUnreadConversations(ctx, f.Browser, prepared.Platform)
		if err != nil {
			return err
		}
		if len(conversations) == 0 {
			return nil
		}
		for _, conversation := range conversations {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := f.processConversation(ctx, prepared, runtime, conversation, stats); err != nil {
				f.log(prepared.Request.TaskID, "conversation_operation", "failed", time.Now(), err)
				if stopErr := errorPolicy.Record(err); stopErr != nil {
					return stopErr
				}
				continue
			}
			errorPolicy.Reset()
		}
		if round+1 < maxRounds {
			wait := prepared.Position.AutoReplyWait
			if wait <= 0 {
				wait = 3
			}
			timer := time.NewTimer(time.Duration(wait) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

// processConversation 平铺执行读取上下文、AI 生成、安全检查、发送和去重保存。
func (f *Flow) processConversation(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime, conversation model.Conversation, stats *shared.Stats) error {
	stats.Processed++
	history, err := runtime.ReadConversation(ctx, f.Browser, prepared.Platform, conversation)
	if err != nil {
		stats.Failed++
		f.log(prepared.Request.TaskID, "read_conversation", "failed", time.Now(), err)
		return fmt.Errorf("read_conversation：%w", err)
	}
	reply, err := f.AI.GenerateReply(ctx, prepared.Position.AI, prepared.Position, conversation, history)
	if err != nil {
		stats.Failed++
		f.log(prepared.Request.TaskID, "generate_reply", "failed", time.Now(), err)
		return fmt.Errorf("generate_reply：%w", err)
	}
	reply = strings.TrimSpace(reply)
	if reply == "" || len([]rune(reply)) > 1000 {
		stats.Failed++
		err := fmt.Errorf("AI 回复为空或超过 1000 字")
		f.log(prepared.Request.TaskID, "reply_safety_check", "failed", time.Now(), err)
		return fmt.Errorf("reply_safety_check：%w", err)
	}
	replyHash := hashReply(reply)
	exists, err := f.Store.ConversationExists(ctx, prepared.Request.TaskID, conversation.Key, replyHash)
	if err != nil {
		stats.Failed++
		f.log(prepared.Request.TaskID, "reply_duplicate_check", "failed", time.Now(), err)
		return fmt.Errorf("reply_duplicate_check：%w", err)
	}
	if exists {
		stats.Skipped++
		return nil
	}
	if err := runtime.ReplyConversation(ctx, f.Browser, prepared.Platform, conversation, reply); err != nil {
		stats.Failed++
		f.log(prepared.Request.TaskID, "send_reply", "failed", time.Now(), err)
		f.saveConversation(ctx, prepared, conversation, replyHash, "failed")
		return fmt.Errorf("send_reply：%w", err)
	}
	stats.Succeeded++
	f.saveConversation(ctx, prepared, conversation, replyHash, "success")
	return nil
}

// saveConversation 保存不含会话正文和回复正文的去重摘要。
func (f *Flow) saveConversation(ctx context.Context, prepared shared.PreparedTask, conversation model.Conversation, replyHash string, result string) {
	if err := f.Store.SaveConversation(context.WithoutCancel(ctx), storage.ConversationRecord{
		TaskID: prepared.Request.TaskID, ConversationKey: conversation.Key,
		PlatformID: prepared.Position.PlatformID, ReplyHash: replyHash, Result: result,
	}); err != nil {
		f.log(prepared.Request.TaskID, "save_conversation", "warning", time.Now(), err)
	}
}

// hashReply 返回回复正文的本地去重哈希。
func hashReply(reply string) string {
	sum := sha256.Sum256([]byte(reply))
	return hex.EncodeToString(sum[:])
}

// log 输出自动回复步骤日志。
func (f *Flow) log(taskID string, step string, status string, startedAt time.Time, err error) {
	if f.Logger != nil {
		f.Logger.Step(taskID, "auto_reply", step, status, startedAt, err)
	}
}
