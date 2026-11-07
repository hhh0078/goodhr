// 本文件只核对客户端已有的候选人编号和来源摘要，用于网页及已登录插件清理各自缓存。
package httpapi

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/lib/pq"
)

// candidateDeletionCheck 是最多一百个不含候选内容的精确标识，服务端只返回用户提交值的已删子集。
type candidateDeletionCheck struct {
	CandidateIDs []string `json:"candidate_ids"`
	SourceHashes []string `json:"source_hashes"`
}

// emptyDeletionCheck 保持空集合为 JSON 数组，便于客户端安全逐项清理。
func emptyDeletionCheck() candidateDeletionCheck {
	return candidateDeletionCheck{CandidateIDs: []string{}, SourceHashes: []string{}}
}

// checkDeletedCandidates 按当前团队权限核验已有标识，资料不可见或不存在都不当作删除证据。
func (s *CandidateService) checkDeletedCandidates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "请提交要核对的简历编号")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	var in candidateDeletionCheck
	if err := decodeAutoReplyJSON(w, r, &in); err != nil {
		return
	}
	if len(in.CandidateIDs)+len(in.SourceHashes) > 100 {
		writeError(w, 400, "一次最多核对100个编号")
		return
	}
	for _, id := range in.CandidateIDs {
		if id == "" || len(id) > 128 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\x00\r\n") {
			writeError(w, 400, "简历编号格式不正确")
			return
		}
	}
	for _, hash := range in.SourceHashes {
		if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 || strings.ToLower(hash) != hash {
			writeError(w, 400, "来源摘要格式不正确")
			return
		}
	}
	store, ok := s.store.(candidatePurgeStore)
	if !ok {
		writeError(w, 503, "简历删除记录暂时没准备好")
		return
	}
	result, err := store.checkCandidateDeletions(r.Context(), scope, in)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"ok": true, "candidate_ids": result.CandidateIDs, "source_hashes": result.SourceHashes})
}

// checkCandidateDeletions 按输入索引查询本租户可见的删除凭证；若来源已被人工再次关联，则保留现有缓存。
func (s *PostgresCandidateStore) checkCandidateDeletions(ctx context.Context, scope CandidateLibraryScope, in candidateDeletionCheck) (candidateDeletionCheck, error) {
	out := emptyDeletionCheck()
	rows, err := s.db.QueryContext(ctx, `SELECT 'id',d.candidate_id::text FROM candidate_deletions d WHERE tenant_id=$1 AND ($2 OR audiences ? $3) AND candidate_id::text=ANY($4::text[])
 UNION SELECT 'source',h FROM unnest($5::text[]) h
 WHERE EXISTS(SELECT 1 FROM candidate_deletions d WHERE d.tenant_id=$1 AND ($2 OR d.audiences ? $3) AND d.source_hashes ? h)
 AND EXISTS(SELECT 1 FROM candidate_deletion_keys k WHERE k.tenant_id=$1 AND k.kind='source' AND k.identity_hash=h AND k.replacement_candidate_id IS NULL)`, scope.TenantID, scope.IsAdmin, purgeDigest(strings.ToLower(scope.UserEmail)), pq.Array(in.CandidateIDs), pq.Array(in.SourceHashes))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, value string
		if err = rows.Scan(&kind, &value); err != nil {
			return out, err
		}
		if kind == "id" {
			out.CandidateIDs = append(out.CandidateIDs, value)
		} else {
			out.SourceHashes = append(out.SourceHashes, value)
		}
	}
	return out, rows.Err()
}

// checkCandidateDeletions 在内存开发模式按同样的团队、角色及重建规则返回已删除的输入子集。
func (s *MemoryCandidateStore) checkCandidateDeletions(_ context.Context, scope CandidateLibraryScope, in candidateDeletionCheck) (candidateDeletionCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := emptyDeletionCheck()
	wantedIDs, wantedHashes := map[string]bool{}, map[string]bool{}
	for _, id := range in.CandidateIDs {
		wantedIDs[id] = true
	}
	for _, h := range in.SourceHashes {
		wantedHashes[h] = true
	}
	for _, e := range s.deletions {
		_, known := e.recipients[strings.ToLower(scope.UserEmail)]
		if e.tenant != scope.TenantID || (!known && !scope.IsAdmin) {
			continue
		}
		if id := e.event.CandidateID; wantedIDs[id] {
			out.CandidateIDs = append(out.CandidateIDs, id)
			delete(wantedIDs, id)
		}
		for _, h := range e.event.SourceHashes {
			if wantedHashes[h] && !s.purgeKeys[scope.TenantID+"|reusedSource|"+h] {
				out.SourceHashes = append(out.SourceHashes, h)
				delete(wantedHashes, h)
			}
		}
	}
	return out, nil
}
