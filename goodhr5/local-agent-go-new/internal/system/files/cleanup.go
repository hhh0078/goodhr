// Package files 文件作用：清理 GoodHR 内部目录中过期的临时文件，不触碰用户下载文件。
package files

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// RemoveOlderThan 删除指定目录中修改时间早于截止时间的普通文件和符号链接。
func RemoveOlderThan(root string, cutoff time.Time) (int, error) {
	deleted := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.ModTime().Before(cutoff) {
			return nil
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		deleted++
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return deleted, fmt.Errorf("清理过期临时文件失败：%w", err)
	}
	return deleted, nil
}
