// Package replybridge 本文件验证目标聊天关闭标志可跨设备回传，附件在两端执行相同数量和大小限制。
package replybridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

type closedChatRuntime struct {
	model.Runtime
	model.AutoReplyRuntime
}

// VerifyAutoReplyConversationIdentity 模拟平台已证实聊天窗关闭，保留包装错误语义。
func (closedChatRuntime) VerifyAutoReplyConversationIdentity(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) error {
	return fmt.Errorf("verify: %w", model.ErrAutoReplyConversationNotOpen)
}

// RequestAutoReplyResume 模拟平台已在点击前确认简历索要入口不可用。
func (closedChatRuntime) RequestAutoReplyResume(context.Context, model.Browser, model.Config, model.AutoReplyConversationSnapshot) error {
	return model.ErrAutoReplyResumeRequestUnavailable
}

// TestResumeUnavailableCrossesBridge 验证明确未发送的备用信号跨RPC保留，技术未知错误不冒充未发送。
func TestResumeUnavailableCrossesBridge(t *testing.T) {
	e := &Executor{PlatformID: "test", Runtime: closedChatRuntime{}}
	res := e.Execute(context.Background(), Command{ID: "resume", RunID: "r", PlatformID: "test", Action: "request_resume", Deadline: time.Now().Add(time.Minute), Payload: []byte("{}")})
	p := NewPlatform("test", func(context.Context, string, any, any) error { return &RemoteError{Code: res.Code, Message: res.Error} }, t.TempDir())
	if err := p.RequestAutoReplyResume(context.Background(), nil, model.Config{}, model.AutoReplyConversationSnapshot{}); !errors.Is(err, model.ErrAutoReplyResumeRequestUnavailable) {
		t.Fatal("服务器未收到明确备用信号", err)
	}
	p = NewPlatform("test", func(context.Context, string, any, any) error {
		return &RemoteError{Code: "ACTION_FAILED", Message: "结果未知"}
	}, t.TempDir())
	if err := p.RequestAutoReplyResume(context.Background(), nil, model.Config{}, model.AutoReplyConversationSnapshot{}); err == nil || errors.Is(err, model.ErrAutoReplyResumeRequestUnavailable) {
		t.Fatal("未知结果不能改用备用发送", err)
	}
}

// TestClosedChatCrossesBridge 检查专用错误码往返后仍能恢复原“回列表打开同一人”的处理。
func TestClosedChatCrossesBridge(t *testing.T) {
	e := &Executor{PlatformID: "test", Runtime: closedChatRuntime{}}
	res := e.Execute(context.Background(), Command{ID: "verify", RunID: "r", PlatformID: "test", Action: "verify", Deadline: time.Now().Add(time.Minute), Payload: []byte("{}")})
	if res.Code != "CONVERSATION_NOT_OPEN" {
		t.Fatalf("code=%s", res.Code)
	}
	p := NewPlatform("test", func(context.Context, string, any, any) error { return &RemoteError{Code: res.Code, Message: res.Error} }, t.TempDir())
	err := p.VerifyAutoReplyConversationIdentity(context.Background(), nil, model.Config{}, model.AutoReplyConversationSnapshot{})
	if !errors.Is(err, model.ErrAutoReplyConversationNotOpen) || errors.Is(&RemoteError{Code: "ACTION_FAILED"}, model.ErrAutoReplyConversationNotOpen) {
		t.Fatal("identity mismatch and closed window confused", err)
	}
}

// TestFileTransferLimitsBothEnds 使用稀疏文件检查64MB与20MB约束，并验证合法小附件仍可传输。
func TestFileTransferLimitsBothEnds(t *testing.T) {
	dir := t.TempDir()
	paths := []string{}
	for i := 0; i < 4; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%d.pdf", i))
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(17 << 20); err != nil {
			t.Fatal(err)
		}
		f.Close()
		paths = append(paths, path)
	}
	if _, err := packFiles(paths); err == nil || !strings.Contains(err.Error(), "64MB") {
		t.Fatal("device accepted too many bytes", err)
	}
	if err := os.Truncate(paths[0], 21<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := packFiles(paths[:1]); err == nil {
		t.Fatal("device accepted oversized file")
	}
	data := make([]byte, 17<<20)
	sum := sha256.Sum256(data)
	entry := File{Name: "file", Data: data, SHA256: hex.EncodeToString(sum[:])}
	if err := ValidateReceivedFiles([]File{entry, entry, entry, entry}); err == nil {
		t.Fatal("server accepted total >64MB")
	}
	if err := os.WriteFile(paths[0], []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := packFiles(paths[:1])
	if err != nil || ValidateReceivedFiles(files) != nil {
		t.Fatal("valid transfer rejected", err)
	}
}
