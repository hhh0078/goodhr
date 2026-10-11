/** 本文件提供控制台和会员页共用的 AI 余额充值及活动选择弹框。 */
"use client";
import { Form, InputNumber, Typography } from "antd";
import { useId } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";
import {
  AI_RECHARGE_POLICY,
  AI_RECHARGE_REFUND_POLICY,
  MIN_AI_RECHARGE_CENTS,
  MAX_AI_RECHARGE_CENTS,
  normalizeAIRechargePromotions,
  rechargeAmountCents,
  rechargeBonusCents,
  rechargeYuanLabel,
} from "@/lib/ai-recharge";

/** AIRechargeDialog 保留自定义充值金额，并展示最高达标活动及退款规则。 */
export default function AIRechargeDialog({
  open, amount, promotions, loading, onAmountChange, onClose, onConfirm,
}: {
  open: boolean;
  amount: string;
  promotions: unknown;
  loading: boolean;
  onAmountChange: (amount: string) => void;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const amountID = useId();
  const activities = normalizeAIRechargePromotions(promotions);
  const cents = rechargeAmountCents(amount);
  const bonus = rechargeBonusCents(amount, activities);
  const selected = activities.some((item) => item.amount_cents === cents)
    ? String(cents) : "custom";
  return (
    <AdminDialog
      open={open}
      title="充值 AI 余额"
      maxWidth="xs"
      cancelText="先不充"
      confirmText="去支付"
      loadingText="下单中"
      loading={loading}
      confirmDisabled={cents === null || cents < MIN_AI_RECHARGE_CENTS || cents > MAX_AI_RECHARGE_CENTS}
      onClose={onClose}
      onConfirm={onConfirm}
    >
      <Form.Item layout="vertical" htmlFor={amountID} label="充值金额（元）" style={{ marginBottom: 8 }}>
        <InputNumber
          id={amountID}
          value={amount ? Number(amount) : null}
          onChange={(value) => onAmountChange(value === null ? "" : String(value))}
          min={MIN_AI_RECHARGE_CENTS / 100}
          max={MAX_AI_RECHARGE_CENTS / 100}
          precision={2}
          step={1}
          prefix="￥"
          disabled={loading}
          style={{ width: "100%" }}
        />
      </Form.Item>
      {activities.length ? (
        <div style={{ marginBottom: 12 }}>
          <ChoiceCards
            label="充值活动"
            compact
            value={selected}
            readOnly={loading}
            options={[
              ...activities.map((item) => ({
                value: String(item.amount_cents),
                label: `充值 ${rechargeYuanLabel(item.amount_cents)} 元`,
                description: `赠送 ${rechargeYuanLabel(item.bonus_cents)} 元`,
              })),
              { value: "custom", label: "自定义金额", description: "自由填写" },
            ]}
            onChange={(value) => {
              if (value !== "custom") onAmountChange((Number(value) / 100).toFixed(2));
              else if (selected !== "custom") onAmountChange("");
            }}
          />
        </div>
      ) : null}
      {bonus > 0 && cents !== null ? (
        <Typography.Paragraph style={{ marginBottom: 8 }} data-testid="ai-recharge-estimate">
          实付 ￥{(cents / 100).toFixed(2)}，赠送 ￥{(bonus / 100).toFixed(2)}，
          预计到账 ￥{((cents + bonus) / 100).toFixed(2)}。
          <Typography.Text type="secondary">按最高达标档赠送，不叠加；以支付订单为准。</Typography.Text>
        </Typography.Paragraph>
      ) : null}
      <Typography.Paragraph type="secondary" style={{ marginBottom: 4 }}>
        {AI_RECHARGE_POLICY}
      </Typography.Paragraph>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        {AI_RECHARGE_REFUND_POLICY}
      </Typography.Paragraph>
    </AdminDialog>
  );
}
