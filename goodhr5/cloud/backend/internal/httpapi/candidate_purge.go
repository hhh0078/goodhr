// 本文件处理单份简历的授权删除及已绑定设备按游标读取删除事件。
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

var errCandidateDeleted = errors.New("这份简历已删除，旧任务无需再上传")

// candidateDeletionEvent 仅向有关设备提供旧云端编号与来源摘要，拒绝携带候选人私密资料。
type candidateDeletionEvent struct {
	Sequence      int64    `json:"sequence"`
	CandidateID   string   `json:"candidate_id"`
	RequestHashes []string `json:"request_hashes,omitempty"`
	SourceHashes  []string `json:"source_hashes"`
}

// candidatePurgeStore 统一真实数据库和开发模式的精确删除及增量事件能力。
type candidatePurgeStore interface {
	checkCandidateDeletions(context.Context, CandidateLibraryScope, candidateDeletionCheck) (candidateDeletionCheck, error)
	purgeCandidate(context.Context, CandidateLibraryScope, string, int64) error
	candidateDeletions(context.Context, string, string, bool, int64) ([]candidateDeletionEvent, error)
	candidateWasDeleted(context.Context, CandidateLibraryScope, string) (bool, error)
}

// purgeDigest 将稳定标识转换为摘要，不向绑定设备传输原始招聘平台编号或联系方式。
func purgeDigest(parts ...string) string {
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}

// purgeSourceHash 只接受非空的平台和该平台候选人编号，拒绝按姓名认人。
func purgeSourceHash(platform, id string) string {
	if strings.TrimSpace(platform) == "" || strings.TrimSpace(id) == "" {
		return ""
	}
	return purgeDigest(platform, id)
}

// isDeferredCandidateImport 区分可在后台重放的附件导入与用户新建普通简历的操作。
func isDeferredCandidateImport(key string) bool {
	return strings.HasPrefix(key, "resume-import:") || strings.HasPrefix(key, "attachment-import:")
}

// canPurgeCandidate 统一简历列表、详情及删除时所需的团队和创建者权限判断。
func canPurgeCandidate(scope CandidateLibraryScope, id, owner string, readonly bool) bool {
	return !readonly && !scope.IsExtension && id != "" && id != builtinCandidateID && (scope.IsAdmin || (owner != "" && strings.EqualFold(owner, scope.UserEmail)))
}

// deleteCandidate 删除已确认版本的单份简历，重新读取服务端权限，杜绝误清全库。
func (s *CandidateService) deleteCandidate(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	if scope.IsExtension {
		writeError(w, 403, "请到简历库删除这份简历")
		return
	}
	version, err := strconv.ParseInt(r.URL.Query().Get("row_version"), 10, 64)
	if err != nil || version < 1 {
		writeError(w, 400, "请刷新这份简历，核对版本后再删除")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/candidates/")
	store, ok := s.store.(candidatePurgeStore)
	if !ok {
		writeError(w, 503, "简历删除暂时没准备好")
		return
	}
	if err := store.purgeCandidate(r.Context(), scope, id, version); err != nil {
		writeCandidateError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "deleted": true, "candidate_id": id})
}

// agentCandidateDeletions 按绑定设备的真实账号和团队领取一页事件，不依赖岗位的会员状态。
func (s *AutoReplyService) agentCandidateDeletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "请用读取方式领取删除记录")
		return
	}
	request, ok := s.authenticatedRequestContext(w, r, false, true)
	if !ok {
		return
	}
	if request.Session.Kind == extensionSessionKind {
		writeAutoReplyError(w, 403, "DEVICE_BINDING_REQUIRED", "请使用已绑定的本地程序")
		return
	}
	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			writeAutoReplyError(w, 400, "INVALID_CURSOR", "删除记录的进度没读明白")
			return
		}
		after = n
	}
	store, ok := s.candidates.(candidatePurgeStore)
	if !ok {
		writeAutoReplyError(w, 503, "STORE_UNAVAILABLE", "删除记录暂时没准备好")
		return
	}
	events, err := store.candidateDeletions(r.Context(), request.Tenant.ID, request.Session.Email, request.IsAdmin, after)
	if err != nil {
		writeAutoReplyStoreError(w, err, "删除记录暂时没读出来")
		return
	}
	more := len(events) > 100
	if more {
		events = events[:100]
	}
	if len(events) > 0 {
		after = events[len(events)-1].Sequence
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"ok": true, "events": events, "next_cursor": after, "has_more": more})
}
