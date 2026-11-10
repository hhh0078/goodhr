-- 文件作用：为全站统计保存不含简历正文的去重事件事实；回填仍在库的数据，不修改钱包、业务记录或历史清理策略。
CREATE TABLE IF NOT EXISTS system_stat_facts (
    event_key TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('login','ai_usage','screening','runtime','greeting')),
    occurred_at TIMESTAMPTZ NOT NULL,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    dimension TEXT NOT NULL DEFAULT '',
    revision BIGINT NOT NULL DEFAULT 1,
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb
);
COMMENT ON TABLE system_stat_facts IS '全站统计去重事实，仅保存数值、模型或平台，不保存候选人身份、账号、请求正文或错误正文';
COMMENT ON COLUMN system_stat_facts.event_key IS '来源编号摘要或用户日登录键，业务明细清理后仍用于防止重传重复计数';
COMMENT ON COLUMN system_stat_facts.kind IS '事实类型：login登录、ai_usage实际扣费、screening筛选、runtime运行事件、greeting成功招呼';
COMMENT ON COLUMN system_stat_facts.occurred_at IS '实际发生时间，查询按北京时间自然日汇总';
COMMENT ON COLUMN system_stat_facts.actor_id IS '仅用于去重活跃人数及留存的内部用户编号，删除用户后置空，不通过统计接口返回';
COMMENT ON COLUMN system_stat_facts.dimension IS 'AI模型、招聘平台或有限运行事件名称，不含业务正文';
COMMENT ON COLUMN system_stat_facts.revision IS '筛选快照修订号，仅接受更高版本，防止断网重传重复累计';
COMMENT ON COLUMN system_stat_facts.metrics IS '数值指标：units扣费、bonus赠送扣费、prompt与completion用量；first_done与first_pass初筛、second_done与second_pass复筛、failed失败、duration_ms耗时、duration_count耗时样本、is_error运行错误';
CREATE INDEX IF NOT EXISTS idx_system_stat_facts_kind_time ON system_stat_facts(kind,occurred_at);
CREATE INDEX IF NOT EXISTS idx_system_stat_facts_actor_time ON system_stat_facts(actor_id,occurred_at) WHERE actor_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_system_statistics_users_created ON users(created_at);
CREATE INDEX IF NOT EXISTS idx_system_statistics_positions_created ON positions(created_at);
CREATE INDEX IF NOT EXISTS idx_system_statistics_resumes_created ON candidate_profiles(created_at);
CREATE INDEX IF NOT EXISTS idx_system_statistics_orders_paid ON payment_orders(paid_at) WHERE status='paid';
CREATE INDEX IF NOT EXISTS idx_system_statistics_refunds_completed ON payment_refunds(completed_at) WHERE status='success';

CREATE TABLE IF NOT EXISTS system_stat_coverage (
    kind TEXT PRIMARY KEY,
    complete_from TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE system_stat_coverage IS '统计完整采集开始时间；之前只回填仍在库的事实，禁止将缺失历史显示为零';
COMMENT ON COLUMN system_stat_coverage.kind IS '统计事实类型';
COMMENT ON COLUMN system_stat_coverage.complete_from IS '该类型从此时开始完整采集，不能被回填时间覆盖';
INSERT INTO system_stat_coverage(kind) VALUES ('login'),('ai_usage'),('screening'),('runtime'),('greeting') ON CONFLICT DO NOTHING;

-- screening_stat_metrics 只投影筛选结果中的数值，不复制候选人身份或判定正文。
CREATE OR REPLACE FUNCTION screening_stat_metrics(snapshot JSONB) RETURNS JSONB LANGUAGE SQL IMMUTABLE AS $$
 SELECT jsonb_build_object(
            'first_done',CASE WHEN snapshot#>>'{first,status}'='completed' THEN 1 ELSE 0 END,
            'first_pass',CASE WHEN snapshot#>>'{first,accepted}'='true' THEN 1 ELSE 0 END,
            'second_done',CASE WHEN snapshot#>>'{second,status}'='completed' THEN 1 ELSE 0 END,
            'second_pass',CASE WHEN snapshot#>>'{second,accepted}'='true' THEN 1 ELSE 0 END,
            'failed',CASE WHEN snapshot#>>'{first,status}'='failed' OR snapshot#>>'{second,status}'='failed' THEN 1 ELSE 0 END,
            'duration_ms',COALESCE((snapshot#>>'{first,duration_ms}')::bigint,0)+COALESCE((snapshot#>>'{second,duration_ms}')::bigint,0),
            'duration_count',CASE WHEN snapshot#>>'{first,duration_ms}' IS NOT NULL THEN 1 ELSE 0 END+CASE WHEN snapshot#>>'{second,duration_ms}' IS NOT NULL THEN 1 ELSE 0 END);
$$;
COMMENT ON FUNCTION screening_stat_metrics(JSONB) IS '从筛选快照提取完成、通过、失败和耗时等数值指标';

-- capture_system_stat 在原业务事务中同步统计事实，失败使原事务回滚，重传通过稳定键和修订号去重。
CREATE OR REPLACE FUNCTION capture_system_stat() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fact_key TEXT; fact_kind TEXT; fact_at TIMESTAMPTZ; actor UUID; dim TEXT; rev BIGINT := 1; value JSONB;
BEGIN
    IF TG_TABLE_NAME='users' THEN
        IF NEW.last_login_at IS NULL OR NEW.last_login_at IS NOT DISTINCT FROM OLD.last_login_at THEN RETURN NEW; END IF;
        fact_key := 'login:'||NEW.id||':'||(NEW.last_login_at AT TIME ZONE 'Asia/Shanghai')::date;
        fact_kind := 'login'; fact_at := NEW.last_login_at; actor := NEW.id; dim := ''; value := '{}';
    ELSIF TG_TABLE_NAME='ai_balance_records' THEN
        IF NEW.category<>'ai_usage' OR NEW.change_units>=0 THEN RETURN NEW; END IF;
        fact_key := 'ai:'||NEW.id; fact_kind := 'ai_usage'; fact_at := NEW.created_at; actor := NEW.user_id; dim := NEW.model_id;
        value := jsonb_build_object('units',-NEW.change_units::numeric,'bonus',CASE WHEN NEW.wallet_ledger_version=1 THEN -NEW.bonus_change_units ELSE NULL END,'prompt',NEW.prompt_tokens,'completion',NEW.completion_tokens);
    ELSIF TG_TABLE_NAME='position_screening_records' THEN
        fact_key := 'screening:'||encode(sha256(convert_to(NEW.tenant_id||'/'||NEW.position_id||'/'||NEW.attempt_id,'UTF8')),'hex');
        fact_kind := 'screening'; fact_at := NEW.occurred_at; actor := NEW.owner_user_id; dim := COALESCE(NEW.snapshot->>'platform_id',''); rev := NEW.revision;
        value := screening_stat_metrics(NEW.snapshot);
    ELSIF TG_TABLE_NAME='positions' THEN
        IF NEW.daily_greeted_count<=0 THEN RETURN NEW; END IF;
        fact_key := 'greeting:'||encode(sha256(convert_to(NEW.id||'/'||NEW.daily_greeted_date,'UTF8')),'hex');
        fact_kind := 'greeting'; fact_at := NEW.daily_greeted_date::timestamp AT TIME ZONE 'Asia/Shanghai'; actor := NEW.user_id;
        dim := NEW.platform_id; rev := NEW.daily_greeted_count; value := jsonb_build_object('count',NEW.daily_greeted_count);
    ELSE
        fact_key := 'runtime:'||NEW.event_id; fact_kind := 'runtime'; fact_at := NEW.created_at; actor := NEW.user_id;
        dim := COALESCE(NEW.payload->>'action',''); value := jsonb_build_object('is_error',CASE WHEN NEW.payload->>'is_error'='true' THEN 1 ELSE 0 END);
    END IF;
    INSERT INTO system_stat_facts(event_key,kind,occurred_at,actor_id,dimension,revision,metrics)
    VALUES(fact_key,fact_kind,fact_at,actor,dim,rev,value)
    ON CONFLICT(event_key) DO UPDATE SET revision=EXCLUDED.revision,metrics=EXCLUDED.metrics
    WHERE system_stat_facts.revision<EXCLUDED.revision;
    RETURN NEW;
END $$;
COMMENT ON FUNCTION capture_system_stat() IS '在原事务内按稳定来源键保存数值统计，筛选仅接受较新修订，所有事实均不保留候选人正文';

DROP TRIGGER IF EXISTS system_stat_login ON users;
CREATE TRIGGER system_stat_login AFTER UPDATE OF last_login_at ON users FOR EACH ROW EXECUTE FUNCTION capture_system_stat();
DROP TRIGGER IF EXISTS system_stat_ai ON ai_balance_records;
CREATE TRIGGER system_stat_ai AFTER INSERT ON ai_balance_records FOR EACH ROW EXECUTE FUNCTION capture_system_stat();
DROP TRIGGER IF EXISTS system_stat_screening ON position_screening_records;
CREATE TRIGGER system_stat_screening AFTER INSERT OR UPDATE ON position_screening_records FOR EACH ROW EXECUTE FUNCTION capture_system_stat();
DROP TRIGGER IF EXISTS system_stat_runtime ON browser_events;
CREATE TRIGGER system_stat_runtime AFTER INSERT ON browser_events FOR EACH ROW EXECUTE FUNCTION capture_system_stat();
DROP TRIGGER IF EXISTS system_stat_greeting ON positions;
CREATE TRIGGER system_stat_greeting AFTER UPDATE OF daily_greeted_count,daily_greeted_date ON positions FOR EACH ROW EXECUTE FUNCTION capture_system_stat();

-- 触发器统一负责新数据；只回填仍能被真实记录证明的数据，不伪造已清理历史。
INSERT INTO system_stat_facts(event_key,kind,occurred_at,actor_id)
SELECT 'login:'||id||':'||(last_login_at AT TIME ZONE 'Asia/Shanghai')::date,'login',last_login_at,id FROM users WHERE last_login_at IS NOT NULL ON CONFLICT DO NOTHING;
INSERT INTO system_stat_facts(event_key,kind,occurred_at,actor_id,dimension,metrics)
SELECT 'ai:'||id,'ai_usage',created_at,user_id,model_id,jsonb_build_object('units',-change_units::numeric,'bonus',CASE WHEN wallet_ledger_version=1 THEN -bonus_change_units ELSE NULL END,'prompt',prompt_tokens,'completion',completion_tokens)
FROM ai_balance_records WHERE category='ai_usage' AND change_units<0 ON CONFLICT DO NOTHING;
INSERT INTO system_stat_facts(event_key,kind,occurred_at,actor_id,dimension,revision,metrics)
SELECT 'screening:'||encode(sha256(convert_to(tenant_id||'/'||position_id||'/'||attempt_id,'UTF8')),'hex'),'screening',occurred_at,owner_user_id,COALESCE(snapshot->>'platform_id',''),revision,screening_stat_metrics(snapshot)
FROM position_screening_records ON CONFLICT DO NOTHING;
INSERT INTO system_stat_facts(event_key,kind,occurred_at,actor_id,dimension,metrics)
SELECT 'runtime:'||event_id,'runtime',created_at,user_id,COALESCE(payload->>'action',''),jsonb_build_object('is_error',CASE WHEN payload->>'is_error'='true' THEN 1 ELSE 0 END)
FROM browser_events ON CONFLICT DO NOTHING;
INSERT INTO system_stat_facts(event_key,kind,occurred_at,actor_id,dimension,revision,metrics)
SELECT 'greeting:'||encode(sha256(convert_to(id||'/'||daily_greeted_date,'UTF8')),'hex'),'greeting',daily_greeted_date::timestamp AT TIME ZONE 'Asia/Shanghai',user_id,platform_id,daily_greeted_count,jsonb_build_object('count',daily_greeted_count)
FROM positions WHERE daily_greeted_count>0 ON CONFLICT DO NOTHING;
