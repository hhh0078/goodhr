//go:build !windows

// Package app 提供非 Windows 平台的下载提示窗占位能力。
package app

// showDownloadToastWindowsNative 在非 Windows 平台不会被实际调用。
// filePath 为下载文件路径，返回用户动作。
func showDownloadToastWindowsNative(filePath string) (string, error) {
	return "", nil
}
