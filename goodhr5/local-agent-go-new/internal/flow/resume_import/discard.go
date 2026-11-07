// Package resume_import 本文件校验当前账号选中的未入库草稿，按草稿内容和受管文件摘要确认移除。
package resume_import

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// discardIntent 只保留确认版本和文件摘要，意外退出后可继续同一项清理。
type discardIntent struct {
	Revision    string `json:"revision"`
	FileHash    string `json:"file_hash,omitempty"`
	CandidateID string `json:"candidate_id,omitempty"`
}

// DiscardPreview 返回当前草稿和本机副本绑定的确认版本，不返回文件路径或内容摘要。
func (m *Manager) DiscardPreview(owner, taskID, itemID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _, intent, err := m.discardSelection(owner, taskID, itemID)
	return intent.Revision, err
}

// DiscardDraft 复核用户确认的同一版本，先持久保存清理意图再删除文件，最后只移除本条草稿。
func (m *Manager) DiscardDraft(ctx context.Context, owner, taskID, itemID, revision string, purgeDownload func(context.Context, string, string) error) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	batch, index, intent, err := m.discardSelection(owner, taskID, itemID)
	if err != nil {
		return Task{}, err
	}
	if revision == "" || revision != intent.Revision {
		return Task{}, fmt.Errorf("%w，请重新查看这份草稿后再确认移除", ErrDraftChanged)
	}
	if err = m.discardLocked(ctx, batch, index, intent, purgeDownload); err != nil {
		return Task{}, err
	}
	return cloneTask(batch.Task), nil
}

// discardLocked 把已核实的草稿移除收据写在同一批次，资料与文件全部清理后才移除收据。
func (m *Manager) discardLocked(ctx context.Context, batch *storedTask, index int, intent discardIntent, purgeDownload func(context.Context, string, string) error) error {
	itemID := batch.Task.Items[index].ID
	if old, ok := batch.Discards[itemID]; !ok || old.Revision != intent.Revision {
		copy := *batch
		copy.Discards = withoutImportItem(batch.Discards, itemID)
		copy.Discards[itemID] = intent
		copy.Task.Items = append([]Item{}, batch.Task.Items...)
		copy.Task.Items[index].Status = "discarding"
		if err := m.persistLocked(&copy); err != nil {
			return err
		}
		*batch = copy
	}
	return m.eraseImportItem(ctx, batch, index, purgeDownload)
}

// ErrDraftChanged 表示一份已预览的草稿或附件已变化，需要用户重新确认。
var ErrDraftChanged = errors.New("草稿或文件已变化")

// discardSelection 仅匹配本账号未入库、没有在途解析或保存的条目，复核磁盘当前文件。
func (m *Manager) discardSelection(owner, taskID, itemID string) (*storedTask, int, discardIntent, error) {
	var intent discardIntent
	batch, err := m.findLocked(owner, taskID)
	if err != nil {
		return nil, 0, intent, err
	}
	for i, item := range batch.Task.Items {
		if item.ID != itemID {
			continue
		}
		if m.active[taskID] != nil {
			return nil, 0, intent, ErrBusy
		}
		// 其他条目的保存回包也持有原切片指针，等待完成再换入新切片。
		for _, other := range batch.Task.Items {
			if _, busy := m.saving[other.ID]; busy {
				return nil, 0, intent, ErrBusy
			}
		}
		if batch.Confirmations[itemID] != nil || item.CandidateID != "" || item.Candidate.ID != "" || item.Status == "saved" || item.Status == "partial_success" {
			return nil, 0, intent, fmt.Errorf("%w，这份简历已有入库记录，请从简历库核对", ErrInvalid)
		}
		hash, err := m.importFileHash(taskID, itemID, batch.Paths[itemID])
		if err != nil {
			return nil, 0, intent, err
		}
		if stored, ok := batch.Discards[itemID]; ok && (hash == "" || hash == stored.FileHash) {
			return batch, i, stored, nil
		}
		raw, err := json.Marshal([]any{owner, taskID, item, batch.Sources[item.ID], hash})
		if err != nil {
			return nil, 0, intent, err
		}
		sum := sha256.Sum256(raw)
		return batch, i, discardIntent{Revision: hex.EncodeToString(sum[:]), FileHash: hash}, nil
	}
	return nil, 0, intent, ErrNotFound
}

// importFileHash 在当前受管根内按生成的编号重建路径，兼容搬迁，拒绝链接和超过上限的文件。
func (m *Manager) importFileHash(batch, id, storedPath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(storedPath))
	if !filepath.IsLocal(batch) || filepath.Base(batch) != batch || !filepath.IsLocal(id) || filepath.Base(id) != id || !supportedExtension(ext) || filepath.Base(filepath.Dir(storedPath)) != batch || filepath.Base(storedPath) != id+ext {
		return "", ErrInvalid
	}
	root, err := os.OpenRoot(m.root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	dir, err := root.Lstat(batch)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !dir.IsDir() {
		return "", ErrInvalid
	}
	name := filepath.Join(batch, id+ext)
	prior, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !prior.Mode().IsRegular() || prior.Size() > MaxFileBytes {
		return "", ErrInvalid
	}
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(prior, info) {
		return "", ErrDraftChanged
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return "", err
	}
	if n != info.Size() {
		return "", ErrDraftChanged
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
