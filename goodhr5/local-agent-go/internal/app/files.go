// Package app 提供本地文件打开和定位能力。
package app

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"goodhr5/local-agent-go/internal/response"
)

const downloadToastTimeoutSeconds = 5

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

// handleDownloadNotify 弹出下载完成提示窗。
// w 为响应对象，r 为请求对象。
func (s *Server) handleDownloadNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	log.Printf("[下载提示] 收到下载完成通知 remote=%s", r.RemoteAddr)
	payload, err := readPayload(r)
	if err != nil {
		log.Printf("[下载提示] 读取请求失败 err=%v", err)
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("[下载提示] 请求参数 file_path=%s path=%s file_name=%s url=%s status=%s", stringValue(payload["file_path"]), stringValue(payload["path"]), stringValue(payload["file_name"]), stringValue(payload["url"]), stringValue(payload["status"]))
	filePath, err := s.downloadFilePathFromPayload(payload)
	if err != nil {
		log.Printf("[下载提示] 文件路径校验失败 err=%v", err)
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("[下载提示] 文件路径校验通过 file_path=%s", filePath)
	payload["file_path"] = filePath
	if stringValue(payload["file_name"]) == "" {
		payload["file_name"] = filepath.Base(filePath)
	}
	if s.db != nil {
		if _, err := s.db.SaveDownload(payload); err != nil {
			log.Printf("[下载提示] 保存下载记录失败 err=%v", err)
		} else {
			log.Printf("[下载提示] 下载记录已保存 file_path=%s", filePath)
		}
	}
	go func() {
		log.Printf("[下载提示] 准备弹出轻量提示窗 file_path=%s os=%s", filePath, goruntime.GOOS)
		if err := showDownloadToast(filePath); err != nil {
			log.Printf("[下载提示] 轻量提示窗失败 file_path=%s err=%v", filePath, err)
			return
		}
		log.Printf("[下载提示] 轻量提示窗流程结束 file_path=%s", filePath)
	}()
	log.Printf("[下载提示] 已接受通知请求 file_path=%s", filePath)
	response.Success(w, map[string]any{"notified": true, "file_path": filePath})
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
	return s.downloadFilePathFromPayload(payload)
}

// downloadFilePathFromPayload 从请求参数中读取并校验下载文件路径。
// payload 为请求 JSON 参数。
func (s *Server) downloadFilePathFromPayload(payload map[string]any) (string, error) {
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

// showDownloadToast 弹出下载完成轻量提示窗。
// filePath 为已经校验过的本地文件路径。
func showDownloadToast(filePath string) error {
	var action string
	var err error
	log.Printf("[下载提示] 开始显示提示窗 file_path=%s os=%s", filePath, goruntime.GOOS)
	switch goruntime.GOOS {
	case "darwin":
		action, err = showDownloadToastDarwin(filePath)
	case "windows":
		action, err = showDownloadToastWindows(filePath)
	default:
		action, err = showDownloadToastLinux(filePath)
	}
	if err != nil {
		log.Printf("[下载提示] 提示窗脚本执行失败 file_path=%s err=%v", filePath, err)
		return err
	}
	log.Printf("[下载提示] 提示窗返回动作 file_path=%s action=%s", filePath, strings.TrimSpace(action))
	switch strings.TrimSpace(action) {
	case "open":
		log.Printf("[下载提示] 用户选择打开文件 file_path=%s", filePath)
		return openLocalFile(filePath)
	case "reveal":
		log.Printf("[下载提示] 用户选择打开文件夹 file_path=%s", filePath)
		return revealLocalFile(filePath)
	default:
		return nil
	}
}

// showDownloadToastDarwin 使用 AppleScript 弹出 macOS 轻量提示窗。
// filePath 为下载文件路径，返回用户动作。
func showDownloadToastDarwin(filePath string) (string, error) {
	log.Printf("[下载提示] macOS AppleScript 提示窗开始 file_name=%s", filepath.Base(filePath))
	script := `
on run argv
set fileName to item 1 of argv
set dialogText to "我下载好了，公主请验收：" & return & fileName
try
	set dialogResult to display dialog dialogText with title "GoodHR" buttons {"打开文件夹", "打开文件", "先放着"} default button "打开文件" cancel button "先放着" giving up after 5
	if gave up of dialogResult is true then
		return "timeout"
	end if
	set clickedButton to button returned of dialogResult
	if clickedButton is "打开文件" then
		return "open"
	else if clickedButton is "打开文件夹" then
		return "reveal"
	else
		return "dismiss"
	end if
on error number -128
	return "dismiss"
end try
end run
`
	out, err := exec.Command("osascript", "-e", script, filepath.Base(filePath)).CombinedOutput()
	if err != nil {
		log.Printf("[下载提示] macOS AppleScript 提示窗失败 output=%s err=%v", strings.TrimSpace(string(out)), err)
	} else {
		log.Printf("[下载提示] macOS AppleScript 提示窗完成 output=%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), err
}

// showDownloadToastWindows 使用 PowerShell 弹出 Windows 轻量提示窗。
// filePath 为下载文件路径，返回用户动作。
func showDownloadToastWindows(filePath string) (string, error) {
	log.Printf("[下载提示] Windows PowerShell 提示窗开始 file_name=%s", filepath.Base(filePath))
	script := `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$fileName = $env:GOODHR_DOWNLOAD_FILE_NAME
if ([string]::IsNullOrWhiteSpace($fileName)) { $fileName = "下载文件" }
$form = New-Object System.Windows.Forms.Form
$form.Text = "GoodHR"
$form.StartPosition = "Manual"
$form.Size = New-Object System.Drawing.Size(380, 140)
$form.FormBorderStyle = "FixedToolWindow"
$form.TopMost = $true
$screen = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea
$form.Location = New-Object System.Drawing.Point(($screen.Right - $form.Width - 16), ($screen.Bottom - $form.Height - 16))
$label = New-Object System.Windows.Forms.Label
$label.Text = "我下载好了，公主请验收：" + [Environment]::NewLine + $fileName
$label.AutoSize = $false
$label.Location = New-Object System.Drawing.Point(14, 12)
$label.Size = New-Object System.Drawing.Size(340, 45)
$form.Controls.Add($label)
$openButton = New-Object System.Windows.Forms.Button
$openButton.Text = "打开文件"
$openButton.Location = New-Object System.Drawing.Point(95, 72)
$openButton.Size = New-Object System.Drawing.Size(88, 28)
$openButton.Add_Click({ $form.Tag = "open"; $form.Close() })
$form.Controls.Add($openButton)
$revealButton = New-Object System.Windows.Forms.Button
$revealButton.Text = "打开文件夹"
$revealButton.Location = New-Object System.Drawing.Point(194, 72)
$revealButton.Size = New-Object System.Drawing.Size(96, 28)
$revealButton.Add_Click({ $form.Tag = "reveal"; $form.Close() })
$form.Controls.Add($revealButton)
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 5000
$timer.Add_Tick({ $timer.Stop(); if (-not $form.Tag) { $form.Tag = "timeout" }; $form.Close() })
$form.Add_Shown({ $timer.Start(); $form.Activate() })
[void]$form.ShowDialog()
if ($form.Tag) { Write-Output $form.Tag } else { Write-Output "dismiss" }
`
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Env = append(os.Environ(), "GOODHR_DOWNLOAD_FILE_NAME="+filepath.Base(filePath))
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[下载提示] Windows PowerShell 提示窗失败 output=%s err=%v", strings.TrimSpace(string(out)), err)
	} else {
		log.Printf("[下载提示] Windows PowerShell 提示窗完成 output=%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), err
}

// showDownloadToastLinux 使用常见 Linux 桌面工具弹出提示窗。
// filePath 为下载文件路径，返回用户动作。
func showDownloadToastLinux(filePath string) (string, error) {
	if _, err := exec.LookPath("zenity"); err == nil {
		log.Printf("[下载提示] Linux zenity 提示窗开始 file_name=%s", filepath.Base(filePath))
		cmd := exec.Command(
			"zenity",
			"--question",
			"--timeout="+fmt.Sprint(downloadToastTimeoutSeconds),
			"--title=GoodHR",
			"--text=我下载好了，公主请验收：\n"+filepath.Base(filePath),
			"--ok-label=打开文件",
			"--cancel-label=先放着",
			"--extra-button=打开文件夹",
		)
		out, err := cmd.Output()
		text := strings.TrimSpace(string(out))
		log.Printf("[下载提示] Linux zenity 提示窗完成 output=%s err=%v", text, err)
		if text == "打开文件夹" {
			return "reveal", nil
		}
		if err == nil {
			return "open", nil
		}
		return "dismiss", nil
	}
	log.Printf("[下载提示] Linux 未找到 zenity，跳过提示窗 file_path=%s", filePath)
	return "", nil
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
