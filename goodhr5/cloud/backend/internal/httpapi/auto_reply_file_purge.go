// 本文件从云端持久待办清理已删除简历的私有附件，岗位停止后也会继续处理。
package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// startReplyFilePurge 定期分批恢复删除任务；未配置服务器附件卷时保留待办。
func startReplyFilePurge(ctx context.Context, db *sql.DB, base string) {
	if !filepath.IsAbs(base) {
		return
	}
	store := NewPostgresAutoReplyStore(db)
	go func() {
		tick := time.NewTicker(15 * time.Minute)
		defer tick.Stop()
		for {
			if err := store.RecoverAutoReplyStagedFiles(ctx); err != nil && ctx.Err() == nil {
				log.Printf("自动回复暂存附件恢复暂未完成：%v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			for i := 0; i < 16; i++ {
				did, err := advanceReplyFilePurge(ctx, db, base)
				if err != nil {
					log.Printf("自动回复附件清理暂未完成：%v", err)
				}
				if !did {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

// advanceReplyFilePurge 在与写入、迁移共用的成员行锁内复查待办和最后引用，物理删除成功才移除待办。
func advanceReplyFilePurge(ctx context.Context, db *sql.DB, base string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var tenant, user, id string
	err = tx.QueryRowContext(ctx, `SELECT o.tenant_id,o.user_id,e.id FROM auto_reply_cloud_entities e
	 JOIN auto_reply_state_owners o USING(tenant_id,user_id)
	 WHERE e.kind='purge_files' AND (e.processed_status<>'retry' OR e.updated_at<$1)
	 ORDER BY e.updated_at,e.tenant_id,e.user_id,e.id LIMIT 1 FOR UPDATE OF o SKIP LOCKED`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)).Scan(&tenant, &user, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var relative, digest string
	var referenced bool
	err = tx.QueryRowContext(ctx, `SELECT coalesce(j.payload->>'path',''),coalesce(j.payload->>'sha256',''),EXISTS(
	 SELECT 1 FROM auto_reply_cloud_entities a WHERE a.tenant_id=j.tenant_id AND a.user_id=j.user_id AND a.kind='attachments' AND a.payload->>'relative_path'=j.payload->>'path')
	 FROM auto_reply_cloud_entities j WHERE j.tenant_id=$1 AND j.user_id=$2 AND j.kind='purge_files' AND j.id=$3`, tenant, user, id).Scan(&relative, &digest, &referenced)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !referenced {
		if removeErr := removeReplyFile(base, tenant, user, relative, digest); removeErr != nil {
			_, err = tx.ExecContext(ctx, `UPDATE auto_reply_cloud_entities SET processed_status='retry',updated_at=$4
			 WHERE tenant_id=$1 AND user_id=$2 AND kind='purge_files' AND id=$3`, tenant, user, id, time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				return false, err
			}
			if err = tx.Commit(); err != nil {
				return false, err
			}
			return true, removeErr
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='purge_files' AND id=$3", tenant, user, id); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE auto_reply_state_owners SET version=version+1 WHERE tenant_id=$1 AND user_id=$2", tenant, user); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// removeReplyFile 在成员锁内解除已通过路径、文件身份和摘要核对的最后一个引用。
func removeReplyFile(base, tenant, user, relative, digest string) error {
	err := withVerifiedReplyFile(base, tenant, user, relative, digest, 0, func(dir *os.Root, name string, _ *os.File, _ os.FileInfo) error { return dir.Remove(name) })
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// withVerifiedReplyFile 逐层固定团队与成员私有目录并校验文件身份、大小和摘要，供读取与回收共用。
func withVerifiedReplyFile(base, tenant, user, relative, digest string, expectedSize int64, use func(*os.Root, string, *os.File, os.FileInfo) error) error {
	owner := "auto_reply:v1:" + replyImportHash(user)
	if !filepath.IsAbs(base) || !validReplyAttachmentPath(relative, digest, owner) {
		return fmt.Errorf("附件路径与所属账号或摘要不一致")
	}
	baseInfo, err := os.Lstat(base)
	if err != nil || !baseInfo.IsDir() {
		return fmt.Errorf("云端附件卷尚未挂载")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(baseInfo, opened) {
		return fmt.Errorf("服务器附件卷已经变化")
	}
	components := append([]string{replyImportHash(tenant + "\x00" + user)}, strings.Split(relative, "/")...)
	for _, dir := range components[:len(components)-1] {
		info, err := root.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err != nil || !info.IsDir() {
			return fmt.Errorf("附件所属目录已被替换")
		}
		child, err := root.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer child.Close()
		got, err := child.Stat(".")
		if err != nil || !os.SameFile(info, got) {
			return fmt.Errorf("附件所属目录正在变化")
		}
		root = child
	}
	name := components[len(components)-1]
	before, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 20<<20 || (expectedSize > 0 && before.Size() != expectedSize) {
		return fmt.Errorf("附件类型或大小不符")
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	now, err := f.Stat()
	if err != nil || !os.SameFile(before, now) {
		return fmt.Errorf("附件已经变化")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, before.Size()+1))
	if err != nil || n != before.Size() || hex.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("附件内容与入库摘要不一致")
	}
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(before, current) || !before.ModTime().Equal(current.ModTime()) || before.Size() != current.Size() {
		return fmt.Errorf("核验期间附件已被替换")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return use(root, name, f, before)
}
