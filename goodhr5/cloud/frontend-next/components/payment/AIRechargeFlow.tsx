/** 本文件复用充值、支付前确认和微信二维码流程，供顶部余额胶囊与控制台共同使用。 */
"use client";
import { useRef, useState } from "react";
import AIRechargeDialog from "./AIRechargeDialog";
import { WechatPayDialog, wechatPaymentFromResponse, type WechatPaymentState } from "./WechatPayDialog";
import { cloudRequest, getToken, requirePaymentLocalAgent } from "@/lib/admin-api";
import { AI_RECHARGE_POLICY, AI_RECHARGE_REFUND_POLICY, defaultAIRechargeAmount, rechargeAmountCents, MIN_AI_RECHARGE_CENTS, MAX_AI_RECHARGE_CENTS } from "@/lib/ai-recharge";

/** AIRechargeFlow 保留现有支付校验和退款说明，重复点击不会创建多笔订单。 */
export default function AIRechargeFlow({ open, wallet, agentBase, onClose, onPaid, confirm, notify, onModelSaved }: {
  open: boolean;
  wallet: { recharge_promotions?: unknown };
  agentBase: string;
  onClose: () => void;
  onPaid: () => Promise<void>;
  confirm: (title: string, message: string) => Promise<boolean>;
  notify: (message: string, severity?: "success" | "error" | "warning" | "info") => void;
  onModelSaved?: (config: Record<string, unknown>) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [payment, setPayment] = useState<WechatPaymentState | null>(null);
  const busy = useRef(false);
  const amount = draft ?? defaultAIRechargeAmount(wallet.recharge_promotions);
  /** recharge 核对金额、连接和支付确认，再复用既有AI充值接口创建一笔订单。 */
  async function recharge() {
    if (busy.current) return;
    const ownerToken = getToken();
    const cents = rechargeAmountCents(amount);
    if (cents === null || cents < MIN_AI_RECHARGE_CENTS || cents > MAX_AI_RECHARGE_CENTS) return notify("充值金额需在 1–1000 元之间，最多保留两位小数。", "warning");
    busy.current = true;
    try {
      await requirePaymentLocalAgent(agentBase);
      const accepted = await confirm("支付前请确认", `AI 余额仅用于 GoodHR 内置 AI 调用。${AI_RECHARGE_POLICY}${AI_RECHARGE_REFUND_POLICY}需要开发票请联系作者处理，发票开出后该笔订单不支持退费。`);
      if (!accepted) return;
      setLoading(true);
      await requirePaymentLocalAgent(agentBase);
      if (!ownerToken || ownerToken !== getToken()) throw new Error("账号已变化，请重新打开充值弹框。");
      const data = await cloudRequest("/api/payment/ai-balance", { method: "POST", body: { amount_yuan: amount } });
      setPayment(wechatPaymentFromResponse(data, "AI 余额充值"));
      onClose();
      notify("微信支付二维码准备好了，扫一下就行。", "success");
    } catch (cause) { notify(cause instanceof Error ? cause.message : "充值订单没创建成功，我们再试一次。", "error"); }
    finally { busy.current = false; setLoading(false); }
  }
  return <>
    <AIRechargeDialog open={open} amount={amount} promotions={wallet.recharge_promotions} loading={loading} onAmountChange={setDraft} onClose={onClose} onConfirm={() => void recharge()} onModelSaved={onModelSaved} />
    <WechatPayDialog payment={payment} onClose={() => setPayment(null)} onPaid={async () => { setPayment(null); notify("支付成功，AI 余额已经到账。", "success"); await onPaid(); }} />
  </>;
}
