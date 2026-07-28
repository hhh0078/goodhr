// Package updater 文件作用：安全解压 Windows 更新 zip 并查找安装器。
package updater

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// prepareInstaller 在 Windows 上解压 zip 更新包，其他格式直接返回原路径。
func prepareInstaller(packagePath string) (string, error) {
	if goruntime.GOOS != "windows" || !strings.EqualFold(filepath.Ext(packagePath), ".zip") {
		return packagePath, nil
	}
	targetDir := strings.TrimSuffix(packagePath, filepath.Ext(packagePath))
	if err := os.RemoveAll(targetDir); err != nil {
		return "", fmt.Errorf("清理更新解压目录失败：%w", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("创建更新解压目录失败：%w", err)
	}
	return extractInstaller(packagePath, targetDir)
}

// extractInstaller 安全解压 zip，并返回其中第一份 exe 安装器。
func extractInstaller(archivePath string, targetDir string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("打开更新压缩包失败：%w", err)
	}
	defer reader.Close()
	installerPath := ""
	for _, file := range reader.File {
		targetPath, pathErr := safeArchivePath(targetDir, file.Name)
		if pathErr != nil {
			return "", pathErr
		}
		if file.FileInfo().IsDir() {
			if err = os.MkdirAll(targetPath, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", err
		}
		source, openErr := file.Open()
		if openErr != nil {
			return "", openErr
		}
		target, createErr := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if createErr != nil {
			_ = source.Close()
			return "", createErr
		}
		_, copyErr := io.Copy(target, source)
		closeErr := target.Close()
		_ = source.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if installerPath == "" && strings.EqualFold(filepath.Ext(targetPath), ".exe") {
			installerPath = targetPath
		}
	}
	if installerPath == "" {
		return "", fmt.Errorf("更新压缩包内没有找到 exe 安装器")
	}
	return installerPath, nil
}

// safeArchivePath 阻止 zip 条目写出目标目录。
func safeArchivePath(targetDir string, name string) (string, error) {
	cleanName := filepath.Clean(name)
	if filepath.IsAbs(cleanName) || cleanName == "." || cleanName == ".." ||
		strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("更新压缩包包含不安全路径：%s", name)
	}
	root, err := filepath.Abs(targetDir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, cleanName))
	if err != nil {
		return "", err
	}
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("更新压缩包包含越界路径：%s", name)
	}
	return target, nil
}
