// Package api 文件作用：将已经验证的云端会话保存、恢复和撤销，并复用现有换号代际与运行绑定。
package api

import (
	"fmt"
	"log"

	"goodhr5/local-agent-go-new/internal/system/sessionfile"
)

// beginCloudSessionRequest 按请求先后登记待确认身份；与已发布账号的运行代际分开，避免旧账号退出取消新登录。
func (s *Server) beginCloudSessionRequest(token string) uint64 {
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	s.sessionGeneration++
	s.sessionPendingToken = token
	return s.sessionGeneration
}

// endCloudSessionRequest 仅结束仍是最新的请求，旧响应不能清掉新账号的待确认标记。
func (s *Server) endCloudSessionRequest(generation uint64) {
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	if generation == s.sessionGeneration {
		s.sessionPendingToken = ""
	}
}

// saveCloudSessionLocked 先完整落盘再发布当前身份；调用方须持有 extensionMu。
func (s *Server) saveCloudSessionLocked(token, userID string) error {
	if s.cfg.DataDir != "" {
		if err := sessionfile.Save(s.cfg.DataDir, s.cfg.CloudURL, s.cfg.CloudWSURL, token); err != nil {
			return fmt.Errorf("本地登录状态没有保存好，请重新连接")
		}
	}
	s.cloudSession.mu.Lock()
	s.cloudSession.token = token
	s.cloudSession.mu.Unlock()
	s.sessionPendingToken = ""
	s.bindExtensionSessionLocked(token, userID)
	if s.incidents != nil {
		s.incidents.Bind(userID, token)
	}
	return nil
}

// restoreCloudSession 只读取本地文件，不因断网阻塞 HTTP 启动；恢复值仍须通过 WS 和任务前置鉴权。
func (s *Server) restoreCloudSession() {
	if s.cfg.DataDir == "" {
		return
	}
	token, err := sessionfile.Load(s.cfg.DataDir, s.cfg.CloudURL, s.cfg.CloudWSURL)
	if err != nil {
		log.Printf("[云端会话] 本地登录记录暂时不可用，请重新连接")
		return
	}
	s.cloudSession.token = token
}

// clearCloudSessionLocked 同时清除磁盘和内存凭证；调用方须持有 extensionMu，删除失败不能报告退出成功。
func (s *Server) clearCloudSessionLocked() error {
	var err error
	if s.cfg.DataDir != "" {
		err = sessionfile.Delete(s.cfg.DataDir)
	}
	s.cloudSession.mu.Lock()
	s.cloudSession.token = ""
	s.cloudSession.mu.Unlock()
	s.extensionGeneration++
	s.bindExtensionSessionLocked("", "")
	if s.incidents != nil {
		s.incidents.Bind("", "")
	}
	return err
}

// CloudSessionRejected 仅在服务器明确拒绝当前凭证时清理；迟到的旧账号 401 不能删除新账号。
func (s *Server) CloudSessionRejected(token string) {
	s.extensionMu.Lock()
	defer s.extensionMu.Unlock()
	if token == "" || !extensionSecretsEqual(token, s.currentCloudToken()) {
		return
	}
	if extensionSecretsEqual(token, s.sessionPendingToken) {
		s.sessionGeneration++
		s.sessionPendingToken = ""
	}
	if s.clearCloudSessionLocked() != nil {
		log.Printf("[云端会话] 失效的本地登录记录未能删除，请重新连接")
	}
}
