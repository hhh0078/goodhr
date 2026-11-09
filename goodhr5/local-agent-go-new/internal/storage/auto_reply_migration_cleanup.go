// Package storage 本文件按云端收据原子清理已迁移账号的旧业务资料，保留可恢复的文件清理队列。
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// AutoReplyExportDigest 基于稳定排序的实体导出计算摘要，识别上传期间发生的并发写入。
func AutoReplyExportDigest(rows []AutoReplyStateChange) (string, error) {
	h := sha256.New()
	if err := json.NewEncoder(h).Encode(rows); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FinishAutoReplyMigration 在同一事务复核全部业务行后清理，未收到云端收据的导出不应调用。
func (s *Store) FinishAutoReplyMigration(ctx context.Context, owner, digest, cleanup string) error {
	return s.AutoReplyTransaction(ctx, owner, func(tx *AutoReplyTx) error {
		current, err := tx.ExportAutoReplyOwner()
		if err != nil {
			return err
		}
		check, err := AutoReplyExportDigest(current)
		if err != nil {
			return err
		}
		if check != digest {
			return fmt.Errorf("旧资料在上传期间有更新，已保留本机资料，请重试")
		}
		// 部分最老的版本仍写整份JSON。仅当源快照与转换时记录的摘要一致才可删除它。
		var raw, converted string
		err = tx.tx.QueryRowContext(ctx, "SELECT k.value,COALESCE(m.source_sha256,'') FROM local_settings k LEFT JOIN auto_reply_migrations m ON m.owner_id=? WHERE k.key=?", owner, owner).Scan(&raw, &converted)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			h := sha256.Sum256([]byte(raw))
			if hex.EncodeToString(h[:]) != converted {
				return fmt.Errorf("旧版整包资料发生变化，原件已保留，请先停止旧版程序")
			}
		}
		kinds := map[string]bool{}
		for _, row := range current {
			kinds[row.Kind] = true
		}
		for kind := range kinds {
			if _, err = tx.tx.ExecContext(ctx, "DELETE FROM auto_reply_"+kind+" WHERE owner_id=?", owner); err != nil {
				return err
			}
		}
		if _, err = tx.tx.ExecContext(ctx, "DELETE FROM local_settings WHERE key=?", owner); err != nil {
			return err
		}
		if _, err = tx.tx.ExecContext(ctx, "DELETE FROM auto_reply_migrations WHERE owner_id=?", owner); err != nil {
			return err
		}
		_, err = tx.tx.ExecContext(ctx, "INSERT INTO local_settings(key,value,updated_at) VALUES(?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at", owner+":cleanup", cleanup)
		return err
	})
}

// FinishAutoReplyFileCleanup 仅删除当前批次的清理队列，重试不会影响其它账号或新批次。
func (s *Store) FinishAutoReplyFileCleanup(ctx context.Context, owner, cleanup string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM local_settings WHERE key=? AND value=?", owner+":cleanup", cleanup)
	return err
}

// HasAutoReplyMigrationData 只检查该账号的索引和旧设置；空账号绑定时无须再访问云或读整份快照。
func (s *Store) HasAutoReplyMigrationData(ctx context.Context, owner string) (bool, error) {
	parts := []string{"SELECT 1 FROM local_settings WHERE key IN (?,?) AND value!=''"}
	args := []any{owner, owner + ":cleanup"}
	kinds := []string{"list_rows", "list_scopes", "purge_keys", "purge_files", "deletion_boundaries", "contact_probes", "deletion_documents"}
	for k := range autoReplyFields {
		if k != "questions" {
			kinds = append(kinds, k)
		}
	}
	for _, k := range kinds {
		parts = append(parts, "SELECT 1 FROM auto_reply_"+k+" WHERE owner_id=?")
		args = append(args, owner)
	}
	var value bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS("+strings.Join(parts, " UNION ALL ")+")", args...).Scan(&value)
	return value, err
}
