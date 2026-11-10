// Package replybridge 在服务器登记平台命令和结果，本机只领取执行；发送指令绝不在超时后重放。
package replybridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
	"sync"
	"time"
)

// Command 是已在服务器登记、只能由当前设备领取一次的浏览器操作。
type Command struct {
	ID         string          `json:"id"`
	RunID      string          `json:"run_id"`
	PlatformID string          `json:"platform_id"`
	Action     string          `json:"action"`
	Payload    json.RawMessage `json:"payload"`
	Deadline   time.Time       `json:"deadline"`
}

// Result 是本机的操作观察结果，服务端落库成功后才向调用者确认。
type Result struct {
	ID    string          `json:"id"`
	Value json.RawMessage `json:"value,omitempty"`
	Error string          `json:"error,omitempty"`
	Code  string          `json:"code,omitempty"`
}

// RemoteError 保留可判定的结果类别，未知发送结果不得降级为重试。
type RemoteError struct{ Code, Message string }

// CommandRejectedError 表示派发前已核实旧草稿失效，设备尚未收到命令，可以安全取消而不重试。
type CommandRejectedError struct{ Message string }

// Error 返回供旧草稿调用方停止本次发送的原因。
func (e *CommandRejectedError) Error() string { return e.Message }

// Error 返回原始动作的可读错误。
func (e *RemoteError) Error() string { return e.Message }

// Unwrap 仅将平台明确确认的错误码还原成稳定类型，身份冲突或未知页面错误不得伪装为已关闭。
func (e *RemoteError) Unwrap() error {
	switch e.Code {
	case "CONVERSATION_NOT_OPEN":
		return model.ErrAutoReplyConversationNotOpen
	case "REPLY_SUPERSEDED":
		return model.ErrAutoReplySupersededBeforeSend
	case "ONLINE_PROFILE_UNAVAILABLE":
		return model.ErrAutoReplyOnlineProfileUnavailable
	}
	return nil
}

// Persist 在命令发出前与完成后同步提交服务器数据库。
type Persist func(context.Context, Command, string, Result) error

type pending struct {
	command     Command
	done        chan Result
	claimed     bool
	caller      context.Context
	abandoned   bool
	finished    bool
	valueDigest [sha256.Size]byte
	resultCode  string
	resultError string
}

// Broker 串行派发一次性平台动作；数据库回执是执行下一步的前提。
type Broker struct {
	mu         sync.Mutex
	callMu     sync.Mutex
	queue      chan *pending
	items      map[string]*pending
	persist    Persist
	RunID      string
	PlatformID string
}

// NewBroker 创建一个任务专属命令通道；持久化回调必须写入权威数据库。
func NewBroker(persist Persist) *Broker {
	return &Broker{queue: make(chan *pending, 1), items: map[string]*pending{}, persist: persist}
}

// Call 先记录命令再等待唯一回执，取消或超时只标记未知，绝不重发。
func (b *Broker) Call(ctx context.Context, action string, payload, output any) error {
	b.callMu.Lock()
	defer b.callMu.Unlock()
	if b.persist == nil {
		return errors.New("云端命令存储未准备好")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(90 * time.Second)
	}
	command := Command{ID: hex.EncodeToString(id), RunID: b.RunID, PlatformID: b.PlatformID, Action: action, Payload: raw, Deadline: deadline}
	p := &pending{command: command, caller: ctx, done: make(chan Result, 1)}
	if err = b.persist(ctx, command, "pending", Result{}); err != nil {
		return err
	}
	b.mu.Lock()
	b.items[command.ID] = p
	b.mu.Unlock()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-timer.C:
		return b.abandon(command, context.DeadlineExceeded, output)
	case b.queue <- p:
	case <-ctx.Done():
		return b.abandon(command, ctx.Err(), output)
	}
	select {
	case result := <-p.done:
		if result.Error != "" {
			return commandResultError(command.Action, result)
		}
		if output != nil {
			return json.Unmarshal(result.Value, output)
		}
		return nil
	case <-ctx.Done():
		return b.abandon(command, ctx.Err(), output)
	case <-timer.C:
		return b.abandon(command, context.DeadlineExceeded, output)
	}
}

// commandResultError 还原旧错误及本机附件阶段；未知命令和旧设备继续使用原有错误。
func commandResultError(action string, result Result) error {
	remote := &RemoteError{Code: result.Code, Message: result.Error}
	if action == "collect_resume" && len(result.Value) > 0 && len(result.Value) <= 1024 {
		var evidence failurediag.AttachmentReceipt
		if json.Unmarshal(result.Value, &evidence) == nil {
			return evidence.Restore(remote)
		}
	}
	return remote
}

// abandon 把失去确认的命令写为未知，保留后续只读核对所需的命令编号。
func (b *Broker) abandon(command Command, cause error, output any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	item := b.items[command.ID]
	if item != nil && item.finished {
		result := <-item.done
		if result.Error != "" {
			return commandResultError(command.Action, result)
		}
		if output != nil {
			return json.Unmarshal(result.Value, output)
		}
		return nil
	}
	if item != nil {
		item.abandoned = true
		item.caller = nil
		if !item.claimed {
			delete(b.items, command.ID)
			item.command.Payload = nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.persist(ctx, command, "unknown", Result{ID: command.ID, Error: cause.Error(), Code: "RESULT_UNKNOWN"}); err != nil {
		return errors.Join(cause, err)
	}
	return &RemoteError{Code: "RESULT_UNKNOWN", Message: "浏览器动作结果尚未确认，请先核对实际页面"}
}

// Next 每个命令只领取一次；断线后调用者须通过云端状态核对，不能再次领取旧发送。
func (b *Broker) Next(ctx context.Context) (Command, error) {
	for {
		select {
		case <-ctx.Done():
			return Command{}, ctx.Err()
		case p := <-b.queue:
			b.mu.Lock()
			if p.abandoned || p.finished || p.claimed || p.caller == nil || p.caller.Err() != nil || !time.Now().Before(p.command.Deadline) {
				b.mu.Unlock()
				continue
			}
			if err := b.persist(ctx, p.command, "executing", Result{}); err != nil {
				var rejected *CommandRejectedError
				if errors.As(err, &rejected) {
					p.finished, p.resultCode, p.resultError = true, "REPLY_SUPERSEDED", rejected.Error()
					p.done <- Result{ID: p.command.ID, Code: p.resultCode, Error: p.resultError}
					b.mu.Unlock()
					continue
				}
				// 此时指令没有交给设备，允许下次仅重试领取事务。
				if p.caller.Err() == nil && time.Now().Before(p.command.Deadline) {
					select {
					case b.queue <- p:
					default:
					}
				}
				b.mu.Unlock()
				return Command{}, err
			}
			if p.caller.Err() != nil || !time.Now().Before(p.command.Deadline) {
				b.mu.Unlock()
				continue
			}
			p.claimed = true
			cmd := p.command
			b.mu.Unlock()
			return cmd, nil
		}
	}
}

// Complete 幂等保存相同命令回执，持久化失败时设备可重交回执但不能重新操作页面。
func (b *Broker) Complete(ctx context.Context, result Result) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.items[result.ID]
	if p == nil || !p.claimed {
		return errors.New("这个命令未由当前任务领取")
	}
	if len(result.Value) > 90<<20 || len(result.Error) > 16<<10 || len(result.Code) > 128 {
		return fmt.Errorf("命令回执超过大小限制")
	}
	digest := sha256.Sum256(result.Value)
	if p.finished {
		if p.valueDigest != digest || p.resultCode != result.Code || p.resultError != result.Error {
			return errors.New("命令的最终回执不能被改写")
		}
		return nil
	}
	persistedResult, err := onlineProfilePersistenceResult(p.command, result)
	if err != nil {
		return err
	}
	if err := b.persist(ctx, p.command, "completed", persistedResult); err != nil {
		return err
	}
	p.finished, p.valueDigest, p.resultCode, p.resultError = true, digest, result.Code, result.Error
	p.command.Payload = nil
	p.caller = nil
	if !p.abandoned {
		p.done <- result
	}

	// 设备只重交尚未收到确认的最后一份结果，已完成历史由服务器数据库负责。
	if len(b.items) > 256 {
		for id, item := range b.items {
			if id != result.ID && (item.finished || item.abandoned && time.Now().After(item.command.Deadline.Add(2*time.Minute))) {
				delete(b.items, id)
			}
			if len(b.items) <= 256 {
				break
			}
		}
	}
	return nil
}
