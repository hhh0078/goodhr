// Package api 文件作用：组装下载简历流程并在网页保存设置后刷新本地开关，不在HTTP层解析文件。
package api

import (
	"fmt"
	downloadresume "goodhr5/local-agent-go-new/internal/flow/resume_download"
	"log"
	"net/http"
)

// initResumeDownloads 复用同一个浏览器下载监控与导入管理器，不增加第二条浏览器链路。
func (s *Server) initResumeDownloads() {
	if s.downloads == nil || s.resumeImports == nil || s.store == nil || s.cloud == nil {
		return
	}
	manager, err := downloadresume.New(s.store, s.cloud, s.resumeImports)
	if err != nil {
		log.Printf("下载简历导入初始化未完成：%v", err)
		return
	}
	s.resumeDownloads = manager
	s.downloads.Observer = manager.Observe
	manager.Start()
}

// handleResumeDownloadSettingsRefresh 验证当前网页登录后重新读云端设置，客户端不能绕过云端开关。
func (s *Server) handleResumeDownloadSettingsRefresh(w http.ResponseWriter, r *http.Request) {
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	if s.resumeDownloads == nil {
		writeError(w, 503, "DOWNLOAD_IMPORT_UNAVAILABLE", fmt.Errorf("下载导入尚未准备好，请更新本地程序"))
		return
	}
	s.extensionMu.Lock()
	s.bindExtensionSessionLocked(token, owner)
	s.extensionMu.Unlock()
	if err := s.resumeDownloads.RefreshSettings(r.Context()); err != nil {
		writeError(w, 502, "DOWNLOAD_SETTINGS_SYNC_FAILED", err)
		return
	}
	writeSuccess(w, 200, struct {
		Refreshed bool `json:"refreshed"`
	}{true})
}
