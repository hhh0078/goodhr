// Package resume_download 本文件只清理受管下载中有精确记录、摘要吻合且没有其他引用的原件。
package resume_download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	resumeimport "goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/storage"
)

// purgeFiles 复用本地全部下载记录和已确认的下载目录，不搜索用户文件夹。
type purgeFiles struct {
	history func(context.Context) ([]storage.DownloadRecord, error)
	allowed func(string) bool
}

// SetFilePurge 在启动下载队列前接入下载根检查；没有完整来源收据时保留磁盘文件。
func (m *Manager) SetFilePurge(history func(context.Context) ([]storage.DownloadRecord, error), allowed func(string) bool) {
	if history != nil && allowed != nil {
		m.files = &purgeFiles{history, allowed}
	}
}

// removeVerifiedFiles 逐条检查下载来源、账号队列和全局历史；其他条目共用路径或文件变化时只移除本项记录。
func (m *Manager) removeVerifiedFiles(ctx context.Context, removed map[string]entry) error {
	history, err := m.files.history(ctx)
	if err != nil {
		return err
	}
	owned := map[string]bool{}
	protected := map[string]bool{}
	for _, record := range history {
		if record.FilePath == "" {
			continue
		}
		item, target := removed[record.ID]
		if target && record.Status == "saved" && downloadPathKey(item.Path) == downloadPathKey(record.FilePath) {
			owned[record.ID] = true
		} else {
			protected[downloadPathKey(record.FilePath)] = true
		}
	}
	for id, item := range m.entries {
		if _, target := removed[id]; !target && item.Path != "" {
			protected[downloadPathKey(item.Path)] = true
		}
	}
	for id, item := range removed {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !owned[id] || protected[downloadPathKey(item.Path)] || len(item.Hash) != 64 || !filepath.IsAbs(item.Path) || item.Platform == "" {
			continue
		}
		if err := m.removeDownloadCopy(item); err != nil {
			return err
		}
	}
	return nil
}

// downloadPathKey 在 Windows 和 macOS 默认卷上按不区分大小写保护其他引用；多保留一个文件优于误删。
func downloadPathKey(path string) string {
	key := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(key); err == nil {
		key = resolved
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(key)
	}
	return key
}

// removeDownloadCopy 在受管目录句柄内复核普通文件、原始摘要和文件身份；先关闭文件以支持Windows删除。
func (m *Manager) removeDownloadCopy(item entry) error {
	path := filepath.Clean(item.Path)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	actual := filepath.Join(parent, filepath.Base(path))
	if !m.files.allowed(actual) {
		return nil
	}
	dir, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	name := filepath.Base(path)
	before, err := dir.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > resumeimport.MaxFileBytes {
		return nil
	}
	file, err := dir.Open(name)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, readErr := io.Copy(h, io.LimitReader(file, resumeimport.MaxFileBytes+1))
	info, statErr := file.Stat()
	closeErr := file.Close()
	if err = errors.Join(readErr, statErr, closeErr); err != nil {
		return err
	}
	if n != before.Size() || !os.SameFile(before, info) || hex.EncodeToString(h.Sum(nil)) != item.Hash {
		return nil
	}
	final, err := dir.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(info, final) || !final.Mode().IsRegular() || info.Size() != final.Size() || !info.ModTime().Equal(final.ModTime()) {
		return nil
	}
	if err = dir.Remove(name); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
