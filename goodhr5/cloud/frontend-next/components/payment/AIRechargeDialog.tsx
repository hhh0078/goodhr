/** 本文件提供控制台和会员页共用的 AI 余额充值及活动选择弹框。 */
"use client";
import { Alert, Form, InputNumber, Segmented, Typography } from "antd";
import { useId, useState } from "react";
import RechargeAIModel from "./RechargeAIModel";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";
import { timePlanPricing } from "@/lib/ai-time-plan-pricing";
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
  open, amount, promotions, loading, onAmountChange, onClose, onConfirm, onModelSaved,
  mode = "balance", onModeChange, timePlans = [], selectedPlan = "", onPlanChange, plansError = "", plansLoading = false, activePackage = false,
}: {
  mode?: "balance" | "time";
  onModeChange?: (mode: "balance" | "time") => void;
  timePlans?: Array<{ id: string; name: string; duration_days: number; price_cents: number; balance_cents: number }>;
  selectedPlan?: string;
  onPlanChange?: (id: string) => void;
  plansError?: string;
  plansLoading?: boolean;
  activePackage?: boolean;
  open: boolean;
  amount: string;
  promotions: unknown;
  loading: boolean;
  onAmountChange: (amount: string) => void;
  onClose: () => void;
  onConfirm: () => void;
  onModelSaved?: (config: Record<string, unknown>) => void;
}) {
  const amountID = useId();
  const [modelSaving, setModelSaving] = useState(false);
  const activities = normalizeAIRechargePromotions(promotions);
  const cents = rechargeAmountCents(amount);
  const bonus = rechargeBonusCents(amount, activities);
  const selected = activities.some((item) => item.amount_cents === cents)
    ? String(cents) : "custom";
  return (
    <AdminDialog
      open={open}
      title="购买套餐 / 充值"
      maxWidth="xs"
      cancelText="先等等"
      confirmText="去支付"
      loadingText="下单中"
      loading={loading || modelSaving}
      confirmDisabled={modelSaving || (mode === "time" ? plansLoading || !timePlans.some(plan => plan.id === selectedPlan) : cents === null || cents < MIN_AI_RECHARGE_CENTS || cents > MAX_AI_RECHARGE_CENTS)}
      onClose={onClose}
      onConfirm={onConfirm}
    >
      {onModeChange ? <Segmented block aria-label="购买方式" style={{ marginBottom: 12 }} value={mode} disabled={loading} options={[{ label: "按量充值", value: "balance" }, { label: "时间套餐", value: "time" }]} onChange={value => onModeChange(value as "balance" | "time")} /> : null}
      {mode === "time" ? <>
        {plansError ? <Alert type="error" showIcon title={plansError} style={{ marginBottom: 12 }} /> : null}
        {timePlans.length ? <ChoiceCards compact className="goodhr-time-plans" label="选择套餐" columns={1} value={selectedPlan} readOnly={loading} options={timePlans.map(plan => {
          const pricing = timePlanPricing(plan, timePlans);
          return { value: plan.id, label: `${plan.name}${pricing.bestValue ? "（最划算）" : ""} · ￥${(plan.price_cents / 100).toFixed(2)} / ${plan.duration_days}天`, description: pricing.description };
        })} onChange={value => onPlanChange?.(String(value))} /> : <Typography.Paragraph type="secondary">{plansLoading ? "正在读取套餐…" : "暂时没有可购买的套餐。"}</Typography.Paragraph>}
        <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 8 }}>有效期内使用，AI 调用照常记账；续费延长有效期。日均价和优惠按各套餐实际天数计算。</Typography.Paragraph>
        <Typography.Paragraph type="secondary">可用额度不足时，需联系作者补充后继续使用。套餐到期后剩余额度清零，按量充值余额保留；套餐退款请联系作者处理。</Typography.Paragraph>
      </> : <>
      {activePackage ? <Alert type="info" showIcon title="套餐有效期内优先使用套餐额度；这里充值的按量余额会保留，套餐到期后使用。套餐额度不足请联系作者补充。" style={{ marginBottom: 12 }} /> : null}
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
      </>}
      {open ? <RechargeAIModel onBusyChange={setModelSaving} onSaved={onModelSaved} /> : null}
      {mode === "balance" ? <>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 4, marginTop: 12 }}>
        {AI_RECHARGE_POLICY}
      </Typography.Paragraph>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
        {AI_RECHARGE_REFUND_POLICY}
      </Typography.Paragraph>
      </> : null}
    </AdminDialog>
  );
}
