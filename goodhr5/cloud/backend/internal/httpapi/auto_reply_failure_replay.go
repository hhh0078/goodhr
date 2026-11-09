// 本文件只在自动回复运行不再驻留时，向已绑定的原设备补发同一账号已持久化的岗位故障小票。
package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// replayAutoReplyFailures 按运行主键和递增事件键读取本岗位诊断的安全字段，不查询会话或候选人资料。
func (s *PostgresAutoReplyStore) replayAutoReplyFailures(ctx context.Context, tenant, user, machine, run string, after int64) ([]json.RawMessage, bool, error) {
	if _, err := hex.DecodeString(run); len(run) != 32 || err != nil || machine == "" || tenant == "" || user == "" || after < 0 {
		return nil, false, newAutoReplyValidationError("设备运行参数不完整")
	}
	auth := `tenant_id=$1 AND user_id=$2 AND kind='runtime_runs' AND id='run:'||$3 AND payload->>'machine_id'=$4`
	var found bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM auto_reply_cloud_entities WHERE "+auth+")", tenant, user, run, machine).Scan(&found); err != nil || !found {
		return nil, found, err
	}
	prefix := "failure:" + run + ":"
	// 以 ID 的固定前缀及 20 位序号定点读取，清理封印已删除的事件自然不会重新出现。
	rows, err := s.db.QueryContext(ctx, `SELECT ticket FROM (SELECT id,jsonb_build_object(
  'sequence',payload->'failure_sequence','position_id',payload->'position_id','status',payload->'status','step',payload->'title',
  'at',payload->'occurred_at','failure_diagnostics',payload->'failure_diagnostics') AS ticket
   FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='runtime_events'
   AND id>$3 AND id<$4 AND payload->>'failure_machine'=$5 AND payload->>'run_id'=$6
   ORDER BY id DESC LIMIT 1000) AS newest ORDER BY id`, tenant, user, fmt.Sprintf("%s%020d", prefix, after), prefix+"~", machine, run)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var result []json.RawMessage
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, false, err
		}
		result = append(result, json.RawMessage(raw))
	}
	return result, true, rows.Err()
}
