// Package replybridge 本文件让服务器流程通过已登记的命令调用本机平台操作，服务器不启动招聘浏览器。
package replybridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// CallFunc 将命令交给服务器登记与设备通道。
type CallFunc func(context.Context, string, any, any) error

// Platform 实现自动回复所需的平台接口，其余招聘功能仍由原平台实现。
type Platform struct {
	model.Runtime
	id      string
	call    CallFunc
	tempDir string
}

// NewPlatform 创建服务器侧平台代理，临时文件仅保存到服务器目录。
func NewPlatform(platformID string, call CallFunc, tempDir string) *Platform {
	return &Platform{id: platformID, call: call, tempDir: tempDir}
}

// PlatformID 返回当前已绑定的平台编号。
func (p *Platform) PlatformID() string { return p.id }

// InitializeAutoReplyPage 让本机复用已登录的平台页并清理当前遮层。
func (p *Platform) InitializeAutoReplyPage(ctx context.Context, _ model.Browser, _ model.Config, _ *model.AutoReplyConversationSnapshot) error {
	return p.call(ctx, "initialize", Request{}, nil)
}

// OpenMessagesPage 页面初始化统一由同一设备命令完成。
func (p *Platform) OpenMessagesPage(ctx context.Context, _ model.Browser, _ model.Config) error {
	return p.call(ctx, "initialize", Request{}, nil)
}

// InitializeMessagesPage 已由初始化命令完成，本方法保留原平台调用协议。
func (p *Platform) InitializeMessagesPage(context.Context, model.Browser, model.Config) error {
	return nil
}

// ScanUnreadConversations 在本机只读取全部联系人；变化判断和水位由服务器维护。
func (p *Platform) ScanUnreadConversations(ctx context.Context, _ model.Browser, _ model.Config) ([]model.Conversation, error) {
	var response Response
	err := p.call(ctx, "scan", Request{}, &response)
	return response.Conversations, err
}

// InspectAutoReplyContact 获取不改变当前聊天的联系人核验结果。
func (p *Platform) InspectAutoReplyContact(ctx context.Context, _ model.Browser, _ model.Config, item model.Conversation) (model.AutoReplyConversationSnapshot, error) {
	var response Response
	err := p.call(ctx, "inspect", Request{Conversation: item}, &response)
	return response.Snapshot, err
}

// OpenAutoReplyConversation 打开指定候选人并按服务器已知消息键补读历史。
func (p *Platform) OpenAutoReplyConversation(ctx context.Context, _ model.Browser, _ model.Config, item model.Conversation, known []string, maxHistory int) (model.AutoReplyConversationSnapshot, error) {
	var response Response
	err := p.call(ctx, "open", Request{Conversation: item, Known: known, MaxHistory: maxHistory}, &response)
	return response.Snapshot, err
}

// ReadAutoReplyMessages 读取原会话的消息窗口，保留平台原始消息编号。
func (p *Platform) ReadAutoReplyMessages(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot, known []string, maxHistory int) ([]model.ConversationMessage, bool, error) {
	var response Response
	err := p.call(ctx, "messages", Request{Snapshot: snapshot, Known: known, MaxHistory: maxHistory}, &response)
	return response.Messages, response.Complete, err
}

// RequestAutoReplyResume 执行平台的简历索取操作，文本由服务器另行准备。
func (p *Platform) RequestAutoReplyResume(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	err := p.call(ctx, "request_resume", Request{Snapshot: snapshot}, nil)
	var remote *RemoteError
	if errors.As(err, &remote) && remote.Code == "RESUME_REQUEST_UNAVAILABLE" {
		return model.ErrAutoReplyResumeRequestUnavailable
	}
	return err
}

// CollectAutoReplyResume 接收真实下载数据并校验摘要，不访问设备上的文件路径。
func (p *Platform) CollectAutoReplyResume(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot) (model.AutoReplyResumeBundle, error) {
	var response Response
	if err := p.call(ctx, "collect_resume", Request{Snapshot: snapshot}, &response); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	response.Resume.AttachmentPaths = nil
	if len(response.Files) > maxTransferFiles {
		return model.AutoReplyResumeBundle{}, errors.New("附件数量超过限制")
	}
	if err := ValidateReceivedFiles(response.Files); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	if err := os.MkdirAll(p.tempDir, 0o700); err != nil {
		return model.AutoReplyResumeBundle{}, err
	}
	for _, file := range response.Files {
		digest := sha256.Sum256(file.Data)
		if len(file.Data) == 0 || len(file.Data) > 32<<20 || hex.EncodeToString(digest[:]) != file.SHA256 {
			return model.AutoReplyResumeBundle{}, errors.New("附件内容校验没有通过")
		}
		name := filepath.Base(file.Name)
		if name != file.Name || name == "." || name == "" {
			return model.AutoReplyResumeBundle{}, errors.New("附件文件名未通过校验")
		}
		path := filepath.Join(p.tempDir, file.SHA256+filepath.Ext(name))
		if err := os.WriteFile(path, file.Data, 0o600); err != nil {
			return model.AutoReplyResumeBundle{}, err
		}
		response.Resume.AttachmentPaths = append(response.Resume.AttachmentPaths, path)
	}
	return response.Resume, nil
}

// SendAutoReplyWithReceipt 只发送一条已核对的草稿，并返回本机读到的新己方气泡编号。
func (p *Platform) SendAutoReplyWithReceipt(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot, message string) (model.ConversationMessage, error) {
	var response Response
	err := p.call(ctx, "send", Request{Snapshot: snapshot, Message: message}, &response)
	var remote *RemoteError
	if errors.As(err, &remote) && remote.Code == "REPLY_SUPERSEDED" {
		return model.ConversationMessage{}, model.ErrAutoReplySupersededBeforeSend
	}
	return response.Latest, err
}

// SendAutoReplyMessage 保持旧调用约定，云端主流程使用带回执的发送入口。
func (p *Platform) SendAutoReplyMessage(ctx context.Context, b model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	_, err := p.SendAutoReplyWithReceipt(ctx, b, cfg, snapshot, message)
	return err
}

// ReadLatestAutoReplyMessage 只读取指定会话当前的末条消息。
func (p *Platform) ReadLatestAutoReplyMessage(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, error) {
	var response Response
	err := p.call(ctx, "latest", Request{Snapshot: snapshot}, &response)
	return response.Latest, err
}

// ReadOpenAutoReplyLatestMessage 确认原聊天仍打开时读取末条，不主动跳转会话。
func (p *Platform) ReadOpenAutoReplyLatestMessage(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot) (model.ConversationMessage, bool, error) {
	var response Response
	err := p.call(ctx, "open_latest", Request{Snapshot: snapshot}, &response)
	return response.Latest, response.Opened, err
}

// CloseAutoReplyConversation 在原会话核验下执行 Esc 与备用关闭按钮。
func (p *Platform) CloseAutoReplyConversation(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	return p.call(ctx, "close", Request{Snapshot: snapshot}, nil)
}

// VerifyAutoReplyConversationIdentity 在设备当前页面核对候选人身份和原消息范围。
func (p *Platform) VerifyAutoReplyConversationIdentity(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	return p.call(ctx, "verify", Request{Snapshot: snapshot}, nil)
}

// ReadAutoReplyAvatar 只接收设备已核实当前候选人的头像图片。
func (p *Platform) ReadAutoReplyAvatar(ctx context.Context, _ model.Browser, _ model.Config, snapshot model.AutoReplyConversationSnapshot) ([]byte, error) {
	var response Response
	err := p.call(ctx, "avatar", Request{Snapshot: snapshot}, &response)
	if err == nil && len(response.Avatar) > 4<<20 {
		return nil, fmt.Errorf("头像超过大小限制")
	}
	return response.Avatar, err
}

// ValidateReceivedFiles 核对整批传输摘要、数量、单件及总字节数，在写入任何服务器文件前完成。
func ValidateReceivedFiles(files []File) error {
	if len(files) > maxTransferFiles {
		return errors.New("本次附件数量超过限制")
	}
	var total int64
	for _, f := range files {
		n := int64(len(f.Data))
		total += n
		if n <= 0 || n > maxTransferFileBytes || total > maxTransferTotalBytes {
			return errors.New("附件大小超限或内容为空")
		}
		digest := sha256.Sum256(f.Data)
		if f.SHA256 != hex.EncodeToString(digest[:]) {
			return errors.New("附件摘要核对失败")
		}
	}
	return nil
}
