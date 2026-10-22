// 本文件消费持久的搜索清理待办，删除已撤销或过期的副本；失败保留待办重试，不触发模型收费。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// resumeSearchInvalidator 后台只使用业务事务保存的团队和版本边界，不接收模型或浏览器的清理条件。
type resumeSearchInvalidator struct {
	db     *sql.DB
	search *elasticResumeSearch
}

// startResumeSearchInvalidation 启动有界清理循环，未配置搜索时保留待办等待管理员接通服务。
func startResumeSearchInvalidation(ctx context.Context, db *sql.DB, search *elasticResumeSearch) {
	if db == nil || search == nil {
		return
	}
	worker := &resumeSearchInvalidator{db: db, search: search}
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			if search.Ready() {
				for count := 0; count < 20; count++ {
					processed, err := worker.once(ctx)
					if err != nil || !processed {
						break
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// once 持锁领取一条待办，搜索成功才删除；其它实例跳过已领取记录，回滚保留原清理意图。
func (s *resumeSearchInvalidator) once(parent context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var tenant, id string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT tenant_id::text,candidate_id::text,through_version FROM resume_search_invalidations ORDER BY created_at,tenant_id,candidate_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&tenant, &id, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = s.search.invalidate(ctx, tenant, id, version); err != nil {
		return false, err
	}
	// 旧版本或旧团队的持久向量也清除，保留业务库当前版本对应的新回执。
	if _, err = tx.ExecContext(ctx, `DELETE FROM candidate_resume_vectors v WHERE v.tenant_id=$1 AND v.candidate_id=$2 AND v.resume_version<=$3 AND NOT EXISTS(SELECT 1 FROM candidate_profiles cp WHERE cp.id=v.candidate_id AND cp.tenant_id=v.tenant_id AND cp.row_version=v.resume_version)`, tenant, id, version); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM resume_search_invalidations WHERE tenant_id=$1 AND candidate_id=$2 AND through_version=$3`, tenant, id, version); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// invalidate 只删旧团队下不高于指定版本的文档，迟到清理不会碰到新版本资料。
func (s *elasticResumeSearch) invalidate(ctx context.Context, tenant, id string, version int64) error {
	if !s.Ready() {
		return errors.New("搜索服务未配置")
	}
	raw, status, err := s.request(ctx, http.MethodPost, "/"+s.index+"/_delete_by_query?conflicts=proceed&refresh=true", map[string]any{"query": map[string]any{"bool": map[string]any{"filter": []any{map[string]any{"term": map[string]string{"tenant_id": tenant}}, map[string]any{"term": map[string]string{"candidate_id": id}}, map[string]any{"range": map[string]any{"resume_version": map[string]int64{"lte": version}}}}}}})
	if status == http.StatusNotFound {
		var missing struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &missing) == nil && missing.Error.Type == "index_not_found_exception" {
			return nil
		}
	}
	if err != nil {
		return err
	}
	var result struct {
		Deleted   *int64            `json:"deleted"`
		TimedOut  bool              `json:"timed_out"`
		Failures  []json.RawMessage `json:"failures"`
		Conflicts int64             `json:"version_conflicts"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Deleted == nil || result.TimedOut || len(result.Failures) > 0 || result.Conflicts > 0 {
		return errors.New("旧搜索副本未完整清理，稍后继续重试")
	}
	return nil
}
