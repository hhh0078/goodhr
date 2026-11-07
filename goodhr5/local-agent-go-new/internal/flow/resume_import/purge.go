// Package resume_import 本文件按已核实的云端编号清理导入草稿、入库收据和任务目录内的专用原件。
package resume_import

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// PurgeCandidate 删除已关联条目，批量导入中的其他简历继续可用。文件先删，收据最后移除以便断点重试。
func (m *Manager) PurgeCandidate(ctx context.Context, owner, candidateID string, requestHashes []string, purgeDownload func(context.Context, string, string) error) error {
	if m == nil || owner == "" || candidateID == "" {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, batch := range m.tasks {
		if batch.OwnerID != owner {
			continue
		}
		// 旧回包可能持有本批次的条目指针，仅在此批匹配删除事件时才需要等这些写入收尾。
		busy := m.active[batch.Task.ID] != nil
		for _, item := range batch.Task.Items {
			if destination, saving := m.saving[item.ID]; saving {
				if destination == candidateID {
					return ErrBusy
				}
				busy = true
			}
		}
		for i := len(batch.Task.Items) - 1; i >= 0; i-- {
			item := batch.Task.Items[i]
			receipt := batch.Confirmations[item.ID]
			matched := item.CandidateID == candidateID || item.Candidate.ID == candidateID
			if receipt != nil {
				sum := sha256.Sum256([]byte(strings.TrimSpace(receipt.Key)))
				matched = matched || receipt.CandidateID == candidateID || receipt.Key != "" && slices.Contains(requestHashes, hex.EncodeToString(sum[:]))
			}
			if !matched {
				continue
			}
			if busy {
				return ErrBusy
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, ok := batch.Sources[item.ID]; ok {
				if purgeDownload == nil {
					return ErrBusy
				}
				if err := purgeDownload(ctx, owner, batch.Task.ID); err != nil {
					return err
				}
			}
			if err := m.removeImportCopy(batch.Task.ID, item.ID, batch.Paths[item.ID]); err != nil {
				return err
			}
			// 先写副本再换入内存，写盘失败时原收据仍能定位已移除的文件。
			copy := *batch
			copy.Task.Items = append(append([]Item{}, batch.Task.Items[:i]...), batch.Task.Items[i+1:]...)
			copy.Paths = withoutImportItem(batch.Paths, item.ID)
			copy.Sources = withoutImportItem(batch.Sources, item.ID)
			copy.Confirmations = withoutImportItem(batch.Confirmations, item.ID)
			copy.AIStarted = withoutImportItem(batch.AIStarted, item.ID)
			m.refreshLocked(&copy)
			if err := m.persistLocked(&copy); err != nil {
				return err
			}
			*batch = copy
		}
	}
	return nil
}

// withoutImportItem 复制同一批次其余条目，以免落盘失败时丢失重试依据。
func withoutImportItem[T any](items map[string]T, id string) map[string]T {
	result := make(map[string]T, len(items))
	for key, value := range items {
		if key != id {
			result[key] = value
		}
	}
	return result
}

// removeImportCopy 在 Windows 与 macOS 都以受限目录句柄操作，仅删除生成的批次/条目文件，外部来源路径不参与删除。
func (m *Manager) removeImportCopy(batch, id, path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	// 老目录搬迁后，SQLite 里仍可能是旧绝对路径，只在当前受管根内重建同一批次和文件编号。
	if path == "" || filepath.Base(batch) != batch || filepath.Base(id) != id || !supportedExtension(ext) ||
		!filepath.IsLocal(batch) || !filepath.IsLocal(id) || filepath.Base(filepath.Dir(path)) != batch || filepath.Base(path) != id+ext {
		return nil
	}
	root, err := os.OpenRoot(m.root)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(batch)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	name := filepath.Join(batch, id+ext)
	info, err = root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	err = root.Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
