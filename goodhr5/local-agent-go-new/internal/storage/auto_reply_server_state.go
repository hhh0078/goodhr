// Package storage 本文件在云编排进程内重用旧事务逻辑，使用内存 SQLite 临时投影，PG 是每次操作的权威来源。
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// AutoReplyStateChange 描述服务器持久层中一条实体或其删除，包含恢复旧聊天顺序所需的元数据。
type AutoReplyStateChange struct {
	StagingName     string          `json:"staging_name,omitempty"`
	Kind            string          `json:"kind"`
	ID              string          `json:"id"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	Revision        int64           `json:"revision,omitempty"`
	Sequence        int64           `json:"sequence,omitempty"`
	UpdatedAt       string          `json:"updated_at,omitempty"`
	Deleted         bool            `json:"deleted,omitempty"`
	MessageOrder    float64         `json:"message_order,omitempty"`
	ProcessedStatus string          `json:"processed_status,omitempty"`
}

// AutoReplyStateSnapshot 表示一个已授权账号在服务端的完整快照。
type AutoReplyStateSnapshot struct {
	Version  int64                  `json:"version"`
	Entities []AutoReplyStateChange `json:"entities"`
}

// AutoReplyStateAuthority 只可由服务器组装；Load 的第二个返回值表示指定版本仍为最新。
type AutoReplyStateAuthority interface {
	Load(context.Context, *int64) (AutoReplyStateSnapshot, bool, error)
	Commit(context.Context, int64, []AutoReplyStateChange) (int64, error)
}

type serverScratch struct {
	mu        sync.Mutex
	authority AutoReplyStateAuthority
	tables    map[string]scratchTable
	version   int64
	valid     bool
	owner     string
}
type scratchTable struct {
	kind    string
	columns []string
	keys    []string
	entity  bool
}

// UsesAutoReplyAuthority 表示受管卷的永久安装与回收必须由持有 PG 账号锁的服务执行。
func (s *Store) UsesAutoReplyAuthority() bool {
	return s != nil && s.scratch != nil
}

// StageAutoReplyFile 将暂存文件交给本事务管理，向受信后端只发送文件名，提交或失败都回收暂存副本。
func (t *AutoReplyTx) StageAutoReplyFile(id, path string) bool {
	if t.serverFiles == nil {
		return false
	}
	if prior := t.serverFiles[id]; prior != "" {
		os.Remove(prior)
	}
	t.serverFiles[id] = path
	return true
}

// OpenAutoReplyScratch 为单个服务器账号打开纯内存投影，并为实际SQL写入安装事务内变更采集。
func OpenAutoReplyScratch(authority AutoReplyStateAuthority) (*Store, error) {
	if authority == nil {
		return nil, fmt.Errorf("缺少服务器状态存储")
	}
	s, err := Open(":memory:")
	if err != nil {
		return nil, err
	}
	m := &serverScratch{authority: authority, tables: map[string]scratchTable{}}
	if err = m.setup(s.db); err != nil {
		s.Close()
		return nil, err
	}
	s.scratch = m
	return s, nil
}

// setup 创建只在私有连接存在的变更表；普通实体与多列主键来源表均使用固定白名单。
func (m *serverScratch) setup(db *sql.DB) error {
	_, err := db.Exec(`CREATE TEMP TABLE ar_dirty (kind TEXT,id TEXT,payload TEXT,revision INTEGER,sequence INTEGER,updated_at TEXT,deleted INTEGER,message_order REAL,processed_status TEXT,PRIMARY KEY(kind,id))`)
	if err != nil {
		return err
	}
	for _, kind := range []string{"runtime_runs", "runtime_commands", "runtime_events"} {
		if _, err = db.Exec("CREATE TABLE auto_reply_" + kind + "(sequence INTEGER PRIMARY KEY AUTOINCREMENT, owner_id TEXT NOT NULL,id TEXT NOT NULL,payload_json TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1,updated_at TEXT NOT NULL,UNIQUE(owner_id,id))"); err != nil {
			return err
		}
	}
	kinds := []string{"runtime_runs", "runtime_commands", "runtime_events", "list_rows", "list_scopes", "purge_keys", "purge_files", "deletion_boundaries", "contact_probes", "deletion_documents"}
	for kind := range autoReplyFields {
		if kind != "questions" {
			kinds = append(kinds, kind)
		}
	}
	for _, kind := range kinds {
		t, err := readScratchTable(context.Background(), db, kind)
		if err != nil {
			return err
		}
		m.tables[kind] = t
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			alias := "NEW."
			deleted := "0"
			if op == "DELETE" {
				alias = "OLD."
				deleted = "1"
			}
			fields := t.fields(alias, deleted)
			_, err = db.Exec("CREATE TEMP TRIGGER ar_" + kind + "_" + op + " AFTER " + op + " ON auto_reply_" + kind + " BEGIN INSERT INTO ar_dirty VALUES(" + strings.Join(fields, ",") + ") ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload,revision=excluded.revision,sequence=excluded.sequence,updated_at=excluded.updated_at,deleted=excluded.deleted,message_order=excluded.message_order,processed_status=excluded.processed_status; END")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// transaction 在执行旧业务回调前校验服务器版本，脏行经 PG CAS 成功后才允许下一步动作。
func (m *serverScratch) transaction(ctx context.Context, db *sql.DB, owner string, action func(*AutoReplyTx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner != "" && m.owner != owner {
		return fmt.Errorf("云端投影不允许跨账号复用")
	}
	m.owner = owner
	var known *int64
	if m.valid {
		known = &m.version
	}
	snapshot, unchanged, err := m.authority.Load(ctx, known)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if !unchanged {
		m.valid = false
		for kind := range m.tables {
			if _, err = tx.ExecContext(ctx, "DELETE FROM auto_reply_"+kind+" WHERE owner_id=?", owner); err != nil {
				return err
			}
		}
		for _, row := range snapshot.Entities {
			tab, ok := m.tables[row.Kind]
			if !ok {
				continue
			}
			if !row.Deleted {
				if err = tab.restore(ctx, tx, owner, row); err != nil {
					return err
				}
			}
		}
		m.version = snapshot.Version
	} else if !m.valid {
		return fmt.Errorf("服务器未返回要求的完整快照")
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM ar_dirty"); err != nil {
		return err
	}
	scoped := &AutoReplyTx{tx: tx, ctx: ctx, owner: owner, serverFiles: make(map[string]string)}
	defer func() {
		for _, temporary := range scoped.serverFiles {
			_ = os.Remove(temporary)
		}
	}()
	if err = action(scoped); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,id,payload,revision,sequence,updated_at,deleted,message_order,processed_status FROM ar_dirty ORDER BY kind,id`)
	if err != nil {
		return err
	}
	changes := []AutoReplyStateChange{}
	for rows.Next() {
		var c AutoReplyStateChange
		var raw string
		if err = rows.Scan(&c.Kind, &c.ID, &raw, &c.Revision, &c.Sequence, &c.UpdatedAt, &c.Deleted, &c.MessageOrder, &c.ProcessedStatus); err != nil {
			rows.Close()
			return err
		}
		if !c.Deleted {
			c.Payload = json.RawMessage(raw)
			if temporary := scoped.serverFiles[c.ID]; c.Kind == "attachments" && temporary != "" {
				c.StagingName = filepath.Base(temporary)
			}
		}
		changes = append(changes, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		// 网络结果未知也需要重新读取权威版本，不能假设失败并重新发送。
		m.valid = false
		next, err := m.authority.Commit(ctx, m.version, changes)
		if err != nil {
			return err
		}
		if next <= m.version {
			return fmt.Errorf("服务器没有确认新的事务版本")
		}
		m.version = next
	}
	if err = tx.Commit(); err != nil {
		m.valid = false
		return err
	}
	m.valid = true
	return nil
}

// restore 根据原始SQL列恢复事务数据；纳秒时间戳按 JSON 整数解析，避免浮点数损失精度。
func (t scratchTable) restore(ctx context.Context, tx *sql.Tx, owner string, c AutoReplyStateChange) error {
	values := map[string]any{"owner_id": owner}
	if t.entity {
		values["id"] = c.ID
		values["revision"] = c.Revision
		values["updated_at"] = c.UpdatedAt
		values["payload_json"] = string(c.Payload)
		if c.Sequence > 0 {
			values["sequence"] = c.Sequence
		}
		if t.kind == "messages" {
			values["message_order"] = c.MessageOrder
			values["processed_status"] = c.ProcessedStatus
		}
	} else {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(c.Payload, &raw); err != nil {
			return err
		}
		for _, column := range t.columns {
			field, ok := raw[column]
			if !ok {
				return fmt.Errorf("状态记录缺少字段 %s.%s", t.kind, column)
			}
			var data any
			dec := json.NewDecoder(strings.NewReader(string(field)))
			dec.UseNumber()
			if err := dec.Decode(&data); err != nil {
				return err
			}
			if n, ok := data.(json.Number); ok {
				v, err := n.Int64()
				if err != nil {
					return err
				}
				data = v
			}
			values[column] = data
		}
	}
	var fields []string
	for k := range values {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	args := make([]any, 0, len(fields))
	for _, k := range fields {
		args = append(args, values[k])
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO auto_reply_"+t.kind+"("+strings.Join(fields, ",")+") VALUES("+strings.TrimSuffix(strings.Repeat("?,", len(fields)), ",")+")", args...)
	return err
}

// ServerLedger 仅在云端内存投影内读写运行租约和已授权的浏览器命令摘要。
func (t *AutoReplyTx) ServerLedger(kind, id string, input any) (json.RawMessage, error) {
	if kind != "runtime_commands" && kind != "runtime_runs" && kind != "runtime_events" {
		return nil, fmt.Errorf("未知的云端运行账本")
	}
	table := "auto_reply_" + kind
	if input == nil {
		var raw string
		err := t.tx.QueryRowContext(t.ctx, "SELECT payload_json FROM "+table+" WHERE owner_id=? AND id=?", t.owner, id).Scan(&raw)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return json.RawMessage(raw), err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO "+table+"(owner_id,id,payload_json,updated_at)VALUES(?,?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(owner_id,id) DO UPDATE SET payload_json=excluded.payload_json,updated_at=excluded.updated_at,revision="+table+".revision+1", t.owner, id, string(raw))
	return raw, err
}

// readScratchTable 读取当前迁移后的实际普通列与主键，跳过由SQLite计算的索引列。
func readScratchTable(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, kind string) (scratchTable, error) {
	t := scratchTable{kind: kind}
	rows, err := db.QueryContext(ctx, "PRAGMA table_xinfo(auto_reply_"+kind+")")
	if err != nil {
		return t, err
	}
	var keys = map[int]string{}
	for rows.Next() {
		var cid, nullable, pk, hidden int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &nullable, &def, &pk, &hidden); err != nil {
			rows.Close()
			return t, err
		}
		if hidden != 0 || name == "owner_id" {
			continue
		}
		t.columns = append(t.columns, name)
		if pk > 0 {
			keys[pk] = name
		}
		if name == "payload_json" {
			t.entity = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return t, err
	}
	if len(t.columns) == 0 {
		return t, fmt.Errorf("缺少服务器状态投影表 %s", kind)
	}
	for i := 1; i <= len(t.columns)+1; i++ {
		if keys[i] != "" {
			t.keys = append(t.keys, keys[i])
		}
	}
	return t, nil
}

// fields 复用同一行投影提取权威快照及事务变更，保证迁移与日常写入的编码相同。
func (t scratchTable) fields(alias, deleted string) []string {
	fields := []string{"'" + t.kind + "'"}
	if t.entity {
		fields = append(fields, alias+"id", alias+"payload_json", alias+"revision", alias+"sequence", alias+"updated_at", deleted)
	} else {
		var labels, details []string
		for _, k := range t.keys {
			labels = append(labels, alias+k)
		}
		for _, k := range t.columns {
			details = append(details, "'"+k+"'", alias+k)
		}
		fields = append(fields, "json_array("+strings.Join(labels, ",")+")", "json_object("+strings.Join(details, ",")+")", "0", "0", "''", deleted)
	}
	if t.kind == "messages" {
		fields = append(fields, alias+"message_order", alias+"processed_status")
	} else {
		fields = append(fields, "0", "''")
	}
	return fields
}

// ExportAutoReplyOwner 在一个账号事务内导出本地业务实体和水位。
func (s *Store) ExportAutoReplyOwner(ctx context.Context, owner string) (rows []AutoReplyStateChange, err error) {
	err = s.AutoReplyTransaction(ctx, owner, func(tx *AutoReplyTx) error { rows, err = tx.ExportAutoReplyOwner(); return err })
	return
}

// ExportAutoReplyOwner 在当前事务内按稳定顺序导出同一账号，可用于清理前再次核对收据。
func (t *AutoReplyTx) ExportAutoReplyOwner() ([]AutoReplyStateChange, error) {
	kinds := []string{"list_rows", "list_scopes", "purge_keys", "purge_files", "deletion_boundaries", "contact_probes", "deletion_documents"}
	for kind := range autoReplyFields {
		if kind != "questions" {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	result := []AutoReplyStateChange{}
	for _, kind := range kinds {
		tab, err := readScratchTable(t.ctx, t.tx, kind)
		if err != nil {
			return nil, err
		}
		fields := tab.fields("", "0")
		rows, err := t.tx.QueryContext(t.ctx, "SELECT "+strings.Join(fields, ",")+" FROM auto_reply_"+kind+" WHERE owner_id=? ORDER BY 2", t.owner)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c AutoReplyStateChange
			var raw string
			if err = rows.Scan(&c.Kind, &c.ID, &raw, &c.Revision, &c.Sequence, &c.UpdatedAt, &c.Deleted, &c.MessageOrder, &c.ProcessedStatus); err != nil {
				rows.Close()
				return nil, err
			}
			c.Payload = json.RawMessage(raw)
			result = append(result, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
