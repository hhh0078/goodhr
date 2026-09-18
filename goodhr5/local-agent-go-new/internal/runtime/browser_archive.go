// Package runtime 文件作用：原子替换浏览器资源包中的指定资源，保留其他压缩数据与首次原始备份。
package runtime

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// replaceBrowserArchiveResources 原样复制未修改的 ZIP 条目，关闭全部句柄后替换并保留原始备份。
func replaceBrowserArchiveResources(archivePath string, replacements map[string][]byte) error {
	info, err := os.Stat(archivePath)
	if err != nil {
		return err
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	temporary, err := os.CreateTemp(filepath.Dir(archivePath), "goodhr-resource-*.ja")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	writer := zip.NewWriter(temporary)
	defer writer.Close()
	remaining := make(map[string][]byte, len(replacements))
	for name, content := range replacements {
		remaining[name] = content
	}
	for _, file := range reader.File {
		header := file.FileHeader
		if content, changed := remaining[file.Name]; changed {
			stream, err := writer.CreateHeader(&header)
			if err != nil {
				return err
			}
			if _, err := stream.Write(content); err != nil {
				return err
			}
			delete(remaining, file.Name)
		} else {
			stream, err := writer.CreateRaw(&header)
			if err != nil {
				return err
			}
			raw, err := file.OpenRaw()
			if err != nil {
				return err
			}
			if _, err := io.Copy(stream, raw); err != nil {
				return err
			}
		}
	}
	for name, content := range remaining {
		stream, err := writer.Create(name)
		if err != nil {
			return err
		}
		if _, err := stream.Write(content); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	if err := reader.Close(); err != nil {
		return err
	}
	backup := archivePath + ".goodhr-original"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		content, err := os.ReadFile(archivePath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(backup, content, 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), archivePath); err != nil {
		return fmt.Errorf("替换浏览器资源失败，请先关闭浏览器：%w", err)
	}
	return nil
}
