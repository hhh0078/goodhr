/** 本文件负责新版后台个人 AI 接口、操作节奏和模拟休息配置。 */
"use client";
import {
  Alert,
  Button,
  Flex,
  Form,
  Input,
  InputNumber,
  Tabs,
  Typography,
} from "antd";
import {
  ApiOutlined,
  ArrowUpOutlined,
  BellOutlined,
  ClockCircleOutlined,
  ExperimentOutlined,
  LockOutlined,
  PlayCircleOutlined,
  RobotOutlined,
  SaveOutlined,
  SlidersOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import type { CSSProperties } from "react";
import styles from "../heavy-ui.module.css";
import Link from "next/link";
import { useEffect, useId, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import NotificationProfileDialog from "@/components/admin/NotificationProfileDialog";
const defaults = {
  base_url:
    "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
  model: "qwen3.7-plus",
  api_key: "",
  click_frequency: 80,
  detail_open_probability: 80,
  candidate_read_concurrency: 100,
  detail_open_delay_min: 1,
  detail_open_delay_max: 2,
  detail_close_delay_min: 0,
  detail_close_delay_max: 0,
  greet_before_delay_min: 1,
  greet_before_delay_max: 2,
  rest_after_candidates_min: 40,
  rest_after_candidates_max: 70,
  rest_times_min: 2,
  rest_times_max: 3,
  rest_duration_min: 2,
  rest_duration_max: 7,
};
/** normalizeAIBaseURL 补全 OpenAI 兼容的 Chat Completions 地址。 */
function normalizeAIBaseURL(baseURL: string) {
  const value = baseURL.trim().replace(/\/+$/, "");
  if (!value) return "";
  if (value.endsWith("/chat/completions")) return value;
  if (value.endsWith("/v1")) return `${value}/chat/completions`;
  return `${value}/v1/chat/completions`;
}
/** PersonalConfigPage 管理 AI 接口和模拟人工操作参数。 */
export default function PersonalConfigPage() {
  const { notify } = useAdmin();
  const [form, setForm] = useState({ ...defaults });
  const [keySet, setKeySet] = useState(false);
  const [loading, setLoading] = useState(false);
  const [profileOpenSignal, setProfileOpenSignal] = useState(0);
  const [activeTab, setActiveTab] = useState("builtin");
  const [passwordForm, setPasswordForm] = useState({
    old_password: "",
    new_password: "",
    confirm_password: "",
  });
  const [passwordSaving, setPasswordSaving] = useState(false);
  /** load 读取个人 AI 配置和操作偏好。 */
  async function load() {
    setLoading(true);
    try {
      const [aiData, preferenceData] = await Promise.all([
        cloudRequest("/api/config/user-ai"),
        cloudRequest("/api/config/user-preferences"),
      ]);
      const ai = aiData.config || {};
      const preference = preferenceData.config || {};
      setKeySet(Boolean(ai.api_key_set));
      setForm({
        ...defaults,
        ...preference,
        base_url: ai.base_url || defaults.base_url,
        model: ai.model || preference.ai_model || defaults.model,
        api_key: ai.api_key || "",
      });
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "个人配置读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  /** testAI 通过云端代理验证当前填写的 AI 接口。 */
  async function testAI() {
    if (!form.api_key.trim()) return notify("测试前请填写 AI Key", "warning");
    const baseURL = normalizeAIBaseURL(form.base_url);
    if (!baseURL || !form.model.trim())
      return notify("请填写 AI 地址和模型", "warning");
    setLoading(true);
    try {
      setForm((current) => ({ ...current, base_url: baseURL }));
      await cloudRequest("/api/config/test-ai", {
        method: "POST",
        body: {
          base_url: baseURL,
          model: form.model.trim(),
          api_key: form.api_key.trim(),
          temperature: 0,
          enabled: true,
        },
      });
      notify("AI 接口测试成功", "success");
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "AI 接口测试失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  /** save 保存 AI 配置和操作偏好。 */
  async function save() {
    const baseURL = normalizeAIBaseURL(form.base_url);
    if (!baseURL || !form.model.trim())
      return notify("请填写 AI 地址和模型", "warning");
    if (!keySet && !form.api_key.trim())
      return notify("请填写 AI Key", "warning");
    if (
      !Number.isInteger(form.candidate_read_concurrency) ||
      form.candidate_read_concurrency < 5 ||
      form.candidate_read_concurrency > 100
    ) {
      return notify("同时读取候选人卡片数请填写 5 到 100 的整数", "warning");
    }
    setLoading(true);
    try {
      setForm((current) => ({ ...current, base_url: baseURL }));
      await cloudRequest("/api/config/user-ai", {
        method: "PUT",
        body: {
          base_url: baseURL,
          model: form.model.trim(),
          api_key: form.api_key.trim(),
          temperature: 0,
          prompt_template: "",
          enabled: true,
        },
      });
      const { base_url: _baseURL, model, api_key: _key, ...preference } = form;
      await cloudRequest("/api/config/user-preferences", {
        method: "PUT",
        body: { ...preference, ai_model: model },
      });
      setKeySet(true);
      notify("个人配置已保存", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "保存配置失败", "error");
    } finally {
      setLoading(false);
    }
  }
  /** useBuiltinAI 切换为系统内置 AI，并把分配好的配置填入表单。 */
  async function useBuiltinAI() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/ai-wallet/use-builtin", {
        method: "POST",
      });
      const config = data.config || {};
      setForm((current) => ({
        ...current,
        base_url: config.base_url || current.base_url,
        model: config.model || current.model,
        api_key: config.api_key || current.api_key,
      }));
      setKeySet(Boolean(config.api_key));
      notify("已切到内置 AI，余额够的话我就能开工。", "success");
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "内置 AI 没切成功，我们再来一次。",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  /** setNumber 更新一个数字配置字段。 */
  function setNumber(key: keyof typeof defaults, value: string) {
    setForm((current) => ({ ...current, [key]: Number(value || 0) }));
  }
  /** changePassword 校验当前密码后更换登录密码。 */
  async function changePassword() {
    if (!passwordForm.old_password)
      return notify("先填一下当前密码，我确认是本人", "warning");
    if (!passwordForm.new_password) return notify("新密码还没填", "warning");
    if ([...passwordForm.new_password].length < 2)
      return notify("新密码至少需要 2 个字符", "warning");
    if (passwordForm.new_password !== passwordForm.confirm_password)
      return notify("两次输入的新密码不一样，再核对一下", "warning");
    setPasswordSaving(true);
    try {
      await cloudRequest("/api/auth/password/change", {
        method: "POST",
        body: {
          old_password: passwordForm.old_password,
          new_password: passwordForm.new_password,
        },
      });
      setPasswordForm({
        old_password: "",
        new_password: "",
        confirm_password: "",
      });
      notify("密码已经换好了，下次登录用新的", "success");
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "密码没换成，我们再来一次",
        "error",
      );
    } finally {
      setPasswordSaving(false);
    }
  }
  return (
    <>
      <NotificationProfileDialog openSignal={profileOpenSignal} />
      <PageHeader
        title="个人配置"
        description="设置 AI 接口和岗位运行操作节奏，保存后会用于本地岗位运行。"
        actions={
          <>
            <Button
              onClick={() => setProfileOpenSignal((value) => value + 1)}
              type={"default"}
              icon={<BellOutlined aria-hidden="true" />}
            >
              通知偏好
            </Button>
            <Button
              disabled={loading}
              onClick={() => void save()}
              type={"primary"}
              icon={<SaveOutlined aria-hidden="true" />}
            >
              {loading ? "处理中" : "保存配置"}
            </Button>
          </>
        }
      />

      <SectionPanel className={styles.s207}>
        <Tabs
          activeKey={activeTab}
          onChange={(value) => setActiveTab(String(value))}
          aria-label="个人配置分类"
          items={[
            { key: "builtin", label: "内置AI" },
            { key: "custom", label: "自定义AI" },
            { key: "timing", label: "随机时间" },
            { key: "other", label: "其他配置" },
            { key: "account", label: "账号安全" },
          ]}
          className={styles.s208}
        />
      </SectionPanel>

      {activeTab === "builtin" ? (
        <SectionPanel className={styles.s209}>
          <SectionTitle
            icon={<ThunderboltOutlined aria-hidden="true" />}
            title="内置 AI"
            description="不想研究接口也没关系，使用系统准备好的 AI 配置就能开始。"
          />
          <Alert
            type={"info"}
            showIcon
            description={
              <>
                {" "}
                内置 AI 会消耗账户中的 AI 余额，省去申请模型、填写地址和保存 Key
                的步骤。
              </>
            }
            className={styles.s210}
          />
          <Flex className={styles.s211}>
            <Button
              disabled={loading}
              onClick={() => void useBuiltinAI()}
              type={"primary"}
              icon={<ThunderboltOutlined aria-hidden="true" />}
              className={styles.s212}
            >
              使用内置 AI
            </Button>
            <Typography.Paragraph className={`${styles.text} ${styles.s213}`}>
              {keySet
                ? "当前已有可用的 AI 配置。"
                : "当前还没有可用的 AI 配置，请先点击使用内置 AI。"}
            </Typography.Paragraph>
          </Flex>
        </SectionPanel>
      ) : null}

      {activeTab === "custom" ? (
        <>
          <div className={styles.s214}>
            <QuickLink
              href="/videos"
              icon={<PlayCircleOutlined aria-hidden="true" />}
              eyebrow="新手推荐"
              title="查看视频教程"
              description="按视频一步步完成 AI 平台申请、接口填写、测试和保存。"
              primary
            />
            <QuickLink
              href="https://www.qianwenai.com/"
              external
              icon={<RobotOutlined aria-hidden="true" />}
              eyebrow="AI 接入"
              title="获取 AI 接口"
              description="前往千问平台申请多模态模型和 API Key。"
            />
          </div>

          <SectionPanel className={styles.s215}>
            <SectionTitle
              icon={<ApiOutlined aria-hidden="true" />}
              title="自定义 AI"
              description="填写自己的 AI 接口，建议先测试成功，再点击页面右上角保存。"
            />
            <Alert
              icon={<ApiOutlined aria-hidden="true" />}
              type={"info"}
              showIcon
              description={
                <>
                  {" "}
                  可接入兼容 OpenAI 格式的多模态模型，例如千问、硅基流动和
                  OpenAI。模型必须支持图片识别；DeepSeek
                  当前不支持图片输入，请不要用于详情 AI 识别。
                </>
              }
              className={styles.s216}
            />
            <div className={styles.s217}>
              <Form.Item
                layout="vertical"
                htmlFor="personal-config-field-1"
                label={"API 地址"}
                extra={
                  <> {"默认使用千问兼容 OpenAI 的 Chat Completions 地址。"} </>
                }
                style={{ marginBottom: 0 }}
              >
                <Input
                  value={form.base_url}
                  onChange={(event) =>
                    setForm({ ...form, base_url: event.target.value })
                  }
                  id="personal-config-field-1"
                  className={styles.s219}
                />
              </Form.Item>
              <Form.Item
                layout="vertical"
                htmlFor="personal-config-field-2"
                label={"模型名称"}
                extra={<> {"例如 qwen3.7-plus"} </>}
                style={{ marginBottom: 0 }}
              >
                <Input
                  value={form.model}
                  onChange={(event) =>
                    setForm({ ...form, model: event.target.value })
                  }
                  id="personal-config-field-2"
                  className={styles.s221}
                />
              </Form.Item>
              <Form.Item
                layout="vertical"
                htmlFor="personal-config-field-3"
                label={"API Key"}
                extra={
                  <> {"这里会明文显示当前保存的 Key，方便复制和修改。"} </>
                }
                className={styles.s222}
                style={{ marginBottom: 0 }}
              >
                <Input
                  value={form.api_key}
                  onChange={(event) =>
                    setForm({ ...form, api_key: event.target.value })
                  }
                  placeholder="请输入 API Key"
                  id="personal-config-field-3"
                  className={styles.s223}
                />
              </Form.Item>
            </div>
            <Flex className={styles.s224}>
              <Button
                disabled={loading}
                onClick={() => void testAI()}
                type={"primary"}
                icon={<ExperimentOutlined aria-hidden="true" />}
                className={styles.s225}
              >
                先测试 AI
              </Button>
              <Typography.Paragraph className={`${styles.text} ${styles.s226}`}>
                {keySet
                  ? "当前已有保存过的 AI Key。"
                  : "当前还没有保存 AI Key。"}
                测试成功后，请点击页面右上角保存配置。
              </Typography.Paragraph>
            </Flex>
          </SectionPanel>
        </>
      ) : null}

      {activeTab === "timing" ? (
        <div className={styles.s227}>
          <SectionPanel>
            <SectionTitle
              icon={<ClockCircleOutlined aria-hidden="true" />}
              title="操作节奏"
              description="在范围内随机等待，让岗位运行操作保持自然。"
            />
            <Flex className={styles.s228}>
              <CompactNumber
                label="点击频率"
                help="控制候选人操作的执行比例。"
                unit="%"
                value={form.click_frequency}
                onChange={(value) => setNumber("click_frequency", value)}
              />
              <CompactNumber
                label="详情查看概率"
                help="仅影响未设置关键词和排除词的任务；需要匹配关键词时会读取详情，避免漏筛。"
                unit="%"
                value={form.detail_open_probability}
                onChange={(value) =>
                  setNumber("detail_open_probability", value)
                }
              />
              <NumberRange
                label="打开详情前延时"
                help="点击候选人详情前随机等待。"
                unit="秒"
                min={form.detail_open_delay_min}
                max={form.detail_open_delay_max}
                onMin={(value) => setNumber("detail_open_delay_min", value)}
                onMax={(value) => setNumber("detail_open_delay_max", value)}
              />
              <NumberRange
                label="关闭详情前延时"
                help="详情提取完成后、关闭页面前随机等待。"
                unit="秒"
                min={form.detail_close_delay_min}
                max={form.detail_close_delay_max}
                onMin={(value) => setNumber("detail_close_delay_min", value)}
                onMax={(value) => setNumber("detail_close_delay_max", value)}
              />
              <NumberRange
                label="打招呼前延时"
                help="候选人通过筛选后、打招呼前随机等待。"
                unit="秒"
                min={form.greet_before_delay_min}
                max={form.greet_before_delay_max}
                onMin={(value) => setNumber("greet_before_delay_min", value)}
                onMax={(value) => setNumber("greet_before_delay_max", value)}
              />
            </Flex>
          </SectionPanel>
          <SectionPanel>
            <SectionTitle
              icon={<RobotOutlined aria-hidden="true" />}
              title="模拟休息"
              description="岗位运行会按配置间歇休息，避免长时间连续操作。"
            />
            <Flex className={styles.s229}>
              <NumberRange
                label="处理多少人后休息"
                help="例如设置 40 到 70，系统会随机选择人数。"
                unit="人"
                min={form.rest_after_candidates_min}
                max={form.rest_after_candidates_max}
                onMin={(value) => setNumber("rest_after_candidates_min", value)}
                onMax={(value) => setNumber("rest_after_candidates_max", value)}
              />
              <NumberRange
                label="单次岗位运行休息次数"
                help="达到本次随机次数后不再休息。"
                unit="次"
                min={form.rest_times_min}
                max={form.rest_times_max}
                onMin={(value) => setNumber("rest_times_min", value)}
                onMax={(value) => setNumber("rest_times_max", value)}
              />
              <NumberRange
                label="每次休息时长"
                help="每次休息会在此范围内随机，并写入岗位运行日志。"
                unit="分钟"
                min={form.rest_duration_min}
                max={form.rest_duration_max}
                onMin={(value) => setNumber("rest_duration_min", value)}
                onMax={(value) => setNumber("rest_duration_max", value)}
              />
            </Flex>
          </SectionPanel>
        </div>
      ) : null}

      {activeTab === "other" ? (
        <SectionPanel className={styles.s230}>
          <SectionTitle
            icon={<SlidersOutlined aria-hidden="true" />}
            title="候选人读取"
            description="控制本地程序同时读取多少张候选人卡片。"
          />
          <Alert
            type={"info"}
            showIcon
            description={
              <>
                {" "}
                默认 100，最多 100，最少
                5。如果你的电脑配置较低，可以把它调小一点，我会分批读完，不会少处理候选人。
              </>
            }
            className={styles.s231}
          />
          <Flex className={styles.s232}>
            <CompactNumber
              label="同时读取候选人卡片数"
              help="遇到姓名读取超时，可以先试试 10 或 20。数值越小越稳，读取时间也会稍长一点。"
              unit="张"
              value={form.candidate_read_concurrency}
              min={5}
              max={100}
              onChange={(value) =>
                setNumber("candidate_read_concurrency", value)
              }
            />
          </Flex>
        </SectionPanel>
      ) : null}

      {activeTab === "account" ? (
        <div className={styles.s233}>
          <SectionPanel>
            <SectionTitle
              icon={<LockOutlined aria-hidden="true" />}
              title="修改登录密码"
              description="需要输入当前密码，确认是本人之后才换。"
            />
            <Flex className={styles.s234}>
              <Form.Item
                layout="vertical"
                htmlFor="personal-config-field-4"
                label={"当前密码"}
                style={{ marginBottom: 0 }}
              >
                <Input.Password
                  value={passwordForm.old_password}
                  onChange={(event) =>
                    setPasswordForm((current) => ({
                      ...current,
                      old_password: event.target.value,
                    }))
                  }
                  size="small"
                  id="personal-config-field-4"
                  className={styles.s236}
                />
              </Form.Item>
              <Form.Item
                layout="vertical"
                htmlFor="personal-config-field-5"
                label={"新密码"}
                extra={<> {"至少 2 个字符，别和当前密码一样。"} </>}
                style={{ marginBottom: 0 }}
              >
                <Input.Password
                  value={passwordForm.new_password}
                  onChange={(event) =>
                    setPasswordForm((current) => ({
                      ...current,
                      new_password: event.target.value,
                    }))
                  }
                  size="small"
                  id="personal-config-field-5"
                  className={styles.s238}
                />
              </Form.Item>
              <Form.Item
                layout="vertical"
                htmlFor="personal-config-field-6"
                label={"再输入一次新密码"}
                style={{ marginBottom: 0 }}
              >
                <Input.Password
                  value={passwordForm.confirm_password}
                  onChange={(event) =>
                    setPasswordForm((current) => ({
                      ...current,
                      confirm_password: event.target.value,
                    }))
                  }
                  size="small"
                  id="personal-config-field-6"
                  className={styles.s240}
                />
              </Form.Item>
              <Button
                disabled={passwordSaving}
                onClick={() => void changePassword()}
                type={"primary"}
                className={styles.s241}
              >
                {passwordSaving ? "处理中" : "确认修改"}
              </Button>
            </Flex>
          </SectionPanel>
          <SectionPanel>
            <SectionTitle
              icon={<LockOutlined aria-hidden="true" />}
              title="密码忘了怎么办"
              description="在登录页点「忘记密码」，用邮箱验证码就能重设，不用找人。"
            />
            <Alert
              type={"info"}
              showIcon
              description={
                <> 重置验证码会发到你注册用的邮箱，5 分钟内有效，只能用一次。</>
              }
              className={styles.s242}
            />
          </SectionPanel>
        </div>
      ) : null}
    </>
  );
}
/** QuickLink 展示个人配置页的外部帮助入口。 */
function QuickLink({
  href,
  icon,
  eyebrow,
  title,
  description,
  external = false,
  primary = false,
}: {
  href: string;
  icon: React.ReactNode;
  eyebrow: string;
  title: string;
  description: string;
  external?: boolean;
  primary?: boolean;
}) {
  const content = (
    <Flex
      className={styles.s243}
      style={
        {
          "--heavy-38": primary ? "20px" : "16px",
          "--heavy-39": primary ? "20px" : "16px",
          "--heavy-40": primary ? "20px" : "16px",
          "--heavy-41": primary ? "140px" : "118px",
          "--heavy-42": primary
            ? "var(--goodhr-primary)"
            : "var(--goodhr-border)",
          "--heavy-43": primary
            ? "var(--goodhr-primary-soft)"
            : "var(--goodhr-hover)",
          "--heavy-44": primary ? "0 18px 44px rgba(17, 17, 17, .1)" : "none",
          "--heavy-45": primary
            ? "var(--goodhr-primary-soft)"
            : "var(--goodhr-hover)",
        } as CSSProperties
      }
    >
      <div
        className={styles.s244}
        style={
          {
            "--heavy-46": primary ? "58px" : "46px",
            "--heavy-47": primary ? "58px" : "46px",
            "--heavy-48": primary
              ? "var(--goodhr-primary)"
              : "var(--goodhr-primary-soft)",
            "--heavy-49": primary ? "#fff" : "var(--goodhr-primary)",
            "--heavy-50": primary ? "31px" : "24px",
          } as CSSProperties
        }
      >
        {icon}
      </div>
      <div className={styles.s245}>
        <Typography.Paragraph className={`${styles.text} ${styles.s246}`}>
          {eyebrow}
        </Typography.Paragraph>
        <Typography.Paragraph
          className={`${styles.text} ${styles.s247}`}
          style={{ "--heavy-51": primary ? "22px" : "17px" } as CSSProperties}
        >
          {title}
        </Typography.Paragraph>
        <Typography.Paragraph className={`${styles.text} ${styles.s248}`}>
          {description}
        </Typography.Paragraph>
      </div>
      <ArrowUpOutlined
        aria-hidden="true"
        className={styles.s249}
        style={
          {
            "--heavy-52": primary
              ? "var(--goodhr-primary)"
              : "var(--goodhr-text-secondary)",
          } as CSSProperties
        }
      />
    </Flex>
  );
  return external ? (
    <Typography.Link
      href={href}
      target="_blank"
      rel="noreferrer"
      className={styles.s250}
    >
      {content}
    </Typography.Link>
  ) : (
    <Link href={href} style={{ textDecoration: "none" }}>
      {content}
    </Link>
  );
}
/** SectionTitle 展示配置区域标题和说明。 */
function SectionTitle({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <Flex className={styles.s251}>
      <div className={styles.s252}>{icon}</div>
      <div>
        <Typography.Title
          level={2}
          className={`${styles.title} ${styles.s253}`}
        >
          {title}
        </Typography.Title>
        <Typography.Paragraph className={`${styles.text} ${styles.s254}`}>
          {description}
        </Typography.Paragraph>
      </div>
    </Flex>
  );
}
/** CompactNumber 展示一个带单位的紧凑数字配置。 */
function CompactNumber({
  label,
  help,
  unit,
  value,
  min,
  max,
  onChange,
}: {
  label: string;
  help: string;
  unit: string;
  value: number;
  min?: number;
  max?: number;
  onChange: (value: string) => void;
}) {
  const fieldID = useId();
  return (
    <div className={styles.s255}>
      <div>
        <Typography.Paragraph className={`${styles.text} ${styles.s256}`}>
          {label}
        </Typography.Paragraph>
        <Typography.Paragraph className={`${styles.text} ${styles.s257}`}>
          {help}
        </Typography.Paragraph>
      </div>
      <Form.Item
        layout="vertical"
        htmlFor={fieldID}
        style={{ marginBottom: 0 }}
      >
        <InputNumber
          aria-label={label}
          precision={0}
          value={value}
          onChange={(value) => onChange(String(value ?? ""))}
          size="small"
          id={fieldID}
          suffix={unit}
          min={min ?? 0}
          max={max ?? (unit === "%" ? 100 : undefined)}
          step={1}
          className={styles.s259}
        />
      </Form.Item>
    </div>
  );
}
/** NumberRange 展示一组带说明的最小值和最大值输入。 */
function NumberRange({
  label,
  help,
  unit,
  min,
  max,
  onMin,
  onMax,
}: {
  label: string;
  help: string;
  unit: string;
  min: number;
  max: number;
  onMin: (value: string) => void;
  onMax: (value: string) => void;
}) {
  const fieldID = useId();
  return (
    <div className={styles.s260}>
      <div>
        <Typography.Paragraph className={`${styles.text} ${styles.s261}`}>
          {label}
        </Typography.Paragraph>
        <Typography.Paragraph className={`${styles.text} ${styles.s262}`}>
          {help}
        </Typography.Paragraph>
      </div>
      <Flex className={styles.s263}>
        <Form.Item
          layout="vertical"
          htmlFor={`${fieldID}-min`}
          className={styles.s264}
          style={{ marginBottom: 0 }}
        >
          <InputNumber
            min={0}
            value={min}
            onChange={(value) => onMin(String(value ?? ""))}
            size="small"
            aria-label={`${label}最小值`}
            id={`${fieldID}-min`}
            className={styles.s265}
          />
        </Form.Item>
        <Typography.Paragraph className={`${styles.text} ${styles.s266}`}>
          至
        </Typography.Paragraph>
        <Form.Item
          layout="vertical"
          htmlFor={`${fieldID}-max`}
          className={styles.s267}
          style={{ marginBottom: 0 }}
        >
          <InputNumber
            min={0}
            value={max}
            onChange={(value) => onMax(String(value ?? ""))}
            size="small"
            aria-label={`${label}最大值`}
            id={`${fieldID}-max`}
            suffix={unit}
            className={styles.s268}
          />
        </Form.Item>
      </Flex>
    </div>
  );
}
