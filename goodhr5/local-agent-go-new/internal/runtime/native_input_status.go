// Package runtime 文件作用：按实际浏览器位置检查原生输入资源，记录安装版本、指纹与失败原因，不把磁盘检查当成运行中版本。
package runtime

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// NativeInputSafetyStatus 表示 Worker 启动前检查的浏览器原生输入资源状态。
type NativeInputSafetyStatus struct {
	ExpectedVersion  string `json:"expected_version"`
	InstalledVersion string `json:"installed_version"`
	ResourceSHA256   string `json:"resource_sha256,omitempty"`
	ArchivePath      string `json:"archive_path,omitempty"`
	Ready            bool   `json:"ready"`
	Error            string `json:"error,omitempty"`
}

// nativeInputArchivePath 按实际可执行文件或安装目录寻找资源，兼容 Windows 嵌套目录和 macOS 应用包。
func nativeInputArchivePath(location string) (string, error) {
	info, err := os.Stat(location)
	if err != nil {
		return "", fmt.Errorf("浏览器位置无法读取：%w", err)
	}
	root := location
	if !info.IsDir() {
		root = filepath.Dir(location)
	}
	for _, path := range []string{
		filepath.Join(root, "omni.ja"),
		filepath.Join(root, "Camoufox.app", "Contents", "Resources", "omni.ja"),
		filepath.Join(root, "Contents", "Resources", "omni.ja"),
		filepath.Join(root, "..", "Resources", "omni.ja"),
	} {
		if fileExists(path) {
			return filepath.Abs(path)
		}
	}
	return "", fmt.Errorf("实际浏览器目录没有找到原生输入资源：%s", root)
}

// InspectCamoufoxMouseSafety 检查已安装资源的版本和关键保护方法，未知或缺失资源明确返回失败状态。
func InspectCamoufoxMouseSafety(location string) NativeInputSafetyStatus {
	status := NativeInputSafetyStatus{ExpectedVersion: nativeInputSafetyVersion, InstalledVersion: "unknown"}
	path, err := nativeInputArchivePath(location)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.ArchivePath = path
	reader, err := zip.OpenReader(path)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer reader.Close()
	for _, entry := range reader.File {
		if entry.Name != mouseHandlerResource {
			continue
		}
		stream, readErr := entry.Open()
		if readErr != nil {
			status.Error = readErr.Error()
			return status
		}
		content, readErr := io.ReadAll(stream)
		stream.Close()
		if readErr != nil {
			status.Error = readErr.Error()
			return status
		}
		status.ResourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(content))
		firstLine := strings.SplitN(string(content), "\n", 2)[0]
		if strings.HasPrefix(firstLine, "// GoodHR native mouse safety ") {
			status.InstalledVersion = strings.TrimSpace(strings.TrimPrefix(firstLine, "// GoodHR native mouse safety "))
		}
		status.Ready = bytes.HasPrefix(content, []byte(mouseSafetyMarker+"\n")) &&
			bytes.Contains(content, []byte("_goodhrEnsureWindowReady(")) &&
			bytes.Contains(content, []byte("GOODHR_MOUSE_WINDOW_NOT_READY:")) &&
			bytes.Contains(content, []byte("mouseUpAcknowledged")) &&
			bytes.Contains(content, []byte("_goodhrRunInput(")) &&
			bytes.Contains(content, []byte("GOODHR_MOUSE_INPUT_TIMEOUT:")) &&
			bytes.Contains(content, []byte("goodhr.mouse.min_ms")) &&
			bytes.Contains(content, []byte("mouse_duration_planned_ms")) &&
			bytes.Contains(content, []byte("GOODHR_MOUSE_APZ_TIMEOUT:wheel"))
		if !status.Ready {
			status.Error = "浏览器原生输入保护资源没有通过版本与方法检查"
		}
		return status
	}
	status.Error = "浏览器缺少标准鼠标协议资源"
	return status
}
