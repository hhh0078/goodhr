// Package resume_import 文件作用：人工确认草稿后独立入库，持久保存各步骤收据，附属失败只补记未完成备注和岗位。
package resume_import

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"regexp"
	"strings"
	"unicode"
)

var mainlandPhone = regexp.MustCompile(`^1[3-9]\d{9}$`)
var internationalPhone = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// NormalizePhone 仅接受完整大陆手机或带国家码的国际号码，不补全任何掩码。
func NormalizePhone(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("()（）-", r) {
			return -1
		}
		return r
	}, value)
	for _, prefix := range []string{"+86", "0086"} {
		if strings.HasPrefix(value, prefix) && mainlandPhone.MatchString(strings.TrimPrefix(value, prefix)) {
			value = strings.TrimPrefix(value, prefix)
		}
	}
	if mainlandPhone.MatchString(value) || internationalPhone.MatchString(value) {
		return value
	}
	return ""
}

// Confirm 只保存用户审核过的条目；每条失败留在草稿，已保存条目无网络副作用。
func (m *Manager) Confirm(ctx context.Context, owner, token, id string, items []ConfirmItem) (Task, error) {
	if len(items) == 0 || len(items) > MaxFiles {
		return Task{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, input := range items {
		if seen[input.ID] {
			return Task{}, ErrInvalid
		}
		seen[input.ID] = true
		if err := m.confirmOne(ctx, owner, token, id, input); err != nil {
			return Task{}, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, err := m.findLocked(owner, id)
	if err != nil {
		return Task{}, err
	}
	return cloneTask(stored.Task), nil
}

// confirmOne 在首个云端写入前持久保存确认内容；重复请求只重放相同幂等步骤。
func (m *Manager) confirmOne(ctx context.Context, owner, token, id string, input ConfirmItem) error {
	m.mu.Lock()
	stored, err := m.findLocked(owner, id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	item := findItem(stored, input.ID)
	if item == nil {
		m.mu.Unlock()
		return ErrInvalid
	}
	if item.Status == "saved" {
		m.mu.Unlock()
		return nil
	}
	if m.saving[input.ID] || item.Status == "queued" || item.Status == "parsing" {
		m.mu.Unlock()
		return ErrBusy
	}
	receipt := stored.Confirmations[input.ID]
	if receipt == nil {
		if item.Status != "ready" && item.Status != "needs_phone" {
			m.mu.Unlock()
			return fmt.Errorf("先解析成功，再审核入库")
		}
		var candidate cloud.LibraryCandidate
		var fields map[string]json.RawMessage
		if len(input.Candidate) > 2<<20 || json.Unmarshal(input.Candidate, &candidate) != nil || json.Unmarshal(input.Candidate, &fields) != nil || fields == nil {
			m.mu.Unlock()
			return fmt.Errorf("简历草稿没读出来，请重新核对")
		}
		phone := NormalizePhone(candidate.Phone)
		if strings.TrimSpace(candidate.CandidateName) == "" || phone == "" {
			m.mu.Unlock()
			return fmt.Errorf("姓名和有效手机号还缺一项，补齐就能入库")
		}
		if strings.TrimSpace(input.CandidateID) != "" && candidate.RowVersion <= 0 {
			m.mu.Unlock()
			return fmt.Errorf("合并前请重新读取这份简历，确认最新版本")
		}
		if len([]rune(input.Note)) > 5000 || len(input.PositionIDs) > 50 {
			m.mu.Unlock()
			return fmt.Errorf("备注或岗位太多，请分开保存")
		}
		positions := []string{}
		positionSet := map[string]bool{}
		for _, position := range input.PositionIDs {
			position = strings.TrimSpace(position)
			if position == "" || len(position) > 128 {
				m.mu.Unlock()
				return ErrInvalid
			}
			if !positionSet[position] {
				positions = append(positions, position)
				positionSet[position] = true
			}
		}
		fields["phone"], _ = json.Marshal(phone)
		delete(fields, "id")
		delete(fields, "idempotency_key")
		delete(fields, "is_resume")
		encoded, _ := json.Marshal(fields)
		hash := sha256.Sum256(append(append([]byte{}, encoded...), []byte(input.CandidateID)...))
		fingerprint := hex.EncodeToString(hash[:])
		receipt = &confirmation{Fields: encoded, CandidateID: strings.TrimSpace(input.CandidateID), Key: "resume-import:" + id + ":" + input.ID + ":" + fingerprint[:16], Fingerprint: fingerprint, Note: strings.TrimSpace(input.Note), PositionIDs: positions, PositionsSaved: []string{}}
		stored.Confirmations[input.ID] = receipt
		candidate.Phone = phone
		item.Candidate = candidate
		if err := m.persistLocked(stored); err != nil {
			delete(stored.Confirmations, input.ID)
			m.mu.Unlock()
			return err
		}
	}
	m.saving[input.ID] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.saving, input.ID); m.mu.Unlock() }()
	if !receipt.CandidateSaved {
		result, saveErr := m.cloud.SaveLibraryCandidate(ctx, token, receipt.CandidateID, receipt.Fields, receipt.Key)
		m.mu.Lock()
		if saveErr != nil {
			item.Error = safeCredentialError(saveErr, token)
			var apiErr *cloud.APIError
			// 明确4xx表示没有写入，允许重新核对字段；超时和5xx保留原请求，避免结果不明时重复创建。
			if errors.As(saveErr, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != 408 && apiErr.StatusCode != 429 {
				delete(stored.Confirmations, input.ID)
			}
			_ = m.persistLocked(stored)
			m.mu.Unlock()
			return nil
		}
		receipt.CandidateSaved = true
		receipt.CandidateID = result.ID
		item.CandidateID = result.ID
		item.Candidate.ID = result.ID
		item.Candidate.RowVersion = result.RowVersion
		item.Status = "partial_success"
		item.Error = ""
		persistErr := m.persistLocked(stored)
		m.mu.Unlock()
		if persistErr != nil {
			return fmt.Errorf("简历已保存，但本地收据没写好，请恢复磁盘后重试")
		}
	}
	if receipt.Note != "" && !receipt.NoteSaved {
		if err := m.cloud.AddLibraryNote(ctx, token, receipt.CandidateID, receipt.Note, receipt.Key+":note"); err != nil {
			return m.recordPartial(stored, item, err, token)
		}
		m.mu.Lock()
		receipt.NoteSaved = true
		err = m.persistLocked(stored)
		m.mu.Unlock()
		if err != nil {
			return err
		}
	}
	for _, position := range receipt.PositionIDs {
		if containsString(receipt.PositionsSaved, position) {
			continue
		}
		hash := sha256.Sum256([]byte(position))
		key := receipt.Key + ":position:" + hex.EncodeToString(hash[:8])
		if err := m.cloud.AddLibraryRecommendation(ctx, token, receipt.CandidateID, position, key); err != nil {
			return m.recordPartial(stored, item, err, token)
		}
		m.mu.Lock()
		receipt.PositionsSaved = append(receipt.PositionsSaved, position)
		err = m.persistLocked(stored)
		m.mu.Unlock()
		if err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item.Status, item.Error = "saved", ""
	m.refreshLocked(stored)
	return m.persistLocked(stored)
}

// recordPartial 保存主体已成功的收据和错误，后续只补齐附属记录。
func (m *Manager) recordPartial(stored *storedTask, item *Item, err error, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.Status, item.Error = "partial_success", safeCredentialError(err, token)
	m.refreshLocked(stored)
	return m.persistLocked(stored)
}

// MarkSaved 验证当前账号确实可访问指定候选人后，记录独立保存收据。
func (m *Manager) MarkSaved(ctx context.Context, owner, token, id string, items []SavedItem) (Task, error) {
	if len(items) == 0 || len(items) > MaxFiles {
		return Task{}, ErrInvalid
	}
	for _, input := range items {
		m.mu.Lock()
		stored, err := m.findLocked(owner, id)
		if err != nil {
			m.mu.Unlock()
			return Task{}, err
		}
		item := findItem(stored, input.ID)
		if item == nil || input.CandidateID == "" {
			m.mu.Unlock()
			return Task{}, ErrInvalid
		}
		if item.Status == "saved" {
			if item.CandidateID != input.CandidateID {
				m.mu.Unlock()
				return Task{}, ErrInvalid
			}
			m.mu.Unlock()
			continue
		}
		if m.saving[input.ID] || stored.Confirmations[input.ID] != nil || item.Status == "queued" || item.Status == "parsing" {
			m.mu.Unlock()
			return Task{}, ErrBusy
		}
		m.saving[input.ID] = true
		m.mu.Unlock()
		result, cloudErr := m.cloud.LibraryCandidateDetail(ctx, token, input.CandidateID)
		m.mu.Lock()
		delete(m.saving, input.ID)
		if cloudErr != nil {
			m.mu.Unlock()
			return Task{}, cloudErr
		}
		item.CandidateID = result.ID
		item.Candidate.ID = result.ID
		item.Candidate.RowVersion = result.RowVersion
		item.Status, item.Error = "saved", ""
		m.refreshLocked(stored)
		err = m.persistLocked(stored)
		m.mu.Unlock()
		if err != nil {
			return Task{}, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, err := m.findLocked(owner, id)
	if err != nil {
		return Task{}, err
	}
	return cloneTask(stored.Task), nil
}

// containsString 判断某个岗位的幂等收据是否已经保存。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
