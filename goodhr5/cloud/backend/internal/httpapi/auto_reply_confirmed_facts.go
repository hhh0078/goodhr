// 本文件处理 HR 已确认的事实补充，按答复版本覆盖岗位与公司归档，保留人工填写的原文。
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const defaultAutoReplySupplementPrompt = "根据岗位、公司、规范化问题及HR答复，提取HR明确确认且可公开的招聘事实。问题和候选人的猜测均不是依据。保留限制条件、数字与范围，不得添加姓名、联系方式、候选人资料或推测。岗位职责、薪资、岗位排班和要求归 position，公司通用政策和福利归 company；两处都适用才归 both。只输出 JSON 对象，字段为 publish_target 和 fact。回答不足以形成独立可用事实时两个字段都返回空字符串。"

const confirmedReplyFactsHeading = "【HR 已确认补充】原描述中标为待确认的同一事项，以以下答复为准。"

// replyDescriptionWithFacts 为执行快照保留人工原文，并明确区分已经由 HR 核实的答复。
func replyDescriptionWithFacts(description string, facts []string) string {
	if len(facts) == 0 {
		return description
	}
	return strings.TrimSpace(description + "\n\n" + confirmedReplyFactsHeading + "\n" + strings.Join(facts, "\n"))
}

// appendConfirmedFacts 仅在执行快照中按岗位和公司合并已归档事实，不修改人工填写的描述。
func (s *PostgresAutoReplyStore) appendConfirmedFacts(ctx context.Context, cfg *PositionAutoReplyConfig, company *CompanyProfile) error {
	rows, err := s.db.QueryContext(ctx, `SELECT position_id::text,publish_target,fact,coalesce(company_profile_id::text,'') FROM auto_reply_cloud_questions WHERE tenant_id=$1 AND status='resolved' AND ((position_id=$2 AND publish_target IN ('position','both')) OR (company_profile_id=nullif($3,'')::uuid AND publish_target IN ('company','both'))) ORDER BY id`, cfg.TenantID, cfg.PositionID, cfg.CompanyProfileID)
	if err != nil {
		return err
	}
	defer rows.Close()
	hash := sha256.New()
	changed := false
	hash.Write([]byte(cfg.PhrasesVersion))
	for rows.Next() {
		var pos, target, fact, co string
		if err := rows.Scan(&pos, &target, &fact, &co); err != nil {
			return err
		}
		if pos == cfg.PositionID && (target == "position" || target == "both") {
			cfg.ConfirmedFacts = append(cfg.ConfirmedFacts, fact)
		}
		if co == company.ID && co != "" && (target == "company" || target == "both") {
			company.ConfirmedFacts = append(company.ConfirmedFacts, fact)
		}
		changed = true
		fmt.Fprintf(hash, "\x00%s\x00%s\x00%s", pos, target, fact)
	}
	if changed {
		hash.Write([]byte(confirmedReplyFactsHeading))
		cfg.PositionDescription = replyDescriptionWithFacts(cfg.PositionDescription, cfg.ConfirmedFacts)
		company.ExtraInfo = replyDescriptionWithFacts(company.ExtraInfo, company.ConfirmedFacts)
		cfg.PhrasesVersion = hex.EncodeToString(hash.Sum(nil))
	}
	return rows.Err()
}

// loadCompanyConfirmedFacts 一次查询团队共享的公司事实，保持可编辑字段与人工确认的补充各自独立。
func (s *PostgresAutoReplyStore) loadCompanyConfirmedFacts(ctx context.Context, tenant string, companies []CompanyProfile) error {
	if len(companies) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT company_profile_id::text,fact FROM auto_reply_cloud_questions WHERE tenant_id=$1 AND company_profile_id IS NOT NULL AND status='resolved' AND publish_target IN ('company','both') ORDER BY id`, tenant)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string]int{}
	for i, c := range companies {
		byID[c.ID] = i
	}
	for rows.Next() {
		var id, fact string
		if err := rows.Scan(&id, &fact); err != nil {
			return err
		}
		if i, ok := byID[id]; ok {
			companies[i].ConfirmedFacts = append(companies[i].ConfirmedFacts, fact)
		}
	}
	return rows.Err()
}
