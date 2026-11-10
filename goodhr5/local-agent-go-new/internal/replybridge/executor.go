// Package replybridge 本文件在已绑定的本机浏览器执行固定的平台操作；不调用AI、不保存业务记录。
package replybridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// Executor 保留本轮页面定位缓存；业务结论和全部持久数据由服务器维护。
type Executor struct {
	seen       map[string]bool
	seenOrder  []string
	transfers  map[string][]transferredFile
	PlatformID string
	Browser    model.Browser
	Runtime    model.Runtime
	Config     model.Config
}

type transferredFile struct {
	path, hash string
	modified   time.Time
}

// NewExecutor 从本机已发布的平台配置初始化执行器，不接受远端CSS或脚本。
func NewExecutor(platformID string, browser model.Browser) (*Executor, error) {
	cfg, err := platform.LoadConfig(platformID)
	if err != nil {
		return nil, err
	}
	r, err := platform.RuntimeFor(platformID)
	if err != nil {
		return nil, err
	}
	if _, ok := r.(model.AutoReplyRuntime); !ok {
		return nil, errors.New("此平台还未提供自动回复操作")
	}
	return &Executor{PlatformID: platformID, Browser: browser, Runtime: r, Config: cfg}, nil
}

// Execute 仅执行白名单操作；传输失败只允许重交结果，不允许再次执行同一条发送。
func (e *Executor) Execute(ctx context.Context, command Command) Result {
	result := Result{ID: command.ID}
	if command.ID == "" || command.PlatformID != e.PlatformID || command.RunID == "" || !command.Deadline.After(time.Now()) {
		result.Error = "浏览器命令不属于当前任务或已过期"
		result.Code = "COMMAND_INVALID"
		return result
	}
	if e.seen == nil {
		e.seen = map[string]bool{}
	}
	if e.seen[command.ID] {
		result.Error = "这个浏览器命令已经执行，请核对服务器记录"
		result.Code = "ALREADY_EXECUTED"
		return result
	}
	e.seen[command.ID] = true
	e.seenOrder = append(e.seenOrder, command.ID)
	if len(e.seenOrder) > 512 {
		delete(e.seen, e.seenOrder[0])
		e.seenOrder = e.seenOrder[1:]
	}
	ctx, cancel := context.WithDeadline(ctx, command.Deadline)
	defer cancel()
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(command.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		result.Error = "浏览器命令格式未通过校验"
		result.Code = "COMMAND_INVALID"
		return result
	}
	response, err := e.execute(ctx, command.Action, request)
	if err != nil {
		result.Error = err.Error()
		if command.Action == "collect_resume" {
			if evidence := failurediag.AttachmentEvidence(err); evidence != nil {
				result.Value, _ = json.Marshal(evidence)
			}
		}
		result.Code = "ACTION_FAILED"
		if errors.Is(err, model.ErrAutoReplySupersededBeforeSend) {
			result.Code = "REPLY_SUPERSEDED"
		} else if errors.Is(err, model.ErrAutoReplyOnlineProfileUnavailable) {
			result.Code = "ONLINE_PROFILE_UNAVAILABLE"
		} else if errors.Is(err, model.ErrAutoReplyConversationNotOpen) {
			result.Code = "CONVERSATION_NOT_OPEN"
		} else if errors.Is(err, model.ErrAutoReplyResumeRequestUnavailable) {
			result.Code = "RESUME_REQUEST_UNAVAILABLE"
		}
		return result
	}
	if len(response.LocalPaths) == len(response.Files) && len(response.Files) > 0 {
		if e.transfers == nil {
			e.transfers = map[string][]transferredFile{}
		}
		for i, path := range response.LocalPaths {
			if info, statErr := os.Lstat(path); statErr == nil {
				e.transfers[command.ID] = append(e.transfers[command.ID], transferredFile{path: path, hash: response.Files[i].SHA256, modified: info.ModTime()})
			}
		}
	}
	result.Value, err = json.Marshal(response)
	if err != nil {
		result.Error = "浏览器结果没能整理完成"
		result.Code = "RESULT_INVALID"
	}
	return result
}

// execute 调用本机既有平台操作，所有AI判断仍由服务器进行。
func (e *Executor) execute(ctx context.Context, action string, request Request) (Response, error) {
	response := Response{ObservedAt: time.Now().UTC()}
	r := e.Runtime.(model.AutoReplyRuntime)
	var err error
	switch action {
	case "initialize":
		err = e.Runtime.OpenMessagesPage(ctx, e.Browser, e.Config)
		if err == nil {
			err = e.Runtime.InitializeMessagesPage(ctx, e.Browser, e.Config)
		}
		if err == nil {
			err = r.InitializeAutoReplyPage(ctx, e.Browser, e.Config, nil)
		}
	case "scan":
		response.Conversations, err = e.Runtime.ScanUnreadConversations(ctx, e.Browser, e.Config)
	case "inspect":
		inspector, ok := e.Runtime.(model.AutoReplyContactInspector)
		if !ok {
			return response, errors.New("平台暂不能单独核对联系人")
		}
		response.Snapshot, err = inspector.InspectAutoReplyContact(ctx, e.Browser, e.Config, request.Conversation)
	case "open":
		response.Snapshot, err = r.OpenAutoReplyConversation(ctx, e.Browser, e.Config, request.Conversation, request.Known, request.MaxHistory)
	case "messages":
		response.Messages, response.Complete, err = r.ReadAutoReplyMessages(ctx, e.Browser, e.Config, request.Snapshot, request.Known, request.MaxHistory)
	case "latest":
		response.Latest, err = r.ReadLatestAutoReplyMessage(ctx, e.Browser, e.Config, request.Snapshot)
	case "open_latest":
		response.Latest, response.Opened, err = r.ReadOpenAutoReplyLatestMessage(ctx, e.Browser, e.Config, request.Snapshot)
	case "verify":
		verifier, ok := e.Runtime.(model.AutoReplyIdentityVerifier)
		if !ok {
			return response, errors.New("平台暂不能核对当前会话身份")
		}
		err = verifier.VerifyAutoReplyConversationIdentity(ctx, e.Browser, e.Config, request.Snapshot)
	case "request_resume":
		err = r.RequestAutoReplyResume(ctx, e.Browser, e.Config, request.Snapshot)
	case "read_online_profile":
		reader, ok := e.Runtime.(model.AutoReplyOnlineProfileReader)
		if !ok {
			return response, model.ErrAutoReplyOnlineProfileUnavailable
		}
		response.OnlineProfile, err = reader.ReadAutoReplyOnlineProfile(ctx, e.Browser, e.Config, request.Snapshot)
		if err == nil {
			err = ValidateOnlineProfileScreenshots(response.OnlineProfile.Screenshots)
		}
	case "request_info":
		requester, ok := e.Runtime.(model.AutoReplyInfoRequester)
		if !ok || !e.Config.Behavior.AutoReplyNativeInfoRequests {
			return response, errors.New("当前平台尚未提供原生资料索要能力")
		}
		if model.CandidateInfoRequestKind(request.InfoRequest) == "" {
			return response, errors.New("每次只能索要一种明确的候选人资料")
		}
		if request.Snapshot.ResumeOfferMessageID != "" && model.CandidateInfoRequestKind(request.InfoRequest) != "resume" {
			return response, errors.New("原生简历邀请只能执行接收简历操作")
		}
		response.InfoResult, err = requester.RequestAutoReplyCandidateInfo(ctx, e.Browser, e.Config, request.Snapshot, request.InfoRequest)
	case "collect_resume":
		response.Resume, err = r.CollectAutoReplyResume(ctx, e.Browser, e.Config, request.Snapshot)
		if err == nil {
			response.LocalPaths = append([]string{}, response.Resume.AttachmentPaths...)
			began := time.Now()
			response.Files, err = packFiles(response.Resume.AttachmentPaths)
			if err != nil {
				err = failurediag.AttachmentFailure(err, "读取文件准备回传", began, 0, 0, "")
			}
			response.Resume.AttachmentPaths = nil
		}
	case "send":
		response.Latest, err = e.sendWithReceipt(ctx, r, request)
	case "close":
		err = r.CloseAutoReplyConversation(ctx, e.Browser, e.Config, request.Snapshot)
	case "avatar":
		response.Avatar, err = e.captureAvatar(ctx, r, request.Snapshot)
	default:
		err = errors.New("服务器请求的操作未在本机白名单中")
	}
	return response, err
}

// Acknowledge 在服务器已保存文件和回执后清理本次临时下载，文件被人工改动时保留。
func (e *Executor) Acknowledge(commandID string) {
	for _, file := range e.transfers[commandID] {
		info, err := os.Lstat(file.path)
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Equal(file.modified) {
			continue
		}
		data, err := os.ReadFile(file.path)
		if err != nil {
			continue
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) == file.hash {
			_ = os.Remove(file.path)
		}
	}
	delete(e.transfers, commandID)
}

// sendWithReceipt 在原平台身份与消息锚点下只发送一次，随后仅通过读取核对新的己方气泡。
func (e *Executor) sendWithReceipt(ctx context.Context, r model.AutoReplyRuntime, request Request) (model.ConversationMessage, error) {
	snapshot := request.Snapshot
	if snapshot.ExpectedSendMessageKey == "" || strings.TrimSpace(request.Message) == "" {
		return model.ConversationMessage{}, errors.New("缺少发送锚点或完整草稿")
	}
	var err error
	snapshot.DeliveryPageURL, err = common.UniqueCurrentPageURL(ctx, e.Browser)
	if err != nil {
		return model.ConversationMessage{}, err
	}
	if err = r.SendAutoReplyMessage(ctx, e.Browser, e.Config, snapshot, request.Message); err != nil {
		return model.ConversationMessage{}, err
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for {
		rows, _, readErr := r.ReadAutoReplyMessages(readCtx, e.Browser, e.Config, snapshot, nil, 8)
		if readErr != nil {
			return model.ConversationMessage{}, readErr
		}
		seen := false
		for _, row := range rows {
			key := row.PlatformMessageID
			if key == "" {
				key = row.Key
			}
			if key == snapshot.ExpectedSendMessageKey {
				seen = true
				continue
			}
			if seen && key != "" && row.Direction == "self" && strings.TrimSpace(row.TextContent) == strings.TrimSpace(request.Message) {
				return row, nil
			}
		}
		select {
		case <-readCtx.Done():
			return model.ConversationMessage{}, errors.New("消息已尝试发送，尚未核实新的己方气泡，请只读核对")
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// packFiles 只传输平台采集实际返回的常规文件，并附完整摘要供服务器验收。
func packFiles(paths []string) ([]File, error) {
	if len(paths) > maxTransferFiles {
		return nil, errors.New("本次附件数量超过限制")
	}
	infos := make([]os.FileInfo, 0, len(paths))
	var total int64
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxTransferFileBytes {
			return nil, errors.New("附件不是可上传的常规文件或超过20MB")
		}
		total += info.Size()
		if total > maxTransferTotalBytes {
			return nil, errors.New("本次附件合计超过64MB")
		}
		infos = append(infos, info)
	}
	files := make([]File, 0, len(paths))
	for i, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		current, err := f.Stat()
		if err != nil || !os.SameFile(current, infos[i]) || current.Size() != infos[i].Size() {
			f.Close()
			return nil, errors.New("附件在读取前发生变化")
		}
		data, err := io.ReadAll(io.LimitReader(f, current.Size()+1))
		f.Close()
		if err != nil || int64(len(data)) != current.Size() {
			return nil, errors.New("未能完整读取已核验的附件")
		}
		digest := sha256.Sum256(data)
		files = append(files, File{Name: filepath.Base(path), Data: data, SHA256: hex.EncodeToString(digest[:])})
	}

	return files, nil
}

// captureAvatar 在当前同一会话前后验证身份，只截取本平台配置的当前候选人头像。
func (e *Executor) captureAvatar(ctx context.Context, r model.AutoReplyRuntime, snapshot model.AutoReplyConversationSnapshot) ([]byte, error) {
	_, opened, err := r.ReadOpenAutoReplyLatestMessage(ctx, e.Browser, e.Config, snapshot)
	if err != nil || !opened {
		return nil, errors.New("当前会话无法核实")
	}
	screenshot, ok := e.Browser.(interface {
		Screenshot(context.Context, contract.ScreenshotRequest) (contract.ScreenshotResult, error)
	})
	if !ok {
		return nil, errors.New("浏览器截图能力未准备好")
	}
	selector, ok := e.Config.Selectors["message.current_avatar"]
	if !ok {
		return nil, errors.New("平台头像尚未配置")
	}
	dir, err := os.MkdirTemp("", "goodhr-avatar-transfer-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	result, err := screenshot.Screenshot(ctx, contract.ScreenshotRequest{Target: &selector, Directory: dir, Filename: "avatar.png", ViewportOnly: true})
	if err != nil {
		return nil, err
	}
	_, opened, err = r.ReadOpenAutoReplyLatestMessage(ctx, e.Browser, e.Config, snapshot)
	if err != nil || !opened {
		return nil, errors.New("截图后会话已变化")
	}
	info, err := os.Stat(result.Path)
	if err != nil || info.Size() > 4<<20 {
		return nil, fmt.Errorf("头像文件不满足上传限制")
	}
	return os.ReadFile(result.Path)
}
