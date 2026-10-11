/** 本文件负责新版后台会员、AI 余额、激活码、支付记录和 AI 使用记录展示。 */
"use client";
import { AdminOverlayModal as Modal } from "@/components/admin/AdminOverlay";
import {
  Button,
  Card,
  Flex,
  Form,
  Input,
  InputNumber,
  Select,
  Table,
  Tabs,
  Tag,
  Typography,
} from "antd";
import {
  CheckOutlined,
  CrownOutlined,
  PayCircleOutlined,
} from "@ant-design/icons";
import type { CSSProperties } from "react";
import styles from "../heavy-ui.module.css";
import { useEffect, useMemo, useState } from "react";
import {
  cloudRequest,
  formatDate,
  requirePaymentLocalAgent,
} from "@/lib/admin-api";
import {
  EmptyState,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AdminDialog from "@/components/admin/AdminDialog";
import AIRechargeDialog from "@/components/payment/AIRechargeDialog";
import AIWalletChange from "@/components/payment/AIWalletChange";
import { AI_RECHARGE_POLICY, AI_RECHARGE_REFUND_POLICY, MAX_AI_RECHARGE_CENTS, MIN_AI_RECHARGE_CENTS, defaultAIRechargeAmount, rechargeAmountCents } from "@/lib/ai-recharge";
import {
  WechatPayDialog,
  wechatPaymentFromResponse,
  type WechatPaymentState,
} from "@/components/payment/WechatPayDialog";
import {
  estimateSubscriptionQuote,
  normalizeSubscriptionPlans,
  type SubscriptionPlan,
  type SubscriptionUpgradeQuote,
} from "@/lib/subscription";
const aiRecordPageSize = 10;
type PendingPayment =
  | {
      type: "membership";
      planID: string;
    }
  | {
      type: "ai_balance";
    };
type RefundType = "membership" | "ai_balance";
type RefundQuote = Record<
  RefundType,
  {
    available: boolean;
    max_amount_cents: number;
    max_amount: string;
    message?: string;
  }
>;
/** SubscriptionPage 展示会员状态、AI 余额和账务记录。 */
export default function SubscriptionPage() {
  const { agentBase, notify, subscription, refreshSession } = useAdmin();
  const [plans, setPlans] = useState<SubscriptionPlan[]>([]);
  const [orders, setOrders] = useState<any[]>([]);
  const [wallet, setWallet] = useState<any>({});
  const [aiConfig, setAIConfig] = useState<any>({});
  const [currentAIModel, setCurrentAIModel] = useState("");
  const [aiRecords, setAIRecords] = useState<any[]>([]);
  const [aiTotal, setAITotal] = useState(0);
  const [aiPage, setAIPage] = useState(1);
  const [recordTab, setRecordTab] = useState<"payments" | "ai">("payments");
  const [code, setCode] = useState("");
  const [activationDialogOpen, setActivationDialogOpen] = useState(false);
  const [redeeming, setRedeeming] = useState(false);
  const [rechargeAmountDraft, setRechargeAmount] = useState<string | null>(null);
  const rechargeAmount = rechargeAmountDraft ?? defaultAIRechargeAmount(wallet.recharge_promotions);
  const [selectedModel, setSelectedModel] = useState("");
  const [loading, setLoading] = useState(false);
  const [aiLoading, setAILoading] = useState(false);
  const [recharging, setRecharging] = useState(false);
  const [savingModel, setSavingModel] = useState(false);
  const [payingPlanID, setPayingPlanID] = useState("");
  const [rechargeDialogOpen, setRechargeDialogOpen] = useState(false);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [refundDialogOpen, setRefundDialogOpen] = useState(false);
  const [refundType, setRefundType] = useState<RefundType>("membership");
  const [refundAmount, setRefundAmount] = useState("");
  const [refundQuote, setRefundQuote] = useState<RefundQuote | null>(null);
  const [refundLoading, setRefundLoading] = useState(false);
  const [refunding, setRefunding] = useState(false);
  const [pendingPayment, setPendingPayment] = useState<PendingPayment | null>(
    null,
  );
  const [wechatPayment, setWechatPayment] = useState<WechatPaymentState | null>(
    null,
  );
  const modelLabel = currentAIModel || wallet.default_model || "未配置";
  const aiPageCount = Math.max(1, Math.ceil(aiTotal / aiRecordPageSize));
  const pendingPlan = useMemo(
    () =>
      pendingPayment?.type === "membership"
        ? plans.find((plan) => plan.id === pendingPayment.planID) || null
        : null,
    [pendingPayment, plans],
  );
  const pendingQuote = pendingPlan
    ? estimateSubscriptionQuote(subscription, plans, pendingPlan)
    : null;
  /** loadSummary 读取会员套餐、支付记录和 AI 余额摘要。 */
  async function loadSummary() {
    setLoading(true);
    try {
      const [planData, orderData, walletData, aiConfigData] = await Promise.all(
        [
          cloudRequest("/api/subscription/plans", { auth: false }),
          cloudRequest("/api/payment/orders"),
          cloudRequest("/api/ai-wallet"),
          cloudRequest("/api/config/user-ai"),
        ],
      );
      setPlans(normalizeSubscriptionPlans(planData.plans));
      setOrders(Array.isArray(orderData.orders) ? orderData.orders : []);
      setWallet(walletData.wallet || walletData || {});
      const config = aiConfigData.config || {};
      setAIConfig(config);
      setCurrentAIModel(config.model || "");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "订阅信息读取失败，我再试也得先缓缓。",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  /** loadAIRecords 分页读取 AI 使用记录。 */
  async function loadAIRecords() {
    setAILoading(true);
    try {
      const params = new URLSearchParams({
        page: String(aiPage),
        page_size: String(aiRecordPageSize),
      });
      const data = await cloudRequest(
        `/api/ai-wallet/records?${params.toString()}`,
      );
      setAIRecords(Array.isArray(data.records) ? data.records : []);
      setAITotal(Number(data.total || 0));
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "AI 使用记录读取失败，我小声记下了。",
        "error",
      );
    } finally {
      setAILoading(false);
    }
  }
  useEffect(() => {
    void loadSummary();
  }, []);
  useEffect(() => {
    void loadAIRecords();
  }, [aiPage]);
  /** refreshAll 刷新订阅页全部信息。 */
  async function refreshAll() {
    await Promise.all([loadSummary(), loadAIRecords(), refreshSession()]);
  }
  /** redeem 兑换会员激活码。 */
  async function redeem() {
    const value = code.trim();
    if (!value) return;
    setRedeeming(true);
    try {
      await cloudRequest("/api/activation-codes/redeem", {
        method: "POST",
        body: { code: value },
      });
      setCode("");
      setActivationDialogOpen(false);
      notify("激活成功，会员时间已到账。", "success");
      await refreshAll();
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "激活码没通过，我也有点尴尬。",
        "error",
      );
    } finally {
      setRedeeming(false);
    }
  }
  /** pay 创建会员套餐支付订单。 */
  async function pay(planID: string) {
    if (!planID) return;
    setPayingPlanID(planID);
    try {
      await requirePaymentLocalAgent(agentBase);
      const data = await cloudRequest("/api/payment/orders", {
        method: "POST",
        body: { plan_id: planID },
      });
      if (data.payment_completed) {
        notify("Plus 剩余时间已经抵完差价，Max 全能版已到账。", "success");
      } else {
        setWechatPayment(wechatPaymentFromResponse(data, "会员订阅"));
        notify("微信支付二维码准备好了，扫一下就行。", "success");
      }
      await loadSummary();
      await refreshSession();
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "订单没创建成功，我们再来一次。",
        "error",
      );
    } finally {
      setPayingPlanID("");
    }
  }
  /** requestPlanPayment 在创建会员订单前打开费用区别和退款政策确认框。 */
  async function requestPlanPayment(planID: string) {
    if (!planID) return;
    const plan = plans.find((item) => item.id === planID);
    if (!plan) return;
    try {
      await requirePaymentLocalAgent(agentBase);
    } catch (error) {
      notify((error as Error).message, "error");
      return;
    }
    setPendingPayment({ type: "membership", planID });
  }
  /** rechargeAI 创建 AI 余额充值订单。 */
  async function rechargeAI() {
    const amountCents = rechargeAmountCents(rechargeAmount);
    if (amountCents === null || amountCents < MIN_AI_RECHARGE_CENTS || amountCents > MAX_AI_RECHARGE_CENTS) {
      notify("充值金额需在 1–1000 元之间，最多保留两位小数。", "warning");
      return;
    }
    setRecharging(true);
    try {
      await requirePaymentLocalAgent(agentBase);
      const data = await cloudRequest("/api/payment/ai-balance", {
        method: "POST",
        body: { amount_yuan: rechargeAmount || "10" },
      });
      setWechatPayment(wechatPaymentFromResponse(data, "AI 余额充值"));
      setRechargeDialogOpen(false);
      notify("微信支付二维码准备好了，扫一下就行。", "success");
      await loadSummary();
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "充值订单没创建成功，我们再试一次。",
        "error",
      );
    } finally {
      setRecharging(false);
    }
  }
  /** requestAIRecharge 校验充值金额并在创建 AI 余额订单前打开确认框。 */
  async function requestAIRecharge() {
    const amountCents = rechargeAmountCents(rechargeAmount);
    if (amountCents === null || amountCents < MIN_AI_RECHARGE_CENTS || amountCents > MAX_AI_RECHARGE_CENTS) {
      notify("充值金额需在 1–1000 元之间，最多保留两位小数。", "warning");
      return;
    }
    try {
      await requirePaymentLocalAgent(agentBase);
    } catch (error) {
      notify((error as Error).message, "error");
      return;
    }
    setRechargeDialogOpen(false);
    setPendingPayment({ type: "ai_balance" });
  }
  /** confirmPendingPayment 在用户明确理解费用区别后执行原支付流程。 */
  async function confirmPendingPayment() {
    const payment = pendingPayment;
    if (!payment) return;
    if (payment.type === "membership") {
      await pay(payment.planID);
    } else {
      await rechargeAI();
    }
    setPendingPayment(null);
  }
  /** openModelDialog 打开 AI 模型选择弹框。 */
  function openModelDialog() {
    const models = Array.isArray(wallet.models) ? wallet.models : [];
    setSelectedModel(
      currentAIModel || wallet.default_model || models[0]?.id || "",
    );
    setModelDialogOpen(true);
  }
  /** saveAIModel 保存当前 AI 模型。 */
  async function saveAIModel() {
    const model = selectedModel.trim();
    if (!model) {
      notify("先选个模型，我才知道该让谁上工。", "warning");
      return;
    }
    setSavingModel(true);
    try {
      const data = await cloudRequest("/api/config/user-ai", {
        method: "PUT",
        body: {
          base_url: aiConfig.base_url || wallet.public_base_url || "",
          model,
          api_key: aiConfig.api_key || "",
          temperature: Number(aiConfig.temperature || 0),
          prompt_template: aiConfig.prompt_template || "",
          enabled: aiConfig.enabled !== false,
        },
      });
      const config = data.config || { ...aiConfig, model };
      setAIConfig(config);
      setCurrentAIModel(config.model || model);
      setModelDialogOpen(false);
      notify("模型已切好，接下来就让它干活。", "success");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "模型没保存成功，我们再来一次。",
        "error",
      );
    } finally {
      setSavingModel(false);
    }
  }
  /** openRefundDialog 打开退款弹框并从服务器读取实时可退额度。 */
  async function openRefundDialog() {
    setRefundDialogOpen(true);
    setRefundLoading(true);
    try {
      const data = await cloudRequest("/api/payment/refund");
      const quote = data as RefundQuote;
      setRefundQuote(quote);
      const preferred: RefundType = quote.membership?.available
        ? "membership"
        : "ai_balance";
      setRefundType(preferred);
      setRefundAmount(quote[preferred]?.max_amount || "");
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "可退金额暂时没查清楚。",
        "error",
      );
    } finally {
      setRefundLoading(false);
    }
  }
  /** changeRefundType 切换退款类型并自动填入该类型最高可退金额。 */
  function changeRefundType(value: RefundType) {
    setRefundType(value);
    setRefundAmount(refundQuote?.[value]?.max_amount || "");
  }
  /** submitRefund 确认后交给服务器重新校验并自动原路退款。 */
  async function submitRefund() {
    const amount = Number(refundAmount || 0);
    const maximum =
      Number(refundQuote?.[refundType]?.max_amount_cents || 0) / 100;
    if (!Number.isFinite(amount) || amount <= 0 || amount > maximum) {
      notify(
        `退款金额需在 0.01 元到 ${maximum.toFixed(2)} 元之间。`,
        "warning",
      );
      return;
    }
    setRefunding(true);
    try {
      const data = await cloudRequest("/api/payment/refund", {
        method: "POST",
        body: { refund_type: refundType, amount_yuan: refundAmount },
      });
      setRefundDialogOpen(false);
      notify(data.message || "退款已提交，请留意微信到账通知。", "success");
      await refreshAll();
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "退款没处理成功，我们再来一次。",
        "error",
      );
    } finally {
      setRefunding(false);
    }
  }
  return (
    <>
      <PageHeader
        title="订阅会员"
        description="会员费就像您包月租了一个小汽车，AI余额 就是您小汽车油箱的余额。"
        actions={
          <>
            <Button onClick={() => setActivationDialogOpen(true)}>
              使用激活码
            </Button>
            <Button
              onClick={() => void openRefundDialog()}
              type={"text"}
              className={styles.s118}
            >
              退款
            </Button>
          </>
        }
      />

      <div className={styles.s119}>
        <InfoCard
          icon={<CrownOutlined aria-hidden="true" />}
          title="当前会员"
          value={subscription.member_name || "免费版"}
          tone={subscription.active ? "dark" : "plain"}
          compact
          className={styles.s120}
        >
          <Flex className={styles.s121}>
            <Tag
              color={subscription.active ? "var(--goodhr-primary)" : "default"}
            >
              {subscription.active
                ? `${subscription.remaining_days} 天 · 自动回复${subscription.allow_auto_reply ? "可用" : "不可用"}`
                : "未开通或已到期"}
            </Tag>
            <Typography.Paragraph
              ellipsis
              className={`${styles.text} ${styles.s122}`}
              style={
                {
                  "--heavy-21": subscription.active
                    ? "rgba(255,255,255,.72)"
                    : "var(--goodhr-text-secondary)",
                } as CSSProperties
              }
            >
              到期：{formatDate(subscription.expires_at) || "--"}
            </Typography.Paragraph>
          </Flex>
        </InfoCard>
        <InfoCard
          icon={<PayCircleOutlined aria-hidden="true" />}
          title="AI 余额"
          value={`￥${wallet.balance || "0.0000"}`}
          compact
          className={styles.s123}
        >
          {Number(wallet.bonus_balance_units || 0) > 0 ? (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              含赠送 ￥{wallet.bonus_balance || "0.0000"}，赠送余额最后使用。
            </Typography.Text>
          ) : null}
          <Flex className={styles.s124}>
            <Button
              onClick={openModelDialog}
              title={`当前模型：${modelLabel}`}
              type="default"
              size="small"
              className={styles.s125}
            >{`当前模型：${modelLabel}`}</Button>
            <Flex className={styles.s126}>
              <Button
                size="small"
                loading={recharging}
                disabled={recharging}
                onClick={() => setRechargeDialogOpen(true)}
                type={"primary"}
              >
                {recharging ? "下单中" : "充值"}
              </Button>
              <Button
                size="small"
                onClick={() => setRecordTab("ai")}
                type={"default"}
              >
                看记录
              </Button>
            </Flex>
          </Flex>
        </InfoCard>
      </div>

      <div className={styles.s132}>
        <Typography.Title
          level={2}
          className={`${styles.title} ${styles.s133}`}
        >
          选择会员套餐
        </Typography.Title>
        <Typography.Paragraph className={`${styles.text} ${styles.s134}`}>
          未到期时购买会从当前到期时间继续增加天数。
        </Typography.Paragraph>
      </div>
      <div className={styles.s135}>
        {plans.map((plan, index) => (
          <PlanCard
            key={plan.id || index}
            plan={plan}
            featured={plan.member_type === "max" || Boolean(plan.recommended)}
            paying={payingPlanID === plan.id}
            quote={estimateSubscriptionQuote(subscription, plans, plan)}
            onPay={() => requestPlanPayment(plan.id)}
          />
        ))}
      </div>

      <SectionPanel className={styles.s136}>
        <Flex className={styles.s137}>
          <Typography.Title
            level={2}
            className={`${styles.title} ${styles.s138}`}
          >
            账务记录
          </Typography.Title>
          <Tabs
            activeKey={recordTab}
            onChange={(value) =>
              setRecordTab(value === "ai" ? "ai" : "payments")
            }
            items={[
              { key: "payments", label: "支付记录" },
              { key: "ai", label: "AI 使用记录" },
            ]}
          />
        </Flex>
        {recordTab === "payments" ? (
          <PaymentRecords orders={orders} loading={loading} />
        ) : (
          <AIRecordList
            records={aiRecords}
            total={aiTotal}
            page={aiPage}
            loading={aiLoading}
            onPageChange={setAIPage}
          />
        )}
      </SectionPanel>

      <Modal
        open={modelDialogOpen}
        onCancel={() => {
          if (!savingModel) setModelDialogOpen(false);
        }}
        closable={!savingModel}
        mask={{ closable: !savingModel }}
        keyboard={!savingModel}
        width={440}
        destroyOnHidden
        title={<> 选择当前模型 </>}
        footer={
          <Flex justify="flex-end" gap={8} wrap>
            <Button
              disabled={savingModel}
              onClick={() => setModelDialogOpen(false)}
              type={"text"}
            >
              先不改
            </Button>
            <Button
              loading={savingModel}
              disabled={savingModel}
              onClick={() => void saveAIModel()}
              type={"primary"}
            >
              {savingModel ? "保存中" : "保存模型"}
            </Button>
          </Flex>
        }
      >
        <Form.Item
          layout="vertical"
          htmlFor="subscription-field-2"
          label={"当前模型"}
          className={styles.s139}
          style={{ marginBottom: 0 }}
        >
          <Select
            value={selectedModel}
            onChange={(value) => setSelectedModel(String(value ?? ""))}
            id="subscription-field-2"
            className={styles.s140}
            options={(Array.isArray(wallet.models) ? wallet.models : []).map(
              (model: any) => ({
                value: String(model.id),
                label: model.name
                  ? model.name + " · " + model.id
                  : String(model.id),
              }),
            )}
            disabled={savingModel}
            optionFilterProp="label"
            showSearch
          />
        </Form.Item>
      </Modal>

      <AdminDialog
        open={activationDialogOpen}
        title="激活会员"
        description="填入会员激活码，我马上给你把会员时间安排上。"
        maxWidth="xs"
        confirmText="立即激活"
        cancelText="先放着"
        loading={redeeming}
        loadingText="激活中"
        confirmDisabled={!code.trim()}
        onClose={() => setActivationDialogOpen(false)}
        onConfirm={() => void redeem()}
      >
        <Form.Item
          layout="vertical"
          htmlFor="subscription-field-1"
          label="会员激活码"
          style={{ marginBottom: 0 }}
        >
          <Input
            value={code}
            onChange={(event) => setCode(event.target.value)}
            onPressEnter={() => {
              if (code.trim() && !redeeming) void redeem();
            }}
            disabled={redeeming}
            autoFocus
            id="subscription-field-1"
          />
        </Form.Item>
      </AdminDialog>

      <AIRechargeDialog
        open={rechargeDialogOpen}
        amount={rechargeAmount}
        promotions={wallet.recharge_promotions}
        loading={recharging}
        onAmountChange={setRechargeAmount}
        onClose={() => setRechargeDialogOpen(false)}
        onConfirm={() => void requestAIRecharge()}
      />

      <PaymentUnderstandingDialog
        open={pendingPayment !== null}
        paymentType={pendingPayment?.type || "membership"}
        plan={pendingPlan}
        quote={pendingQuote}
        loading={
          pendingPayment?.type === "membership"
            ? payingPlanID === pendingPayment.planID
            : recharging
        }
        onClose={() => setPendingPayment(null)}
        onConfirm={() => void confirmPendingPayment()}
      />
      <Modal
        open={refundDialogOpen}
        onCancel={refunding ? undefined : () => setRefundDialogOpen(false)}
        width={440}
        destroyOnHidden
        closable={!refunding}
        mask={{ closable: !refunding }}
        keyboard={!refunding}
        title={<> 申请退款 </>}
        footer={
          <Flex justify="flex-end" gap={8} wrap>
            <Button
              disabled={refunding}
              onClick={() => setRefundDialogOpen(false)}
              type={"text"}
            >
              先不退
            </Button>
            <Button
              disabled={
                refundLoading ||
                refunding ||
                !refundQuote?.[refundType]?.available
              }
              onClick={() => void submitRefund()}
              type={"primary"}
            >
              {refunding ? "退款处理中" : "确认退款"}
            </Button>
          </Flex>
        }
      >
        <Form.Item
          layout="vertical"
          htmlFor="subscription-field-4"
          label={"退款类型"}
          className={styles.s146}
          style={{ marginBottom: 0 }}
        >
          <Select
            value={refundType}
            onChange={(value) =>
              changeRefundType(String(value ?? "") as RefundType)
            }
            disabled={refundLoading || refunding}
            id="subscription-field-4"
            className={styles.s147}
            options={[
              { value: "membership", label: "会员退款" },
              { value: "ai_balance", label: "AI 余额退款" },
            ]}
          />
        </Form.Item>
        <Form.Item
          layout="vertical"
          htmlFor="subscription-field-5"
          label={"退款金额（元）"}
          className={styles.s148}
          style={{ marginBottom: 0 }}
        >
          <InputNumber
            id="subscription-field-5"
            value={refundAmount ? Number(refundAmount) : null}
            onChange={(value) =>
              setRefundAmount(value === null ? "" : String(value))
            }
            min={0.01}
            max={Number(refundQuote?.[refundType]?.max_amount || 0)}
            precision={2}
            prefix="￥"
            disabled={refundLoading || refunding}
            style={{ width: "100%" }}
          />
        </Form.Item>
        <Typography.Paragraph className={`${styles.text} ${styles.s150}`}>
          {refundLoading
            ? "我正在核对成功支付订单和剩余权益。"
            : refundQuote?.[refundType]?.available
              ? `当前最多可退 ￥${refundQuote[refundType].max_amount}。确认后会自动原路退回，并同步扣减对应权益。`
              : refundQuote?.[refundType]?.message || "当前没有可退金额。"}
        </Typography.Paragraph>
        {refundType === "ai_balance" ? (
          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            赠送的余额不可退款。{AI_RECHARGE_REFUND_POLICY}
          </Typography.Paragraph>
        ) : null}
      </Modal>
      <WechatPayDialog
        payment={wechatPayment}
        onClose={() => setWechatPayment(null)}
        onPaid={async () => {
          setWechatPayment(null);
          notify("支付成功，钱和权益都认真记好了。", "success");
          await refreshAll();
        }}
      />
    </>
  );
}
/** PaymentUnderstandingDialog 在创建订单前说明会员费、AI 余额和现有退款政策。 */
function PaymentUnderstandingDialog({
  open,
  paymentType,
  plan,
  quote,
  loading,
  onClose,
  onConfirm,
}: {
  open: boolean;
  paymentType: PendingPayment["type"];
  plan: SubscriptionPlan | null;
  quote: SubscriptionUpgradeQuote | null;
  loading: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Modal
      open={open}
      onCancel={loading ? undefined : onClose}
      width={640}
      destroyOnHidden
      closable={!loading}
      mask={{ closable: !loading }}
      keyboard={!loading}
      title={<> 支付前请确认 </>}
      footer={
        <Flex justify="flex-end" gap={8} wrap>
          <Button disabled={loading} onClick={onClose} type={"text"}>
            我再想想
          </Button>
          <Button disabled={loading} onClick={onConfirm} type={"primary"}>
            {loading ? "正在创建订单" : "我已理解，继续支付"}
          </Button>
        </Flex>
      }
    >
      <Typography.Paragraph className={`${styles.text} ${styles.s151}`}>
        {paymentType === "membership"
          ? "你即将购买会员套餐。继续前，请确认已经理解下面的费用区别和退款政策。"
          : "你即将充值 AI 余额。继续前，请确认已经理解下面的费用区别和退款政策。"}
      </Typography.Paragraph>
      {paymentType === "ai_balance" ? (
        <Typography.Paragraph type="secondary">
          {AI_RECHARGE_POLICY}{AI_RECHARGE_REFUND_POLICY}
        </Typography.Paragraph>
      ) : null}
      <Flex className={styles.s152}>
        {paymentType === "membership" && plan && quote ? (
          <Card
            styles={{
              body: {
                padding: 0,
                height: "100%",
                flex: 1,
                display: "flex",
                flexDirection: "column",
              },
            }}
            className={styles.s153}
          >
            <Typography.Paragraph className={`${styles.text} ${styles.s154}`}>
              {plan.name}
            </Typography.Paragraph>
            <Typography.Paragraph className={`${styles.text} ${styles.s155}`}>
              {quote.sourceMemberType === "max"
                ? `支付成功后会立即切换为 Plus，原 Max 剩余时间不折算，Plus 从付款时间重新计算 ${plan.duration_days} 天。`
                : quote.upgrade
                  ? `Plus 剩余时间抵扣 ￥${(quote.creditCents / 100).toFixed(2)}，预计实付 ￥${(quote.amountCents / 100).toFixed(2)}。最终金额以后端下单时的剩余时间为准。`
                  : `本次预计实付 ￥${(quote.amountCents / 100).toFixed(2)}。`}
            </Typography.Paragraph>
          </Card>
        ) : null}
        <div className={styles.s156}>
          <Typography.Paragraph className={`${styles.text} ${styles.s157}`}>
            会员费
          </Typography.Paragraph>
          <Typography.Paragraph className={`${styles.text} ${styles.s158}`}>
            {paymentType === "membership" && quote?.replacement
              ? "本次属于套餐切换。支付成功后，新套餐从付款时间重新计算，原套餐剩余时间不顺延。"
              : "购买的是会员使用期限，用于解锁会员功能。会员未到期时再次购买同一套餐，新天数会从当前到期时间继续增加。"}
          </Typography.Paragraph>
        </div>
        <div className={styles.s159}>
          <Typography.Paragraph className={`${styles.text} ${styles.s160}`}>
            AI 余额
          </Typography.Paragraph>
          <Typography.Paragraph className={`${styles.text} ${styles.s161}`}>
            仅用于 GoodHR 内置 AI 调用，并按实际使用扣减；自己配置 AI Key
            时不扣这里的余额。会员费不包含 AI 余额，AI 余额也不能替代会员订阅。
          </Typography.Paragraph>
        </div>
        <Card
          styles={{
            body: {
              padding: 0,
              height: "100%",
              flex: 1,
              display: "flex",
              flexDirection: "column",
            },
          }}
          className={styles.s162}
        >
          <Typography.Paragraph className={`${styles.text} ${styles.s163}`}>
            现有退款政策
          </Typography.Paragraph>
          <Typography.Paragraph className={`${styles.text} ${styles.s164}`}>
            会员退款按该笔订单实付金额和购买天数折算，只退还尚未使用的部分；AI
            余额最多退还当前剩余且有成功充值订单支撑的实付余额，赠送余额不可退款。
            {AI_RECHARGE_REFUND_POLICY}
          </Typography.Paragraph>
          <Typography.Paragraph className={`${styles.text} ${styles.s165}`}>
            需要开发票请联系作者处理。发票开出后，该笔订单不支持退费。
          </Typography.Paragraph>
        </Card>
      </Flex>
    </Modal>
  );
}
/** InfoCard 展示订阅页顶部可随容器宽度调整的摘要卡片。 */
function InfoCard({
  icon,
  title,
  value,
  tone = "plain",
  compact = false,
  className,
  style,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  value: string;
  tone?: "plain" | "dark";
  compact?: boolean;
  className?: string;
  style?: CSSProperties;
  children?: React.ReactNode;
}) {
  const dark = tone === "dark";
  return (
    <SectionPanel
      className={[styles.s166, className].filter(Boolean).join(" ")}
      style={
        {
          ...style,
          ...({
            "--heavy-22": dark ? "#202a24" : "var(--goodhr-hover)",
            "--heavy-23": dark ? "#ffffff" : "var(--goodhr-text)",
            "--heavy-24": dark
              ? "var(--goodhr-primary)"
              : "var(--goodhr-border)",
          } as CSSProperties),
        } as CSSProperties
      }
    >
      <Flex className={styles.s167}>
        <Flex className={styles.s168}>
          <div
            className={styles.s169}
            style={
              {
                "--heavy-25": dark
                  ? "var(--goodhr-primary-soft)"
                  : "var(--goodhr-primary-soft)",
              } as CSSProperties
            }
          >
            {icon}
          </div>
          <div className={styles.s170}>
            <Typography.Paragraph
              ellipsis
              className={`${styles.text} ${styles.s171}`}
              style={{ "--heavy-26": dark ? "0.72" : "0.68" } as CSSProperties}
            >
              {title}
            </Typography.Paragraph>
            {value ? (
              <Typography.Paragraph
                ellipsis
                className={`${styles.text} ${styles.s172}`}
                style={
                  { "--heavy-27": compact ? "22px" : "24px" } as CSSProperties
                }
              >
                {value}
              </Typography.Paragraph>
            ) : null}
          </div>
        </Flex>
        {children ? <div className={styles.s173}>{children}</div> : null}
      </Flex>
    </SectionPanel>
  );
}
/** PaymentRecords 展示会员和余额支付订单。 */
function PaymentRecords({
  orders,
  loading,
}: {
  orders: any[];
  loading: boolean;
}) {
  return (
    <Table
      rowKey={(order) => String(order.order_no || order.id || order.created_at)}
      loading={loading}
      dataSource={orders}
      size="small"
      pagination={{
        pageSize: 10,
        hideOnSinglePage: true,
        showSizeChanger: false,
      }}
      scroll={{ x: 640 }}
      locale={{ emptyText: <EmptyState text="暂无支付记录" /> }}
      columns={[
        {
          title: "支付订单",
          key: "order",
          render: (_, order) => (
            <Flex vertical gap={2}>
              <Typography.Text strong>
                {order.plan_name || order.order_type || "支付订单"}
              </Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {order.order_no}
              </Typography.Text>
            </Flex>
          ),
        },
        {
          title: "实付金额",
          key: "amount",
          width: 100,
          render: (_, order) => (
            <Flex vertical gap={2} style={{ whiteSpace: "nowrap" }}>
              <Typography.Text strong>
                ￥{(Number(order.amount_cents || 0) / 100).toFixed(2)}
              </Typography.Text>
              {Number(order.bonus_amount_cents || 0) > 0 ? (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  赠送 ￥{(Number(order.bonus_amount_cents) / 100).toFixed(2)}
                </Typography.Text>
              ) : null}
            </Flex>
          ),
        },
        {
          title: "状态",
          dataIndex: "status",
          width: 100,
          render: (status) => (
            <Tag color={status === "paid" ? "success" : "default"}>
              {statusText(status)}
            </Tag>
          ),
        },
        {
          title: "时间",
          dataIndex: "created_at",
          width: 180,
          render: (value) => formatDate(value),
        },
      ]}
      style={{ marginTop: 12 }}
    />
  );
}
/** AIRecordList 展示 AI 余额流水并分页。 */
function AIRecordList({
  records,
  total,
  page,
  loading,
  onPageChange,
}: {
  records: any[];
  total: number;
  page: number;
  loading: boolean;
  onPageChange: (value: number) => void;
}) {
  return (
    <Table
      rowKey={(record) =>
        String(record.id || record.created_at + "-" + record.reason)
      }
      loading={loading}
      dataSource={records}
      size="small"
      scroll={{ x: 820 }}
      pagination={{
        current: page,
        pageSize: aiRecordPageSize,
        total,
        onChange: onPageChange,
        hideOnSinglePage: true,
        showSizeChanger: false,
      }}
      locale={{
        emptyText: (
          <EmptyState text="这里暂时空空的，等你开始使用内置 AI 后我再认真记账。" />
        ),
      }}
      columns={[
        {
          title: "类型",
          dataIndex: "category",
          width: 95,
          render: (value) => <CategoryChip category={value} />,
        },
        {
          title: "余额变动",
          key: "amount",
          width: 130,
          render: (_, record) => (
            <AIWalletChange record={record} />
          ),
        },
        {
          title: "余额",
          dataIndex: "balance_after",
          width: 120,
          render: (value) => "￥" + (value || "0.00"),
        },
        {
          title: "模型 / Token",
          key: "model",
          width: 170,
          render: (_, record) => <TokenInfo record={record} />,
        },
        {
          title: "说明",
          key: "reason",
          render: (_, record) => <ReasonText record={record} />,
        },
        {
          title: "时间",
          dataIndex: "created_at",
          width: 180,
          render: (value) => formatDate(value),
        },
      ]}
      style={{ marginTop: 12 }}
    />
  );
}

/** CategoryChip 展示流水类别。 */
function CategoryChip({ category }: { category: string }) {
  const labelMap: Record<string, string> = {
    ai_usage: "AI调用",
    recharge: "充值",
    refund: "退款",
    refund_release: "退款退回",
    signup_bonus: "注册赠送",
    admin_adjust: "后台调整",
  };
  const color =
    category === "ai_usage"
      ? "warning"
      : category === "admin_adjust"
        ? "processing"
        : "success";
  return <Tag color={color}>{labelMap[category] || category || "记录"}</Tag>;
}
/** TokenInfo 展示模型和 token 用量。 */
function TokenInfo({ record }: { record: any }) {
  const tokens = Number(record.total_tokens || 0);
  return (
    <>
      {record.model_id || "无模型"}
      {tokens ? ` / ${tokens} tokens` : ""}
    </>
  );
}
/** ReasonText 展示记录说明和关联订单号。 */
function ReasonText({ record }: { record: any }) {
  return (
    <>
      {record.reason || "暂无说明"}
      {record.related_order_no ? `（订单 ${record.related_order_no}）` : ""}
    </>
  );
}
/** PlanCard 展示一个随套餐网格宽度自适应的会员套餐卡片。 */
function PlanCard({
  plan,
  featured,
  paying,
  quote,
  onPay,
}: {
  plan: SubscriptionPlan;
  featured: boolean;
  paying: boolean;
  quote: SubscriptionUpgradeQuote;
  onPay: () => void;
}) {
  const price = quote.amountCents / 100;
  const originalPrice = Number(plan.original_price || 0);
  return (
    <Card
      styles={{
        body: {
          padding: 0,
          height: "100%",
          flex: 1,
          display: "flex",
          flexDirection: "column",
        },
      }}
      className={styles.s192}
      style={
        {
          "--heavy-29": featured ? "#b69957" : "var(--goodhr-border)",
          "--heavy-30": featured ? "#fffdf7" : "var(--goodhr-hover)",
          "--heavy-31": featured ? "0 12px 28px rgba(76, 61, 28, .09)" : "none",
        } as CSSProperties
      }
    >
      {featured ? <Tag className={styles.s193}>{"推荐"}</Tag> : null}
      <Typography.Title
        level={3}
        className={`${styles.title} ${styles.s194}`}
        style={{ "--heavy-32": featured ? "56px" : "0px" } as CSSProperties}
      >
        {plan.name || "会员套餐"}
      </Typography.Title>
      <Flex className={styles.s195}>
        <Typography.Paragraph
          className={`${styles.text} ${styles.s196}`}
          style={
            {
              "--heavy-33": featured ? "#80621f" : "var(--goodhr-text)",
            } as CSSProperties
          }
        >
          ￥{price}
        </Typography.Paragraph>
        {originalPrice > price ? (
          <Typography.Paragraph className={`${styles.text} ${styles.s197}`}>
            ￥{originalPrice}
          </Typography.Paragraph>
        ) : null}
      </Flex>
      <Typography.Paragraph className={`${styles.text} ${styles.s198}`}>
        {plan.description || "解锁更多智能招聘功能。"}
      </Typography.Paragraph>
      {quote.upgrade ? (
        <Typography.Paragraph className={`${styles.text} ${styles.s199}`}>
          已按 Plus 剩余时间抵扣 ￥{(quote.creditCents / 100).toFixed(2)}
        </Typography.Paragraph>
      ) : null}
      {quote.sourceMemberType === "max" ? (
        <Typography.Paragraph className={`${styles.text} ${styles.s200}`}>
          立即切换 Plus，原 Max 剩余时间不折算
        </Typography.Paragraph>
      ) : null}
      <Flex className={styles.s201}>
        {(Array.isArray(plan.features) ? plan.features : []).map(
          (feature: string) => (
            <Flex key={feature} className={styles.s202}>
              <CheckOutlined
                aria-hidden="true"
                className={styles.s203}
                style={
                  {
                    "--heavy-34": featured
                      ? "#9c7b32"
                      : "var(--goodhr-primary)",
                  } as CSSProperties
                }
              />
              <Typography.Paragraph className={`${styles.text} ${styles.s204}`}>
                {feature}
              </Typography.Paragraph>
            </Flex>
          ),
        )}
      </Flex>
      {originalPrice > 0 ? (
        <Button
          disabled={paying}
          onClick={onPay}
          type={featured ? "primary" : "default"}
          block
          className={styles.s205}
          style={
            {
              "--heavy-35": featured ? "#202a24" : undefined,
              "--heavy-36": featured ? "#f8e6b8" : undefined,
              "--heavy-37": featured ? "#2b382f" : undefined,
            } as CSSProperties
          }
        >
          {paying
            ? "正在创建订单"
            : quote.sourceMemberType === "max"
              ? "切换为 Plus"
              : quote.upgrade
                ? "补差价升级 Max"
                : "立即订阅"}
        </Button>
      ) : (
        <Tag className={styles.s206}>{"永久免费"}</Tag>
      )}
    </Card>
  );
}
/** statusText 将支付状态转换成中文。 */
function statusText(status: string) {
  return status === "paid"
    ? "已支付"
    : status === "closed"
      ? "已关闭"
      : "待支付";
}
