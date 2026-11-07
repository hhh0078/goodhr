// 本文件在无 PostgreSQL 的开发环境模拟候选人删除事务、独立存储清理及设备增量通知。
package httpapi

import (
	"context"
	"strings"
)

// memoryCandidateDeletion 保存内存模式下的团队删除事件和按成员隔离的导入回执。
type memoryCandidateDeletion struct {
	event         candidateDeletionEvent
	tenant, owner string
	version       int64
	recipients    map[string][]string
}

// memoryCandidatePurgeLinks 关联本服务当前进程的独立开发存储，不触碰其他团队或账号的数据。
type memoryCandidatePurgeLinks struct {
	recruitment *MemoryRecruitmentStore
	mail        *MemoryRecruitmentMailStore
	shares      *MemoryCandidateShareStore
	screening   *MemoryScreeningRecordStore
	vectors     *memoryResumeVectorStore
	attachments *memoryAttachmentParseStore
}

// candidateWasDeleted 在本团队内检查已经删除的云端编号。
func (s *MemoryCandidateStore) candidateWasDeleted(_ context.Context, scope CandidateLibraryScope, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.deletions {
		_, known := e.recipients[strings.ToLower(scope.UserEmail)]
		if e.tenant == scope.TenantID && e.event.CandidateID == id && (scope.IsAdmin || known) {
			return true, nil
		}
	}
	return false, nil
}

// candidateDeletions 读取游标后属于该设备账号的事件，不泄露其他成员的请求摘要。
func (s *MemoryCandidateStore) candidateDeletions(_ context.Context, tenant, email string, admin bool, after int64) ([]candidateDeletionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []candidateDeletionEvent{}
	for _, e := range s.deletions {
		keys, ok := e.recipients[strings.ToLower(email)]
		if e.tenant == tenant && e.event.Sequence > after && (admin || ok) {
			item := e.event
			item.SourceHashes = []string{}
			for _, h := range e.event.SourceHashes {
				if !s.purgeKeys[tenant+"|reusedSource|"+h] {
					item.SourceHashes = append(item.SourceHashes, h)
				}
			}
			item.RequestHashes = keys
			out = append(out, item)
			if len(out) == 101 {
				break
			}
		}
	}
	return out, nil
}

// purgeCandidate 删除指定候选人的内存实体、可访问链接和精确匹配的业务副本。
func (s *MemoryCandidateStore) purgeCandidate(ctx context.Context, scope CandidateLibraryScope, id string, version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, found := s.profiles[id]
	if !found {
		for _, e := range s.deletions {
			if e.tenant == scope.TenantID && e.event.CandidateID == id && canPurgeCandidate(scope, id, e.owner, false) {
				if e.version != version {
					return ErrCandidateVersion
				}
				return nil
			}
		}
		return ErrNotFound
	}
	if !candidateVisible(scope, item) || !canPurgeCandidate(scope, id, item.UserEmail, item.LibraryFields["readonly"] == true) {
		return ErrNotFound
	}
	if item.RowVersion != version {
		return ErrCandidateVersion
	}
	tenant := scope.TenantID
	recipients := map[string][]string{strings.ToLower(item.UserEmail): {}, strings.ToLower(scope.UserEmail): {}}
	anchors := map[string]string{}
	for k, r := range s.requests {
		p := strings.SplitN(k, "|", 4)
		if len(p) == 4 && p[0] == tenant && r.ResultID == id && strings.HasPrefix(p[2], "candidate:") {
			actor := strings.ToLower(p[1])
			recipients[actor] = append(recipients[actor], purgeDigest(p[3]))
			if task := attachmentImportReceiptID(p[3]); task != "" {
				anchors[purgeDigest(actor)+"|attachment|"+task] = r.Fingerprint
			}
		}
	}
	for k, e := range s.engagements {
		if e.CandidateID == id {
			if e.UserEmail != "" {
				actor := strings.ToLower(e.UserEmail)
				if _, ok := recipients[actor]; !ok {
					recipients[actor] = nil
				}
			}
			delete(s.engagements, k)
		}
	}
	for k, r := range s.recommendations {
		if r.CandidateID == id {
			delete(s.recommendations, k)
		}
	}
	hashes := []string{}
	sources := map[string]bool{}
	for _, source := range append(append([]CandidateSource{}, item.Sources...), CandidateSource{PlatformID: item.PlatformID, PlatformCandidateID: item.PlatformCandidateID}) {
		hash := purgeSourceHash(source.PlatformID, source.PlatformCandidateID)
		if hash != "" && !sources[hash] {
			hashes = append(hashes, hash)
			sources[hash] = true
			s.purgeKeys[tenant+"|source|"+hash] = true
			delete(s.purgeKeys, tenant+"|reusedSource|"+hash)
		}
	}
	phone := normalizeCandidatePhone(item.Phone)
	if phone != "" {
		shared := false
		for otherID, other := range s.profiles {
			if otherID != id && other.TenantID == tenant && normalizeCandidatePhone(other.Phone) == phone {
				shared = true
				break
			}
		}
		if !shared {
			s.purgeKeys[tenant+"|phone|"+purgeDigest(tenant, phone)] = true
		}
	}
	links := s.purgeLinks
	if links.shares != nil {
		_ = links.shares.RevokeCandidateShares(tenant, id)
	}
	if links.recruitment != nil {
		links.recruitment.mu.Lock()
		defer links.recruitment.mu.Unlock()
		apps := map[string]bool{}
		links.recruitment.purgedCandidates[tenant+"|"+id] = true
		for k, v := range links.recruitment.applications {
			if v.TenantID == tenant && v.CandidateID == id {
				apps[v.ID] = true
				delete(links.recruitment.applications, k)
			}
		}
		for k, v := range links.recruitment.invitations {
			if v.TenantID == tenant && apps[v.ApplicationID] {
				delete(links.recruitment.invitations, k)
			}
		}
		if links.mail != nil {
			links.mail.mu.Lock()
			defer links.mail.mu.Unlock()
			for app := range apps {
				links.mail.purgedApps[tenant+"|"+app] = true
			}
			for k, v := range links.mail.deliveries {
				if v.TenantID == tenant && apps[v.ApplicationID] {
					delete(links.mail.deliveries, k)
					delete(links.mail.receipts, recruitmentMailReceiptKey(tenant, v.ActorEmail, v.IdempotencyKey))
				}
			}
		}
	}
	if links.screening != nil {
		links.screening.mu.Lock()
		defer links.screening.mu.Unlock()
		for h := range sources {
			links.screening.purged[tenant+"|source|"+h] = true
		}
		for k, v := range links.screening.records {
			r := v.Record
			if strings.HasPrefix(k, tenant+"\x00") && sources[purgeSourceHash(r.PlatformID, r.PlatformCandidateID)] {
				actor := strings.ToLower(v.UserEmail)
				if _, ok := recipients[actor]; !ok {
					recipients[actor] = nil
				}
				links.screening.purged[k] = true
				delete(links.screening.records, k)
			}
		}
	}
	if links.vectors != nil {
		links.vectors.mu.Lock()
		delete(links.vectors.records, tenant+":"+id)
		links.vectors.mu.Unlock()
	}
	s.purgeKeys[tenant+"|candidate|"+id] = true
	if cache := links.attachments; cache != nil {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		// 第一遍从已确认入库的任务建立同账号、同捕获、同正文的证据；再去除解析和入库两个阶段。
		for _, r := range cache.records {
			owner := purgeDigest(r.Owner) + "|attachment|"
			if verifiedAttachmentImport(r, tenant, id, hashes, anchors[owner+r.Task.ID]) {
				s.purgeKeys[owner+r.Task.ID] = true
				if proof := attachmentCaptureProof(r.Task); proof != "" {
					s.purgeKeys[owner+proof] = true
				}
			}
		}
		for k, r := range cache.records {
			owner := purgeDigest(r.Owner) + "|attachment|"
			if s.purgeKeys[owner+r.Task.ID] || s.purgeKeys[owner+attachmentCaptureProof(r.Task)] {
				delete(cache.records, k)
			}
		}
	}
	delete(s.folderLinks, id)
	delete(s.events, id)
	delete(s.notes, id)
	delete(s.profiles, id)
	s.deletions = append(s.deletions, memoryCandidateDeletion{event: candidateDeletionEvent{Sequence: int64(len(s.deletions) + 1), CandidateID: id, SourceHashes: hashes}, tenant: tenant, owner: item.UserEmail, version: version, recipients: recipients})
	return nil
}
