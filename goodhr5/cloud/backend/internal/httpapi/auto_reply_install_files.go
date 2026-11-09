// Package httpapi 本文件在 PG 账号锁内安装简历原件，并恢复遇到断电或网络中断的文件操作。
package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type stagedReplyFile struct {
	replyImportFile
	StagingName string `json:"staging_name"`
}
type replyInstallIntent struct {
	Tenant string            `json:"tenant"`
	User   string            `json:"user"`
	Files  []stagedReplyFile `json:"files"`
}

var runtimeStageName = regexp.MustCompile(`^[0-9a-f]{64}-[0-9]+$`)

// verifiedReplyFile 检查普通文件、大小和完整摘要，拒绝符号链接、并发替换和跨租户暂存。
func verifiedReplyFile(path string, info replyImportFile) error {
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() || before.Size() != info.SizeBytes {
		return fmt.Errorf("云端附件大小不符")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(before, actual) {
		return fmt.Errorf("云端附件已发生变化")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, info.SizeBytes+1))
	if err != nil || n != info.SizeBytes || hex.EncodeToString(h.Sum(nil)) != info.SHA256 {
		return fmt.Errorf("云端附件摘要不符")
	}
	return nil
}

// replyVolumeDir 在私有卷内逐层核对已知目录，拒绝用符号链接将附件读写重定向到别处。
func replyVolumeDir(base string, create bool, parts ...string) error {
	root, err := os.Lstat(base)
	if err != nil || !root.IsDir() {
		return fmt.Errorf("云端附件卷尚未就绪")
	}
	dir := base
	for _, part := range parts {
		dir = filepath.Join(dir, part)
		info, err := os.Lstat(dir)
		if create && errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(dir)
		}
		if err != nil || !info.IsDir() {
			return fmt.Errorf("受管附件目录已变化")
		}
	}
	return nil
}

// syncReplyDir 在目录项变化后提交文件系统元数据，使断电后能按持久意图恢复。
func syncReplyDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// installReplyStateAttachments 先完整验签再安装；变更已通过账号 CAS 和删除封印，提交不明时按磁盘意图重查。
func installReplyStateAttachments(ctx context.Context, tenant, user string, changes []AutoReplyStateChange) (string, error) {
	scope := replyImportHash(tenant + "\x00" + user)
	owner := "auto_reply:v1:" + replyImportHash(user)
	base := os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR")
	stage := filepath.Join(base, ".runtime-stage")
	intent := replyInstallIntent{Tenant: tenant, User: user}
	for _, ch := range changes {
		if ch.Kind != "attachments" || ch.Deleted {
			if ch.StagingName != "" {
				return "", fmt.Errorf("无效的附件暂存引用")
			}
			continue
		}
		var file replyImportFile
		if err := json.Unmarshal(ch.Payload, &file); err != nil {
			return "", err
		}
		if file.RelativePath == "" && ch.StagingName == "" {
			continue
		}
		if !filepath.IsAbs(base) || !checkedReplyImportPath(file, owner) {
			return "", fmt.Errorf("受管附件路径与账号不匹配")
		}
		if err := replyVolumeDir(base, ch.StagingName != "", scope, replyImportHash(owner), "attachments"); err != nil {
			return "", err
		}
		if ch.StagingName == "" {
			// 已有附件可能仅更新候选关联或提取文字，仍须保证最终路径存在且内容正确。
			if err := verifiedReplyFile(filepath.Join(base, scope, filepath.FromSlash(file.RelativePath)), file); err != nil {
				return "", err
			}
			continue
		}
		if !runtimeStageName.MatchString(ch.StagingName) || !strings.HasPrefix(ch.StagingName, scope+"-") {
			return "", fmt.Errorf("暂存附件不属于当前账号")
		}
		if err := replyVolumeDir(base, false, ".runtime-stage"); err != nil {
			return "", err
		}
		if err := verifiedReplyFile(filepath.Join(stage, ch.StagingName), file); err != nil {
			return "", err
		}
		intent.Files = append(intent.Files, stagedReplyFile{replyImportFile: file, StagingName: ch.StagingName})
	}
	if len(intent.Files) == 0 {
		return "", nil
	}
	journal, err := newReplyInstallJournal(stage, intent)
	if err != nil {
		return "", err
	}
	for _, f := range intent.Files {
		if err = ctx.Err(); err != nil {
			return journal, err
		}
		dest := filepath.Join(base, scope, filepath.FromSlash(f.RelativePath))
		err = os.Link(filepath.Join(stage, f.StagingName), dest)
		if errors.Is(err, os.ErrExist) {
			err = verifiedReplyFile(dest, f.replyImportFile)
		}
		if err != nil {
			return journal, err
		}
	}
	if err = syncReplyDir(filepath.Join(base, scope, replyImportHash(owner), "attachments")); err != nil {
		return journal, err
	}
	return journal, nil
}

// newReplyInstallJournal 在发布任何最终文件前持久化待恢复意图，迁移与普通服务器写入共用。
func newReplyInstallJournal(stage string, intent replyInstallIntent) (string, error) {
	if len(intent.Files) == 0 || len(intent.Files) > 5 {
		return "", fmt.Errorf("附件安装意图数量不符")
	}
	if err := os.MkdirAll(stage, 0700); err != nil {
		return "", err
	}
	if st, err := os.Lstat(stage); err != nil || !st.IsDir() {
		return "", fmt.Errorf("暂存目录不符合要求")
	}
	journal, err := os.CreateTemp(stage, "intent-")
	if err != nil {
		return "", err
	}
	// 先落盘意图，进程在任意一份硬链接后退出都可按已提交引用恢复。
	err = json.NewEncoder(journal).Encode(intent)
	if err == nil {
		err = journal.Sync()
	}
	closeErr := journal.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(journal.Name())
		return "", fmt.Errorf("附件安装意图未能保存")
	}
	if err = syncReplyDir(stage); err != nil {
		return "", err
	}
	return journal.Name(), nil
}

// RecoverAutoReplyStagedFiles 专门扫描运行暂存目录，仅处理超过两小时的记录，活跃上传保持原状。
func (s *PostgresAutoReplyStore) RecoverAutoReplyStagedFiles(ctx context.Context) error {
	base := os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR")
	if !filepath.IsAbs(base) {
		return nil
	}
	dir := filepath.Join(base, ".runtime-stage")
	if st, err := os.Lstat(dir); err == nil && !st.IsDir() {
		return fmt.Errorf("运行暂存目录已变化")
	}
	items, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var combined error
	for _, e := range items {
		if err = ctx.Err(); err != nil {
			return err
		}
		if e.Type()&os.ModeSymlink != 0 || e.IsDir() {
			continue
		}
		st, err := e.Info()
		if err != nil {
			combined = errors.Join(combined, err)
			continue
		}
		if time.Since(st.ModTime()) < 2*time.Hour {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if strings.HasPrefix(e.Name(), "intent-") {
			if err = s.recoverReplyInstall(ctx, base, path); err != nil {
				combined = errors.Join(combined, err)
				continue
			}
		} else if !runtimeStageName.MatchString(e.Name()) {
			continue
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

// recoverReplyInstall 在与正常提交、删除相同的账号锁内复核当前真实引用，清除已失去所有引用的文件。
func (s *PostgresAutoReplyStore) recoverReplyInstall(ctx context.Context, base, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var intent replyInstallIntent
	if err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&intent); err != nil {
		return err
	}
	if intent.Tenant == "" || intent.User == "" || len(intent.Files) == 0 || len(intent.Files) > 5 {
		return fmt.Errorf("附件恢复意图不完整")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockCandidateDeletion(ctx, tx, intent.Tenant, true); err != nil {
		return err
	}
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM auto_reply_state_owners WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE`, intent.Tenant, intent.User).Scan(&version); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	owner := "auto_reply:v1:" + replyImportHash(intent.User)
	for _, f := range intent.Files {
		if !checkedReplyImportPath(f.replyImportFile, owner) {
			return fmt.Errorf("恢复意图的附件路径无效")
		}
		var live bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='attachments' AND payload->>'relative_path'=$3)`, intent.Tenant, intent.User, f.RelativePath).Scan(&live); err != nil {
			return err
		}
		if !live {
			if err = removeReplyFile(base, intent.Tenant, intent.User, f.RelativePath, f.SHA256); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
