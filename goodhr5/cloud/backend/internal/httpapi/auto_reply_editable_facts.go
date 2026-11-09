// 本文件把HR核实事实原子合入真实可编辑资料，防止旧答复覆盖后续人工修改。
package httpapi

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"
)

// replyFactContext 固定模型分析时看到的可编辑资料及版本，落库前必须复核。
type replyFactContext struct {
	PositionDescription string         `json:"position_description"`
	PositionVersion     int            `json:"-"`
	Company             CompanyProfile `json:"company"`
}

// replyFactPromptMaterial 只提供当前可编辑招聘资料，不把锁版本、内部编号或编辑者信息交给模型。
type replyFactPromptMaterial struct {
	PositionDescription string `json:"position_description"`
	CompanyName         string `json:"company_name"`
	CompanyAddress      string `json:"company_address"`
	CompanyOverview     string `json:"company_overview"`
	CompanyExtraInfo    string `json:"company_extra_info"`
}

// questionFactContext 仅读取岗位当前绑定的公司资料，不把历史答复当作不可修改的事实源。
func (s *PostgresAutoReplyStore) questionFactContext(ctx context.Context, tenant, pos string) (replyFactContext, error) {
	var out replyFactContext
	err := s.db.QueryRowContext(ctx, `SELECT cfg.position_description,cfg.version,coalesce(c.id::text,''),coalesce(c.name,''),coalesce(c.address,''),coalesce(c.overview,''),coalesce(c.extra_info,''),coalesce(c.updated_at,'epoch'::timestamptz)
	 FROM position_auto_reply_configs cfg LEFT JOIN tenant_company_profiles c ON c.id=cfg.company_profile_id AND c.tenant_id=cfg.tenant_id WHERE cfg.tenant_id=$1 AND cfg.position_id=$2`, tenant, pos).Scan(
		&out.PositionDescription, &out.PositionVersion, &out.Company.ID, &out.Company.Name, &out.Company.Address, &out.Company.Overview, &out.Company.ExtraInfo, &out.Company.UpdatedAt)
	if err == sql.ErrNoRows {
		return out, ErrNotFound
	}
	return out, err
}

// mergeEditableReplyFact 只替换本问题上次写入且仍完整存在的片段，保留其它人工正文。
func mergeEditableReplyFact(description, oldFact, newFact string) string {
	description, oldFact, newFact = strings.TrimSpace(description), strings.TrimSpace(oldFact), strings.TrimSpace(newFact)
	// 边界是完整独立片段；“不保证双休”不能被当成仍保留了先前写入的“双休”。
	padded := "\n" + description + "\n"
	if oldFact != "" && strings.Contains(padded, "\n"+oldFact+"\n") {
		description = strings.TrimSpace(strings.Replace(padded, "\n"+oldFact+"\n", "\n"+newFact+"\n", 1))
	} else if newFact != "" && !strings.Contains(padded, "\n"+newFact+"\n") {
		if !strings.Contains(description, confirmedReplyFactsHeading) {
			if description != "" {
				description += "\n\n"
			}
			description += confirmedReplyFactsHeading
		}
		description += "\n" + newFact
	}
	if strings.HasSuffix(strings.TrimSpace(description), confirmedReplyFactsHeading) {
		description = strings.TrimSuffix(strings.TrimSpace(description), confirmedReplyFactsHeading)
	}
	return strings.TrimSpace(description)
}

// applyQuestionFact 在已领取的答复事务内写入真实字段，旧版本或旧模型快照不会覆盖人工编辑。
func applyQuestionFact(ctx context.Context, tx *sql.Tx, tenant, pos, company, target, fact, oldFact, oldTarget, oldCompany string, expected []replyFactContext) error {
	var description, currentCompany string
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT position_description,version,coalesce(company_profile_id::text,'') FROM position_auto_reply_configs WHERE tenant_id=$1 AND position_id=$2 FOR UPDATE`, tenant, pos).Scan(&description, &version, &currentCompany); err != nil {
		return err
	}
	if len(expected) > 0 && (version != expected[0].PositionVersion || currentCompany != expected[0].Company.ID) {
		return errReplyStateConflict
	}
	if (target == "company" || target == "both") && (company == "" || company != currentCompany) {
		return errReplyStateConflict
	}
	var extra string
	var updated time.Time
	needsCompany := currentCompany != "" && (target == "company" || target == "both" || oldCompany == currentCompany && (oldTarget == "company" || oldTarget == "both"))
	// 分类也参考公司正文；即使只补入岗位，也不能沿用人工刚刚撤回的公司上下文。
	if needsCompany || len(expected) > 0 && currentCompany != "" {
		if err := tx.QueryRowContext(ctx, `SELECT extra_info,updated_at FROM tenant_company_profiles WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, currentCompany).Scan(&extra, &updated); err != nil {
			return err
		}
		if len(expected) > 0 && !updated.Equal(expected[0].Company.UpdatedAt) {
			return errReplyStateConflict
		}
	}
	oldPosition, newPosition, oldCompanyFact, newCompanyFact := "", "", "", ""
	if oldTarget == "position" || oldTarget == "both" {
		oldPosition = oldFact
	}
	if target == "position" || target == "both" {
		newPosition = fact
	}
	if oldCompany == currentCompany && (oldTarget == "company" || oldTarget == "both") {
		oldCompanyFact = oldFact
	}
	if target == "company" || target == "both" {
		newCompanyFact = fact
	}
	merged := mergeEditableReplyFact(description, oldPosition, newPosition)
	if utf8.RuneCountInString(merged) > 20000 {
		return newAutoReplyValidationError("岗位资料已超过20000字，请整理正文后重试归档")
	}
	if merged != description {
		if _, err := tx.ExecContext(ctx, `UPDATE position_auto_reply_configs SET position_description=$3,version=version+1,updated_at=now() WHERE tenant_id=$1 AND position_id=$2`, tenant, pos, merged); err != nil {
			return err
		}
	}
	if needsCompany {
		merged = mergeEditableReplyFact(extra, oldCompanyFact, newCompanyFact)
		if utf8.RuneCountInString(merged) > 10000 {
			return newAutoReplyValidationError("公司补充资料已超过10000字，请整理正文后重试归档")
		}
		if merged != extra {
			if _, err := tx.ExecContext(ctx, `UPDATE tenant_company_profiles SET extra_info=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenant, currentCompany, merged); err != nil {
				return err
			}
		}
	}
	return nil
}
