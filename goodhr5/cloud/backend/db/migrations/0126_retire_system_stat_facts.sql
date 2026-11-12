-- 文件作用：取消逐条统计事实的重复存储及全部采集触发器；兼容入口改为实时读取既有业务表，不创建新的统计明细。
-- 执行前必须完成整库备份及隔离验收；只移除system_stat_facts中的冗余投影，不修改原始业务记录、钱包或既有日汇总。
-- 旧版仍可通过同名只读视图查询；已清理的业务历史不再由统计明细补存，历史不完整由新版接口明确说明。
DROP TRIGGER IF EXISTS system_stat_login ON users;
DROP TRIGGER IF EXISTS system_stat_ai ON ai_balance_records;
DROP TRIGGER IF EXISTS system_stat_screening ON position_screening_records;
DROP TRIGGER IF EXISTS system_stat_runtime ON browser_events;
DROP TRIGGER IF EXISTS system_stat_greeting ON positions;
DROP FUNCTION IF EXISTS capture_system_stat();

-- 按实际对象类型处理，重复执行只重建无存储视图，不重新创建或回填明细表。
DO $$
DECLARE relation_kind "char";
BEGIN
    SELECT relkind INTO relation_kind FROM pg_class WHERE oid=to_regclass('system_stat_facts');
    IF relation_kind IN ('r','p') THEN
        DROP TABLE system_stat_facts;
    ELSIF relation_kind='v' THEN
        DROP VIEW system_stat_facts;
    ELSIF relation_kind IS NOT NULL THEN
        RAISE EXCEPTION '统计兼容入口类型不符合预期';
    END IF;
END $$;

CREATE VIEW system_stat_facts(event_key,kind,occurred_at,actor_id,dimension,revision,metrics) AS
SELECT 'login:'||id||':'||(last_login_at AT TIME ZONE 'Asia/Shanghai')::date,
       'login'::text,last_login_at,id,''::text,1::bigint,'{}'::jsonb
FROM users WHERE last_login_at IS NOT NULL
UNION ALL
SELECT 'ai:'||id,'ai_usage',created_at,user_id,model_id,1::bigint,
       jsonb_build_object('units',-change_units::numeric,'bonus',CASE WHEN wallet_ledger_version=1 THEN -bonus_change_units ELSE NULL END,'prompt',prompt_tokens,'completion',completion_tokens)
FROM ai_balance_records WHERE category='ai_usage' AND change_units<0
UNION ALL
SELECT 'screening:'||encode(sha256(convert_to(tenant_id||'/'||position_id||'/'||attempt_id,'UTF8')),'hex'),
       'screening',occurred_at,owner_user_id,COALESCE(snapshot->>'platform_id',''),revision,screening_stat_metrics(snapshot)
FROM position_screening_records
UNION ALL
SELECT 'runtime:'||event_id,'runtime',created_at,user_id,COALESCE(payload->>'action',''),1::bigint,
       jsonb_build_object('is_error',CASE WHEN payload->>'is_error'='true' THEN 1 ELSE 0 END)
FROM browser_events WHERE payload->>'action'='agent.start' OR payload->>'is_error'='true'
UNION ALL
SELECT 'greeting:'||encode(sha256(convert_to(id||'/'||daily_greeted_date,'UTF8')),'hex'),
       'greeting',daily_greeted_date::timestamp AT TIME ZONE 'Asia/Shanghai',user_id,platform_id,daily_greeted_count::bigint,
       jsonb_build_object('count',daily_greeted_count)
FROM positions WHERE daily_greeted_count>0;

COMMENT ON VIEW system_stat_facts IS '旧版统计只读兼容视图：实时读取已有业务记录，不存储或累积逐条统计明细；原业务历史清理后不再补存';
COMMENT ON COLUMN system_stat_facts.event_key IS '从当前业务记录即时计算的稳定来源键，不单独存储';
COMMENT ON COLUMN system_stat_facts.kind IS '即时分类：登录、AI扣费、筛选、必要运行事件和岗位当前日招呼';
COMMENT ON COLUMN system_stat_facts.occurred_at IS '原业务记录的实际时间，历史完整性受原业务保留期限制';
COMMENT ON COLUMN system_stat_facts.actor_id IS '原业务记录关联的内部用户编号，接口只返回汇总';
COMMENT ON COLUMN system_stat_facts.dimension IS '原业务模型、平台或有限事件类型';
COMMENT ON COLUMN system_stat_facts.revision IS '原业务修订号或岗位当前日招呼累计值';
COMMENT ON COLUMN system_stat_facts.metrics IS '从现有业务字段即时投影的数值，不保存候选人或请求正文';
