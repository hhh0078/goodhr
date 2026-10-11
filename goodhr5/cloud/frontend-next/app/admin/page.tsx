/** 本文件负责新版后台控制台概览、醒目新手引导和本地状态。 */
"use client";
import { AdminOverlayModal as Modal } from "@/components/admin/AdminOverlay";
import {
  Button,
  Card,
  Flex,
  Form,
  Input,
  Progress,
  Select,
  Tag,
  Typography,
} from "antd";
import {
  CheckCircleOutlined,
  CreditCardOutlined,
  WechatOutlined,
  DownloadOutlined,
  FileTextOutlined,
  LoadingOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SolutionOutlined,
} from "@ant-design/icons";
import type { CSSProperties } from "react";
import styles from "./heavy-ui.module.css";
import Link from "next/link";
import { useEffect, useMemo, useState, type ElementType } from "react";
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
import { type UserFlowState, type UserFlowStep } from "@/lib/user-flow";
type GuideStep = {
  key: UserFlowStep;
  title: string;
  description: string;
  tips: string[];
  href: string;
  action: string;
  icon: ElementType;
};
const guideSteps: GuideStep[] = [
  {
    key: "agent_detected",
    title: "确认本地程序已启动",
    description: "浏览器控制、截图和 OCR 都依赖本地 GoodHR 程序。",
    tips: [
      "启动本地 GoodHR 程序",
      "连接成功后会自动完成",
      "未安装时前往组件信息页面",
    ],
    href: "/admin/agent-download",
    action: "检查本地程序",
    icon: DownloadOutlined,
  },
  {
    key: "runtime_ready",
    title: "准备运行组件",
    description: "确认 Node 和浏览器组件已经安装，可以稳定执行岗位运行。",
    tips: ["启动本地程序", "按提示安装缺少的组件", "组件可用后会自动完成"],
    href: "/admin/agent-download",
    action: "检查运行组件",
    icon: DownloadOutlined,
  },
  {
    key: "position_created",
    title: "创建岗位管理",
    description: "岗位管理决定筛选条件、岗位要求和打招呼逻辑。",
    tips: ["进入岗位管理", "点击新建岗位", "填写岗位要求或关键词后保存"],
    href: "/admin/positions",
    action: "创建岗位",
    icon: SolutionOutlined,
  },
  {
    key: "platform_login_verified",
    title: "登录招聘平台",
    description: "开始岗位运行时确认招聘平台账号处于登录状态。",
    tips: ["点击开始岗位运行", "在打开的招聘平台完成登录", "回到 GoodHR 继续"],
    href: "/admin/positions",
    action: "检查平台登录",
    icon: PlayCircleOutlined,
  },
  {
    key: "position_started",
    title: "启动第一条岗位运行",
    description: "让本地程序成功接手并开始执行岗位运行。",
    tips: ["确认启动条件", "点击开始", "看到运行中状态"],
    href: "/admin/positions",
    action: "启动岗位运行",
    icon: PlayCircleOutlined,
  },
  {
    key: "first_resume_processed",
    title: "处理首份简历",
    description: "成功读取并处理第一位候选人的简历。",
    tips: ["保持岗位运行运行", "等待匹配到候选人", "处理成功后自动完成"],
    href: "/admin/positions",
    action: "查看岗位运行",
    icon: FileTextOutlined,
  },
  {
    key: "first_greet_success",
    title: "首次打招呼成功",
    description: "向第一位合适候选人成功发出招呼。",
    tips: ["保持平台登录", "等待筛选完成", "成功打招呼后流程跑通"],
    href: "/admin/positions",
    action: "查看进度",
    icon: SolutionOutlined,
  },
];
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
  const doneCount = guideSteps.filter(
    (item) => guideProgress.steps[item.key]?.status === "completed",
  ).length;
  const showGuide = guideProgress.state !== "completed";
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
        <OnboardingGuide progress={guideProgress} doneCount={doneCount} />
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
/** OnboardingGuide 展示云端真实业务事件计算的新手引导。 */
function OnboardingGuide({
  progress,
  doneCount,
}: {
  progress: UserFlowState;
  doneCount: number;
}) {
  const activeKey =
    guideSteps.find((item) => progress.steps[item.key]?.status !== "completed")
      ?.key || "";
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
      className={styles.s304}
    >
      <Flex className={styles.s305}>
        <div>
          <Flex className={styles.s306}>
            <Tag color={"var(--goodhr-primary)"} className={styles.s307}>
              {"新手必看"}
            </Tag>
            <Typography.Title
              level={2}
              className={`${styles.title} ${styles.s308}`}
            >
              跑通第一条招聘岗位运行
            </Typography.Title>
          </Flex>
          <Typography.Paragraph className={`${styles.text} ${styles.s309}`}>
            每完成一步都会自动记录；全部完成后，这个教程会自动隐藏。
          </Typography.Paragraph>
        </div>
        <div className={styles.s310}>
          <Flex className={styles.s311}>
            <Typography.Paragraph className={`${styles.text} ${styles.s312}`}>
              上手进度
            </Typography.Paragraph>
            <Typography.Paragraph className={`${styles.text} ${styles.s313}`}>
              {doneCount}/{guideSteps.length}
            </Typography.Paragraph>
          </Flex>
          <Progress
            percent={(doneCount / guideSteps.length) * 100}
            showInfo={false}
            strokeWidth={9}
            className={styles.s314}
          />
        </div>
      </Flex>
      <div className={styles.s315}>
        {guideSteps.map((step, index) => (
          <GuideCard
            key={step.key}
            step={step}
            index={index + 1}
            done={progress.steps[step.key]?.status === "completed"}
            active={activeKey === step.key}
          />
        ))}
      </div>
    </Card>
  );
}
/** GuideCard 展示一个新手步骤、完成状态和操作入口。 */
function GuideCard({
  step,
  index,
  done,
  active,
}: {
  step: GuideStep;
  index: number;
  done: boolean;
  active: boolean;
}) {
  const Icon = step.icon;
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
      className={styles.s316}
      style={
        {
          "--heavy-54": active
            ? "var(--goodhr-primary)"
            : "var(--goodhr-border)",
          "--heavy-55": done ? "rgba(255,255,255,.58)" : "#fff",
          "--heavy-56": active ? "0 10px 24px rgba(17, 17, 17, .11)" : "none",
          "--heavy-57": done ? "0.76" : "1",
        } as CSSProperties
      }
    >
      <Flex className={styles.s317}>
        <Flex className={styles.s318}>
          <div className={styles.s319}>
            <Icon aria-hidden="true" className={styles.s320} />
          </div>
          <Typography.Paragraph className={`${styles.text} ${styles.s321}`}>
            第 {index}步
          </Typography.Paragraph>
        </Flex>
        {done ? (
          <Tag
            icon={<CheckCircleOutlined aria-hidden="true" />}
            color={"success"}
          >
            {"已完成"}
          </Tag>
        ) : active ? (
          <Tag className={styles.s322}>{"当前需要"}</Tag>
        ) : null}
      </Flex>
      <Typography.Title level={3} className={`${styles.title} ${styles.s323}`}>
        {step.title}
      </Typography.Title>
      <Typography.Paragraph className={`${styles.text} ${styles.s324}`}>
        {step.description}
      </Typography.Paragraph>
      <ol className={styles.s325}>
        {step.tips.map((tip) => (
          <li key={tip}>{tip}</li>
        ))}
      </ol>
      {!done ? (
        <Button
          href={step.href}
          size="small"
          type={active ? "primary" : "default"}
          className={styles.s326}
        >
          {step.action}
        </Button>
      ) : null}
    </Card>
  );
}
