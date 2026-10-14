/** 本文件负责 邮件与批量调整 的 Ant Design 页面。 */
"use client";
import {
  Alert,
  Button,
  Checkbox,
  Flex,
  Form,
  Input,
  InputNumber,
  Progress,
  Radio,
  Select,
  Tag,
  Typography,
} from "antd";
import { MailOutlined, SendOutlined, SettingOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import dynamic from "next/dynamic";
import type { IEditorConfig, IToolbarConfig } from "@wangeditor/editor";
import "@wangeditor/editor/dist/css/style.css";
import { useEffect, useMemo, useState } from "react";
import {
  EmptyState,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import {
  CLOUD_API_BASE,
  cloudRequest,
  formatDate,
  getToken,
} from "@/lib/admin-api";
/** MailEditor 仅在浏览器加载编辑器及实例清理逻辑，避免编辑器库修改服务端全局对象。 */
const MailEditor = dynamic(() => import("./MailEditor"), { ssr: false });
const profileOptions = [
  ["hr", "企业HR"],
  ["headhunter", "猎头"],
  ["recruiting_manager", "招聘负责人"],
  ["owner", "老板"],
  ["female", "女性"],
  ["male", "男性"],
  ["boss", "Boss直聘"],
  ["liepin", "猎聘"],
  ["zhaopin", "智联"],
];
const flowOptions = [
  ["agent_detected", "下一步：启动本地程序"],
  ["runtime_ready", "下一步：准备运行组件"],
  ["position_created", "下一步：创建岗位"],
  ["platform_login_verified", "下一步：登录招聘平台"],
  ["position_started", "下一步：启动岗位运行"],
  ["first_resume_processed", "下一步：处理首份简历"],
  ["first_greet_success", "下一步：首次打招呼"],
  ["completed", "核心流程已跑通"],
];
type EmailBatch = {
  id: string;
  subject: string;
  total_count: number;
  sent_count: number;
  failed_count: number;
  opened_count: number;
  created_at: string;
};
type EmailRecipient = {
  id: string;
  email: string;
  status: string;
  opened: boolean;
};
type BatchAdjustResult = {
  email: string;
  days_adjusted: boolean;
  balance_adjusted: boolean;
  errors: string[];
};
/** AdminMailPage 展示超管邮件群发工作台。 */
export default function AdminMailPage() {
  const { user, notify, confirm } = useAdmin();
  const [operation, setOperation] = useState<"mail" | "adjust">("mail");
  const [subject, setSubject] = useState("");
  const [mailHtml, setMailHtml] = useState("");
  const [mode, setMode] = useState("filter");
  const [emails, setEmails] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [flows, setFlows] = useState<string[]>([]);
  const [lastLoginBeforeDays, setLastLoginBeforeDays] = useState("");
  const [batches, setBatches] = useState<EmailBatch[]>([]);
  const [activeBatch, setActiveBatch] = useState<EmailBatch | null>(null);
  const [recipients, setRecipients] = useState<EmailRecipient[]>([]);
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [adjusting, setAdjusting] = useState(false);
  const [adjustEmails, setAdjustEmails] = useState("");
  const [adjustDays, setAdjustDays] = useState("");
  const [adjustAmount, setAdjustAmount] = useState("");
  const [adjustReason, setAdjustReason] = useState("");
  const [adjustResults, setAdjustResults] = useState<BatchAdjustResult[]>([]);
  const progress = activeBatch?.total_count
    ? Math.round(
        ((activeBatch.sent_count + activeBatch.failed_count) /
          activeBatch.total_count) *
          100,
      )
    : 0;
  const toolbarConfig: Partial<IToolbarConfig> = useMemo(() => ({}), []);
  const editorConfig: Partial<IEditorConfig> = useMemo(
    () => ({
      placeholder: "写邮件正文，图片请用编辑器里的上传图片按钮。",
      MENU_CONF: {
        uploadImage: {
          server: `${CLOUD_API_BASE}/api/admin/emails/upload-image`,
          fieldName: "file",
          headers: { Authorization: `Bearer ${getToken()}` },
          maxFileSize: 5 * 1024 * 1024,
          allowedFileTypes: ["image/*"],
          customInsert(
            response: any,
            insertFn: (url: string, alt?: string, href?: string) => void,
          ) {
            const url = response?.absolute_url || response?.url;
            if (url) insertFn(url);
          },
        },
      },
    }),
    [],
  );
  useEffect(() => {
    if (user?.role === "super_admin") void load();
  }, [user?.role]);
  useEffect(() => {
    if (!activeBatch?.id || progress >= 100) return;
    const timer = window.setInterval(
      () => void loadBatch(activeBatch.id),
      1500,
    );
    return () => window.clearInterval(timer);
  }, [activeBatch?.id, progress]);
  /** load 读取最近邮件批次。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/admin/emails");
      setBatches(data.batches || []);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "邮件记录读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  /** loadBatch 读取指定邮件批次进度。 */
  async function loadBatch(id: string) {
    const data = await cloudRequest(`/api/admin/emails/${id}`);
    setActiveBatch(data.batch);
    setRecipients(data.recipients || []);
    await load();
  }
  /** send 发送邮件并开始轮询进度。 */
  async function send() {
    if (!subject.trim()) return notify("邮件标题要填一下", "warning");
    if (!mailHtml.replace(/<[^>]+>/g, "").trim() && !mailHtml.includes("<img"))
      return notify("正文还空着，我发不出去", "warning");
    setSending(true);
    try {
      const data = await cloudRequest("/api/admin/emails", {
        method: "POST",
        body: {
          subject,
          html: mailHtml,
          mode,
          emails: emails
            .split(/[\n,，]/)
            .map((item) => item.trim())
            .filter(Boolean),
          tags,
          flows,
          last_login_before_days: Math.max(0, Number(lastLoginBeforeDays) || 0),
        },
      });
      notify("邮件批次已创建，正在发送", "success");
      await loadBatch(data.batch.id);
    } catch (error) {
      notify(error instanceof Error ? error.message : "发送失败", "error");
    } finally {
      setSending(false);
    }
  }
  /** adjustBatch 批量调整指定用户或全部用户的会员天数和 AI 余额。 */
  async function adjustBatch() {
    const days = Number(adjustDays || 0);
    const amount = Number(adjustAmount || 0);
    if (!Number.isInteger(days))
      return notify("天数要填整数，我先没敢动", "warning");
    if (!Number.isFinite(amount))
      return notify("余额金额不太对，我先没敢动", "warning");
    if (days === 0 && amount === 0)
      return notify("天数和余额至少填一个，我才能开工", "warning");
    const emailItems = adjustEmails
      .split(/[\n,，;；\s]+/)
      .map((item) => item.trim())
      .filter(Boolean);
    if (!emailItems.length)
      return notify("请填写用户邮箱，输入 all 可以调整全部用户", "warning");
    const isAll = emailItems.some((item) => item.toLowerCase() === "all");
    const targetText = isAll
      ? "系统内全部用户"
      : `${emailItems.length} 个指定用户`;
    const changeText = [
      days ? `会员天数 ${days > 0 ? "+" : ""}${days}` : "",
      amount ? `AI 余额 ${amount > 0 ? "+" : ""}${amount} 元` : "",
    ]
      .filter(Boolean)
      .join("，");
    const ok = await confirm(
      "公主请确认批量调整",
      `将为${targetText}调整：${changeText}。每位用户都会收到通知邮件，确认继续吗？`,
    );
    if (!ok) return;
    setAdjusting(true);
    setAdjustResults([]);
    try {
      const data = await cloudRequest("/api/admin/users/batch-adjust", {
        method: "POST",
        body: {
          target: isAll ? "all" : "emails",
          emails: emailItems,
          days,
          amount_yuan: adjustAmount.trim(),
          reason: adjustReason.trim(),
        },
      });
      setAdjustResults(data.results || []);
      const failed = Number(data.failed_count || 0);
      notify(
        failed
          ? `调整完成：成功 ${data.success_count || 0}，失败 ${failed}`
          : `调整完成，${data.success_count || 0} 位用户已处理`,
        failed ? "warning" : "success",
      );
    } catch (error) {
      notify(error instanceof Error ? error.message : "批量调整失败", "error");
    } finally {
      setAdjusting(false);
    }
  }
  if (user?.role !== "super_admin")
    return (
      <SectionPanel>
        <EmptyState text="只有超级管理员可以访问此页面" />
      </SectionPanel>
    );
  return (
    <>
      <PageHeader
        title="邮件与批量调整"
        description="发邮件，或批量调整会员天数和 AI 余额。该通知的我都会认真通知。"
      />
      <Radio.Group
        value={operation}
        onChange={(event) => setOperation(event.target.value)}
        optionType="button"
        buttonStyle="solid"
        style={{ marginBottom: 16 }}
        options={[
          {
            value: "mail",
            label: (
              <>
                <MailOutlined aria-hidden="true" /> 发邮件
              </>
            ),
          },
          {
            value: "adjust",
            label: (
              <>
                <SettingOutlined aria-hidden="true" /> 调整天数、余额
              </>
            ),
          },
        ]}
      />
      {operation === "mail" ? (
        <div className={styles.layout18} style={{ display: "grid", gap: 16 }}>
          <SectionPanel>
            <Flex vertical={true} gap={2 * 8}>
              <Form.Item
                htmlFor="goodhr-mail-field-1"
                layout="vertical"
                style={{ marginBottom: 0, minWidth: 0 }}
                label={"邮件标题"}
              >
                <Input
                  id="goodhr-mail-field-1"
                  style={{ width: "100%" }}
                  value={subject}
                  onChange={(event) => setSubject(event.target.value)}
                />
              </Form.Item>
              <Form.Item
                htmlFor="goodhr-mail-field-2"
                layout="vertical"
                style={{ marginBottom: 0, minWidth: 0 }}
                label={"发送对象"}
              >
                <Select
                  id="goodhr-mail-field-2"
                  onChange={(value) => setMode(value)}
                  style={{ width: "100%" }}
                  value={mode}
                >
                  <Select.Option value="filter">
                    按邮箱 / 标记 / 流程卡点
                  </Select.Option>
                  <Select.Option value="all">全部用户</Select.Option>
                </Select>
              </Form.Item>
              {mode !== "all" ? (
                <>
                  <Form.Item
                    htmlFor="goodhr-mail-field-3"
                    layout="vertical"
                    style={{ marginBottom: 0, minWidth: 0 }}
                    label={"指定邮箱"}
                    extra={"一行一个，也支持逗号分隔。"}
                  >
                    <Input.TextArea
                      id="goodhr-mail-field-3"
                      autoSize={{ minRows: 3 }}
                      value={emails}
                      onChange={(event) => setEmails(event.target.value)}
                    />
                  </Form.Item>
                  <Form.Item
                    htmlFor="goodhr-mail-field-4"
                    layout="vertical"
                    style={{ marginBottom: 0, minWidth: 0 }}
                    label={"最近未登录天数"}
                    extra={"比如填 5，就是筛选至少 5 天没上线的用户。"}
                  >
                    <InputNumber<string>
                      id="goodhr-mail-field-4"
                      stringMode
                      min="1"
                      step={1}
                      style={{ width: "100%" }}
                      value={lastLoginBeforeDays}
                      onChange={(value) =>
                        setLastLoginBeforeDays(String(value ?? ""))
                      }
                    />
                  </Form.Item>
                  <OptionGroup
                    title="用户标记"
                    value={tags}
                    options={profileOptions}
                    onChange={setTags}
                  />
                  <OptionGroup
                    title="流程卡点"
                    value={flows}
                    options={flowOptions}
                    onChange={setFlows}
                  />
                </>
              ) : null}
              <MailEditor
                html={mailHtml}
                editorConfig={editorConfig}
                toolbarConfig={toolbarConfig}
                onChange={setMailHtml}
              />
              <Flex className="goodhr-toolbar-actions">
                <Button
                  type={"primary"}
                  icon={<SendOutlined aria-hidden="true" />}
                  disabled={sending}
                  onClick={() => void send()}
                >
                  {sending ? "正在创建批次" : "发送邮件"}
                </Button>
              </Flex>
            </Flex>
          </SectionPanel>
          <Flex vertical={true} gap={2 * 8}>
            <SectionPanel>
              <Typography.Title
                level={2}
                style={{
                  display: "block",
                  margin: 0,
                  fontSize: 18,
                  fontWeight: 780,
                }}
              >
                发送进度
              </Typography.Title>
              {activeBatch ? (
                <div style={{ marginTop: 12 }}>
                  <Typography.Text
                    style={{ display: "block", margin: 0, fontWeight: 720 }}
                  >
                    {activeBatch.subject}
                  </Typography.Text>
                  <Progress
                    percent={progress}
                    showInfo={false}
                    style={{ marginTop: 10, height: 8, borderRadius: 3996 }}
                  />
                  <Flex
                    gap={1 * 8}
                    style={{ marginTop: 8, flexWrap: "wrap", rowGap: 8 }}
                  >
                    <Tag>{`总数 ${activeBatch.total_count || 0}`}</Tag>
                    <Tag
                      color={"success"}
                    >{`成功 ${activeBatch.sent_count || 0}`}</Tag>
                    <Tag
                      color={"error"}
                    >{`失败 ${activeBatch.failed_count || 0}`}</Tag>
                    <Tag
                      color={"processing"}
                    >{`查看 ${activeBatch.opened_count || 0}`}</Tag>
                  </Flex>
                  <div
                    style={{ marginTop: 12, maxHeight: 220, overflow: "auto" }}
                  >
                    {recipients.map((item) => (
                      <Typography.Text
                        style={{
                          display: "block",
                          margin: 0,
                          paddingTop: 4,
                          paddingBottom: 4,
                          fontSize: 12,
                          borderBottom: "1px solid",
                          borderColor: "var(--goodhr-border)",
                        }}
                        key={item.id}
                      >
                        {item.email}· {statusText(item.status)}·{" "}
                        {item.opened ? "已查看" : "未查看"}
                      </Typography.Text>
                    ))}
                  </div>
                </div>
              ) : (
                <EmptyState text="发送后这里会显示进度" />
              )}
            </SectionPanel>
            <SectionPanel>
              <Typography.Title
                level={2}
                style={{
                  display: "block",
                  margin: 0,
                  fontSize: 18,
                  fontWeight: 780,
                }}
              >
                最近批次
              </Typography.Title>
              {batches.length ? (
                <Flex vertical={true} style={{ marginTop: 8 }}>
                  {batches.map((batch) => (
                    <Button
                      style={{
                        height: "auto",
                        whiteSpace: "normal",
                        justifyContent: "flex-start",
                        textAlign: "left",
                        paddingTop: 10,
                        paddingBottom: 10,
                      }}
                      key={batch.id}
                      onClick={() => void loadBatch(batch.id)}
                    >
                      <div style={{ minWidth: 0 }}>
                        <Typography.Text
                          ellipsis={true}
                          style={{
                            display: "block",
                            margin: 0,
                            fontWeight: 720,
                          }}
                        >
                          {batch.subject}
                        </Typography.Text>
                        <Typography.Text
                          style={{
                            display: "block",
                            margin: 0,
                            color: "var(--goodhr-text-secondary)",
                            fontSize: 12,
                          }}
                        >
                          {batch.sent_count}/{batch.total_count}成功 · 查看{" "}
                          {batch.opened_count || 0}·{" "}
                          {formatDate(batch.created_at)}
                        </Typography.Text>
                      </div>
                    </Button>
                  ))}
                </Flex>
              ) : (
                <EmptyState text="暂无邮件批次" />
              )}
            </SectionPanel>
          </Flex>
        </div>
      ) : (
        <div className={styles.layout26} style={{ display: "grid", gap: 16 }}>
          <SectionPanel>
            <Flex vertical={true} gap={2 * 8}>
              <Alert
                type={"info"}
                showIcon={true}
                title={
                  <>
                    会逐个复用用户管理里的调整方法，所以余额流水、到期时间和通知邮件都会正常执行。
                  </>
                }
              />
              <Form.Item
                htmlFor="goodhr-mail-field-5"
                layout="vertical"
                style={{ marginBottom: 0, minWidth: 0 }}
                label={"调整对象"}
                extra={"一行一个邮箱，也支持逗号分隔；输入 all 代表全部用户。"}
              >
                <Input.TextArea
                  id="goodhr-mail-field-5"
                  autoSize={{ minRows: 4 }}
                  style={{ width: "100%" }}
                  value={adjustEmails}
                  onChange={(event) => setAdjustEmails(event.target.value)}
                  placeholder={
                    "user@example.com\nother@example.com\n或输入 all"
                  }
                />
              </Form.Item>
              <div
                className={styles.layout19}
                style={{ display: "grid", gap: 16 }}
              >
                <Form.Item
                  htmlFor="goodhr-mail-field-6"
                  layout="vertical"
                  style={{ marginBottom: 0, minWidth: 0 }}
                  label={"调整会员天数"}
                  extra={"正数增加，负数扣减，不调整可留空。"}
                >
                  <InputNumber<string>
                    id="goodhr-mail-field-6"
                    stringMode
                    step={1}
                    style={{ width: "100%" }}
                    value={adjustDays}
                    onChange={(value) => setAdjustDays(String(value ?? ""))}
                  />
                </Form.Item>
                <Form.Item
                  htmlFor="goodhr-mail-field-7"
                  layout="vertical"
                  style={{ marginBottom: 0, minWidth: 0 }}
                  label={"调整 AI 余额（元）"}
                  extra={"正数增加，负数扣减，不调整可留空。"}
                >
                  <InputNumber<string>
                    id="goodhr-mail-field-7"
                    stringMode
                    step={"0.01"}
                    style={{ width: "100%" }}
                    value={adjustAmount}
                    onChange={(value) => setAdjustAmount(String(value ?? ""))}
                  />
                </Form.Item>
              </div>
              <Form.Item
                htmlFor="goodhr-mail-field-8"
                layout="vertical"
                style={{ marginBottom: 0, minWidth: 0 }}
                label={"备注"}
                extra={"备注会写进流水和通知邮件，方便以后对账。"}
              >
                <Input.TextArea
                  id="goodhr-mail-field-8"
                  autoSize={{ minRows: 2 }}
                  style={{ width: "100%" }}
                  value={adjustReason}
                  onChange={(event) => setAdjustReason(event.target.value)}
                  placeholder="例如：活动赠送、服务补偿"
                />
              </Form.Item>
              <Flex className="goodhr-toolbar-actions">
                <Button
                  type={"primary"}
                  icon={<SettingOutlined aria-hidden="true" />}
                  disabled={adjusting}
                  onClick={() => void adjustBatch()}
                >
                  {adjusting ? "正在逐个处理，请稍等" : "开始批量调整"}
                </Button>
              </Flex>
            </Flex>
          </SectionPanel>
          <SectionPanel>
            <Typography.Title
              level={2}
              style={{
                display: "block",
                margin: 0,
                fontSize: 18,
                fontWeight: 780,
              }}
            >
              调整结果
            </Typography.Title>
            {adjustResults.length ? (
              <Flex
                vertical={true}
                style={{ marginTop: 12, maxHeight: 520, overflow: "auto" }}
              >
                {adjustResults.map((item) => (
                  <div
                    style={{
                      paddingTop: 9.6,
                      paddingBottom: 9.6,
                      borderBottom: "1px solid",
                      borderColor: "var(--goodhr-border)",
                    }}
                    key={item.email}
                  >
                    <Typography.Text
                      style={{
                        display: "block",
                        margin: 0,
                        fontSize: 13,
                        fontWeight: 720,
                        wordBreak: "break-all",
                      }}
                    >
                      {item.email}
                    </Typography.Text>
                    <Flex
                      gap={0.75 * 8}
                      style={{ marginTop: 5.6, flexWrap: "wrap", rowGap: 6 }}
                    >
                      {item.days_adjusted ? (
                        <Tag color={"success"}>{"天数已调整"}</Tag>
                      ) : null}
                      {item.balance_adjusted ? (
                        <Tag color={"success"}>{"余额已调整"}</Tag>
                      ) : null}
                      {item.errors?.map((error) => (
                        <Tag
                          className={styles.layout22}
                          color={"error"}
                          style={{ height: "auto" }}
                          key={error}
                        >
                          {error}
                        </Tag>
                      ))}
                    </Flex>
                  </div>
                ))}
              </Flex>
            ) : (
              <EmptyState text="调整后，这里会认真汇报每位用户的结果" />
            )}
          </SectionPanel>
        </div>
      )}
    </>
  );
}
/** OptionGroup 展示可多选的筛选标签。 */
function OptionGroup({
  title,
  value,
  options,
  onChange,
}: {
  title: string;
  value: string[];
  options: string[][];
  onChange: (value: string[]) => void;
}) {
  return (
    <div>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginBottom: 6,
          fontSize: 13,
          fontWeight: 720,
        }}
      >
        {title}
      </Typography.Text>
      <Checkbox.Group
        value={value}
        onChange={(next) => onChange(next.map(String))}
        options={options.map(([key, label]) => ({ value: key, label }))}
        style={{ display: "flex", flexWrap: "wrap", gap: 12 }}
      />
    </div>
  );
}
/** statusText 返回邮件收件人的发送状态文案。 */
function statusText(value: string) {
  return value === "sent" ? "已发送" : value === "failed" ? "失败" : "等待中";
}
