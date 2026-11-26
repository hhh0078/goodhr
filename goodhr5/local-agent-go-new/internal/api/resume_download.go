// Package api 文件作用：组装下载简历流程并在网页保存设置后刷新本地开关，不在HTTP层解析文件。
package api

import (
	"fmt"
	downloadresume "goodhr5/local-agent-go-new/internal/flow/resume_download"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	// 用户选择的下载目录（包括 ~/Downloads）只清索引；仅 Agent 数据目录中的专用下载副本可删除。
	manager.SetFilePurge(s.store.ListDownloads, s.privateDownloadPath)
	s.resumeDownloads = manager
	s.downloads.Observer = manager.Observe
	manager.Start()
}

// privateDownloadPath 只允许数据目录内的实体 downloads 子目录，不接受指向个人下载目录的符号链接。
func (s *Server) privateDownloadPath(path string) bool {
	base, err := filepath.EvalSymlinks(s.cfg.DataDir)
	if err != nil || !filepath.IsAbs(base) {
		return false
	}
	root := filepath.Join(base, "downloads")
	info, err := os.Lstat(root)
	return err == nil && info.IsDir() && pathWithin(path, root)
}

// handleResumeDownloadSettingsRefresh 验证当前网页登录后重新读云端设置，客户端不能绕过云端开关。
func (s *Server) handleResumeDownloadSettingsRefresh(w http.ResponseWriter, r *http.Request) {
	if !sessionWriteAllowed(w, r) {
		return
	}
	// 此入口也会更新网页登录身份；在云端校验前登记，确保在途退出可以取消本次恢复。
	header := strings.Fields(r.Header.Get("Authorization"))
	var generation uint64
	if len(header) == 2 && strings.EqualFold(header[0], "Bearer") {
		generation = s.beginCloudSessionRequest(header[1])
		defer s.endCloudSessionRequest(generation)
	}
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	if s.resumeDownloads == nil {
		writeError(w, 503, "DOWNLOAD_IMPORT_UNAVAILABLE", fmt.Errorf("下载导入尚未准备好，请更新本地程序"))
		return
	}
	s.extensionMu.Lock()
	if generation != s.sessionGeneration {
		s.extensionMu.Unlock()
		writeError(w, 409, "CLOUD_SESSION_CHANGED", fmt.Errorf("登录账号已经变化，请重新连接"))
		return
	}
	err := s.saveCloudSessionLocked(token, owner)
	s.extensionMu.Unlock()
	if err != nil {
		writeError(w, 500, "CLOUD_SESSION_SAVE_FAILED", err)
		return
	}
	if err := s.resumeDownloads.RefreshSettings(r.Context()); err != nil {
		writeError(w, 502, "DOWNLOAD_SETTINGS_SYNC_FAILED", err)
		return
	}
	writeSuccess(w, 200, struct {
		Refreshed bool `json:"refreshed"`
	}{true})
}
