//go:build !darwin && !windows

// Package notification 文件作用：为暂未支持下载提示窗的系统提供安全降级。
package notification

import (
	"context"
	"os"
)

// NotifyDownload 在当前系统确认文件存在后安静跳过提示窗。
func (Notifier) NotifyDownload(_ context.Context, filePath string) (DownloadAction, error) {
	if _, err := os.Stat(filePath); err != nil {
		return DownloadDismiss, err
	}
	return DownloadDismiss, nil
}
