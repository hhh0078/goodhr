/** 本文件为管理员提供只修改 AI 充值活动字段的金额档位增删表单。 */
"use client";
import { Button, Flex, Form, InputNumber, Typography } from "antd";
import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import {
  parseAIRechargeConfig,
  MIN_AI_RECHARGE_CENTS,
  MAX_AI_RECHARGE_CENTS,
  MAX_AI_RECHARGE_PROMOTIONS,
  rechargeAmountCents,
  updateAIRechargePromotions,
} from "@/lib/ai-recharge";

/** AIRechargePromotionForm 用元金额编辑充值活动，并保留全部其他系统 JSON 字段。 */
export default function AIRechargePromotionForm({ value, onChange, disabled = false }: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const config = parseAIRechargeConfig(value);
  const source = config?.ai_recharge_promotions;
  const rows: Record<string, unknown>[] = Array.isArray(source)
    ? source.map((item) => item && typeof item === "object" ? item : {}) : [];
  const editable = Boolean(config) && (source === undefined || Array.isArray(source));
  /** changeRows 仅更新活动数组，无法解析的配置保持原文以便修复。 */
  function changeRows(next: unknown[]) {
    const updated = updateAIRechargePromotions(value, next);
    if (updated !== null) onChange(updated);
  }
  /** changeAmount 将元金额写成整数分，空输入保留为待补全状态。 */
  function changeAmount(index: number, field: string, amount: number | null) {
    changeRows(rows.map((row, rowIndex) => rowIndex === index
      ? { ...row, [field]: rechargeAmountCents(amount) } : row));
  }
  return (
    <Flex vertical gap={12}>
      <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
        可以配置多个活动，例如充值 50 元赠送 5 元、100 元赠送 20 元、200 元赠送 50 元。
        达到门槛时按最高档赠送，不叠加；没有活动时按实付金额到账。
      </Typography.Paragraph>
      {rows.map((row, index) => (
        <Flex key={index} gap={8} wrap align="end" style={{ borderBottom: "1px solid var(--goodhr-border)", paddingBottom: 12 }}>
          <Form.Item layout="vertical" label={`充值门槛 ${index + 1}（元）`} htmlFor={`ai-promotion-amount-${index}`} style={{ flex: "1 1 130px", marginBottom: 0 }}>
            <InputNumber id={`ai-promotion-amount-${index}`} value={typeof row.amount_cents === "number" ? row.amount_cents / 100 : null}
              onChange={(amount) => changeAmount(index, "amount_cents", amount)} min={MIN_AI_RECHARGE_CENTS / 100} max={MAX_AI_RECHARGE_CENTS / 100} precision={2} prefix="￥" disabled={disabled || !editable} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item layout="vertical" label={`赠送金额 ${index + 1}（元）`} htmlFor={`ai-promotion-bonus-${index}`} style={{ flex: "1 1 130px", marginBottom: 0 }}>
            <InputNumber id={`ai-promotion-bonus-${index}`} value={typeof row.bonus_cents === "number" ? row.bonus_cents / 100 : null}
              onChange={(amount) => changeAmount(index, "bonus_cents", amount)} min={0.01} max={MAX_AI_RECHARGE_CENTS / 100} precision={2} prefix="￥" disabled={disabled || !editable} style={{ width: "100%" }} />
          </Form.Item>
          <Button danger aria-label={`删除活动 ${index + 1}`} icon={<DeleteOutlined aria-hidden="true" />} disabled={disabled || !editable}
            onClick={() => changeRows(rows.filter((_, rowIndex) => rowIndex !== index))}>删除</Button>
        </Flex>
      ))}
      {!rows.length ? <Typography.Text type="secondary">还没有充值活动，添加并保存后就能使用。</Typography.Text> : null}
      <Flex>
        <Button icon={<PlusOutlined aria-hidden="true" />} disabled={disabled || !editable || rows.length >= MAX_AI_RECHARGE_PROMOTIONS}
          onClick={() => changeRows([...rows, { amount_cents: null, bonus_cents: null }])}>新增活动</Button>
      </Flex>
    </Flex>
  );
}
