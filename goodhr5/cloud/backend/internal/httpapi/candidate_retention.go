// 本文件提供带独立鉴权的30天临时简历清理任务；一次每日请求由服务器分批完成，复用完整删除及恢复封印。
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

var errCandidateRetentionChanged = errors.New("临时简历已有新的资料、手机号或沟通，已跳过本次清理")

// CandidateRetentionJob 是不含候选人信息的持久化清理进度。
type CandidateRetentionJob struct {
	ID           string    `json:"id"`
	Cutoff       time.Time `json:"cutoff"`
	Status       string    `json:"status"`
	DeletedCount int64     `json:"deleted_count"`
	SkippedCount int64     `json:"skipped_count"`
	UpdatedAt    time.Time `json:"updated_at"`
	Error        string    `json:"error,omitempty"`
}

// CandidateRetentionService 仅维护已明确请求的数据库任务，不在设备执行数据清理。
type CandidateRetentionService struct {
	store     *PostgresCandidateStore
	running   atomic.Bool
	requested atomic.Bool
	now       func() time.Time
}

// NewCandidateRetentionService 复用正式简历存储；配置专用密钥后才恢复以前已经请求的任务。
func NewCandidateRetentionService(store CandidateStore) *CandidateRetentionService {
	pg, _ := store.(*PostgresCandidateStore)
	s := &CandidateRetentionService{store: pg, now: time.Now}
	if pg != nil && len(strings.TrimSpace(os.Getenv("GOODHR_TEMP_RESUME_CLEANUP_TOKEN"))) >= 32 {
		s.launch()
		// 崩溃前留下的短租约到期后再检查一次，不把恢复旧任务变成未经请求的每日定时删除。
		time.AfterFunc(2*time.Minute, s.launch)
	}
	return s
}

// Cleanup 的POST登记当天任务、GET只读进度；密钥只能放请求头，登录态与URL参数不能代替维护权限。
func (s *CandidateRetentionService) Cleanup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeError(w, 405, "清理接口使用 POST，查看进度使用 GET")
		return
	}
	expected := strings.TrimSpace(os.Getenv("GOODHR_TEMP_RESUME_CLEANUP_TOKEN"))
	if len(expected) < 32 {
		writeError(w, 503, "临时简历清理密钥尚未配置")
		return
	}
	provided := bearerToken(r.Header.Get("Authorization"))
	if provided == "" {
		provided = r.Header.Get("X-Cleanup-Token")
	}
	want, got := sha256.Sum256([]byte(expected)), sha256.Sum256([]byte(provided))
	if provided == "" || subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		writeError(w, 401, "清理凭据没有核对上")
		return
	}
	if s == nil || s.store == nil {
		writeError(w, 503, "临时简历清理需要服务器数据库")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var row *sql.Row
	if r.Method == http.MethodPost {
		now := s.now()
		day := now.In(chinaLocation()).Format("2006-01-02")
		row = s.store.db.QueryRowContext(ctx, `INSERT INTO candidate_retention_jobs(request_day,cutoff) VALUES($1,$2)
 ON CONFLICT(request_day) DO UPDATE SET status=CASE WHEN candidate_retention_jobs.status='failed' THEN 'pending' ELSE candidate_retention_jobs.status END,error=CASE WHEN candidate_retention_jobs.status='failed' THEN '' ELSE candidate_retention_jobs.error END
 RETURNING id::text,cutoff,status,deleted_count,skipped_count,updated_at,error`, day, now.Add(-30*24*time.Hour))
	} else {
		row = s.store.db.QueryRowContext(ctx, `SELECT id::text,cutoff,status,deleted_count,skipped_count,updated_at,error FROM candidate_retention_jobs ORDER BY created_at DESC LIMIT 1`)
	}
	job, err := scanCandidateRetentionJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 200, struct {
			Status string `json:"status"`
		}{"idle"})
		return
	}
	if err != nil {
		writeError(w, 500, "清理任务还没记好，原资料已保留，请稍后重试")
		return
	}
	if r.Method == http.MethodPost {
		s.launch()
	}
	code := http.StatusOK
	if r.Method == http.MethodPost && job.Status != "completed" {
		code = http.StatusAccepted
	}
	writeJSON(w, code, job)
}

// scanCandidateRetentionJob 统一读取单份数据库进度，不把SQL错误或密钥写进结果。
func scanCandidateRetentionJob(row *sql.Row) (CandidateRetentionJob, error) {
	var j CandidateRetentionJob
	err := row.Scan(&j.ID, &j.Cutoff, &j.Status, &j.DeletedCount, &j.SkippedCount, &j.UpdatedAt, &j.Error)
	return j, err
}

// launch 在当前服务最多运行一份执行器，数据库短租约继续保护多副本和重启恢复。
func (s *CandidateRetentionService) launch() {
	if s == nil || s.store == nil {
		return
	}
	s.requested.Store(true)
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer func() {
			s.running.Store(false)
			if s.requested.Swap(false) {
				s.launch()
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		for ctx.Err() == nil {
			s.requested.Store(false)
			job, err := scanCandidateRetentionJob(s.store.db.QueryRowContext(ctx, `UPDATE candidate_retention_jobs SET status='running',lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=(SELECT id FROM candidate_retention_jobs WHERE status='pending' OR (status='running' AND lease_until<now()) ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING id::text,cutoff,status,deleted_count,skipped_count,updated_at,error`))
			if err != nil {
				return
			}
			if err = s.runJob(ctx, job); err != nil {
				_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE candidate_retention_jobs SET status='failed',lease_until=NULL,error='这批清理未完成，可重复当天请求重试',updated_at=now() WHERE id=$1`, job.ID)
				return
			}
		}
	}()
}

// expiredCandidates 每批最多读取100份无有效手机号的过期资料，不读取简历正文、号码或聊天内容。
func (s *CandidateRetentionService) expiredCandidates(ctx context.Context, cutoff time.Time) ([]struct {
	Tenant, ID string
	Version    int64
}, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT tenant_id::text,id::text,row_version FROM candidate_profiles cp
 WHERE normalized_phone='' AND regexp_replace(phone,'[[:space:]()（）-]','','g') !~ '^(1[3-9][0-9]{9}|(\+|00)[1-9][0-9]{7,14}|861[3-9][0-9]{9})$'
	 AND greatest(updated_at,coalesce(last_communication_at,created_at))<$1 AND id::text<>$2 AND `+retentionOnlineTemporarySQL+` AND NOT `+retentionProtectedActorSQL+`
 ORDER BY greatest(updated_at,coalesce(last_communication_at,created_at)),id LIMIT 100`, cutoff, builtinCandidateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []struct {
		Tenant, ID string
		Version    int64
	}{}
	for rows.Next() {
		var item struct {
			Tenant, ID string
			Version    int64
		}
		if err = rows.Scan(&item.Tenant, &item.ID, &item.Version); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// runJob 在每份完整删除事务内再次核对活动和手机号，数据被补齐或并发更新时保留并跳过。
func (s *CandidateRetentionService) runJob(ctx context.Context, job CandidateRetentionJob) error {
	for ctx.Err() == nil {
		items, err := s.expiredCandidates(ctx, job.Cutoff)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			unlinked, readErr := s.expiredUnlinkedCandidates(ctx, job.Cutoff)
			if readErr != nil {
				return readErr
			}
			if len(unlinked) > 0 {
				for _, item := range unlinked {
					if purgeErr := s.purgeUnlinkedCandidate(ctx, job, item); purgeErr != nil && !errors.Is(purgeErr, errCandidateRetentionChanged) {
						return purgeErr
					}
				}
				continue
			}
			_, err = s.store.db.ExecContext(ctx, `UPDATE candidate_retention_jobs SET status='completed',lease_until=NULL,error='',updated_at=now() WHERE id=$1`, job.ID)
			return err
		}
		for _, item := range items {
			scope := CandidateLibraryScope{TenantID: item.Tenant, IsAdmin: true, retentionCutoff: &job.Cutoff, retentionJobID: job.ID}
			if err = s.store.purgeCandidate(ctx, scope, item.ID, item.Version); err != nil {
				if !errors.Is(err, errCandidateRetentionChanged) && !errors.Is(err, ErrCandidateVersion) && !errors.Is(err, ErrNotFound) {
					return err
				}
				if _, err = s.store.db.ExecContext(ctx, `UPDATE candidate_retention_jobs SET skipped_count=skipped_count+1,lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=$1`, job.ID); err != nil {
					return err
				}
			}
		}
	}
	return ctx.Err()
}
