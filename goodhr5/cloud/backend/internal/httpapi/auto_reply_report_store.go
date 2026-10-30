// 本文件独立保存本地生成的自动回复报告，避免与简历库人工推荐关系混用。
package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const autoReplyReportColumns = `id,public_id,tenant_id,candidate_id,position_id,version,input_hash,report_data,model,token_usage,share_enabled,generated_at,created_at,updated_at`

// scanAutoReplyReport 把数据库结果转换成兼容前端的结构化报告模型。
func scanAutoReplyReport(row interface{ Scan(...any) error }) (CandidateAutoReplyReport, error) {
	var item CandidateAutoReplyReport
	var raw []byte
	err := row.Scan(&item.ID, &item.PublicID, &item.TenantID, &item.CandidateID, &item.PositionID, &item.Version, &item.InputHash, &raw, &item.Model, &item.TokenUsage, &item.ShareEnabled, &item.GeneratedAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal(raw, &item.Report); err != nil {
		return item, err
	}
	item.Status = "completed"
	item.Summary = item.Report.ExecutiveSummary
	item.MatchScore = item.Report.MatchScore
	item.RecommendationLevel = item.Report.RecommendationLevel
	item.SourceCutoffAt = item.Report.SourceCutoffAt
	return item, nil
}

// SaveAutoReplyReport 幂等保存已生成报告，首次同步默认不开放公开访问。
func (s *PostgresAutoReplyStore) SaveAutoReplyReport(ctx context.Context, item CandidateAutoReplyReport) (CandidateAutoReplyReport, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return item, err
	}
	publicID := hex.EncodeToString(buffer)
	raw, err := json.Marshal(item.Report)
	if err != nil {
		return item, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, item.TenantID+":"+item.CandidateID+":"+item.PositionID); err != nil {
		return item, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO candidate_auto_reply_reports(public_id,tenant_id,candidate_id,position_id,version,input_hash,report_data,model,token_usage,generated_at)
	 SELECT $1,$2,$3,$4,COALESCE(MAX(version),0)+1,$5,$6::jsonb,$7,$8,$9 FROM candidate_auto_reply_reports WHERE tenant_id=$2 AND candidate_id=$3 AND position_id=$4
	 ON CONFLICT(tenant_id,candidate_id,position_id,input_hash) DO NOTHING`, publicID, item.TenantID, item.CandidateID, item.PositionID, item.InputHash, string(raw), item.Model, item.TokenUsage, item.Report.GeneratedAt)
	if err != nil {
		return item, err
	}
	saved, err := scanAutoReplyReport(tx.QueryRowContext(ctx, `SELECT `+autoReplyReportColumns+` FROM candidate_auto_reply_reports WHERE tenant_id=$1 AND candidate_id=$2 AND position_id=$3 AND input_hash=$4`, item.TenantID, item.CandidateID, item.PositionID, item.InputHash))
	if err != nil {
		return saved, err
	}
	return saved, tx.Commit()
}

// ListCandidateAutoReplyReports 只返回指定团队候选人的结构化报告。
func (s *PostgresAutoReplyStore) ListCandidateAutoReplyReports(ctx context.Context, tenantID, candidateID string) ([]CandidateAutoReplyReport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+autoReplyReportColumns+` FROM candidate_auto_reply_reports WHERE tenant_id=$1 AND candidate_id=$2 ORDER BY created_at DESC LIMIT 100`, tenantID, candidateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CandidateAutoReplyReport{}
	for rows.Next() {
		item, err := scanAutoReplyReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetPublicAutoReplyReport 仅允许读取用户明确开启分享的随机地址。
func (s *PostgresAutoReplyStore) GetPublicAutoReplyReport(ctx context.Context, publicID string) (CandidateAutoReplyReport, error) {
	return scanAutoReplyReport(s.db.QueryRowContext(ctx, `SELECT `+autoReplyReportColumns+` FROM candidate_auto_reply_reports WHERE public_id=$1 AND share_enabled=true`, strings.TrimSpace(publicID)))
}

// SetAutoReplyReportShare 修改本账号关联报告的分享状态，团队成员不能随意公开他人的简历。
func (s *PostgresAutoReplyStore) SetAutoReplyReportShare(ctx context.Context, tenantID, userEmail, publicID string, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE candidate_auto_reply_reports report SET share_enabled=$4,updated_at=now() FROM positions position,users owner WHERE report.tenant_id=$1 AND report.public_id=$3 AND position.id=report.position_id AND owner.id=position.user_id AND LOWER(owner.email)=LOWER($2)`, tenantID, userEmail, publicID, enabled)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrNotFound
	}
	return err
}
