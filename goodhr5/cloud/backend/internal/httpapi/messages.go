// 文件作用：提供消息标题列表、红点状态、展开即已读与当前账号推送接口，并定时清理已读72小时的正文。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// MessageService 复用同一Push方法处理本地程序与其他云端业务通知。
type MessageService struct {
	auth      *AuthService
	store     MessageStore
	startOnce sync.Once
	now       func() time.Time
}

// NewMessageService 创建消息中心服务，调度器须由服务组装处显式启动。
func NewMessageService(auth *AuthService, store MessageStore) *MessageService {
	return &MessageService{auth: auth, store: store, now: time.Now}
}

// privateAccountSession 读取完整账号会话，拒绝插件受限会话及空账号。
func privateAccountSession(auth *AuthService, w http.ResponseWriter, r *http.Request) (Session, bool) {
	if auth == nil {
		writeError(w, http.StatusServiceUnavailable, "账号服务还没准备好，请稍后再试")
		return Session{}, false
	}
	session, err := auth.SessionFromRequest(r)
	if err != nil || strings.TrimSpace(session.Email) == "" {
		writeError(w, http.StatusUnauthorized, "登录状态暂时失效了，请重新登录")
		return Session{}, false
	}
	if session.Kind == extensionSessionKind {
		writeError(w, http.StatusForbidden, "请到GoodHR控制台查看和处理消息")
		return Session{}, false
	}
	session.Email = strings.ToLower(strings.TrimSpace(session.Email))
	return session, true
}

// decodePrivateJSON 限制请求大小并拒绝额外字段及尾随JSON，防止客户端夹带收件账号。
func decodePrivateJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "内容没读明白，请检查后再试")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "这次内容有点多，请只提交一份")
		return false
	}
	return true
}

// normalizeMessageInput 校验纯文本和站内跳转地址，不接收任意外链或空幂等编号。
func normalizeMessageInput(input MessageInput) (MessageInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	input.Type = strings.TrimSpace(input.Type)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.ActionURL = strings.TrimSpace(input.ActionURL)
	if input.Title == "" || len([]rune(input.Title)) > 160 {
		return input, errors.New("消息标题请填写1到160个字")
	}
	if input.Body == "" || len([]rune(input.Body)) > 12000 {
		return input, errors.New("消息正文请填写1到12000个字")
	}
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 {
		return input, errors.New("请带上有效的消息编号，我才不会重复提醒")
	}
	if input.Type == "" {
		input.Type = "system"
	}
	if len(input.Type) > 64 {
		return input, errors.New("消息类型太长了，请缩短后再试")
	}
	if input.ActionURL != "" {
		value, err := url.Parse(input.ActionURL)
		if err != nil || len(input.ActionURL) > 2000 || !strings.HasPrefix(input.ActionURL, "/") || strings.HasPrefix(input.ActionURL, "//") || strings.ContainsAny(input.ActionURL, "\\\r\n") || value.IsAbs() || value.Host != "" {
			return input, errors.New("消息入口请使用GoodHR站内地址")
		}
	}
	return input, nil
}

// Push 为云端业务提供统一消息推送方法；收件账号由调用方业务身份确定。
func (s *MessageService) Push(userEmail string, input MessageInput) (MessagePushResult, error) {
	userEmail = strings.ToLower(strings.TrimSpace(userEmail))
	if userEmail == "" {
		return MessagePushResult{}, errors.New("消息收件账号还没确定")
	}
	if s == nil || s.store == nil {
		return MessagePushResult{}, errors.New("消息服务还没准备好")
	}
	input, err := normalizeMessageInput(input)
	if err != nil {
		return MessagePushResult{}, err
	}
	return s.store.Push(userEmail, input)
}

// PushCurrent 只给当前登录账号推送，不允许请求指定其他收件人。
func (s *MessageService) PushCurrent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "请使用POST推送消息")
		return
	}
	session, ok := privateAccountSession(s.auth, w, r)
	if !ok {
		return
	}
	var input MessageInput
	if !decodePrivateJSON(w, r, &input) {
		return
	}
	input, err := normalizeMessageInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.Push(session.Email, input)
	if errors.Is(err, ErrMessageIdempotency) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "消息还没记上，请用原编号再试一次")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": result.Message, "created": result.Created, "summary": result.Summary})
}

// Summary 返回缓存中的未读数量与版本，供前端每10秒检查红点。
func (s *MessageService) Summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "请使用GET查看消息状态")
		return
	}
	session, ok := privateAccountSession(s.auth, w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "消息服务还没准备好")
		return
	}
	summary, err := s.store.Summary(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "消息状态暂时没读到，稍后我再试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "unread_count": summary.UnreadCount, "version": summary.Version})
}

// Collection 仅在展开消息中心时查询标题列表，正文不会进入列表。
func (s *MessageService) Collection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "请使用GET查看消息列表")
		return
	}
	session, ok := privateAccountSession(s.auth, w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "消息服务还没准备好")
		return
	}
	result, err := s.store.List(session.Email, parsePositiveInt(r.URL.Query().Get("page")), parsePositiveInt(r.URL.Query().Get("page_size")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "消息列表暂时没读到，请稍后再试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "messages": result.Messages, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

// Read 返回本账号正文并标记首次已读，跨账号编号与不存在编号统一返回404。
func (s *MessageService) Read(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "请使用POST展开消息")
		return
	}
	session, ok := privateAccountSession(s.auth, w, r)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/messages/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "read" {
		writeError(w, http.StatusNotFound, "没有找到这条消息")
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "消息服务还没准备好")
		return
	}
	message, summary, err := s.store.Read(session.Email, parts[0])
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "没有找到这条消息，可能已经清理了")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "这条消息没读到，请稍后再试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message, "summary": summary})
}

// CleanupRead 删除已读满72小时的消息正文，时间依据首次展开而非创建时间。
func (s *MessageService) CleanupRead(now time.Time) error {
	if s == nil || s.store == nil {
		return nil
	}
	_, err := s.store.CleanupRead(now.Add(-72 * time.Hour))
	return err
}

// StartCleanup 启动单一半小时清理任务，取消上下文后自动停止。
func (s *MessageService) StartCleanup(ctx context.Context) {
	if s == nil || s.store == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	s.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Minute)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				if err := s.CleanupRead(s.now()); err != nil {
					log.Print("GoodHR已读消息清理未完成，下次继续")
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}
