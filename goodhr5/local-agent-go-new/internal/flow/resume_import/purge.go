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
	"time"
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
			if !linkedImport(item, batch.Confirmations[item.ID], candidateID, requestHashes) {
				continue
			}
			if busy {
				return ErrBusy
			}
			if err := m.eraseImportItem(ctx, batch, i, purgeDownload); err != nil {
				return err
			}
		}
	}
	return nil
}

// PurgeCandidateCopies 从已核实的文件建立锚，先处理同正文的不同PDF，再处理同字节PDF，最后由调用方移除明确入库收据。
func (m *Manager) PurgeCandidateCopies(ctx context.Context, owner, candidateID string, deletedAt time.Time, documentHashes, requestHashes []string, purgeDownload func(context.Context, string, string) error) error {
	if m == nil || owner == "" || candidateID == "" {
		return ErrInvalid
	}
	if deletedAt.IsZero() {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	type copyProof struct {
		batch             *storedTask
		item              Item
		fileHash, textKey string
		linked            bool
	}
	var copies []copyProof
	hashes, texts := map[string]bool{}, map[string]bool{}
	sharedFiles, sharedTexts := map[string]bool{}, map[string]bool{}
	for _, h := range documentHashes {
		if len(h) == 64 {
			hashes[h] = true
		}
	}
	for _, batch := range m.tasks {
		if batch.OwnerID != owner || batch.CreatedAt <= 0 {
			continue
		}
		for _, item := range batch.Task.Items {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !strings.EqualFold(filepath.Ext(item.FileName), ".pdf") {
				continue
			}
			linked := linkedImport(item, batch.Confirmations[item.ID], candidateID, requestHashes)
			other := !linked && (item.Candidate.ID != "" || item.CandidateID != "" || batch.Confirmations[item.ID] != nil)
			if batch.CreatedAt > deletedAt.UnixNano() && !other {
				continue
			}
			digest, err := m.importFileHash(batch.Task.ID, item.ID, batch.Paths[item.ID])
			if err != nil {
				if errors.Is(err, ErrInvalid) {
					continue
				}
				return err
			}
			textKey := importTextKey(batch, item, digest)
			// 连另一云编号或仍有未确认写入的同一份资料，不作为自动扩展清理的依据。
			if other {
				if digest != "" {
					sharedFiles[digest] = true
				}
				if textKey != "" {
					sharedTexts[textKey] = true
				}
				continue
			}
			copies = append(copies, copyProof{batch, item, digest, textKey, linked})
			if linked && digest != "" {
				hashes[digest] = true
			}
		}
	}
	for digest := range sharedFiles {
		delete(hashes, digest)
	}
	for _, p := range copies {
		if hashes[p.fileHash] && !sharedTexts[p.textKey] && p.textKey != "" {
			texts[p.textKey] = true
		}
	}
	// 保存同内容匹配的证据到最后，断在任何一步都能由原事件和剩余收据重试。
	for _, exact := range []bool{false, true} {
		for _, p := range copies {
			if p.linked || sharedFiles[p.fileHash] || sharedTexts[p.textKey] || hashes[p.fileHash] != exact {
				continue
			}
			pending := p.batch.Discards[p.item.ID]
			if pending.CandidateID != candidateID && (pending.Revision != "" || !exact && !texts[p.textKey]) {
				continue
			}
			_, index, intent, err := m.discardSelection(owner, p.batch.Task.ID, p.item.ID)
			if err != nil {
				return err
			}
			if pending.Revision != "" && pending.Revision != intent.Revision {
				// 清理期间文件被替换，原事件不能授权删除新版本。保留新版供本人重新预览确认。
				continue
			}
			if pending.Revision == "" && p.fileHash != intent.FileHash {
				return ErrDraftChanged
			}
			intent.CandidateID = candidateID
			if err = m.discardLocked(ctx, p.batch, index, intent, purgeDownload); err != nil {
				return err
			}
		}
	}
	return nil
}

// importTextKey 仅用下载导入时绑定的原文件和本地提取全文；正文必须含完整联系方式，绝不靠模型字段单独归人。
func importTextKey(batch *storedTask, item Item, digest string) string {
	source := batch.Sources[item.ID]
	candidate := item.Candidate
	if digest == "" || source.PlatformID == "" || candidate.IsResume == nil || !*candidate.IsResume {
		return ""
	}
	id := sha256.Sum256([]byte(batch.OwnerID + ":" + digest))
	if batch.Task.ID != hex.EncodeToString(id[:16]) {
		return ""
	}
	text := strings.Join(strings.Fields(candidate.RawText), "")
	name := strings.TrimSpace(candidate.CandidateName)
	phone := NormalizePhone(candidate.Phone)
	email := strings.ToLower(strings.TrimSpace(candidate.Email))
	if len([]rune(text)) < 128 || name == "" || phone == "" || !strings.Contains(email, "@") || !strings.Contains(text, name) || !strings.Contains(text, phone) || !strings.Contains(strings.ToLower(text), email) {
		return ""
	}
	sum := sha256.Sum256([]byte(source.PlatformID + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

// linkedImport 以条目、确认收据或云端幂等键核对已保存的关联，以显式云编号为优先依据。
func linkedImport(item Item, receipt *confirmation, candidateID string, requestHashes []string) bool {
	matched := false
	for _, id := range []string{item.CandidateID, item.Candidate.ID} {
		if id != "" && id != candidateID {
			return false
		}
		if id == candidateID {
			matched = true
		}
	}
	if receipt == nil {
		return matched
	}
	if receipt.CandidateID != "" {
		return receipt.CandidateID == candidateID
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(receipt.Key)))
	return matched || receipt.Key != "" && slices.Contains(requestHashes, hex.EncodeToString(sum[:]))
}

// eraseImportItem 在持锁状态下先清理来源和专用副本，再持久化剩余条目；写盘失败允许用原收据重试。
func (m *Manager) eraseImportItem(ctx context.Context, batch *storedTask, i int, purgeDownload func(context.Context, string, string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := batch.Task.Items[i].ID
	if _, ok := batch.Sources[id]; ok {
		if purgeDownload == nil {
			return ErrBusy
		}
		if err := purgeDownload(ctx, batch.OwnerID, batch.Task.ID); err != nil {
			return err
		}
	}
	// 下载队列可能等文件占用或数据库写入，物理删除前再次核对已授权的私有副本。
	if intent, ok := batch.Discards[id]; ok {
		now, err := m.importFileHash(batch.Task.ID, id, batch.Paths[id])
		if err != nil {
			return err
		}
		if now != "" && now != intent.FileHash {
			return ErrDraftChanged
		}
	}
	if err := m.removeImportCopy(batch.Task.ID, id, batch.Paths[id]); err != nil {
		return err
	}
	copy := *batch
	copy.Task.Items = append(append([]Item{}, batch.Task.Items[:i]...), batch.Task.Items[i+1:]...)
	copy.Paths = withoutImportItem(batch.Paths, id)
	copy.Sources = withoutImportItem(batch.Sources, id)
	copy.Confirmations = withoutImportItem(batch.Confirmations, id)
	copy.AIStarted = withoutImportItem(batch.AIStarted, id)
	copy.Discards = withoutImportItem(batch.Discards, id)
	m.refreshLocked(&copy)
	if err := m.persistLocked(&copy); err != nil {
		return err
	}
	*batch = copy
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
