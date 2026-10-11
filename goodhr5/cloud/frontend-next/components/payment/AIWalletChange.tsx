/** 本文件统一展示 AI 余额流水，区分账户变动、实际退款和赠送余额收回。 */
"use client";
import { Flex, Typography } from "antd";

/** AIWalletChange 展示总余额变动，并明确其中的充值赠送或退款赠送收回。 */
export default function AIWalletChange({ record }: { record: any }) {
  const units = Number(record.change_units || 0);
  const bonusUnits = Number(record.bonus_change_units || 0);
  // 历史赠送变化可能来自迁移重放，只有新算法流水能据此还原退款本金。
  const separatedRefund = record.category === "refund" && Number(record.wallet_ledger_version) === 1;
  const legacyRefund = record.category === "refund" && !separatedRefund;
  const positive = units ? units > 0 : !String(record.change || "").trim().startsWith("-");
  return (
    <Flex vertical gap={2} style={{ whiteSpace: "nowrap" }}>
      <Typography.Text style={{ color: positive ? "#238653" : "#d46b08", fontWeight: 700 }}>
        {positive ? "+" : ""}￥{record.change || (units / 10000).toFixed(4)}
      </Typography.Text>
      {separatedRefund ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          实际退款 ￥{(Math.max(0, -(units - bonusUnits)) / 10000).toFixed(2)}
        </Typography.Text>
      ) : null}
      {legacyRefund ? (
        <Typography.Text type="secondary" style={{ fontSize: 12, whiteSpace: "normal" }}>
          历史退款，金额请以支付渠道记录为准。
        </Typography.Text>
      ) : null}
      {bonusUnits !== 0 && !legacyRefund ? (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {bonusUnits > 0 ? "含赠送" : record.category === "refund" ? "收回赠送" : "使用赠送"}
          {" ￥"}{(Math.abs(bonusUnits) / 10000).toFixed(4)}
        </Typography.Text>
      ) : null}
    </Flex>
  );
}
