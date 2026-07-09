// Package app 提供本地文件打开和定位能力。
package app

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"goodhr5/local-agent-go/internal/response"
)

// handleFileOpen 用系统默认程序打开下载文件。
// w 为响应对象，r 为请求对象。
func (s *Server) handleFileOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	filePath, err := s.downloadFilePathFromRequest(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := openLocalFile(filePath); err != nil {
		response.Error(w, http.StatusInternalServerError, "文件没打开成功，我小声记下了："+err.Error())
		return
	}
	response.Success(w, map[string]any{"file_path": filePath})
}

// handleFileReveal 在系统文件管理器中定位下载文件。
// w 为响应对象，r 为请求对象。
func (s *Server) handleFileReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	filePath, err := s.downloadFilePathFromRequest(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := revealLocalFile(filePath); err != nil {
		response.Error(w, http.StatusInternalServerError, "文件夹没打开成功，我小声记下了："+err.Error())
		return
	}
	response.Success(w, map[string]any{"file_path": filePath})
}

// downloadFilePathFromRequest 从请求中读取并校验下载文件路径。
// r 为 HTTP 请求对象，返回可安全打开的绝对文件路径。
func (s *Server) downloadFilePathFromRequest(r *http.Request) (string, error) {
	payload, err := readPayload(r)
	if err != nil {
		return "", err
	}
	rawPath := firstNonEmptyString(stringValue(payload["file_path"]), stringValue(payload["path"]))
	if strings.TrimSpace(rawPath) == "" {
		return "", fmt.Errorf("文件路径不能为空")
	}
	filePath, err := safeDownloadFilePath(rawPath, s.cfg.DownloadsDir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("文件不存在或暂时摸不到")
	}
	if info.IsDir() {
		return "", fmt.Errorf("这里需要一个文件路径，不是文件夹")
	}
	return filePath, nil
}

// safeDownloadFilePath 校验文件路径必须位于下载目录内。
// rawPath 为请求中的路径，downloadsDir 为允许访问的下载目录。
func safeDownloadFilePath(rawPath string, downloadsDir string) (string, error) {
	filePath, err := filepath.Abs(strings.TrimSpace(rawPath))
	if err != nil {
		return "", fmt.Errorf("文件路径不太对")
	}
	baseDir, err := filepath.Abs(strings.TrimSpace(downloadsDir))
	if err != nil || baseDir == "" {
		return "", fmt.Errorf("下载目录不太对")
	}
	if evaluated, err := filepath.EvalSymlinks(filePath); err == nil {
		filePath = evaluated
	}
	if evaluated, err := filepath.EvalSymlinks(baseDir); err == nil {
		baseDir = evaluated
	}
	if !isPathInside(baseDir, filePath) {
		return "", fmt.Errorf("只能打开 GoodHR 下载目录里的文件")
	}
	return filePath, nil
}

// isPathInside 判断目标路径是否位于基础目录内。
// baseDir 为基础目录，targetPath 为目标路径。
func isPathInside(baseDir string, targetPath string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil || rel == "." {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if goruntime.GOOS == "windows" {
		return !strings.HasPrefix(strings.ToLower(rel), "..\\")
	}
	return true
}

// openLocalFile 使用系统默认程序打开文件。
// filePath 为已经校验过的本地文件路径。
func openLocalFile(filePath string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", filePath)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", filePath)
	default:
		cmd = exec.Command("xdg-open", filePath)
	}
	hideCommandWindow(cmd)
	return cmd.Start()
}

// revealLocalFile 在系统文件管理器中定位文件。
// filePath 为已经校验过的本地文件路径。
func revealLocalFile(filePath string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", filePath)
	case "windows":
		cmd = exec.Command("explorer", "/select,"+filePath)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(filePath))
	}
	hideCommandWindow(cmd)
	return cmd.Start()
}
