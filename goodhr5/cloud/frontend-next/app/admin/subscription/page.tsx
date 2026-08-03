/** 本文件负责新版后台会员、AI 余额、激活码、支付记录和 AI 使用记录展示。 */
"use client";

import ArrowDownwardRoundedIcon from "@mui/icons-material/ArrowDownwardRounded";
import ArrowUpwardRoundedIcon from "@mui/icons-material/ArrowUpwardRounded";
import CheckRoundedIcon from "@mui/icons-material/CheckRounded";
import PaidRoundedIcon from "@mui/icons-material/PaidRounded";
import WorkspacePremiumRoundedIcon from "@mui/icons-material/WorkspacePremiumRounded";
import {
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  MenuItem,
  Pagination,
  Stack,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tabs,
  TextField,
  Typography,
} from "@mui/material";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import {
  estimateSubscriptionQuote,
  normalizeSubscriptionPlans,
  type SubscriptionPlan,
  type SubscriptionUpgradeQuote,
} from "@/lib/subscription";

const aiRecordPageSize = 10;

type PendingPayment =
  | { type: "membership"; planID: string }
  | { type: "ai_balance" };

/** SubscriptionPage 展示会员状态、AI 余额和账务记录。 */
export default function SubscriptionPage() {
  const { notify, subscription, refreshSession, agentBase } = useAdmin();
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
  const [rechargeAmount, setRechargeAmount] = useState("10");
  const [selectedModel, setSelectedModel] = useState("");
  const [loading, setLoading] = useState(false);
  const [aiLoading, setAILoading] = useState(false);
  const [recharging, setRecharging] = useState(false);
  const [savingModel, setSavingModel] = useState(false);
  const [payingPlanID, setPayingPlanID] = useState("");
  const [rechargeDialogOpen, setRechargeDialogOpen] = useState(false);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [pendingPayment, setPendingPayment] = useState<PendingPayment | null>(
    null,
  );

  const modelLabel = currentAIModel || wallet.default_model || "未配置";
  const rechargeUnavailableMessage = "本系统为测试系统 无需充值";
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
    try {
      await cloudRequest("/api/subscription/redeem", {
        method: "POST",
        body: { code: value },
      });
      setCode("");
      notify("激活成功，会员时间已到账。", "success");
      await refreshAll();
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "激活码没通过，我也有点尴尬。",
        "error",
      );
    }
  }

  /** pay 创建会员套餐支付订单。 */
  async function pay(planID: string) {
    if (!planID) return;
    setPayingPlanID(planID);
    try {
      const data = await cloudRequest("/api/payment/orders", {
        method: "POST",
        body: { plan_id: planID },
      });
      if (data.payment_completed) {
        notify("Plus 剩余时间已经抵完差价，Max 全能版已到账。", "success");
      } else {
        submitPayment(data.payment);
        notify("支付页面已打开，付完我会回来认真记账。", "success");
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
  function requestPlanPayment(planID: string) {
    if (!agentBase) {
      notify(rechargeUnavailableMessage, "warning");
      return;
    }
    if (!planID) return;
    const plan = plans.find((item) => item.id === planID);
    if (!plan) return;
    setPendingPayment({ type: "membership", planID });
  }

  /** openRechargeDialog 在展示 AI 充值弹框前确认本地程序连接状态。 */
  function openRechargeDialog() {
    if (!agentBase) {
      notify(rechargeUnavailableMessage, "warning");
      return;
    }
    setRechargeDialogOpen(true);
  }

  /** rechargeAI 创建 AI 余额充值订单。 */
  async function rechargeAI() {
    const amount = Number(rechargeAmount || 0);
    if (!Number.isFinite(amount) || amount <= 0) {
      notify("充值金额得大于 0，我先小声拦一下。", "warning");
      return;
    }
    setRecharging(true);
    try {
      const data = await cloudRequest("/api/payment/ai-balance", {
        method: "POST",
        body: { amount_yuan: rechargeAmount || "10" },
      });
      submitPayment(data.payment);
      setRechargeDialogOpen(false);
      notify("AI 余额支付页面已打开，付完我再回来记账。", "success");
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
  function requestAIRecharge() {
    if (!agentBase) {
      notify(rechargeUnavailableMessage, "warning");
      setRechargeDialogOpen(false);
      return;
    }
    const amount = Number(rechargeAmount || 0);
    if (!Number.isFinite(amount) || amount <= 0) {
      notify("充值金额得大于 0，我先小声拦一下。", "warning");
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

  return (
    <>
      <PageHeader
        title='订阅会员'
        description='会员费就像您包月租了一个小汽车，AI余额 就是您小汽车油箱的余额。'
        actions={
          <RefreshButton
            loading={loading || aiLoading}
            onClick={() => void refreshAll()}
          />
        }
      />

      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          gap: 2,
          mb: 3,
          alignItems: "stretch",
          "& > *": { minWidth: 0 },
        }}
      >
        <InfoCard
          icon={<WorkspacePremiumRoundedIcon />}
          title='当前会员'
          value={subscription.member_name || "免费版"}
          tone={subscription.active ? "dark" : "plain"}
          compact
          sx={{
            flex: { xs: "1 1 100%", md: "0 0 auto" },
            width: { xs: "100%", md: "auto" },
          }}
        >
          <Stack
            spacing={1}
            sx={{
              alignItems: { xs: "flex-start", sm: "flex-end" },
              textAlign: { xs: "left", sm: "right" },
            }}
          >
            <Chip
              size='small'
              label={
                subscription.active
                  ? `${subscription.remaining_days} 天 · 自动回复${subscription.allow_auto_reply ? "可用" : "不可用"}`
                  : "未开通或已到期"
              }
              color={subscription.active ? "primary" : "default"}
            />
            <Typography
              noWrap
              sx={{
                color: subscription.active
                  ? "rgba(255,255,255,.72)"
                  : "text.secondary",
                fontSize: 13,
              }}
            >
              到期：{formatDate(subscription.expires_at) || "--"}
            </Typography>
          </Stack>
        </InfoCard>

        <InfoCard
          icon={<PaidRoundedIcon />}
          title='AI 余额'
          value={`￥${wallet.balance || "0.0000"}`}
          compact
          sx={{
            flex: { xs: "1 1 100%", md: "0 0 auto" },
            width: { xs: "100%", md: "auto" },
            minWidth: { md: 470 },
          }}
        >
          <Stack
            spacing={1}
            sx={{ alignItems: { xs: "flex-start", sm: "flex-end" } }}
          >
            <Chip
              size='small'
              label={`当前模型：${modelLabel}`}
              onClick={openModelDialog}
              sx={{
                width: "fit-content",
                bgcolor: "action.selected",
                color: "primary.dark",
                fontWeight: 700,
              }}
            />
            <Stack direction='row' spacing={1} sx={{ flexWrap: "nowrap" }}>
              <Button
                size='small'
                variant='contained'
                disabled={recharging}
                onClick={openRechargeDialog}
              >
                {recharging ? "下单中" : "充值"}
              </Button>
              <Button
                size='small'
                variant='outlined'
                onClick={() => setRecordTab("ai")}
              >
                看记录
              </Button>
            </Stack>
          </Stack>
        </InfoCard>

        <SectionPanel
          sx={{
            display: "flex",
            flex: "1 1 260px",
            alignItems: "center",
            bgcolor: "action.hover",
          }}
        >
          <Stack
            direction='row'
            spacing={1.25}
            sx={{ alignItems: "center", minWidth: 0, width: "100%" }}
          >
            <TextField
              size='small'
              label='会员激活码'
              value={code}
              onChange={(event) => setCode(event.target.value)}
              fullWidth
              sx={{ minWidth: 0, flex: "1 1 auto" }}
            />
            <Button
              variant='contained'
              disabled={!code.trim()}
              onClick={() => void redeem()}
              sx={{ minWidth: 76, flexShrink: 0 }}
            >
              激活
            </Button>
          </Stack>
        </SectionPanel>
      </Box>

      <Box sx={{ mb: 2.25 }}>
        <Typography component='h2' sx={{ fontSize: 22, fontWeight: 780 }}>
          选择会员套餐
        </Typography>
        <Typography sx={{ mt: 0.5, color: "text.secondary" }}>
          未到期时购买会从当前到期时间继续增加天数。
        </Typography>
      </Box>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "minmax(0, 1fr)",
            sm: "repeat(2, minmax(0, 1fr))",
            lg: "repeat(3, minmax(0, 1fr))",
          },
          gap: 2,
          alignItems: "stretch",
        }}
      >
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
      </Box>

      <SectionPanel sx={{ mt: 2 }}>
        <Stack
          direction={{ xs: "column", sm: "row" }}
          sx={{
            justifyContent: "space-between",
            gap: 1,
            alignItems: { sm: "center" },
          }}
        >
          <Typography component='h2' sx={{ fontSize: 19, fontWeight: 760 }}>
            账务记录
          </Typography>
          <Tabs value={recordTab} onChange={(_, value) => setRecordTab(value)}>
            <Tab value='payments' label='支付记录' />
            <Tab value='ai' label='AI 使用记录' />
          </Tabs>
        </Stack>
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

      <Dialog
        open={modelDialogOpen}
        onClose={() => setModelDialogOpen(false)}
        fullWidth
        maxWidth='xs'
      >
        <DialogTitle>选择当前模型</DialogTitle>
        <DialogContent>
          <TextField
            select
            fullWidth
            label='当前模型'
            value={selectedModel}
            onChange={(event) => setSelectedModel(event.target.value)}
            sx={{ mt: 1 }}
          >
            {(Array.isArray(wallet.models) ? wallet.models : []).map(
              (model: any) => (
                <MenuItem key={model.id} value={model.id}>
                  <Box>
                    <Typography sx={{ fontWeight: 800 }}>
                      {model.name || model.id}
                    </Typography>
                    <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
                      {model.id}
                    </Typography>
                  </Box>
                </MenuItem>
              ),
            )}
          </TextField>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setModelDialogOpen(false)}>先不改</Button>
          <Button
            variant='contained'
            disabled={savingModel}
            onClick={() => void saveAIModel()}
          >
            {savingModel ? "保存中" : "保存模型"}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={rechargeDialogOpen}
        onClose={() => setRechargeDialogOpen(false)}
        fullWidth
        maxWidth='xs'
      >
        <DialogTitle>充值 AI 余额</DialogTitle>
        <DialogContent>
          <TextField
            fullWidth
            label='充值金额（元）'
            value={rechargeAmount}
            onChange={(event) => setRechargeAmount(event.target.value)}
            slotProps={{ htmlInput: { inputMode: "decimal" } }}
            sx={{ mt: 1 }}
          />
          <Typography sx={{ mt: 1.25, color: "text.secondary", fontSize: 13 }}>
            默认 5 元，够先跑一阵。填完我就去创建支付订单。
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRechargeDialogOpen(false)}>先不充</Button>
          <Button
            variant='contained'
            disabled={recharging}
            onClick={requestAIRecharge}
          >
            {recharging ? "下单中" : "去支付"}
          </Button>
        </DialogActions>
      </Dialog>

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
    <Dialog
      open={open}
      onClose={loading ? undefined : onClose}
      fullWidth
      maxWidth='sm'
    >
      <DialogTitle>支付前请确认</DialogTitle>
      <DialogContent>
        <Typography sx={{ color: "text.secondary", lineHeight: 1.75 }}>
          {paymentType === "membership"
            ? "你即将购买会员套餐。继续前，请确认已经理解下面的费用区别和退款政策。"
            : "你即将充值 AI 余额。继续前，请确认已经理解下面的费用区别和退款政策。"}
        </Typography>
        <Stack spacing={1.25} sx={{ mt: 2 }}>
          {paymentType === "membership" && plan && quote ? (
            <Box
              sx={{
                p: 1.75,
                borderRadius: 2,
                bgcolor: "action.selected",
                border: "1px solid",
                borderColor: "primary.light",
              }}
            >
              <Typography sx={{ fontWeight: 800 }}>{plan.name}</Typography>
              <Typography sx={{ mt: 0.5, color: "text.secondary" }}>
                {quote.sourceMemberType === "max"
                  ? `支付成功后会立即切换为 Plus，原 Max 剩余时间不折算，Plus 从付款时间重新计算 ${plan.duration_days} 天。`
                  : quote.upgrade
                  ? `Plus 剩余时间抵扣 ￥${(quote.creditCents / 100).toFixed(2)}，预计实付 ￥${(quote.amountCents / 100).toFixed(2)}。最终金额以后端下单时的剩余时间为准。`
                  : `本次预计实付 ￥${(quote.amountCents / 100).toFixed(2)}。`}
              </Typography>
            </Box>
          ) : null}
          <Box
            sx={{ p: 1.75, borderRadius: 2, bgcolor: "action.hover" }}
          >
            <Typography sx={{ fontWeight: 800 }}>会员费</Typography>
            <Typography sx={{ mt: 0.5, color: "text.secondary", lineHeight: 1.7 }}>
              {paymentType === "membership" && quote?.replacement
                ? "本次属于套餐切换。支付成功后，新套餐从付款时间重新计算，原套餐剩余时间不顺延。"
                : "购买的是会员使用期限，用于解锁会员功能。会员未到期时再次购买同一套餐，新天数会从当前到期时间继续增加。"}
            </Typography>
          </Box>
          <Box
            sx={{ p: 1.75, borderRadius: 2, bgcolor: "#f7f7f4" }}
          >
            <Typography sx={{ fontWeight: 800 }}>AI 余额</Typography>
            <Typography sx={{ mt: 0.5, color: "text.secondary", lineHeight: 1.7 }}>
              仅用于 GoodHR 内置 AI 调用，并按实际使用扣减；自己配置 AI Key 时不扣这里的余额。会员费不包含 AI 余额，AI 余额也不能替代会员订阅。
            </Typography>
          </Box>
          <Box
            sx={{
              p: 1.75,
              borderRadius: 2,
              bgcolor: "#fff8ed",
              border: "1px solid #f0d8ac",
            }}
          >
            <Typography sx={{ fontWeight: 800, color: "#714700" }}>
              现有退款政策
            </Typography>
            <Typography sx={{ mt: 0.5, color: "#714700", lineHeight: 1.7 }}>
              需要退款时，按套餐原价折算剩余天数，并扣除支付渠道产生的 5% 手续费。
            </Typography>
            <Typography sx={{ mt: 0.75, color: "#714700", lineHeight: 1.7 }}>
              需要开发票请联系作者处理。发票开出后，该笔订单不支持退费。
            </Typography>
          </Box>
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5 }}>
        <Button disabled={loading} onClick={onClose}>
          我再想想
        </Button>
        <Button variant='contained' disabled={loading} onClick={onConfirm}>
          {loading ? "正在创建订单" : "我已理解，继续支付"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

/** InfoCard 展示订阅页顶部三列摘要。 */
function InfoCard({
  icon,
  title,
  value,
  tone = "plain",
  compact = false,
  sx = {},
  children,
}: {
  icon: React.ReactNode;
  title: string;
  value: string;
  tone?: "plain" | "dark";
  compact?: boolean;
  sx?: Record<string, unknown>;
  children?: React.ReactNode;
}) {
  const dark = tone === "dark";
  return (
    <SectionPanel
      sx={{
        bgcolor: dark ? "secondary.main" : "action.hover",
        color: dark ? "#ffffff" : "text.primary",
        borderColor: dark ? "primary.main" : "divider",
        ...sx,
      }}
    >
      <Stack
        direction={{ xs: "column", sm: "row" }}
        spacing={1.5}
        sx={{
          height: "100%",
          alignItems: { xs: "flex-start", sm: "center" },
          justifyContent: "space-between",
        }}
      >
        <Stack
          direction='row'
          spacing={1.5}
          sx={{ alignItems: "center", minWidth: 0, flexShrink: 0 }}
        >
          <Box
            sx={{
              width: 44,
              height: 44,
              borderRadius: "8px",
              display: "grid",
              placeItems: "center",
              flexShrink: 0,
              bgcolor: dark ? "primary.light" : "action.selected",
              color: "primary.dark",
            }}
          >
            {icon}
          </Box>
          <Box sx={{ minWidth: 0 }}>
            <Typography
              noWrap
              sx={{ fontSize: 13, opacity: dark ? 0.72 : 0.68 }}
            >
              {title}
            </Typography>
            {value ? (
              <Typography
                noWrap
                sx={{ mt: 0.5, fontSize: compact ? 22 : 24, fontWeight: 820 }}
              >
                {value}
              </Typography>
            ) : null}
          </Box>
        </Stack>
        {children ? (
          <Box
            sx={{
              minWidth: 0,
              width: { xs: "100%", sm: undefined },
              flex: "0 1 auto",
              maxWidth: { xs: "100%", sm: "52%" },
            }}
          >
            {children}
          </Box>
        ) : null}
      </Stack>
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
  return orders.length ? (
    <Stack sx={{ mt: 1.5 }}>
      {orders.map((order) => (
        <Stack
          key={order.order_no}
          direction={{ xs: "column", md: "row" }}
          spacing={1.5}
          sx={{
            py: 1.5,
            borderBottom: "1px solid",
            borderColor: "divider",
            alignItems: { md: "center" },
          }}
        >
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography sx={{ fontWeight: 700 }}>
              {order.plan_name || order.order_type || "支付订单"}
            </Typography>
            <Typography noWrap sx={{ color: "text.secondary", fontSize: 12 }}>
              {order.order_no}
            </Typography>
          </Box>
          <Typography sx={{ minWidth: 80, fontWeight: 700 }}>
            ￥{(Number(order.amount_cents || 0) / 100).toFixed(2)}
          </Typography>
          <Chip
            size='small'
            color={order.status === "paid" ? "success" : "default"}
            label={statusText(order.status)}
          />
          <Typography
            sx={{ color: "text.secondary", fontSize: 13, minWidth: 168 }}
          >
            {formatDate(order.created_at)}
          </Typography>
        </Stack>
      ))}
    </Stack>
  ) : (
    <EmptyState text={loading ? "正在读取支付记录" : "暂无支付记录"} />
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
  const pageCount = Math.max(1, Math.ceil(total / aiRecordPageSize));
  return records.length ? (
    <>
      <TableContainer sx={{ mt: 1.5, display: { xs: "none", md: "block" } }}>
        <Table size='small'>
          <TableHead>
            <TableRow sx={{ bgcolor: "action.hover" }}>
              <TableCell>类型</TableCell>
              <TableCell>金额</TableCell>
              <TableCell>余额</TableCell>
              <TableCell>模型 / Token</TableCell>
              <TableCell>说明</TableCell>
              <TableCell>时间</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {records.map((record) => (
              <AIRecordRow
                key={record.id || `${record.created_at}-${record.reason}`}
                record={record}
              />
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      <Stack
        spacing={1.25}
        sx={{ mt: 1.5, display: { xs: "flex", md: "none" } }}
      >
        {records.map((record) => (
          <AIRecordCard
            key={record.id || `${record.created_at}-${record.reason}`}
            record={record}
          />
        ))}
      </Stack>
      <Pagination
        page={page}
        count={pageCount}
        onChange={(_, value) => onPageChange(value)}
        sx={{ mt: 2, display: "flex", justifyContent: "center" }}
      />
    </>
  ) : (
    <EmptyState
      text={
        loading
          ? "正在读取 AI 使用记录"
          : "这里暂时空空的，等你开始使用内置 AI 后我再认真记账。"
      }
    />
  );
}

/** AIRecordRow 展示桌面端单条 AI 记录。 */
function AIRecordRow({ record }: { record: any }) {
  return (
    <TableRow hover>
      <TableCell>
        <CategoryChip category={record.category} />
      </TableCell>
      <TableCell>
        <AmountText
          units={Number(record.change_units || 0)}
          value={record.change}
        />
      </TableCell>
      <TableCell>￥{record.balance_after || "0.00"}</TableCell>
      <TableCell>
        <TokenInfo record={record} />
      </TableCell>
      <TableCell>
        <ReasonText record={record} />
      </TableCell>
      <TableCell>{formatDate(record.created_at)}</TableCell>
    </TableRow>
  );
}

/** AIRecordCard 展示移动端单条 AI 记录。 */
function AIRecordCard({ record }: { record: any }) {
  return (
    <Box
      sx={{
        p: 1.5,
        border: "1px solid",
        borderColor: "divider",
        borderRadius: "8px",
        bgcolor: "action.hover",
      }}
    >
      <Stack direction='row' sx={{ justifyContent: "space-between", gap: 1 }}>
        <CategoryChip category={record.category} />
        <AmountText
          units={Number(record.change_units || 0)}
          value={record.change}
        />
      </Stack>
      <Typography sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}>
        <ReasonText record={record} />
      </Typography>
      <Typography sx={{ mt: 0.75, color: "text.secondary", fontSize: 12 }}>
        <TokenInfo record={record} /> · 余额 ￥{record.balance_after || "0.00"}{" "}
        · {formatDate(record.created_at)}
      </Typography>
    </Box>
  );
}

/** CategoryChip 展示流水类别。 */
function CategoryChip({ category }: { category: string }) {
  const labelMap: Record<string, string> = {
    ai_usage: "AI调用",
    recharge: "充值",
    signup_bonus: "注册赠送",
    admin_adjust: "后台调整",
  };
  const color =
    category === "ai_usage"
      ? "warning"
      : category === "admin_adjust"
        ? "info"
        : "success";
  return (
    <Chip
      size='small'
      color={color as any}
      variant='outlined'
      label={labelMap[category] || category || "记录"}
    />
  );
}

/** AmountText 展示余额变动金额。 */
function AmountText({ units, value }: { units: number; value: string }) {
  const positive = units
    ? units > 0
    : !String(value || "")
        .trim()
        .startsWith("-");
  const Icon = positive ? ArrowUpwardRoundedIcon : ArrowDownwardRoundedIcon;
  return (
    <Stack
      direction='row'
      spacing={0.4}
      sx={{
        alignItems: "center",
        color: positive ? "success.main" : "warning.dark",
        fontWeight: 800,
      }}
    >
      <Icon sx={{ fontSize: 16 }} />
      <Typography sx={{ fontWeight: 800 }}>
        {positive ? "+" : ""}￥{value || "0.00"}
      </Typography>
    </Stack>
  );
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
    <Box
      component='article'
      sx={{
        position: "relative",
        display: "flex",
        flexDirection: "column",
        minHeight: 390,
        p: 2.5,
        border: "1px solid",
        borderColor: featured ? "#b69957" : "divider",
        borderRadius: "8px",
        bgcolor: featured ? "#fffdf7" : "action.hover",
        boxShadow: featured ? "0 12px 28px rgba(76, 61, 28, .09)" : "none",
      }}
    >
      {featured ? (
        <Chip
          size='small'
          label='推荐'
          sx={{
            position: "absolute",
            top: 14,
            right: 14,
            bgcolor: "#202a24",
            color: "#f5dfaa",
            fontWeight: 700,
          }}
        />
      ) : null}
      <Typography
        component='h3'
        sx={{ pr: featured ? 7 : 0, fontSize: 20, fontWeight: 780 }}
      >
        {plan.name || "会员套餐"}
      </Typography>
      <Stack direction='row' spacing={1} sx={{ mt: 2, alignItems: "baseline" }}>
        <Typography
          sx={{
            fontSize: 36,
            lineHeight: 1,
            fontWeight: 850,
            color: featured ? "#80621f" : "text.primary",
          }}
        >
          ￥{price}
        </Typography>
        {originalPrice > price ? (
          <Typography
            sx={{ color: "text.disabled", textDecoration: "line-through" }}
          >
            ￥{originalPrice}
          </Typography>
        ) : null}
      </Stack>
      <Typography
        sx={{
          mt: 1.5,
          color: "text.secondary",
          lineHeight: 1.65,
          minHeight: 52,
        }}
      >
        {plan.description || "解锁更多智能招聘功能。"}
      </Typography>
      {quote.upgrade ? (
        <Typography sx={{ mt: 1, color: "#80621f", fontSize: 13, fontWeight: 700 }}>
          已按 Plus 剩余时间抵扣 ￥{(quote.creditCents / 100).toFixed(2)}
        </Typography>
      ) : null}
      {quote.sourceMemberType === "max" ? (
        <Typography sx={{ mt: 1, color: "#80621f", fontSize: 13, fontWeight: 700 }}>
          立即切换 Plus，原 Max 剩余时间不折算
        </Typography>
      ) : null}
      <Stack spacing={1.1} sx={{ mt: 2.25, flex: 1 }}>
        {(Array.isArray(plan.features) ? plan.features : []).map(
          (feature: string) => (
            <Stack
              key={feature}
              direction='row'
              spacing={1}
              sx={{ alignItems: "flex-start" }}
            >
              <CheckRoundedIcon
                sx={{
                  mt: 0.15,
                  color: featured ? "#9c7b32" : "primary.main",
                  fontSize: 19,
                }}
              />
              <Typography sx={{ fontSize: 14, lineHeight: 1.55 }}>
                {feature}
              </Typography>
            </Stack>
          ),
        )}
      </Stack>
      {originalPrice > 0 ? (
        <Button
          fullWidth
          variant={featured ? "contained" : "outlined"}
          disabled={paying}
          sx={{
            mt: 2.5,
            bgcolor: featured ? "#202a24" : undefined,
            color: featured ? "#f8e6b8" : undefined,
            "&:hover": { bgcolor: featured ? "#2b382f" : undefined },
          }}
          onClick={onPay}
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
        <Chip
          label='永久免费'
          sx={{
            mt: 2.5,
            alignSelf: "flex-start",
            bgcolor: "action.selected",
            color: "primary.dark",
            fontWeight: 700,
          }}
        />
      )}
    </Box>
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

/** submitPayment 创建并提交第三方支付表单。 */
function submitPayment(payment: any) {
  if (!payment?.submit_url) throw new Error("支付平台没有返回可打开的支付地址");
  const form = document.createElement("form");
  form.method = payment.submit_method || "POST";
  form.action = payment.submit_url;
  form.target = "_blank";
  Object.entries(payment.submit_fields || {}).forEach(([key, value]) => {
    const input = document.createElement("input");
    input.type = "hidden";
    input.name = key;
    input.value = String(value ?? "");
    form.appendChild(input);
  });
  document.body.appendChild(form);
  form.submit();
  form.remove();
}
