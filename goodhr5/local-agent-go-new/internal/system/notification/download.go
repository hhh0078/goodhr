// Package notification 文件作用：显示 macOS 下载完成提示，并返回用户选择的文件动作。
package notification

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const downloadDialogScript = `
on run argv
	set fileName to item 1 of argv
	try
		set dialogResult to display dialog ("我下载好了，公主请验收：" & return & fileName) buttons {"先放着", "打开文件夹", "打开文件"} default button "打开文件" cancel button "先放着" with title "GoodHR" giving up after 10
		if gave up of dialogResult then
			return "dismiss"
		end if
		set clickedButton to button returned of dialogResult
		if clickedButton is "打开文件" then
			return "open"
		else if clickedButton is "打开文件夹" then
			return "reveal"
		end if
		return "dismiss"
	on error number -128
		return "dismiss"
	end try
end run`

// DownloadAction 表示下载提示窗返回的用户动作。
type DownloadAction string

const (
	// DownloadDismiss 表示用户关闭提示或暂不处理。
	DownloadDismiss DownloadAction = "dismiss"
	// DownloadOpen 表示用户希望打开下载文件。
	DownloadOpen DownloadAction = "open"
	// DownloadReveal 表示用户希望在 Finder 中显示下载文件。
	DownloadReveal DownloadAction = "reveal"
)

// NotifyDownload 显示下载完成提示，十秒未操作时自动关闭。
func (Notifier) NotifyDownload(ctx context.Context, filePath string) (DownloadAction, error) {
	if _, err := os.Stat(filePath); err != nil {
		return DownloadDismiss, fmt.Errorf("下载文件不存在：%w", err)
	}
	osascript, err := exec.LookPath("osascript")
	if err != nil {
		return DownloadDismiss, fmt.Errorf("系统没有找到 osascript：%w", err)
	}
	output, err := exec.CommandContext(
		ctx,
		osascript,
		"-e",
		downloadDialogScript,
		filepath.Base(filePath),
	).Output()
	if err != nil {
		return DownloadDismiss, fmt.Errorf("显示下载提示失败：%w", err)
	}
	switch strings.TrimSpace(string(output)) {
	case string(DownloadOpen):
		return DownloadOpen, nil
	case string(DownloadReveal):
		return DownloadReveal, nil
	default:
		return DownloadDismiss, nil
	}
}
