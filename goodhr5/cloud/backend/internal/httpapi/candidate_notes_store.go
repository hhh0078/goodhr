// 本文件实现专用候选人备注存储，保留登录作者、实际发生时间、权限和幂等，备注不写入自动事件表。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

// SaveCandidateNote 在内存中独立追加备注，同一作者重复提交同一请求时返回原备注。
func (s *MemoryCandidateStore) SaveCandidateNote(scope CandidateLibraryScope, note CandidateNote) (CandidateNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.profiles[note.CandidateID]
	if !exists || !candidateVisible(scope, item) {
		return CandidateNote{}, ErrNotFound
	}
	note.Content = strings.TrimSpace(note.Content)
	if note.Content == "" || len([]rune(note.Content)) > 1000 {
		return CandidateNote{}, errors.New("备注请填写1到1000字")
	}
	for _, old := range s.notes[note.CandidateID] {
		if note.IdempotencyKey != "" && old.AuthorEmail == scope.UserEmail && old.IdempotencyKey == note.IdempotencyKey {
			if old.Content != note.Content || (!note.OccurredAt.IsZero() && !old.OccurredAt.Equal(note.OccurredAt)) {
				return CandidateNote{}, ErrCandidateRequest
			}
			return old, nil
		}
	}
	note.ID, note.TenantID = s.nextID("note"), scope.TenantID
	note.AuthorEmail = scope.UserEmail
	note.AuthorName = firstNonEmpty(scope.AuthorName, scope.UserEmail)
	note.CreatedAt = s.now()
	if note.OccurredAt.IsZero() {
		note.OccurredAt = note.CreatedAt
	}
	s.notes[note.CandidateID] = append(s.notes[note.CandidateID], note)
	return note, nil
}

// candidateNotesLocked 返回发生时间优先的独立备注，调用方必须持有候选人存储锁。
func (s *MemoryCandidateStore) candidateNotesLocked(id string) []CandidateNote {
	notes := append([]CandidateNote{}, s.notes[id]...)
	sort.Slice(notes, func(i, j int) bool {
		if !notes[i].OccurredAt.Equal(notes[j].OccurredAt) {
			return notes[i].OccurredAt.After(notes[j].OccurredAt)
		}
		if !notes[i].CreatedAt.Equal(notes[j].CreatedAt) {
			return notes[i].CreatedAt.After(notes[j].CreatedAt)
		}
		return notes[i].ID > notes[j].ID
	})
	return notes
}

// SaveCandidateNote 在数据库事务中检查候选人权限并独立写入备注，幂等冲突不会覆盖旧正文。
func (s *PostgresCandidateStore) SaveCandidateNote(scope CandidateLibraryScope, note CandidateNote) (CandidateNote, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateNote{}, err
	}
	defer tx.Rollback()
	if _, err = candidateReadTx(ctx, tx, scope, note.CandidateID, true); err != nil {
		return CandidateNote{}, err
	}
	note.Content = strings.TrimSpace(note.Content)
	if note.Content == "" || len([]rune(note.Content)) > 1000 {
		return CandidateNote{}, errors.New("备注请填写1到1000字")
	}
	implicitTime := note.OccurredAt.IsZero()
	if implicitTime {
		note.OccurredAt = time.Now().UTC()
	}
	var saved CandidateNote
	err = tx.QueryRowContext(ctx, `INSERT INTO candidate_notes(tenant_id,candidate_id,author_email,author_name,content,occurred_at,idempotency_key)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(tenant_id,candidate_id,author_email,idempotency_key) WHERE idempotency_key<>'' DO UPDATE SET content=candidate_notes.content
WHERE candidate_notes.content=EXCLUDED.content AND ($8::boolean OR candidate_notes.occurred_at=EXCLUDED.occurred_at)
RETURNING id::text,tenant_id::text,candidate_id::text,author_email,author_name,content,occurred_at,created_at,idempotency_key`, scope.TenantID, note.CandidateID, scope.UserEmail, firstNonEmpty(scope.AuthorName, scope.UserEmail), note.Content, note.OccurredAt, note.IdempotencyKey, implicitTime).Scan(&saved.ID, &saved.TenantID, &saved.CandidateID, &saved.AuthorEmail, &saved.AuthorName, &saved.Content, &saved.OccurredAt, &saved.CreatedAt, &saved.IdempotencyKey)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateNote{}, ErrCandidateRequest
	}
	if err != nil {
		return CandidateNote{}, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateNote{}, err
	}
	return saved, nil
}
