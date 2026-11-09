// 本文件为服务器自动回复引擎提供按账号隔离的 PostgreSQL 原子快照与增量提交。
package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/lib/pq"
	"net/http"
	"os"
	"strings"
)

var errReplyStateConflict = errors.New("自动回复状态已更新，请重新读取")

// AutoReplyStateChange 保留业务实体及原顺序、水位；删除提交只需类型和编号。
type AutoReplyStateChange struct {
	StagingName     string          `json:"staging_name,omitempty"`
	Kind            string          `json:"kind"`
	ID              string          `json:"id"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	Revision        int64           `json:"revision"`
	Sequence        int64           `json:"sequence"`
	UpdatedAt       string          `json:"updated_at"`
	MessageOrder    float64         `json:"message_order,omitempty"`
	ProcessedStatus string          `json:"processed_status,omitempty"`
	Deleted         bool            `json:"deleted,omitempty"`
}

// AutoReplyStateSnapshot 是服务器进程恢复单个招聘成员业务事务所需的带版本快照。
type AutoReplyStateSnapshot struct {
	Version  int64                  `json:"version"`
	Entities []AutoReplyStateChange `json:"entities"`
}

// replyEntityRelations 仅通过明确业务外键级联，不按姓名、聊天内容或手机号判断归属。
var replyEntityRelations = map[string][]string{
	"candidates": nil, "identities": {"candidates:candidate_id"}, "conversations": {"candidates:candidate_id", "identities:platform_identity_id"},
	"messages": {"conversations:conversation_id"}, "confirmations": {"candidates:candidate_id", "conversations:conversation_id"}, "attachments": {"candidates:candidate_id", "conversations:conversation_id"},
	"runs": {"candidates:candidate_id", "conversations:conversation_id"}, "tools": {"runs:ai_run_id"}, "suggestions": {"conversations:conversation_id"},
	"reports": {"candidates:report.candidate.id", "cloud:report.candidate.id"}, "recommendations": {"candidates:candidate_id", "conversations:conversation_id", "reports:input_hash"},
	"proposal_receipts": {"conversations:conversation_id"}, "outbox": {"candidates:payload.local_candidate_id", "conversations:payload.conversation_id", "reports:payload.key"},
	"cloud_links": nil, "sends": {"conversations:conversation_id"}, "runtime_events": {"conversations:conversation_id"}, "runtime_commands": {"conversations:conversation_id"}, "runtime_runs": nil, "list_rows": nil, "list_scopes": nil, "purge_keys": nil, "purge_files": nil, "deletion_boundaries": nil, "contact_probes": nil, "deletion_documents": nil,
}

// stateParents 验证稳定实体编号，并只从各类业务字段提取父节点。
func stateParents(c AutoReplyStateChange) ([]string, error) {
	paths, ok := replyEntityRelations[c.Kind]
	if !ok || c.ID == "" || len(c.ID) > 4096 {
		return nil, newAutoReplyValidationError("自动回复实体类型或编号未识别")
	}
	parents := []string{c.Kind + ":" + c.ID}
	if c.Deleted {
		return parents, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(c.Payload, &obj); err != nil || obj == nil {
		return nil, newAutoReplyValidationError("自动回复实体需要有效对象")
	}
	for _, p := range paths {
		kind, path, _ := strings.Cut(p, ":")
		var v any = obj
		for _, field := range strings.Split(path, ".") {
			m, _ := v.(map[string]any)
			v = m[field]
		}
		if id, ok := v.(string); ok && id != "" {
			parents = append(parents, kind+":"+id)
		}
	}
	if c.Kind == "cloud_links" {
		if id, ok := strings.CutPrefix(c.ID, "candidate:"); ok {
			parents = append(parents, "candidates:"+id)
			if v, ok := obj["value"].(string); ok {
				parents = append(parents, "cloud:"+v)
			}
		}
		if id, ok := strings.CutPrefix(c.ID, "hash:"); ok {
			root, _, _ := strings.Cut(id, "|")
			parents = append(parents, "candidates:"+root)
		}
	}
	return parents, nil
}

// LoadAutoReplyState 通过同一数据库快照读取账号版本和全部实体，已知版本一致时跳过正文。
func (s *PostgresAutoReplyStore) LoadAutoReplyState(ctx context.Context, tenantID, userID string, knownVersion ...int64) (AutoReplyStateSnapshot, error) {
	var state AutoReplyStateSnapshot
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT coalesce(o.version,0) FROM users u LEFT JOIN auto_reply_state_owners o ON o.tenant_id=u.tenant_id AND o.user_id=u.id WHERE u.tenant_id=$1 AND u.id=$2 AND u.status='active'`, tenantID, userID).Scan(&state.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrAutoReplyForbidden
	}
	if err != nil {
		return state, err
	}
	if len(knownVersion) > 0 && knownVersion[0] == state.Version {
		return state, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,id,payload,revision,sequence,updated_at,message_order,processed_status FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 ORDER BY kind,sequence,id`, tenantID, userID)
	if err != nil {
		return state, err
	}
	state.Entities = []AutoReplyStateChange{}
	defer rows.Close()
	for rows.Next() {
		var e AutoReplyStateChange
		if err := rows.Scan(&e.Kind, &e.ID, &e.Payload, &e.Revision, &e.Sequence, &e.UpdatedAt, &e.MessageOrder, &e.ProcessedStatus); err != nil {
			return state, err
		}
		state.Entities = append(state.Entities, e)
	}
	if err = rows.Err(); err != nil {
		return state, err
	}
	return state, tx.Commit()
}

// CommitAutoReplyState 在账号锁内核验预期版本、删除封印并提交整个差量；任一项失败则整体回滚。
func (s *PostgresAutoReplyStore) CommitAutoReplyState(ctx context.Context, tenantID, userID string, expectedVersion int64, changes []AutoReplyStateChange) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = lockCandidateDeletion(ctx, tx, tenantID, false); err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO auto_reply_state_owners(tenant_id,user_id) SELECT tenant_id,id FROM users WHERE tenant_id=$1 AND id=$2 AND status='active' ON CONFLICT DO NOTHING`, tenantID, userID)
	if err != nil {
		return 0, err
	}
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT o.version FROM auto_reply_state_owners o JOIN users u ON u.tenant_id=o.tenant_id AND u.id=o.user_id AND u.status='active' WHERE o.tenant_id=$1 AND o.user_id=$2 FOR UPDATE OF o`, tenantID, userID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrAutoReplyForbidden
	}
	if err != nil {
		return 0, err
	}
	if version != expectedVersion {
		return version, errReplyStateConflict
	}
	for _, change := range changes {
		if err = writeAutoReplyStateChange(ctx, tx, tenantID, userID, change); err != nil {
			return version, err
		}
	}
	if len(changes) > 0 {
		version++
		if _, err = tx.ExecContext(ctx, "UPDATE auto_reply_state_owners SET version=$3 WHERE tenant_id=$1 AND user_id=$2", tenantID, userID, version); err != nil {
			return 0, err
		}
	}
	journal, err := installReplyStateAttachments(ctx, tenantID, userID, changes)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		// 提交结果可能已在 PG 生效。恢复器会持账号锁重新检查实际引用。
		return 0, err
	}
	if journal != "" {
		_ = os.Remove(journal)
	}
	return version, nil
}

// writeAutoReplyStateChange 在调用方锁住账号的事务内写入单条实体，迁移与常规运行共用删除封印校验。
func writeAutoReplyStateChange(ctx context.Context, tx *sql.Tx, tenantID, userID string, change AutoReplyStateChange) error {
	parents, e := stateParents(change)
	if e != nil {
		return e
	}
	if change.Deleted {
		_, e = tx.ExecContext(ctx, "DELETE FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind=$3 AND id=$4", tenantID, userID, change.Kind, change.ID)
	} else {
		var blocked bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auto_reply_deleted_entity_keys k JOIN unnest($3::text[]) t ON k.key_hash=encode(sha256(convert_to(t,'UTF8')),'hex') WHERE k.tenant_id=$1 AND k.user_id=$2) OR EXISTS(SELECT 1 FROM candidate_deletions d JOIN unnest($3::text[]) t ON t='cloud:'||d.candidate_id::text WHERE d.tenant_id=$1)`, tenantID, userID, pq.Array(parents)).Scan(&blocked)
		if e != nil {
			return e
		}
		if blocked {
			return errCandidateDeleted
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,message_order,processed_status,parents) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(tenant_id,user_id,kind,id) DO UPDATE SET payload=excluded.payload,revision=excluded.revision,sequence=excluded.sequence,updated_at=excluded.updated_at,message_order=excluded.message_order,processed_status=excluded.processed_status,parents=excluded.parents`, tenantID, userID, change.Kind, change.ID, string(change.Payload), change.Revision, change.Sequence, change.UpdatedAt, change.MessageOrder, change.ProcessedStatus, pq.Array(parents))
	}
	if e != nil {
		return e
	}
	return nil
}

// internalState 只允许持有服务器服务凭证的流程读取和提交持久状态，浏览器用户登录凭证不能调用。
func (s *AutoReplyService) internalState(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN"))
	auth := r.Header.Get("Authorization")
	if token == "" || subtle.ConstantTimeCompare([]byte(auth), []byte("Bearer "+token)) != 1 {
		writeAutoReplyError(w, 403, "RUNTIME_AUTH_REQUIRED", "缺少服务器流程凭证")
		return
	}
	if s.store == nil {
		writeAutoReplyError(w, 503, "STORAGE_UNAVAILABLE", "服务端存储尚未准备好")
		return
	}
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "请使用服务器事务入口")
		return
	}
	var req struct {
		TenantID string                 `json:"tenant_id"`
		UserID   string                 `json:"user_id"`
		Expected *int64                 `json:"expected_version"`
		Known    *int64                 `json:"known_version"`
		Changes  []AutoReplyStateChange `json:"changes"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeAutoReplyError(w, 400, "INVALID_STATE", "状态请求格式未识别")
		return
	}
	switch r.URL.Path {
	case "/internal/auto-reply/state/load":
		var known []int64
		if req.Known != nil {
			known = []int64{*req.Known}
		}
		state, err := s.store.LoadAutoReplyState(r.Context(), req.TenantID, req.UserID, known...)
		if err != nil {
			writeAutoReplyStoreError(w, err, "服务器状态暂时读不出来")
			return
		}
		if req.Known != nil && *req.Known == state.Version {
			w.WriteHeader(304)
			return
		}
		writeJSON(w, 200, state)
	case "/internal/auto-reply/state/commit":
		if req.Expected == nil {
			writeAutoReplyError(w, 400, "VERSION_REQUIRED", "提交需要指定账号状态版本")
			return
		}
		version, err := s.store.CommitAutoReplyState(r.Context(), req.TenantID, req.UserID, *req.Expected, req.Changes)
		if err != nil {
			writeAutoReplyStoreError(w, err, "服务器状态暂时没保存成功")
			return
		}
		writeJSON(w, 200, map[string]any{"version": version})
	default:
		writeAutoReplyError(w, 404, "NOT_FOUND", "未识别的服务器状态入口")
	}
}
