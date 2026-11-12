// 本文件使用只读一致性快照聚合全站统计；财务复用权威流水和日账，其他指标读取现有业务，不复制保存统计明细。
package httpapi

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// postgresSystemStatistics 仅查询云端业务表，不访问本地浏览器或招聘平台。
type postgresSystemStatistics struct{ db *sql.DB }

// Summary 在同一个只读事务读取汇总与图表，期间数据和当前快照分别返回。
func (s *postgresSystemStatistics) Summary(ctx context.Context, start, end, now time.Time) (SystemStatistics, error) {
	r := SystemStatistics{StartDate: start.Format(time.DateOnly), EndDate: end.AddDate(0, 0, -1).Format(time.DateOnly), AsOf: now, Metrics: map[string]int64{}, Amounts: map[string]string{}, Current: map[string]int64{}, CurrentAmounts: map[string]string{}, Funnel: []SystemFunnelStep{}, Curve: []SystemConsumptionDay{}, Platforms: []SystemStatisticGroup{}, Models: []SystemStatisticGroup{}, Sources: []SystemStatisticGroup{}, Versions: []SystemStatisticGroup{}, Errors: []SystemStatisticGroup{}, Blocked: []SystemStatisticGroup{}, Warnings: []string{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if err = statisticsCurrent(ctx, tx, &r); err != nil {
		return r, err
	}
	if err = statisticsFinance(ctx, tx, &r, start, end); err != nil {
		return r, err
	}
	if err = statisticsBusiness(ctx, tx, &r, start, end); err != nil {
		return r, err
	}
	if err = statisticsRetention(ctx, tx, &r, start, end, now); err != nil {
		return r, err
	}
	if err = statisticsFunnel(ctx, tx, &r, start, end); err != nil {
		return r, err
	}
	curveStart := start
	if end.Sub(start) == 24*time.Hour {
		curveStart = end.AddDate(0, 0, -7)
	}
	if err = statisticsCurve(ctx, tx, &r, curveStart, end); err != nil {
		return r, err
	}
	if err = statisticsGroups(ctx, tx, &r, start, end); err != nil {
		return r, err
	}
	if err = statisticsCoverage(ctx, tx, &r, start); err != nil {
		return r, err
	}
	return r, tx.Commit()
}

// statisticsCurrent 读取当前总量和真实本金/赠送余额，不套用用户管理页的固定赠送估算。
func statisticsCurrent(ctx context.Context, tx *sql.Tx, r *SystemStatistics) error {
	var users, positions, running, resumes, teams, bindings, balance, bonus int64
	err := tx.QueryRowContext(ctx, `SELECT
	 (SELECT COUNT(*) FROM users),(SELECT COUNT(*) FROM positions),(SELECT COUNT(*) FROM positions WHERE status='running'),
	 (SELECT COUNT(*) FROM candidate_profiles WHERE id<>$1::uuid),(SELECT COUNT(*) FROM tenants),
	 (SELECT COUNT(DISTINCT user_id) FROM local_agents WHERE bind_status='active'),
	 (SELECT COALESCE(SUM(ai_balance_units::numeric),0) FROM users),(SELECT COALESCE(SUM(ai_bonus_balance_units::numeric),0) FROM users)`, builtinCandidateID).Scan(&users, &positions, &running, &resumes, &teams, &bindings, &balance, &bonus)
	if err != nil {
		return err
	}
	r.Current = map[string]int64{"users": users, "positions": positions, "running_positions": running, "resumes": resumes, "teams": teams, "bound_users": bindings}
	principal, err := checkedAIWalletAdd(balance, -bonus)
	if err != nil {
		return err
	}
	r.CurrentAmounts = map[string]string{"balance": aiUnitsToYuanString(balance), "bonus": aiUnitsToYuanString(bonus), "principal": aiUnitsToYuanString(principal)}
	return nil
}

// statisticsFinance 以实际支付、成功退款及扣费发生时间入账，消费金额包含已清理明细的权威日汇总。
func statisticsFinance(ctx context.Context, tx *sql.Tx, r *SystemStatistics, start, end time.Time) error {
	var units, calls, knownCalls, knownUnits, bonusKnown, bonus, prompt, completion, consumers int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(units),0),COALESCE(SUM(calls),0) FROM (
	 SELECT -change_units::numeric AS units,1 AS calls FROM ai_balance_records WHERE category='ai_usage' AND change_units<0 AND created_at >= $1 AND created_at < $2
	 UNION ALL SELECT usage_units::numeric,record_count FROM ai_usage_daily WHERE usage_day >= ($1 AT TIME ZONE 'Asia/Shanghai')::date AND usage_day < ($2 AT TIME ZONE 'Asia/Shanghai')::date
	) x`, start, end).Scan(&units, &calls)
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM((metrics->>'units')::numeric),0),COUNT(metrics->>'bonus'),COALESCE(SUM((metrics->>'bonus')::numeric),0),COALESCE(SUM((metrics->>'prompt')::numeric),0),COALESCE(SUM((metrics->>'completion')::numeric),0),COUNT(DISTINCT actor_id) FROM system_stat_facts WHERE kind='ai_usage' AND occurred_at >= $1 AND occurred_at < $2`, start, end).Scan(&knownCalls, &knownUnits, &bonusKnown, &bonus, &prompt, &completion, &consumers)
	if err != nil {
		return err
	}
	r.Amounts["consumption"] = aiUnitsToYuanString(units)
	r.Metrics["charged_calls"] = calls
	if knownCalls == calls && knownUnits == units {
		r.Metrics["prompt_tokens"], r.Metrics["completion_tokens"], r.Metrics["consumers"] = prompt, completion, consumers
		if bonusKnown == calls {
			principal, err := checkedAIWalletAdd(units, -bonus)
			if err != nil {
				return err
			}
			r.Amounts["bonus_consumption"], r.Amounts["principal_consumption"] = aiUnitsToYuanString(bonus), aiUnitsToYuanString(principal)
		} else {
			r.Warnings = append(r.Warnings, "部分旧消费没有本金与赠送余额拆分，消费总额仍准确，拆分金额暂不展示。")
		}
	} else {
		r.Warnings = append(r.Warnings, "部分历史AI明细已清理：消费金额和扣费笔数完整，Token、消费人数及模型分布仅保留已采集部分，不用零代替缺失。")
	}
	var receipts, aiReceipts, refunds, paid, payers, refundCount, gift, adjustPlus, adjustMinus, legacyGift int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents::numeric),0),COALESCE(SUM(amount_cents::numeric) FILTER(WHERE order_type='ai_balance'),0),COUNT(*),COUNT(DISTINCT user_id) FROM payment_orders WHERE status='paid' AND paid_at >= $1 AND paid_at < $2`, start, end).Scan(&receipts, &aiReceipts, &paid, &payers)
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents::numeric),0),COUNT(*) FROM payment_refunds WHERE status='success' AND completed_at >= $1 AND completed_at < $2`, start, end).Scan(&refunds, &refundCount)
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(bonus_change_units::numeric) FILTER(WHERE bonus_change_units>0 AND category<>'refund_release'),0),COALESCE(SUM(change_units::numeric) FILTER(WHERE category='admin_adjust' AND change_units>0),0),COALESCE(SUM(-change_units::numeric) FILTER(WHERE category='admin_adjust' AND change_units<0),0),COUNT(*) FILTER(WHERE wallet_ledger_version<>1 AND change_units>0 AND category<>'refund_release') FROM ai_balance_records WHERE created_at >= $1 AND created_at < $2`, start, end).Scan(&gift, &adjustPlus, &adjustMinus, &legacyGift)
	if err != nil {
		return err
	}
	net, err := checkedAIWalletAdd(receipts, -refunds)
	if err != nil {
		return err
	}
	r.Amounts["receipts"], r.Amounts["ai_recharge"], r.Amounts["refunds"], r.Amounts["net_receipts"] = adminStatsCentsToYuanString(receipts), adminStatsCentsToYuanString(aiReceipts), adminStatsCentsToYuanString(refunds), adminStatsCentsToYuanString(net)
	if legacyGift == 0 {
		r.Amounts["gift"] = aiUnitsToYuanString(gift)
	} else {
		r.Warnings = append(r.Warnings, "这段时间有旧版入账缺少赠送拆分，新增赠送余额暂不展示，实付收款不受影响。")
	}
	r.Amounts["admin_increase"], r.Amounts["admin_decrease"] = aiUnitsToYuanString(adjustPlus), aiUnitsToYuanString(adjustMinus)
	r.Metrics["paid_orders"], r.Metrics["payers"], r.Metrics["refund_count"] = paid, payers, refundCount
	return nil
}

// statisticsBusiness 汇总期间发生的增长、筛选、简历、推荐和面试；当前状态不冒充期间完成量。
func statisticsBusiness(ctx context.Context, tx *sql.Tx, r *SystemStatistics, start, end time.Time) error {
	queries := []struct{ key, sql string }{
		{"registered", `SELECT COUNT(*) FROM users WHERE created_at >= $1 AND created_at < $2`},
		{"active_users", `SELECT COUNT(DISTINCT actor_id) FROM system_stat_facts WHERE occurred_at >= $1 AND occurred_at < $2 AND (kind IN ('login','screening','ai_usage') OR (kind='runtime' AND dimension='agent.start'))`},
		{"active_teams", `SELECT COUNT(DISTINCT u.tenant_id) FROM system_stat_facts f JOIN users u ON u.id=f.actor_id WHERE f.occurred_at >= $1 AND f.occurred_at < $2 AND (f.kind IN ('login','screening','ai_usage') OR (f.kind='runtime' AND f.dimension='agent.start'))`},
		{"screening_users", `SELECT COUNT(DISTINCT actor_id) FROM system_stat_facts WHERE kind='screening' AND occurred_at >= $1 AND occurred_at < $2`},
		{"new_positions", `SELECT COUNT(*) FROM positions WHERE created_at >= $1 AND created_at < $2`},
		{"greeted", `SELECT COALESCE(SUM((metrics->>'count')::numeric),0) FROM system_stat_facts WHERE kind='greeting' AND occurred_at >= $1 AND occurred_at < $2`},
		{"processed_resumes", `SELECT COALESCE(SUM(processed_resume_count::numeric),0) FROM system_daily_stats WHERE stat_date >= ($1 AT TIME ZONE 'Asia/Shanghai')::date AND stat_date < ($2 AT TIME ZONE 'Asia/Shanghai')::date`},
		{"new_resumes", `SELECT COUNT(*) FROM candidate_profiles WHERE created_at >= $1 AND created_at < $2 AND id<>'00000000-0000-4000-8000-000000000001'::uuid`},
		{"recommendations", `SELECT COUNT(*) FROM candidate_recommendations WHERE created_at >= $1 AND created_at < $2`},
		{"recommendation_updates", `SELECT COUNT(*) FROM candidate_recommendation_events WHERE created_at >= $1 AND created_at < $2`},
		{"applications", `SELECT COUNT(*) FROM recruitment_applications WHERE created_at >= $1 AND created_at < $2`},
		{"interviews", `SELECT COUNT(*) FROM recruitment_applications a CROSS JOIN LATERAL jsonb_array_elements(COALESCE(a.payload->'interviews','[]'::jsonb)) i WHERE (i->>'starts_at')::timestamptz >= $1 AND (i->>'starts_at')::timestamptz < $2 AND i->>'status'<>'cancelled'`},
		{"onboardings", `SELECT COUNT(*) FROM recruitment_applications WHERE NULLIF(payload->>'entry_date','')::date >= ($1 AT TIME ZONE 'Asia/Shanghai')::date AND NULLIF(payload->>'entry_date','')::date < ($2 AT TIME ZONE 'Asia/Shanghai')::date AND status='completed' AND payload->>'stage'='hired'`},
		{"new_customers", `SELECT COUNT(*) FROM headhunter_customers WHERE created_at >= $1 AND created_at < $2`},
		{"new_customer_jobs", `SELECT COUNT(*) FROM headhunter_jobs WHERE created_at >= $1 AND created_at < $2`},
		{"runtime_errors", `SELECT COUNT(*) FROM system_stat_facts WHERE kind='runtime' AND metrics->>'is_error'='1' AND occurred_at >= $1 AND occurred_at < $2`},
		{"affected_users", `SELECT COUNT(DISTINCT actor_id) FROM system_stat_facts WHERE kind='runtime' AND metrics->>'is_error'='1' AND occurred_at >= $1 AND occurred_at < $2`},
	}
	for _, q := range queries {
		var value int64
		if err := tx.QueryRowContext(ctx, q.sql, start, end).Scan(&value); err != nil {
			return fmt.Errorf("%s: %w", q.key, err)
		}
		r.Metrics[q.key] = value
	}
	var count, firstDone, firstPass, secondDone, secondPass, failed, duration, samples int64
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM((metrics->>'first_done')::bigint),0),COALESCE(SUM((metrics->>'first_pass')::bigint),0),COALESCE(SUM((metrics->>'second_done')::bigint),0),COALESCE(SUM((metrics->>'second_pass')::bigint),0),COALESCE(SUM((metrics->>'failed')::bigint),0),COALESCE(SUM((metrics->>'duration_ms')::numeric),0),COALESCE(SUM((metrics->>'duration_count')::bigint),0) FROM system_stat_facts WHERE kind='screening' AND occurred_at >= $1 AND occurred_at < $2`, start, end).Scan(&count, &firstDone, &firstPass, &secondDone, &secondPass, &failed, &duration, &samples)
	if err != nil {
		return err
	}
	r.Metrics["screenings"], r.Metrics["first_done"], r.Metrics["first_pass"], r.Metrics["second_done"], r.Metrics["second_pass"], r.Metrics["screening_failed"] = count, firstDone, firstPass, secondDone, secondPass, failed
	if samples > 0 {
		r.Metrics["average_screening_ms"] = duration / samples
	}
	return nil
}

// statisticsRetention 只计算观察日已结束且完整采集的注册用户，未到期和缺历史不当成未回访。
func statisticsRetention(ctx context.Context, tx *sql.Tx, r *SystemStatistics, start, end, now time.Time) error {
	retained, err := statisticsRetainsFacts(ctx, tx)
	if err != nil {
		return err
	}
	if !retained {
		for _, offset := range []int{1, 7} {
			r.Metrics[fmt.Sprintf("retention_%d_eligible", offset)] = 0
			r.Metrics[fmt.Sprintf("retention_%d_returned", offset)] = 0
		}
		r.Warnings = append(r.Warnings, "历史登录明细不再保存，留存率暂不展示。")
		return nil
	}
	for _, offset := range []int{1, 7} {
		var eligible, returned int64
		err := tx.QueryRowContext(ctx, `WITH cohort AS (
		 SELECT id,((created_at AT TIME ZONE 'Asia/Shanghai')::date+$3::int) AS observed_day
		 FROM users WHERE created_at >= $1 AND created_at < $2
		), mature AS (
		 SELECT * FROM cohort WHERE observed_day < ($4 AT TIME ZONE 'Asia/Shanghai')::date
		 AND (observed_day::timestamp AT TIME ZONE 'Asia/Shanghai') >= (SELECT MAX(complete_from) FROM system_stat_coverage)
		) SELECT COUNT(*),COUNT(*) FILTER(WHERE EXISTS(
		 SELECT 1 FROM system_stat_facts f WHERE f.actor_id=m.id AND f.occurred_at >= (m.observed_day::timestamp AT TIME ZONE 'Asia/Shanghai')
		 AND f.occurred_at < ((m.observed_day+1)::timestamp AT TIME ZONE 'Asia/Shanghai')
		 AND (f.kind IN ('login','screening','ai_usage') OR (f.kind='runtime' AND f.dimension='agent.start'))
		)) FROM mature m`, start, end, offset, now).Scan(&eligible, &returned)
		if err != nil {
			return err
		}
		r.Metrics[fmt.Sprintf("retention_%d_eligible", offset)], r.Metrics[fmt.Sprintf("retention_%d_returned", offset)] = eligible, returned
	}
	return nil
}

// statisticsRetainsFacts 识别旧明细表与新版无存储视图，避免把缺失的历史活动当成零或完整留存。
func statisticsRetainsFacts(ctx context.Context, tx *sql.Tx) (bool, error) {
	var retained bool
	err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT relkind IN ('r','p') FROM pg_class WHERE oid=to_regclass('system_stat_facts')),false)`).Scan(&retained)
	return retained, err
}

// statisticsFunnel 使用所选期间注册的同一批用户依次统计，人数去重且不把跨批次活动拼成转化率。
func statisticsFunnel(ctx context.Context, tx *sql.Tx, r *SystemStatistics, start, end time.Time) error {
	r.Funnel = append(r.Funnel, SystemFunnelStep{Key: "registered", Label: "注册账号", Count: r.Metrics["registered"]})
	rows, err := tx.QueryContext(ctx, `WITH cohort AS (SELECT flow_state FROM users WHERE created_at >= $1 AND created_at < $2)
	 SELECT step.key,COUNT(*) FILTER(WHERE NOT EXISTS(SELECT 1 FROM unnest($3::text[]) WITH ORDINALITY previous(key,n) WHERE previous.n<=step.n AND COALESCE(c.flow_state->'steps'->previous.key->>'status','')<>'completed'))
	 FROM unnest($3::text[]) WITH ORDINALITY step(key,n) LEFT JOIN cohort c ON true GROUP BY step.key,step.n ORDER BY step.n`, start, end, pq.Array(userFlowStepOrder))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item SystemFunnelStep
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return err
		}
		item.Label = userFlowStepNames[item.Key]
		r.Funnel = append(r.Funnel, item)
	}
	return rows.Err()
}

// statisticsCurve 一次读取权威日汇总，补齐无消费自然日；单日筛选默认显示截至该日的七天曲线。
func statisticsCurve(ctx context.Context, tx *sql.Tx, r *SystemStatistics, start, end time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT day::text,SUM(units),SUM(calls) FROM (
	 SELECT (created_at AT TIME ZONE 'Asia/Shanghai')::date AS day,-change_units::numeric AS units,1::bigint AS calls FROM ai_balance_records WHERE category='ai_usage' AND change_units<0 AND created_at >= $1 AND created_at < $2
	 UNION ALL SELECT usage_day,usage_units::numeric,record_count FROM ai_usage_daily WHERE usage_day>=($1 AT TIME ZONE 'Asia/Shanghai')::date AND usage_day<($2 AT TIME ZONE 'Asia/Shanghai')::date
	 ) x GROUP BY day ORDER BY day`, start, end)
	if err != nil {
		return err
	}
	defer rows.Close()
	days := map[string]SystemConsumptionDay{}
	for rows.Next() {
		var day string
		var units, calls int64
		if err := rows.Scan(&day, &units, &calls); err != nil {
			return err
		}
		days[day] = SystemConsumptionDay{Date: day, Amount: aiUnitsToYuanString(units), Calls: calls}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		value, ok := days[key]
		if !ok {
			value = SystemConsumptionDay{Date: key, Amount: "0.0000"}
		}
		r.Curve = append(r.Curve, value)
	}
	return nil
}

// statisticsGroups 返回平台、模型、来源、版本和错误类型的聚合，不返回个人排行或个人数据。
func statisticsGroups(ctx context.Context, tx *sql.Tx, r *SystemStatistics, start, end time.Time) error {
	queries := []struct {
		target *[]SystemStatisticGroup
		query  string
	}{
		{&r.Platforms, `SELECT dimension,COUNT(*),0::numeric FROM system_stat_facts WHERE kind='screening' AND occurred_at >= $1 AND occurred_at < $2 GROUP BY dimension ORDER BY COUNT(*) DESC,dimension`},
		{&r.Models, `SELECT dimension,COUNT(*),COALESCE(SUM((metrics->>'units')::numeric),0) FROM system_stat_facts WHERE kind='ai_usage' AND occurred_at >= $1 AND occurred_at < $2 GROUP BY dimension ORDER BY SUM((metrics->>'units')::numeric) DESC,dimension`},
		{&r.Sources, `SELECT COALESCE(NULLIF(LOWER(BTRIM(library_fields->>'source_type')),''),'automatic'),COUNT(*),0::numeric FROM candidate_profiles WHERE created_at >= $1 AND created_at < $2 AND id<>'00000000-0000-4000-8000-000000000001'::uuid GROUP BY 1 ORDER BY COUNT(*) DESC,1`},
		{&r.Errors, `SELECT dimension,COUNT(*),0::numeric FROM system_stat_facts WHERE kind='runtime' AND metrics->>'is_error'='1' AND occurred_at >= $1 AND occurred_at < $2 GROUP BY dimension ORDER BY COUNT(*) DESC,dimension LIMIT 10`},
		{&r.Blocked, `SELECT COALESCE(flow_state->>'stage','unknown'),COUNT(*),0::numeric FROM users WHERE created_at >= $1 AND created_at < $2 AND flow_state->>'state'='blocked' GROUP BY 1 ORDER BY COUNT(*) DESC,1`},
	}
	for _, q := range queries {
		rows, err := tx.QueryContext(ctx, q.query, start, end)
		if err != nil {
			return err
		}
		for rows.Next() {
			var group SystemStatisticGroup
			var units int64
			if err := rows.Scan(&group.Name, &group.Count, &units); err != nil {
				rows.Close()
				return err
			}
			if q.target == &r.Models {
				group.Amount = aiUnitsToYuanString(units)
			}
			*q.target = append(*q.target, group)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT agent_version,COUNT(*) FROM (SELECT DISTINCT ON(user_id) user_id,agent_version FROM local_agents WHERE bind_status='active' ORDER BY user_id,last_seen_at DESC NULLS LAST,id DESC) x GROUP BY agent_version ORDER BY COUNT(*) DESC,agent_version`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var group SystemStatisticGroup
		if err := rows.Scan(&group.Name, &group.Count); err != nil {
			return err
		}
		r.Versions = append(r.Versions, group)
	}
	return rows.Err()
}

// statisticsCoverage 直说新统计采集起点，禁止将回填不全的老记录冒充全天或历史完整统计。
func statisticsCoverage(ctx context.Context, tx *sql.Tx, r *SystemStatistics, start time.Time) error {
	retained, err := statisticsRetainsFacts(ctx, tx)
	if err != nil {
		return err
	}
	if !retained {
		r.Warnings = append(r.Warnings, "统计按当前保留的业务记录计算，历史活跃、筛选、运行异常和招呼可能不完整。消费金额与扣费笔数仍包含日汇总。")
		return nil
	}
	var since time.Time
	if err := tx.QueryRowContext(ctx, `SELECT MAX(complete_from) FROM system_stat_coverage`).Scan(&since); err != nil {
		return err
	}
	if start.Before(since) {
		r.Warnings = append(r.Warnings, "活跃、筛选与运行异常从北京时间"+since.In(aiUsageDay(start).Location()).Format("2006-01-02 15:04")+"开始完整采集；此前只统计仍有记录的部分。注册、支付和消费金额按已有权威记录汇总。")
	}
	return nil
}
