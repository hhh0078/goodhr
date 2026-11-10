// 本文件接收已绑定设备的旧自动回复归档，校验账号、全部实体和内容寻址附件，只安装云端仍引用的文件。
package httpapi

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type replyImportFile struct {
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
}
type replyImportManifest struct {
	Version     int               `json:"version"`
	Owner       string            `json:"owner"`
	Records     int               `json:"records"`
	StateDigest string            `json:"state_digest"`
	Files       []replyImportFile `json:"files"`
}

var replyImportName = regexp.MustCompile(`^[a-f0-9]{64}(\.[a-z0-9]{1,12})?$`)

const replyImportArchiveLimit int64 = 4 << 30
const replyImportEntitiesLimit int64 = 256 << 20

// replyImportHash 与设备的历史文件和账号寻址保持一致，租户账号由已认证会话推导。
func replyImportHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// readReplyImportJSON 读取大小受限且仅含一个 JSON 文档的归档成员，同时校验完整内容的 SHA256。
func readReplyImportJSON(entry *zip.File, limit int64, out any) (string, error) {
	if entry == nil || entry.UncompressedSize64 > uint64(limit) {
		return "", fmt.Errorf("归档缺少资料或文件超出大小限制")
	}
	r, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	dec := json.NewDecoder(io.TeeReader(io.LimitReader(r, limit+1), h))
	if err = dec.Decode(out); err != nil {
		return "", err
	}
	if err = dec.Decode(&struct{}{}); err != io.EOF {
		return "", fmt.Errorf("归档里有多余的数据")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkedReplyImportPath 禁止设备指定任意目录；只接受该账号已有内容摘要格式的附件。
func checkedReplyImportPath(f replyImportFile, owner string) bool {
	return validReplyAttachmentPath(f.RelativePath, f.SHA256, owner) && f.SizeBytes > 0 && f.SizeBytes <= 20<<20
}

// validReplyAttachmentPath 仅允许该账号的内容寻址文件，迁移安装和物理清理共用同一规则。
func validReplyAttachmentPath(relative, digest, owner string) bool {
	name := path.Base(relative)
	return replyImportName.MatchString(name) && digest == strings.TrimSuffix(name, path.Ext(name)) && relative == replyImportHash(owner)+"/attachments/"+name
}

// unpackReplyArchive 在私有暂存目录核对完整数据、唯一文件列表、路径、字节数和 SHA256。
func unpackReplyArchive(file *os.File, size int64, staging, owner string) ([]AutoReplyStateChange, []replyImportFile, error) {
	zipr, err := zip.NewReader(file, size)
	if err != nil {
		return nil, nil, err
	}
	entries := map[string]*zip.File{}
	for _, entry := range zipr.File {
		if _, exists := entries[entry.Name]; exists || !entry.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("归档包含重复或特殊文件")
		}
		entries[entry.Name] = entry
	}
	var m replyImportManifest
	if _, err = readReplyImportJSON(entries["manifest.json"], 16<<20, &m); err != nil {
		return nil, nil, err
	}
	if m.Owner != owner || m.Version != 1 || m.Records <= 0 {
		return nil, nil, fmt.Errorf("归档与当前账号不符或内容为空")
	}
	if len(entries) != len(m.Files)+2 {
		return nil, nil, fmt.Errorf("附件数量与清单不一致")
	}
	var rows []AutoReplyStateChange
	digest, err := readReplyImportJSON(entries["entities.json"], replyImportEntitiesLimit, &rows)
	if err != nil {
		return nil, nil, err
	}
	if digest != m.StateDigest || len(rows) != m.Records {
		return nil, nil, fmt.Errorf("实体数量或摘要与清单不符")
	}
	expected := map[string]replyImportFile{}
	for i := range rows {
		if strings.HasPrefix(rows[i].Kind, "legacy_") || strings.HasPrefix(rows[i].Kind, "runtime_") || rows[i].Deleted {
			return nil, nil, fmt.Errorf("旧归档不得包含服务器运行指令")
		}
		if rows[i].Kind != "attachments" {
			continue
		}
		var f replyImportFile
		if err = json.Unmarshal(rows[i].Payload, &f); err != nil {
			return nil, nil, err
		}
		portable := strings.ReplaceAll(f.RelativePath, "\\", "/")
		if portable != f.RelativePath {
			var raw map[string]json.RawMessage
			if err = json.Unmarshal(rows[i].Payload, &raw); err != nil {
				return nil, nil, err
			}
			raw["relative_path"], _ = json.Marshal(portable)
			rows[i].Payload, err = json.Marshal(raw)
			if err != nil {
				return nil, nil, err
			}
			f.RelativePath = portable
		}
		if !checkedReplyImportPath(f, owner) {
			return nil, nil, fmt.Errorf("附件不属于当前账号的受管目录")
		}
		if prior, exists := expected[f.RelativePath]; exists && prior != f {
			return nil, nil, fmt.Errorf("附件元数据冲突")
		}
		expected[f.RelativePath] = f
	}
	if len(expected) != len(m.Files) {
		return nil, nil, fmt.Errorf("附件登记数与清单不同")
	}
	for _, f := range m.Files {
		if registered, ok := expected[f.RelativePath]; !ok || registered != f {
			return nil, nil, fmt.Errorf("附件没有对应的实体")
		}
		delete(expected, f.RelativePath)
		entry := entries["files/"+f.RelativePath]
		if entry == nil || entry.UncompressedSize64 != uint64(f.SizeBytes) {
			return nil, nil, fmt.Errorf("附件未完整上传")
		}
		if err = extractReplyImportFile(entry, filepath.Join(staging, path.Base(f.RelativePath)), f); err != nil {
			return nil, nil, err
		}
	}
	return rows, m.Files, nil
}

// extractReplyImportFile 写入临时普通文件，校验摘要后才允许进入长期存储目录。
func extractReplyImportFile(entry *zip.File, dest string, f replyImportFile) error {
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(src, f.SizeBytes+1))
	if err == nil {
		err = out.Sync()
	}
	closing := out.Close()
	if err != nil {
		return err
	}
	if closing != nil {
		return closing
	}
	if n != f.SizeBytes || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("附件摘要校验不通过")
	}
	return nil
}

// installReplyImportFiles 在账号锁内安装可达附件；内容寻址与排他硬链接防止覆盖已有文件。
func installReplyImportFiles(ctx context.Context, staging, scopeRoot string, files []replyImportFile, live map[string]bool) error {
	for _, f := range files {
		if !live[f.RelativePath] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		dest := filepath.Join(scopeRoot, filepath.FromSlash(f.RelativePath))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		linkErr := os.Link(filepath.Join(staging, path.Base(f.RelativePath)), dest)
		if linkErr == nil {
			continue
		}
		if !errors.Is(linkErr, os.ErrExist) {
			return linkErr
		}
		info, err := os.Lstat(dest)
		if err != nil || !info.Mode().IsRegular() || info.Size() != f.SizeBytes {
			return fmt.Errorf("已有云端附件的大小不匹配")
		}
		existing, err := os.Open(dest)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, existing)
		existing.Close()
		if err != nil || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
			return fmt.Errorf("已有云端附件的摘要不匹配")
		}
	}
	return nil
}

// journalReplyImportFiles 在安装归档的第一份附件前记录全部可达目标，导入中断后复用服务器的成员锁恢复。
func journalReplyImportFiles(base, tenant, user string, files []replyImportFile, live map[string]bool) ([]string, error) {
	var pending []stagedReplyFile
	for _, f := range files {
		if live[f.RelativePath] {
			pending = append(pending, stagedReplyFile{replyImportFile: f})
		}
	}
	var journals []string
	for len(pending) > 0 {
		n := min(5, len(pending))
		intent := replyInstallIntent{Tenant: tenant, User: user, Files: pending[:n]}
		path, err := newReplyInstallJournal(filepath.Join(base, ".runtime-stage"), intent)
		if err != nil {
			return journals, err
		}
		journals = append(journals, path)
		pending = pending[n:]
	}
	return journals, nil
}

// agentImportArchive 拒绝未绑定设备及跨账号归档，成功响应必须对应已提交的 PostgreSQL 收据。
func (s *AutoReplyService) agentImportArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "迁移只支持上传")
		return
	}
	access, ok := s.currentRequestContext(w, r, false, true)
	if !ok {
		return
	}
	user, err := s.store.activeTenantUserID(r.Context(), access.Tenant.ID, access.Session.Email)
	if err != nil {
		writeAutoReplyStoreError(w, err, "无法确认当前团队成员")
		return
	}
	owner := "auto_reply:v1:" + replyImportHash(user)
	digest := r.Header.Get("X-GoodHR-Archive-SHA256")
	raw, err := hex.DecodeString(digest)
	if err != nil || len(raw) != 32 || digest != strings.ToLower(digest) {
		writeAutoReplyError(w, 400, "INVALID_DIGEST", "缺少有效的归档摘要")
		return
	}
	base := os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR")
	if !filepath.IsAbs(base) {
		writeAutoReplyError(w, 503, "STORAGE_UNAVAILABLE", "云端附件目录尚未配置")
		return
	}
	scopeRoot := filepath.Join(base, replyImportHash(access.Tenant.ID+"\x00"+user))
	if err = os.MkdirAll(scopeRoot, 0700); err != nil {
		writeAutoReplyInternalError(w, "STORAGE_UNAVAILABLE", "暂时无法准备云端存储", err)
		return
	}
	staging, err := os.MkdirTemp(scopeRoot, ".migration-")
	if err != nil {
		writeAutoReplyInternalError(w, "STORAGE_UNAVAILABLE", "暂时无法准备迁移", err)
		return
	}
	defer os.RemoveAll(staging)
	archive, err := os.OpenFile(filepath.Join(staging, "source.zip"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		writeAutoReplyInternalError(w, "STORAGE_UNAVAILABLE", "暂时无法保存临时档案", err)
		return
	}
	defer archive.Close()
	r.Body = http.MaxBytesReader(w, r.Body, replyImportArchiveLimit)
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(archive, h), r.Body)
	if err != nil || digest != hex.EncodeToString(h.Sum(nil)) {
		writeAutoReplyError(w, 400, "INCOMPLETE_ARCHIVE", "归档未完整到达，原资料可重新上传")
		return
	}
	rows, files, err := unpackReplyArchive(archive, size, staging, owner)
	if err != nil {
		writeAutoReplyError(w, 400, "INVALID_ARCHIVE", "归档资料、附件或所属账号未通过校验")
		return
	}
	var journals []string
	receipt, err := s.store.ImportAutoReplyState(r.Context(), access.Tenant.ID, user, digest, rows, len(files), func(live map[string]bool) error {
		journals, err = journalReplyImportFiles(base, access.Tenant.ID, user, files, live)
		if err != nil {
			return err
		}
		return installReplyImportFiles(r.Context(), staging, scopeRoot, files, live)
	})
	if err != nil {
		writeAutoReplyStoreError(w, err, "导入未完成，旧资料已保留")
		return
	}
	for _, path := range journals {
		_ = os.Remove(path)
	}
	writeJSON(w, 200, receipt)
}
