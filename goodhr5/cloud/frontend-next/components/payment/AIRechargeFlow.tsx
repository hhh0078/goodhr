/** 本文件复用充值、支付前确认和微信二维码流程，供顶部余额胶囊与控制台共同使用。 */
"use client";
import { useEffect, useRef, useState } from "react";
import AIRechargeDialog from "./AIRechargeDialog";
import { WechatPayDialog, wechatPaymentFromResponse, type WechatPaymentState } from "./WechatPayDialog";
import { cloudRequest, getToken, requirePaymentLocalAgent } from "@/lib/admin-api";
import { AI_RECHARGE_POLICY, AI_RECHARGE_REFUND_POLICY, defaultAIRechargeAmount, rechargeAmountCents, MIN_AI_RECHARGE_CENTS, MAX_AI_RECHARGE_CENTS } from "@/lib/ai-recharge";

/** AIRechargeFlow 保留现有支付校验和退款说明，重复点击不会创建多笔订单。 */
export default function AIRechargeFlow({ open, wallet, agentBase, onClose, onPaid, confirm, notify, onModelSaved }: {
  open: boolean;
  wallet: { recharge_promotions?: unknown; time_package?: { active?: boolean } };
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
  const [mode, setMode] = useState<"balance" | "time">("balance");
  const [plans, setPlans] = useState<Array<{ id: string; name: string; duration_days: number; price_cents: number; balance_cents: number }>>([]);
  const [selectedPlan, setSelectedPlan] = useState("");
  const [plansError, setPlansError] = useState("");
  const [plansLoading, setPlansLoading] = useState(false);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const owner = getToken();
    setPlansLoading(true); setPlansError(""); setPlans([]);
    void cloudRequest("/api/payment/time-plans", { signal: controller.signal }).then(data => {
      if (controller.signal.aborted || owner !== getToken()) return;
      if (!Array.isArray(data?.plans)) throw new Error("套餐信息没读完整，请稍后再试。");
      setPlans(data.plans);
      setSelectedPlan(current => data.plans.some((plan: {id: string}) => plan.id === current) ? current : data.plans[0]?.id || "");
    }).catch(cause => { if (!controller.signal.aborted && owner === getToken()) setPlansError(cause instanceof Error ? cause.message : "套餐暂时没读到。"); })
      .finally(() => { if (!controller.signal.aborted) setPlansLoading(false); });
    return () => controller.abort();
  }, [open]);
  const busy = useRef(false);
  const amount = draft ?? defaultAIRechargeAmount(wallet.recharge_promotions);
  /** recharge 核对金额、连接和支付确认，再复用既有AI充值接口创建一笔订单。 */
  async function recharge() {
    if (busy.current) return;
    const ownerToken = getToken();
    const cents = rechargeAmountCents(amount);
    if (mode === "balance" && (cents === null || cents < MIN_AI_RECHARGE_CENTS || cents > MAX_AI_RECHARGE_CENTS)) return notify("充值金额需在 1–1000 元之间，最多保留两位小数。", "warning");
    const plan = plans.find(item => item.id === selectedPlan);
    if (mode === "time" && !plan) return notify("请先选择一个可购买的套餐。", "warning");
    busy.current = true;
    try {
      await requirePaymentLocalAgent(agentBase);
      const accepted = await confirm("支付前请确认", mode === "time" ? `${plan?.name}，实付￥${((plan?.price_cents || 0) / 100).toFixed(2)}，有效${plan?.duration_days}天。套餐额度无限，AI调用照常记账；可用额度不足时需联系作者补充后继续使用。续费延长有效期。套餐到期额度清零，按量充值余额保留。套餐退款和发票请联系作者处理。` : `AI 余额仅用于 GoodHR 内置 AI 调用。${AI_RECHARGE_POLICY}${AI_RECHARGE_REFUND_POLICY}需要开发票请联系作者处理，发票开出后该笔订单不支持退费。`);
      if (!accepted) return;
      setLoading(true);
      await requirePaymentLocalAgent(agentBase);
      if (!ownerToken || ownerToken !== getToken()) throw new Error("账号已变化，请重新打开充值弹框。");
      const data = await cloudRequest(mode === "time" ? "/api/payment/time-plan" : "/api/payment/ai-balance", { method: "POST", body: mode === "time" ? { plan_id: selectedPlan } : { amount_yuan: amount } });
      setPayment(wechatPaymentFromResponse(data, "AI 余额充值"));
      onClose();
      notify("微信支付二维码准备好了，扫一下就行。", "success");
    } catch (cause) { notify(cause instanceof Error ? cause.message : "充值订单没创建成功，我们再试一次。", "error"); }
    finally { busy.current = false; setLoading(false); }
  }
  return <>
    <AIRechargeDialog mode={mode} onModeChange={setMode} timePlans={plans} selectedPlan={selectedPlan} onPlanChange={setSelectedPlan} plansError={plansError} plansLoading={plansLoading} activePackage={Boolean(wallet.time_package?.active)} open={open} amount={amount} promotions={wallet.recharge_promotions} loading={loading} onAmountChange={setDraft} onClose={onClose} onConfirm={() => void recharge()} onModelSaved={onModelSaved} />
    <WechatPayDialog payment={payment} onClose={() => setPayment(null)} onPaid={async () => { setPayment(null); notify("支付成功，额度和有效期已更新。", "success"); await onPaid(); }} />
  </>;
}
