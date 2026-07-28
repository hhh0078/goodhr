// Package runtime 文件作用：安全解压运行组件压缩包并原子替换组件目录。
package runtime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractArchive 根据后缀解压 zip 或 tar.gz 运行组件。
func extractArchive(archivePath string, targetDir string) error {
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, targetDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGZ(archivePath, targetDir)
	default:
		return fmt.Errorf("暂不支持的压缩包格式：%s", filepath.Base(archivePath))
	}
}

// extractZip 安全解压 zip 文件并拒绝符号链接。
func extractZip(archivePath string, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, entry := range reader.File {
		targetPath, err := safeJoin(targetDir, entry.Name)
		if err != nil {
			return err
		}
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("压缩包包含不支持的符号链接：%s", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
		if err != nil {
			_ = source.Close()
			return err
		}
		_, copyErr := io.Copy(target, source)
		closeErr := target.Close()
		_ = source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// extractTarGZ 安全解压 tar.gz 文件并拒绝链接和特殊文件。
func extractTarGZ(archivePath string, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		targetPath, err := safeJoin(targetDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err = os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return err
			}
			target, openErr := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode).Perm())
			if openErr != nil {
				return openErr
			}
			_, copyErr := io.Copy(target, reader)
			closeErr := target.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("压缩包包含不支持的链接或特殊文件：%s", header.Name)
		}
	}
}

// safeJoin 把压缩包条目限制在指定解压目录内。
func safeJoin(root string, name string) (string, error) {
	cleanName := filepath.Clean(name)
	if filepath.IsAbs(cleanName) || cleanName == "." || cleanName == ".." ||
		strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("压缩包包含不安全路径：%s", name)
	}
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetPath, err := filepath.Abs(filepath.Join(rootPath, cleanName))
	if err != nil {
		return "", err
	}
	if targetPath != rootPath && !strings.HasPrefix(targetPath, rootPath+string(os.PathSeparator)) {
		return "", fmt.Errorf("压缩包包含越界路径：%s", name)
	}
	return targetPath, nil
}

// installRoot 跳过无意义的单层包装目录，但保留 Chromium.app 结构。
func installRoot(stagingDir string, component string) string {
	current := stagingDir
	for {
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) != 1 || !entries[0].IsDir() {
			return current
		}
		name := entries[0].Name()
		if component == "cloakbrowser" && strings.HasSuffix(strings.ToLower(name), ".app") {
			return current
		}
		current = filepath.Join(current, name)
	}
}

// replaceDirectory 用新目录替换旧组件，并在失败时恢复旧目录。
func replaceDirectory(sourceDir string, targetDir string) error {
	backupDir := targetDir + ".backup"
	_ = os.RemoveAll(backupDir)
	if _, err := os.Stat(targetDir); err == nil {
		if err = os.Rename(targetDir, backupDir); err != nil {
			return err
		}
	}
	if err := os.Rename(sourceDir, targetDir); err != nil {
		if _, backupErr := os.Stat(backupDir); backupErr == nil {
			_ = os.Rename(backupDir, targetDir)
		}
		return err
	}
	_ = os.RemoveAll(backupDir)
	return nil
}
