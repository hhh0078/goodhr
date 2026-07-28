// Package api 文件作用：提供下载记录、下载目录切换和下载文件打开接口。
package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/files"
)

// handleDownloads 返回当前浏览器下载记录。
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	result, err := s.browser.Downloads(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DOWNLOAD_LIST_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, result)
}

// handleDownloadHistory 返回 SQLite 中已经结束的下载历史。
func (s *Server) handleDownloadHistory(w http.ResponseWriter, r *http.Request) {
	records, err := s.downloads.History(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DOWNLOAD_HISTORY_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		Downloads []storage.DownloadRecord `json:"downloads"`
		Count     int                      `json:"count"`
	}{Downloads: records, Count: len(records)})
}

// handleDownloadsConfigure 切换 Worker 后续下载保存目录。
func (s *Server) handleDownloadsConfigure(w http.ResponseWriter, r *http.Request) {
	var request contract.DownloadConfigureRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	directory, err := normalizeDownloadRoot(request.Directory)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	request.Directory = directory
	if err = s.browser.ConfigureDownloads(r.Context(), request); err != nil {
		writeError(w, http.StatusBadGateway, "DOWNLOAD_CONFIGURE_FAILED", err)
		return
	}
	s.rememberDownloadRoot(directory)
	writeSuccess(w, http.StatusOK, struct {
		Configured bool   `json:"configured"`
		Directory  string `json:"directory"`
	}{Configured: true, Directory: directory})
}

// handleDownloadsClear 清空 Worker 下载记录，不删除用户文件。
func (s *Server) handleDownloadsClear(w http.ResponseWriter, r *http.Request) {
	if err := s.browser.ClearDownloads(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, "DOWNLOAD_CLEAR_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		Cleared      bool `json:"cleared"`
		FilesDeleted bool `json:"files_deleted"`
	}{Cleared: true, FilesDeleted: false})
}

// handleFileOpen 使用系统默认程序打开本地文件。
func (s *Server) handleFileOpen(w http.ResponseWriter, r *http.Request) {
	s.handleFileAction(w, r, files.Open)
}

// handleFileReveal 在 Finder 中显示本地文件。
func (s *Server) handleFileReveal(w http.ResponseWriter, r *http.Request) {
	s.handleFileAction(w, r, files.Reveal)
}

// handleFileAction 解析文件路径并调用指定系统能力。
func (s *Server) handleFileAction(w http.ResponseWriter, r *http.Request, action func(context.Context, string) error) {
	var request struct {
		Path string `json:"path"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	safePath, err := s.safeDownloadFilePath(request.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILE_PATH", err)
		return
	}
	if err = action(r.Context(), safePath); err != nil {
		writeError(w, http.StatusBadRequest, "FILE_ACTION_FAILED", err)
		return
	}
	writeSuccess(w, http.StatusOK, struct {
		Success bool `json:"success"`
	}{Success: true})
}

// safeDownloadFilePath 把文件操作限制在已经确认过的下载目录内。
func (s *Server) safeDownloadFilePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("只能打开 GoodHR 下载目录内的文件")
	}
	cleanPath := filepath.Clean(value)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return "", fmt.Errorf("下载文件不存在：%w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("当前路径不是下载文件")
	}
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return "", fmt.Errorf("检查下载文件路径失败：%w", err)
	}
	if !s.downloadPathAllowed(resolvedPath) {
		return "", fmt.Errorf("只能打开 GoodHR 下载目录内的文件")
	}
	return cleanPath, nil
}

// rememberDownloadRoot 记录 Worker 已经成功切换过的下载目录。
func (s *Server) rememberDownloadRoot(directory string) {
	s.downloadRootsMu.Lock()
	defer s.downloadRootsMu.Unlock()
	if s.downloadRoots == nil {
		s.downloadRoots = make(map[string]struct{})
	}
	s.downloadRoots[filepath.Clean(directory)] = struct{}{}
}

// downloadPathAllowed 判断真实文件路径是否位于任一已配置下载目录。
func (s *Server) downloadPathAllowed(filePath string) bool {
	s.downloadRootsMu.RLock()
	roots := make([]string, 0, len(s.downloadRoots)+1)
	for root := range s.downloadRoots {
		roots = append(roots, root)
	}
	s.downloadRootsMu.RUnlock()
	if len(roots) == 0 {
		roots = append(roots, s.cfg.DownloadsDir)
	}
	for _, root := range roots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if pathWithin(filePath, resolvedRoot) {
			return true
		}
	}
	return false
}

// normalizeDownloadRoot 校验并规范化用户选择的绝对下载目录。
func normalizeDownloadRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("下载目录不能为空")
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("下载目录必须使用完整路径")
	}
	return filepath.Clean(value), nil
}
