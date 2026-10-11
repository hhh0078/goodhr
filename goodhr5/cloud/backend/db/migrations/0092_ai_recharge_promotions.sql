-- 本迁移新增充值活动赠送快照与分笔 AI 钱包，按历史流水重放本金优先的消费顺序，保留每个用户原总余额。
-- 无法核对的历史金额仅作为不可退款资金保留，并暂停该账号自动退款，避免把赠送或缺失记录变成现金。
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS bonus_amount_cents INTEGER NOT NULL DEFAULT 0 CHECK (bonus_amount_cents >= 0);
COMMENT ON COLUMN payment_orders.bonus_amount_cents IS '创建 AI 充值订单时冻结的活动赠送金额，单位为分，不计入微信实付金额';

ALTER TABLE payment_refunds
    ADD COLUMN IF NOT EXISTS revoked_bonus_units BIGINT NOT NULL DEFAULT 0 CHECK (revoked_bonus_units >= 0),
    ADD COLUMN IF NOT EXISTS rights_reserved BOOLEAN NOT NULL DEFAULT false;
COMMENT ON COLUMN payment_refunds.revoked_bonus_units IS '申请退款时收回该笔充值尚未使用的赠送金额，单位为0.0001元';
COMMENT ON COLUMN payment_refunds.rights_reserved IS '退款本金与赠送是否已经预扣；仅确认失败且此值为真时允许原子返还';

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS ai_bonus_balance_units BIGINT NOT NULL DEFAULT 0 CHECK (ai_bonus_balance_units >= 0),
    ADD COLUMN IF NOT EXISTS ai_wallet_refund_block_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ai_wallet_ledger_version SMALLINT NOT NULL DEFAULT 0;
COMMENT ON COLUMN users.ai_bonus_balance_units IS '当前不可退款 AI 赠送余额缓存，单位为0.0001元，不能超过非负总余额';
COMMENT ON COLUMN users.ai_wallet_refund_block_reason IS '历史钱包账本无法核对时暂停自动退款的原因，空值表示可正常按本金退款';
COMMENT ON COLUMN users.ai_wallet_ledger_version IS '钱包历史分笔重放版本，1表示已初始化，防止重复运行迁移再次拆分资金';

ALTER TABLE ai_balance_records
    ADD COLUMN IF NOT EXISTS bonus_change_units BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_balance_after_units BIGINT NOT NULL DEFAULT 0 CHECK (bonus_balance_after_units >= 0),
    ADD COLUMN IF NOT EXISTS wallet_ledger_version SMALLINT NOT NULL DEFAULT 0;
COMMENT ON COLUMN ai_balance_records.bonus_change_units IS '本次余额变化中实际赠送余额的增减，单位为0.0001元，欠费抵扣后可能少于订单赠送';
COMMENT ON COLUMN ai_balance_records.bonus_balance_after_units IS '本次变化后的不可退款赠送余额，单位为0.0001元';
COMMENT ON COLUMN ai_balance_records.wallet_ledger_version IS '流水资金算法版本，0为历史流水，1为本金赠送分笔算法流水';

CREATE TABLE IF NOT EXISTS ai_wallet_lots (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_no TEXT NOT NULL DEFAULT '',
    source_category TEXT NOT NULL DEFAULT '',
    principal_units BIGINT NOT NULL DEFAULT 0 CHECK (principal_units >= 0),
    bonus_units BIGINT NOT NULL DEFAULT 0 CHECK (bonus_units >= 0),
    remaining_principal_units BIGINT NOT NULL DEFAULT 0 CHECK (remaining_principal_units >= 0 AND remaining_principal_units <= principal_units),
    remaining_bonus_units BIGINT NOT NULL DEFAULT 0 CHECK (remaining_bonus_units >= 0 AND remaining_bonus_units <= bonus_units),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
COMMENT ON TABLE ai_wallet_lots IS 'AI 钱包分笔资金账本，全部本金按ID先用，再使用全部赠送；退款只退原充值剩余本金';
COMMENT ON COLUMN ai_wallet_lots.id IS '资金到账先后顺序主键，用于本金及赠送先进先出消费';
COMMENT ON COLUMN ai_wallet_lots.user_id IS '资金所属用户主键';
COMMENT ON COLUMN ai_wallet_lots.order_no IS '充值支付订单号，非充值赠送留空';
COMMENT ON COLUMN ai_wallet_lots.source_category IS '入账来源，例如recharge、signup_bonus、admin_adjust、gift或legacy_unreconciled';
COMMENT ON COLUMN ai_wallet_lots.principal_units IS '该笔原始现金本金，单位为0.0001元，抵历史欠费也计入原始金额';
COMMENT ON COLUMN ai_wallet_lots.bonus_units IS '该笔原始赠送金额，单位为0.0001元';
COMMENT ON COLUMN ai_wallet_lots.remaining_principal_units IS '该笔尚未消费且未被退款预扣的现金本金，单位为0.0001元';
COMMENT ON COLUMN ai_wallet_lots.remaining_bonus_units IS '该笔尚未消费且未被退款收回的赠送金额，单位为0.0001元';
COMMENT ON COLUMN ai_wallet_lots.created_at IS '原入账时间，历史重放保留原流水时间';
CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_wallet_lots_order ON ai_wallet_lots(order_no) WHERE order_no <> '';
CREATE INDEX IF NOT EXISTS idx_ai_wallet_lots_user_fifo ON ai_wallet_lots(user_id, id);

-- 重放过程中锁住原始金额表，避免新旧代码写入与重放穿插。
LOCK TABLE users, ai_balance_records, payment_orders, payment_refunds IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    v_user RECORD;
    v_record RECORD;
    v_lot RECORD;
    v_order RECORD;
    v_refund RECORD;
    v_balance NUMERIC;
    v_before_bonus BIGINT;
    v_bonus BIGINT;
    v_principal BIGINT;
    v_cash_remaining BIGINT;
    v_bonus_remaining BIGINT;
    v_debt NUMERIC;
    v_cost NUMERIC;
    v_used BIGINT;
    v_order_no TEXT;
    v_source TEXT;
    v_reason TEXT;
    v_target_lot BIGINT;
    v_target_remaining BIGINT;
BEGIN
    FOR v_user IN SELECT id, email, ai_balance_units FROM users WHERE ai_wallet_ledger_version = 0 ORDER BY id FOR UPDATE LOOP
        v_balance := 0;
        v_reason := '';
        -- 半初始化分笔没有可靠版本，不保留其可退本金；归零剩余分笔并保留原总额为异常赠送。
        IF EXISTS(SELECT 1 FROM ai_wallet_lots WHERE user_id=v_user.id) THEN
            UPDATE ai_wallet_lots SET remaining_principal_units=0,remaining_bonus_units=0 WHERE user_id=v_user.id;
            IF v_user.ai_balance_units>0 THEN
                INSERT INTO ai_wallet_lots(user_id,source_category,bonus_units,remaining_bonus_units)
                VALUES(v_user.id,'legacy_unreconciled',v_user.ai_balance_units,v_user.ai_balance_units);
            END IF;
            UPDATE users SET ai_bonus_balance_units=GREATEST(ai_balance_units,0), ai_wallet_refund_block_reason='历史分笔初始化版本不完整', ai_wallet_ledger_version=1 WHERE id=v_user.id;
            CONTINUE;
        END IF;
        FOR v_record IN SELECT * FROM ai_balance_records WHERE user_id=v_user.id ORDER BY created_at, id LOOP
            SELECT COALESCE(SUM(remaining_bonus_units),0) INTO v_before_bonus FROM ai_wallet_lots WHERE user_id=v_user.id;
            IF v_record.change_units > 0 THEN
                v_principal := 0;
                v_bonus := v_record.change_units;
                v_order_no := '';
                v_source := v_record.category;
                IF v_record.category='recharge' THEN
                    SELECT order_no,user_id,status,order_type,amount_cents INTO v_order FROM payment_orders WHERE order_no=v_record.related_order_no;
                    IF FOUND AND v_order.user_id=v_user.id AND v_order.status='paid' AND v_order.order_type='ai_balance'
                       AND v_order.amount_cents::BIGINT*100=v_record.change_units
                       AND NOT EXISTS(SELECT 1 FROM ai_wallet_lots WHERE order_no=v_order.order_no) THEN
                        v_principal := v_record.change_units;
                        v_bonus := 0;
                        v_order_no := v_record.related_order_no;
                    ELSE
                        v_source := 'legacy_unreconciled';
                        v_reason := '历史充值流水与成功支付订单无法核对';
                    END IF;
                ELSIF v_record.category NOT IN ('signup_bonus','admin_adjust','gift') THEN
                    v_source := 'legacy_unreconciled';
                    v_reason := '历史正向余额来源不完整';
                END IF;
                v_debt := GREATEST(0,-v_balance);
                v_cash_remaining := GREATEST(0,v_principal::NUMERIC-v_debt)::BIGINT;
                v_debt := GREATEST(0,v_debt-v_principal);
                v_bonus_remaining := GREATEST(0,v_bonus::NUMERIC-v_debt)::BIGINT;
                INSERT INTO ai_wallet_lots(user_id,order_no,source_category,principal_units,bonus_units,remaining_principal_units,remaining_bonus_units,created_at)
                VALUES(v_user.id,v_order_no,v_source,v_principal,v_bonus,v_cash_remaining,v_bonus_remaining,v_record.created_at);
            ELSIF v_record.change_units < 0 THEN
                v_cost := -v_record.change_units::NUMERIC;
                v_target_lot := NULL;
                IF v_record.category='refund' THEN
                    SELECT refund_no,order_no,amount_cents,status INTO v_refund FROM payment_refunds WHERE refund_no=v_record.related_order_no AND user_id=v_user.id AND refund_type='ai_balance';
                    IF FOUND AND v_refund.amount_cents::BIGINT*100=v_cost THEN
                        SELECT id,remaining_principal_units INTO v_target_lot,v_target_remaining FROM ai_wallet_lots WHERE user_id=v_user.id AND order_no=v_refund.order_no AND source_category='recharge';
                        IF FOUND AND v_target_remaining>=v_cost THEN
                            UPDATE ai_wallet_lots SET remaining_principal_units=remaining_principal_units-v_cost::BIGINT WHERE id=v_target_lot;
                            UPDATE payment_refunds SET rights_reserved=(status NOT IN('failed','closed')) WHERE refund_no=v_refund.refund_no;
                            IF v_refund.status IN('failed','closed') THEN v_reason := '历史失败退款存在扣款流水，需人工核对'; END IF;
                            v_cost := 0;
                        ELSE
                            v_reason := '历史退款扣款与原充值剩余本金无法核对';
                        END IF;
                    ELSE
                        v_reason := '历史退款记录缺失或金额不一致';
                    END IF;
                ELSIF v_record.category NOT IN('ai_usage','admin_adjust') THEN
                    v_reason := '历史扣款来源不完整';
                END IF;
                -- 历史普通消费与异常扣款先扣全部本金，再扣全部赠送，超出余额保留为欠费。
                FOR v_lot IN SELECT id,remaining_principal_units FROM ai_wallet_lots WHERE user_id=v_user.id AND remaining_principal_units>0 ORDER BY id LOOP
                    EXIT WHEN v_cost<=0;
                    v_used := LEAST(v_lot.remaining_principal_units::NUMERIC,v_cost)::BIGINT;
                    UPDATE ai_wallet_lots SET remaining_principal_units=remaining_principal_units-v_used WHERE id=v_lot.id;
                    v_cost := v_cost-v_used;
                END LOOP;
                FOR v_lot IN SELECT id,remaining_bonus_units FROM ai_wallet_lots WHERE user_id=v_user.id AND remaining_bonus_units>0 ORDER BY id LOOP
                    EXIT WHEN v_cost<=0;
                    v_used := LEAST(v_lot.remaining_bonus_units::NUMERIC,v_cost)::BIGINT;
                    UPDATE ai_wallet_lots SET remaining_bonus_units=remaining_bonus_units-v_used WHERE id=v_lot.id;
                    v_cost := v_cost-v_used;
                END LOOP;
            END IF;
            v_balance := v_balance+v_record.change_units;
            IF v_balance<>v_record.balance_after_units THEN v_reason := '历史余额流水前后金额不连续'; END IF;
            SELECT COALESCE(SUM(remaining_bonus_units),0) INTO v_bonus FROM ai_wallet_lots WHERE user_id=v_user.id;
            UPDATE ai_balance_records SET bonus_change_units=v_bonus-v_before_bonus,bonus_balance_after_units=v_bonus WHERE id=v_record.id;
        END LOOP;
        -- 支付已成功但没有到账流水，或旧退款没有可核对的扣款，均不能凭订单记录退款。
        IF EXISTS(SELECT 1 FROM payment_orders o WHERE o.user_id=v_user.id AND o.order_type='ai_balance' AND o.status='paid' AND NOT EXISTS(SELECT 1 FROM ai_wallet_lots l WHERE l.order_no=o.order_no AND l.source_category='recharge')) THEN
            v_reason := '历史成功充值缺少可核对的到账流水';
        END IF;
        IF EXISTS(SELECT 1 FROM payment_refunds f WHERE f.user_id=v_user.id AND f.refund_type='ai_balance' AND f.status IN('pending','unknown','processing','action_required','success') AND NOT f.rights_reserved) THEN
            v_reason := '历史退款是否扣款尚未核对';
        END IF;
        IF v_balance<>v_user.ai_balance_units THEN
            v_reason := '历史总余额与流水合计不一致';
            IF v_balance<v_user.ai_balance_units THEN
                v_debt := GREATEST(0,-v_balance);
                v_bonus_remaining := GREATEST(0,v_user.ai_balance_units::NUMERIC-v_balance-v_debt)::BIGINT;
                IF v_bonus_remaining>0 THEN
                    INSERT INTO ai_wallet_lots(user_id,source_category,bonus_units,remaining_bonus_units)
                    VALUES(v_user.id,'legacy_unreconciled',v_bonus_remaining,v_bonus_remaining);
                END IF;
            ELSE
                v_cost := v_balance-v_user.ai_balance_units;
                FOR v_lot IN SELECT id,remaining_principal_units FROM ai_wallet_lots WHERE user_id=v_user.id AND remaining_principal_units>0 ORDER BY id LOOP
                    EXIT WHEN v_cost<=0;
                    v_used := LEAST(v_lot.remaining_principal_units::NUMERIC,v_cost)::BIGINT;
                    UPDATE ai_wallet_lots SET remaining_principal_units=remaining_principal_units-v_used WHERE id=v_lot.id;
                    v_cost := v_cost-v_used;
                END LOOP;
                FOR v_lot IN SELECT id,remaining_bonus_units FROM ai_wallet_lots WHERE user_id=v_user.id AND remaining_bonus_units>0 ORDER BY id LOOP
                    EXIT WHEN v_cost<=0;
                    v_used := LEAST(v_lot.remaining_bonus_units::NUMERIC,v_cost)::BIGINT;
                    UPDATE ai_wallet_lots SET remaining_bonus_units=remaining_bonus_units-v_used WHERE id=v_lot.id;
                    v_cost := v_cost-v_used;
                END LOOP;
            END IF;
        END IF;
        SELECT COALESCE(SUM(remaining_bonus_units),0) INTO v_bonus FROM ai_wallet_lots WHERE user_id=v_user.id;
        UPDATE users SET ai_bonus_balance_units=v_bonus,ai_wallet_refund_block_reason=v_reason,ai_wallet_ledger_version=1 WHERE id=v_user.id;
    END LOOP;
END $$;

-- 新账号和新流水直接使用分笔算法；历史版本标记保留，重跑迁移不会再次消费历史资金。
ALTER TABLE users ALTER COLUMN ai_wallet_ledger_version SET DEFAULT 1;
ALTER TABLE ai_balance_records ALTER COLUMN wallet_ledger_version SET DEFAULT 1;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ai_balance_records_refund_ledger
    ON ai_balance_records(category,related_order_no)
    WHERE wallet_ledger_version=1 AND category IN('refund','refund_release') AND related_order_no<>'';
DO $$ BEGIN
    IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='users_ai_bonus_balance_available_check' AND conrelid='users'::regclass) THEN
        ALTER TABLE users ADD CONSTRAINT users_ai_bonus_balance_available_check CHECK(ai_bonus_balance_units<=GREATEST(ai_balance_units,0));
    END IF;
END $$;
