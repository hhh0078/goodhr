/** 本文件负责新版后台控制台概览、醒目新手引导和本地状态。 */
"use client";
import { AdminOverlayModal as Modal } from "@/components/admin/AdminOverlay";
import {
  Button,
  Card,
  Flex,
  Form,
  Input,
  Select,
  Tag,
  Typography,
} from "antd";
import {
  CreditCardOutlined,
  WechatOutlined,
  FileTextOutlined,
  LoadingOutlined,
  ReloadOutlined,
  SolutionOutlined,
} from "@ant-design/icons";
import type { CSSProperties } from "react";
import styles from "./heavy-ui.module.css";
import { useEffect, useMemo, useState } from "react";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AIRechargeDialog from "@/components/payment/AIRechargeDialog";
import { AI_RECHARGE_POLICY, AI_RECHARGE_REFUND_POLICY, MAX_AI_RECHARGE_CENTS, MIN_AI_RECHARGE_CENTS, defaultAIRechargeAmount, rechargeAmountCents } from "@/lib/ai-recharge";
import {
  WechatPayDialog,
  wechatPaymentFromResponse,
  type WechatPaymentState,
} from "@/components/payment/WechatPayDialog";
import {
  cloudRequest,
  localRequest,
  requirePaymentLocalAgent,
} from "@/lib/admin-api";
import { type UserFlowState } from "@/lib/user-flow";
import { publishOnboardingEvent } from "@/lib/onboarding-events";
/** DashboardPage 展示用户当前最需要关注的招聘和本地运行状态。 */
export default function DashboardPage() {
  const {
    agentBase,
    user,
    wechatOfficialStatus,
    openWechatOfficial,
    refreshWechatOfficialStatus,
    subscription,
    refreshAgent,
    refreshSession,
    notify,
    confirm,
  } = useAdmin();
  const [positions, setPositions] = useState<any[]>([]);
  const [resumeCount, setResumeCount] = useState(0);
  const [runtime, setRuntime] = useState<any>({});
  const [aiConfig, setAIConfig] = useState<any>({});
  const [currentAIModel, setCurrentAIModel] = useState("");
  const [wallet, setWallet] = useState<any>({});
  const [rechargeAmountDraft, setRechargeAmount] = useState<string | null>(null);
  const rechargeAmount = rechargeAmountDraft ?? defaultAIRechargeAmount(wallet.recharge_promotions);
  const [recharging, setRecharging] = useState(false);
  const [rechargeDialogOpen, setRechargeDialogOpen] = useState(false);
  const [wechatPayment, setWechatPayment] = useState<WechatPaymentState | null>(
    null,
  );
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [selectedModel, setSelectedModel] = useState("");
  const [savingModel, setSavingModel] = useState(false);
  const [guideProgress, setGuideProgress] = useState<UserFlowState>({
    version: 2,
    stage: "agent_detected",
    stage_name: "启动本地程序",
    state: "pending",
    steps: {},
  });
  const [loading, setLoading] = useState(true);
  /** load 读取控制台概览和新手引导需要的云端数据。 */
  async function load() {
    setLoading(true);
    try {
      const results = await Promise.allSettled([
        cloudRequest("/api/positions"),
        cloudRequest("/api/candidates?page=1&page_size=1"),
        cloudRequest("/api/config/user-ai"),
        cloudRequest("/api/ai-wallet"),
        cloudRequest("/api/user-flow"),
      ]);
      if (results[0].status === "fulfilled")
        setPositions(results[0].value.positions || []);
      if (results[1].status === "fulfilled")
        setResumeCount(Number(results[1].value.total || 0));
      if (results[2].status === "fulfilled") {
        const config = results[2].value.config || {};
        setAIConfig(config);
        setCurrentAIModel(config.model || "");
      }
      if (results[3].status === "fulfilled") setWallet(results[3].value || {});
      if (results[4].status === "fulfilled")
        setGuideProgress(results[4].value.flow);
    } finally {
      setLoading(false);
    }
  }
  /** loadRuntime 在本地连接变化时读取运行组件状态。 */
  async function loadRuntime(baseURL = agentBase) {
    if (!baseURL) {
      setRuntime({});
      return;
    }
    try {
      const [runtimeResult, healthResult] = await Promise.allSettled([
        localRequest(baseURL, "/api/v1/runtime/status"),
        localRequest(baseURL, "/health"),
      ]);
      const runtimeData =
        runtimeResult.status === "fulfilled" ? runtimeResult.value : {};
      const healthData =
        healthResult.status === "fulfilled" ? healthResult.value : {};
      setRuntime({ ...runtimeData, ...healthData });
    } catch {
      setRuntime({});
    }
  }
  useEffect(() => {
    void load();
  }, []);
  useEffect(() => {
    void loadRuntime();
  }, [agentBase]);
  const summary = useMemo(
    () => ({
      today: positions.reduce(
        (sum, item) => sum + Number(item.today_greeted_count || 0),
        0,
      ),
      running: positions.filter((item) => item.status === "running").length,
    }),
    [positions],
  );
  const metrics = [
    ["今日打招呼", summary.today, SolutionOutlined],
    ["运行中岗位运行", summary.running, SolutionOutlined],
    ["简历数量", resumeCount, FileTextOutlined],
  ] as const;
  /** rechargeAI 创建内置 AI 余额充值订单。 */
  async function rechargeAI() {
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
    const accepted = await confirm(
      "支付前请确认",
      `AI 余额仅用于 GoodHR 内置 AI 调用。${AI_RECHARGE_POLICY}${AI_RECHARGE_REFUND_POLICY}需要开发票请联系作者处理，发票开出后该笔订单不支持退费。`,
    );
    if (!accepted) return;
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
  /** openModelDialog 打开内置 AI 模型选择弹框。 */
  function openModelDialog() {
    const models = Array.isArray(wallet.models) ? wallet.models : [];
    setSelectedModel(
      currentAIModel || wallet.default_model || models[0]?.id || "",
    );
    setModelDialogOpen(true);
  }
  /** saveAIModel 将选择的模型保存到个人 AI 配置。 */
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
  const showGuide =
    guideProgress?.tutorial_completed !== true &&
    guideProgress?.state !== "completed";
  return (
    <>
      <PageHeader
        title="控制台"
        description="今天的招聘进展、本地组件和常用账号都在这里。"
        actions={
          <>
            {wechatOfficialStatus?.enabled &&
            wechatOfficialStatus.configured &&
            !wechatOfficialStatus.bound ? (
              <Button
                type="primary"
                icon={<WechatOutlined aria-hidden="true" />}
                onClick={openWechatOfficial}
              >
                {wechatOfficialStatus.gift_granted ||
                user?.wechat_follow_gift_granted
                  ? "关注公众号"
                  : "关注领会员"}
              </Button>
            ) : null}
            <Button
              disabled={loading}
              onClick={() =>
                void Promise.all([
                  refreshAgent(),
                  load(),
                  loadRuntime(),
                  refreshWechatOfficialStatus(),
                ]).catch((error) =>
                  notify(
                    error instanceof Error
                      ? error.message
                      : "状态暂时没同步，再刷新一次就好",
                    "warning",
                  ),
                )
              }
              type={"default"}
              icon={<ReloadOutlined aria-hidden="true" />}
            >
              刷新状态
            </Button>
          </>
        }
      />

      {showGuide ? (
        <OnboardingGuideEntry />
      ) : null}

      <div
        className={styles.s269}
        style={{ "--heavy-53": showGuide ? "20px" : "0px" } as CSSProperties}
      >
        {metrics.map(([label, value, Icon]) => (
          <Card
            key={label}
            styles={{
              body: {
                padding: 0,
                height: "100%",
                flex: 1,
                display: "flex",
                flexDirection: "column",
              },
            }}
            className={styles.s270}
          >
            <Flex className={styles.s271}>
              <Typography.Paragraph className={`${styles.text} ${styles.s272}`}>
                {label}
              </Typography.Paragraph>
              <Icon aria-hidden="true" className={styles.s273} />
            </Flex>
            <Typography.Paragraph className={`${styles.text} ${styles.s274}`}>
              {loading ? (
                <LoadingOutlined
                  aria-hidden="true"
                  spin
                  className={styles.s275}
                />
              ) : (
                value
              )}
            </Typography.Paragraph>
          </Card>
        ))}
      </div>
      <div className={styles.s276}>
        <SectionPanel>
          <Flex className={styles.s277}>
            <div>
              <Flex className={styles.s278}>
                <Typography.Title
                  level={2}
                  className={`${styles.title} ${styles.s279}`}
                >
                  本地程序
                </Typography.Title>
                <Tag color={agentBase ? "success" : "error"}>
                  {agentBase ? "已连接" : "未连接"}
                </Tag>
              </Flex>
              <Typography.Paragraph className={`${styles.text} ${styles.s280}`}>
                {agentBase
                  ? `${agentBase} · ${runtime.version || runtime.agent_version || "版本未知"} · ${subscription.active ? `${subscription.member_name} 剩${subscription.remaining_days}天` : "免费版"}`
                  : "尚未检测到本地程序"}
              </Typography.Paragraph>
            </div>
            <Flex className={styles.s281}>
              <Button
                href="/admin/agent-download"
                size="small"
                type={"primary"}
              >
                组件
              </Button>
              <Button href="/admin/local-data" size="small" type={"default"}>
                诊断
              </Button>
            </Flex>
          </Flex>
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
            className={styles.s282}
          >
            <Typography.Paragraph className={`${styles.text} ${styles.s283}`}>
              我小声提醒一下：由于浏览器限制，在浏览器内下载的文件请到“我的电脑
              - 下载”里查看。如果没有，请在以下目录内查看：
            </Typography.Paragraph>
            <Typography.Paragraph className={`${styles.text} ${styles.s284}`}>
              {runtime.downloadsDir ||
                runtime.downloads_dir ||
                "本地程序未返回下载目录"}
            </Typography.Paragraph>
          </Card>
        </SectionPanel>
      </div>
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
              onClick={saveAIModel}
              type={"primary"}
            >
              {savingModel ? "保存中" : "保存模型"}
            </Button>
          </Flex>
        }
      >
        <Form.Item
          layout="vertical"
          htmlFor="admin-field-1"
          label={"当前模型"}
          className={styles.s285}
          style={{ marginBottom: 0 }}
        >
          <Select
            value={selectedModel}
            onChange={(value) => setSelectedModel(String(value ?? ""))}
            id="admin-field-1"
            className={styles.s286}
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
      <AIRechargeDialog
        open={rechargeDialogOpen}
        amount={rechargeAmount}
        promotions={wallet.recharge_promotions}
        loading={recharging}
        onAmountChange={setRechargeAmount}
        onClose={() => setRechargeDialogOpen(false)}
        onConfirm={() => void rechargeAI()}
      />
      <WechatPayDialog
        payment={wechatPayment}
        onClose={() => setWechatPayment(null)}
        onPaid={async () => {
          setWechatPayment(null);
          notify("支付成功，AI 余额已经到账。", "success");
          await Promise.all([load(), refreshSession()]);
        }}
      />
    </>
  );
}
/** AIWalletCard 展示内置 AI 余额和充值入口。 */
function AIWalletCard({
  wallet,
  currentModel,
  loading,
  onOpenRechargeDialog,
  onOpenModelDialog,
}: {
  wallet: any;
  currentModel: string;
  loading: boolean;
  onOpenRechargeDialog: () => void;
  onOpenModelDialog: () => void;
}) {
  const modelLabel = currentModel || wallet.default_model || "未配置";
  return (
    <SectionPanel>
      <Flex className={styles.s292}>
        <Flex className={styles.s293}>
          <Flex className={styles.s294}>
            <CreditCardOutlined aria-hidden="true" className={styles.s295} />
            <Typography.Title
              level={2}
              className={`${styles.title} ${styles.s296}`}
            >
              AI 余额
            </Typography.Title>
          </Flex>
          <Button
            onClick={onOpenModelDialog}
            type="default"
            size="small"
            className={styles.s297}
          >{`当前模型：${modelLabel}`}</Button>
        </Flex>
        <Flex className={styles.s298}>
          <Typography.Paragraph className={`${styles.text} ${styles.s299}`}>
            ￥{wallet.balance || "0.0000"}
            {Number(wallet.bonus_balance_units || 0) > 0 ? (
              <Typography.Text type="secondary" style={{ display: "block", fontSize: 12 }}>
                含赠送 ￥{wallet.bonus_balance || "0.0000"}
              </Typography.Text>
            ) : null}
          </Typography.Paragraph>
          <Flex className={styles.s300}>
            <Button
              disabled={loading}
              onClick={onOpenRechargeDialog}
              type={"primary"}
              className={styles.s301}
            >
              {loading ? "下单中" : "充值"}
            </Button>
            <Button
              href="/admin/ai-records"
              type={"default"}
              className={styles.s302}
            >
              使用记录
            </Button>
          </Flex>
        </Flex>
        <Typography.Paragraph className={`${styles.text} ${styles.s303}`}>
          默认已接入 GoodHR 内置 AI，也可以去个人配置里换成自己的 Key。
        </Typography.Paragraph>
      </Flex>
    </SectionPanel>
  );
}
/** OnboardingGuideEntry 提供紧凑的新手教程入口，真实操作和总进度交给全局引导器。 */
function OnboardingGuideEntry() {
  return (
    <Card
      styles={{ body: { padding: 0 } }}
      className={styles.s304}
      data-testid="onboarding-dashboard-entry"
    >
      <Flex align="center" justify="space-between" gap={16} wrap>
        <div>
          <Typography.Title level={5} style={{ margin: 0 }}>
            新手必看 · 跟我跑通第一轮招聘
          </Typography.Title>
          <Typography.Paragraph type="secondary" style={{ margin: "6px 0 0" }}>
            从登录平台到第一条筛选结果，我一步步带路，进度会自动保存。
          </Typography.Paragraph>
          <Typography.Paragraph type="secondary" style={{ margin: "4px 0 0", fontSize: 12 }}>
            本地程序已准备好时约 5–10 分钟；首次安装时间另计。
          </Typography.Paragraph>
        </div>
        <Button
          type="primary"
          data-onboarding="dashboard-guide-start"
          onClick={() => publishOnboardingEvent({ type: "guide-requested" })}
        >
          带我跑一遍
        </Button>
      </Flex>
    </Card>
  );
}
