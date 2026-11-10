// 本文件按简历、招聘成员、会话及原始来信授权展示和下载私有附件，外部请求无法指定磁盘路径。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/lib/pq"
)

type replyAttachmentView struct {
	ID              string `json:"id"`
	FileName        string `json:"file_name"`
	SourceMessageID string `json:"source_message_id"`
	SizeBytes       int64  `json:"size_bytes"`
}

// replyAttachmentName 清理路径、控制字符与方向控制符，原始名字若不完整则只显示安全的文件类型。
func replyAttachmentName(original, relative string) string {
	name := path.Base(strings.ReplaceAll(original, "\\", "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." || name == ".." || len([]rune(name)) > 200 {
		return "简历附件" + path.Ext(relative)
	}
	return name
}

// readHistoryAttachments 批量查本页已展示的真实来信与附件，只返回成员私有卷中规范化记录的公开字段。
func readHistoryAttachments(ctx context.Context, tx *sql.Tx, tenant, user, thread string, messages []replyMessageView) error {
	byID := map[string]int{}
	ids := make([]string, 0, len(messages))
	for i, m := range messages {
		if m.Side == "candidate" {
			byID[m.ID] = i
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.payload->>'source_message_id',coalesce(a.payload->>'original_name',''),coalesce(a.payload->>'relative_path',''),
	 coalesce(a.payload->>'sha256',''),CASE WHEN a.payload->>'size_bytes' ~ '^[1-9][0-9]{0,8}$' THEN (a.payload->>'size_bytes')::bigint ELSE 0 END
	 FROM auto_reply_cloud_entities a JOIN auto_reply_cloud_entities c ON c.tenant_id=a.tenant_id AND c.user_id=a.user_id AND c.kind='conversations' AND c.id=$3
	 WHERE a.tenant_id=$1 AND a.user_id=$2 AND a.kind='attachments' AND a.payload->>'conversation_id'=c.id AND a.payload->>'source_message_id'=ANY($4::text[])
	 AND (nullif(a.payload->>'candidate_id','') IS NULL OR a.payload->>'candidate_id'=c.payload->>'candidate_id') ORDER BY a.sequence,a.id`, tenant, user, thread, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	owner := "auto_reply:v1:" + replyImportHash(user)
	for rows.Next() {
		var a replyAttachmentView
		var file replyImportFile
		var name string
		if err = rows.Scan(&a.ID, &a.SourceMessageID, &name, &file.RelativePath, &file.SHA256, &file.SizeBytes); err != nil {
			return err
		}
		if !checkedReplyImportPath(file, owner) {
			continue
		}
		a.FileName = replyAttachmentName(name, file.RelativePath)
		a.SizeBytes = file.SizeBytes
		i := byID[a.SourceMessageID]
		messages[i].Attachments = append(messages[i].Attachments, a)
	}
	return rows.Err()
}

// candidateReplyFile 查实时简历映射、当前成员和精确来源，任何错配与已删除记录均返回相同的不存在结果。
func (s *PostgresAutoReplyStore) candidateReplyFile(ctx context.Context, scope CandidateLibraryScope, candidate, threadID, attachment string) (file replyImportFile, name, user string, err error) {
	user, thread, ok := strings.Cut(threadID, ":")
	if !ok || user == "" || thread == "" {
		return file, "", "", ErrNotFound
	}
	err = s.db.QueryRowContext(ctx, `WITH authorized AS (
	 SELECT c.tenant_id,c.user_id,c.id,c.payload->>'candidate_id' AS candidate `+candidateReplyThreadScope+` AND c.user_id::text=$5 AND c.id=$6)
	 SELECT coalesce(a.payload->>'relative_path',''),coalesce(a.payload->>'sha256',''),CASE WHEN a.payload->>'size_bytes' ~ '^[1-9][0-9]{0,8}$' THEN (a.payload->>'size_bytes')::bigint ELSE 0 END,coalesce(a.payload->>'original_name','')
	 FROM authorized c JOIN auto_reply_cloud_entities a ON a.tenant_id=c.tenant_id AND a.user_id=c.user_id AND a.kind='attachments' AND a.id=$7 AND a.payload->>'conversation_id'=c.id
	 JOIN auto_reply_cloud_entities m ON m.tenant_id=c.tenant_id AND m.user_id=c.user_id AND m.kind='messages' AND m.id=a.payload->>'source_message_id'
	 WHERE m.payload->>'direction'='candidate' AND m.payload->>'conversation_id'=c.id AND (nullif(a.payload->>'candidate_id','') IS NULL OR a.payload->>'candidate_id'=c.candidate)`,
		scope.TenantID, candidate, scope.IsAdmin, scope.UserEmail, user, thread, attachment).Scan(&file.RelativePath, &file.SHA256, &file.SizeBytes, &name)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !checkedReplyImportPath(file, "auto_reply:v1:"+replyImportHash(user))) {
		return file, "", "", ErrNotFound
	}
	return file, replyAttachmentName(name, file.RelativePath), user, err
}

// autoReplyAttachment 校验管理员网页登录、简历和会话权限，并在实际点击时重新验签；仅真实 PDF 支持内联预览。
func (s *CandidateService) autoReplyAttachment(w http.ResponseWriter, r *http.Request, candidate, attachment string) {
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, 405, "method not allowed")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	if scope.IsExtension {
		writeAutoReplyError(w, 403, "SESSION_NOT_ALLOWED", "请从简历库打开附件")
		return
	}
	if _, err := s.store.GetPositionCandidate(scope.TenantID, candidate, "", scope.UserEmail, scope.IsAdmin); err != nil {
		writeAutoReplyStoreError(w, err, "这份简历暂时无法读取")
		return
	}
	if s.autoReply == nil {
		writeAutoReplyError(w, 503, "STORAGE_UNAVAILABLE", "云端附件暂时无法连接")
		return
	}
	file, name, user, err := s.autoReply.candidateReplyFile(r.Context(), scope, candidate, r.URL.Query().Get("thread_id"), attachment)
	if err != nil {
		writeAutoReplyStoreError(w, err, "简历附件暂时没读出来")
		return
	}
	err = serveVerifiedReplyAttachment(w, r, os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR"), scope.TenantID, user, name, file)
	if errors.Is(err, os.ErrNotExist) {
		writeAutoReplyStoreError(w, ErrNotFound, "附件已不存在")
		return
	}
	if err != nil {
		writeAutoReplyError(w, 503, "ATTACHMENT_UNAVAILABLE", "附件原件暂时无法核实，请稍后重新打开")
	}
}

// serveVerifiedReplyAttachment 对同一已核验描述符检查文件头并传输，下载响应支持范围请求且不可公开缓存。
func serveVerifiedReplyAttachment(w http.ResponseWriter, r *http.Request, base, tenant, user, name string, file replyImportFile) error {
	return withVerifiedReplyFile(base, tenant, user, file.RelativePath, file.SHA256, file.SizeBytes, func(_ *os.Root, _ string, f *os.File, info os.FileInfo) error {
		var header [512]byte
		n, err := f.ReadAt(header[:], 0)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		ctype, mode := "application/octet-stream", "attachment"
		if strings.EqualFold(path.Ext(name), ".pdf") && strings.EqualFold(path.Ext(file.RelativePath), ".pdf") && http.DetectContentType(header[:n]) == "application/pdf" {
			ctype = "application/pdf"
			if r.URL.Query().Get("inline") == "1" {
				mode = "inline"
			}
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Content-Disposition", mime.FormatMediaType(mode, map[string]string{"filename": name}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("Cache-Control", "private, no-store")
		http.ServeContent(w, r, name, info.ModTime(), f)
		return nil
	})
}
