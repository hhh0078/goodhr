/** 本文件负责 管理员用户管理 的 Ant Design 页面。 */
"use client";
import {
  Button,
  Flex,
  Form,
  Input,
  InputNumber,
  Pagination,
  Select,
  Table,
  Tag,
  Typography,
} from "antd";
import { SearchOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import Link from "next/link";
import { useEffect, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { membershipName } from "@/lib/subscription";
type NotificationProfile = {
  completed?: boolean;
  user_type?: string;
  gender?: string;
  platforms?: string[];
  os?: string;
  browser?: string;
};
type AdminUserItem = {
  email: string;
  role: string;
  status: string;
  inviter_email?: string;
  created_at?: string;
  last_login_at?: string;
  ai_balance?: string;
  ai_balance_units?: number;
  ai_balance_cents?: number;
  subscription?: {
    member_type?: string;
    member_name?: string;
    expires_at?: string;
    active?: boolean;
  };
  agent?: {
    machine_id?: string;
    agent_version?: string;
  };
  notification_profile?: NotificationProfile;
  flow?: {
    stage?: string;
    stage_name?: string;
    state?: "pending" | "blocked" | "completed";
    reason_code?: string;
    message?: string;
  };
};
/** UsersPage 提供超级管理员用户列表和用户操作。 */
export default function UsersPage() {
  const { user, notify, confirm } = useAdmin();
  const [items, setItems] = useState<AdminUserItem[]>([]);
  const [stats, setStats] = useState<any>({});
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [form, setForm] = useState({
    email: "",
    days: "7",
    member_type: "max",
    reason: "",
  });
  const [balanceForm, setBalanceForm] = useState({
    email: "",
    amount_yuan: "10",
    reason: "补充AI余额",
  });
  const [dialogOpen, setDialogOpen] = useState(false);
  const [balanceDialogOpen, setBalanceDialogOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [adjusting, setAdjusting] = useState(false);
  const [adjustingBalance, setAdjustingBalance] = useState(false);
  /** load 按分页和关键词读取用户。 */
  async function load(
    nextPage = page,
    nextPageSize = pageSize,
    nextQuery = query,
  ) {
    setLoading(true);
    try {
      const params = new URLSearchParams({
        page: String(nextPage),
        page_size: String(nextPageSize),
      });
      if (nextQuery.trim()) params.set("q", nextQuery.trim());
      const data = await cloudRequest(`/api/admin/users?${params}`);
      setItems(data.users || []);
      setStats(data.stats || {});
      setTotal(Number(data.total || 0));
      setPage(Number(data.page || nextPage));
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "用户列表读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    if (user?.role === "super_admin") void load(1);
  }, [user]);
  /** openAdjust 打开会员天数调整弹框。 */
  function openAdjust(item: AdminUserItem, days: number) {
    setForm({
      email: item.email,
      days: String(days),
      member_type: item.subscription?.member_type === "plus" ? "plus" : "max",
      reason: days > 0 ? "补偿会员天数" : "扣减会员天数",
    });
    setDialogOpen(true);
  }
  /** openBalanceAdjust 打开 AI 余额调整弹框。 */
  function openBalanceAdjust(item: AdminUserItem) {
    setBalanceForm({
      email: item.email,
      amount_yuan: "10",
      reason: "补充AI余额",
    });
    setBalanceDialogOpen(true);
  }
  /** adjust 提交会员天数调整。 */
  async function adjust() {
    if (adjusting) return;
    const days = Number(form.days);
    if (!Number.isFinite(days) || days === 0)
      return notify("天数不能为 0，正数增加，负数减少", "warning");
    setAdjusting(true);
    try {
      await cloudRequest("/api/admin/users", {
        method: "POST",
        body: { ...form, days },
      });
      notify("会员时间已调整", "success");
      setDialogOpen(false);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "调整失败", "error");
    } finally {
      setAdjusting(false);
    }
  }
  /** adjustBalance 提交 AI 余额调整。 */
  async function adjustBalance() {
    if (adjustingBalance) return;
    const amount = Number(balanceForm.amount_yuan);
    if (!Number.isFinite(amount) || amount === 0)
      return notify("金额不能为 0，正数增加，负数减少", "warning");
    setAdjustingBalance(true);
    try {
      await cloudRequest("/api/admin/users/adjust-ai-balance", {
        method: "POST",
        body: balanceForm,
      });
      notify("AI余额已调整", "success");
      setBalanceDialogOpen(false);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "余额调整失败", "error");
    } finally {
      setAdjustingBalance(false);
    }
  }
  /** unbind 解除用户本地程序绑定。 */
  async function unbind(item: AdminUserItem) {
    const ok = await confirm(
      "确认解绑账号设备",
      `要解绑 ${item.email} 当前占用的全部电脑吗？解绑后，这些电脑就可以绑定其他账号。`,
    );
    if (!ok) return;
    try {
      await cloudRequest("/api/admin/users/unbind-agent", {
        method: "POST",
        body: { email: item.email },
      });
      notify("账号设备已解绑，这些电脑现在可以换绑了。", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "解绑失败", "error");
    }
  }
  /** resetSearch 清空搜索条件并返回第一页。 */
  function resetSearch() {
    setQuery("");
    setPage(1);
    void load(1, pageSize, "");
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
        title="用户管理"
        description="查看注册、登录、会员、用户画像和本地程序绑定情况。"
        actions={
          <RefreshButton loading={loading} onClick={() => void load()} />
        }
      />
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginBottom: 12,
          color: "var(--goodhr-text-secondary)",
          fontSize: 13,
        }}
      >
        今日注册 {Number(stats.today_registered_count || 0)}人，绑定程序{" "}
        {Number(stats.agent_binding_count || 0)}台
      </Typography.Text>
      <div
        className={styles.layout2}
        style={{
          display: "grid",
          gap: 10,
          marginBottom: 16,
          alignItems: "center",
        }}
      >
        <Form.Item
          htmlFor="goodhr-users-field-1"
          layout="vertical"
          style={{ marginBottom: 0, minWidth: 0 }}
          label={"搜索用户"}
        >
          <Input
            id="goodhr-users-field-1"
            size="small"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") void load(1);
            }}
            placeholder="邮箱、角色、状态或邀请人"
          />
        </Form.Item>
        <Form.Item
          htmlFor="goodhr-users-field-2"
          layout="vertical"
          style={{ marginBottom: 0, minWidth: 0 }}
          label={"每页"}
        >
          <Select
            id="goodhr-users-field-2"
            onChange={(value) => {
              const size = Number(value);
              setPageSize(size);
              void load(1, size);
            }}
            style={{ width: "100%" }}
            size="small"
            value={pageSize}
          >
            <Select.Option value={10}>10</Select.Option>
            <Select.Option value={20}>20</Select.Option>
            <Select.Option value={50}>50</Select.Option>
            <Select.Option value={100}>100</Select.Option>
          </Select>
        </Form.Item>
        <Button
          type={"primary"}
          icon={<SearchOutlined aria-hidden="true" />}
          onClick={() => void load(1)}
        >
          搜索
        </Button>
        <Button onClick={resetSearch}>重置</Button>
      </div>

      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        {items.length ? (
          <>
            <div className={styles.layout13}>
              <Table<AdminUserItem>
                rowKey="email"
                dataSource={items}
                loading={loading}
                pagination={false}
                size="small"
                scroll={{ x: 1200 }}
                columns={[
                  {
                    key: "identity",
                    title: "用户",
                    width: 300,
                    render: (_, item) => <UserIdentity item={item} />,
                  },
                  {
                    key: "subscription",
                    title: "会员",
                    width: 170,
                    render: (_, item) => <SubscriptionInfo item={item} />,
                  },
                  {
                    key: "balance",
                    title: "AI余额",
                    width: 110,
                    render: (_, item) => <BalanceInfo item={item} />,
                  },
                  {
                    key: "time",
                    title: "时间",
                    width: 180,
                    render: (_, item) => <TimeInfo item={item} />,
                  },
                  {
                    key: "agent",
                    title: "本地程序",
                    width: 150,
                    render: (_, item) => <AgentInfo item={item} />,
                  },
                  {
                    key: "flow",
                    title: "流程卡点",
                    width: 180,
                    render: (_, item) => <FlowInfo item={item} />,
                  },
                  {
                    key: "actions",
                    title: "操作",
                    width: 210,
                    render: (_, item) => (
                      <UserActions
                        item={item}
                        openAdjust={openAdjust}
                        openBalanceAdjust={openBalanceAdjust}
                        unbind={unbind}
                      />
                    ),
                  },
                ]}
              />
            </div>
            <Flex
              className={styles.layout14}
              vertical={true}
              gap={1.25 * 8}
              style={{ padding: 10 }}
            >
              {items.map((item) => (
                <UserCard
                  key={item.email}
                  item={item}
                  openAdjust={openAdjust}
                  openBalanceAdjust={openBalanceAdjust}
                  unbind={unbind}
                />
              ))}
            </Flex>
          </>
        ) : (
          <EmptyState text={loading ? "正在读取用户" : "暂无用户"} />
        )}
      </SectionPanel>

      <Flex
        className={styles.layout17}
        gap={2 * 8}
        style={{
          marginTop: 16,
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
          }}
        >
          共 {total}个用户，第 {page}/{" "}
          {Math.max(1, Math.ceil(total / pageSize))}页
        </Typography.Text>
        <Pagination
          current={page}
          total={total}
          pageSize={pageSize}
          onChange={(value) => void load(value)}
          showSizeChanger={false}
        />
      </Flex>

      <AdminDialog
        open={dialogOpen}
        title="调整会员时间"
        description={`当前用户：${form.email || "--"}`}
        confirmText="确认调整"
        loading={adjusting}
        onClose={() => setDialogOpen(false)}
        onConfirm={() => void adjust()}
      >
        <Flex vertical={true} gap={2 * 8}>
          <Form.Item
            htmlFor="goodhr-users-field-3"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"用户邮箱"}
          >
            <Input
              id="goodhr-users-field-3"
              style={{ width: "100%" }}
              value={form.email}
              disabled
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-users-field-4"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"调整天数"}
            extra={"正数增加，负数减少，不能为 0。"}
          >
            <InputNumber<string>
              id="goodhr-users-field-4"
              stringMode
              style={{ width: "100%" }}
              value={form.days}
              onChange={(value) =>
                setForm({
                  ...form,
                  days: String(value ?? ""),
                })
              }
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-users-field-5"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"会员类型"}
            extra={"Plus 不含自动回复，Max 支持自动回复。"}
          >
            <Select
              id="goodhr-users-field-5"
              onChange={(value) => setForm({ ...form, member_type: value })}
              style={{ width: "100%" }}
              value={form.member_type}
            >
              <Select.Option value="plus">Plus 基础版</Select.Option>
              <Select.Option value="max">Max 全能版</Select.Option>
            </Select>
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-users-field-6"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"调整原因"}
          >
            <Input
              id="goodhr-users-field-6"
              style={{ width: "100%" }}
              value={form.reason}
              onChange={(event) =>
                setForm({ ...form, reason: event.target.value })
              }
            />
          </Form.Item>
        </Flex>
      </AdminDialog>
      <AdminDialog
        open={balanceDialogOpen}
        title="调整AI余额"
        description={`当前用户：${balanceForm.email || "--"}`}
        confirmText="确认调整"
        loading={adjustingBalance}
        onClose={() => setBalanceDialogOpen(false)}
        onConfirm={() => void adjustBalance()}
      >
        <Flex vertical={true} gap={2 * 8}>
          <Form.Item
            htmlFor="goodhr-users-field-7"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"用户邮箱"}
          >
            <Input
              id="goodhr-users-field-7"
              style={{ width: "100%" }}
              value={balanceForm.email}
              disabled
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-users-field-8"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"调整金额"}
            extra={"单位是元，正数增加，负数减少。"}
          >
            <InputNumber<string>
              id="goodhr-users-field-8"
              stringMode
              style={{ width: "100%" }}
              value={balanceForm.amount_yuan}
              onChange={(value) =>
                setBalanceForm({
                  ...balanceForm,
                  amount_yuan: String(value ?? ""),
                })
              }
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-users-field-9"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"调整原因"}
          >
            <Input
              id="goodhr-users-field-9"
              style={{ width: "100%" }}
              value={balanceForm.reason}
              onChange={(event) =>
                setBalanceForm({ ...balanceForm, reason: event.target.value })
              }
            />
          </Form.Item>
        </Flex>
      </AdminDialog>
    </>
  );
}
/** UserCard 展示移动端用户卡片。 */
function UserCard({
  item,
  openAdjust,
  openBalanceAdjust,
  unbind,
}: {
  item: AdminUserItem;
  openAdjust: (item: AdminUserItem, days: number) => void;
  openBalanceAdjust: (item: AdminUserItem) => void;
  unbind: (item: AdminUserItem) => Promise<void>;
}) {
  const memberLabel = item.subscription?.active ? "会员有效" : "已过期";
  const agentLabel = item.agent?.machine_id ? "已连接" : "未连接";
  const flowLabel = userFlowLabel(item.flow);
  return (
    <div
      style={{
        padding: 12,
        border: "1px solid",
        borderColor: "var(--goodhr-border)",
        borderRadius: "8px",
        backgroundColor: "#fff",
        boxShadow: "0 8px 20px rgba(17, 17, 17, 0.06)",
      }}
    >
      <Flex vertical={true} gap={0.75 * 8} style={{ minWidth: 0 }}>
        <Typography.Text
          ellipsis={true}
          style={{
            display: "block",
            margin: 0,
            fontFamily: "monospace",
            fontSize: 14,
            fontWeight: 800,
            color: "var(--goodhr-text)",
          }}
          title={item.email}
        >
          {item.email}
        </Typography.Text>
        <Flex gap={0.75 * 8} style={{ flexWrap: "wrap", rowGap: 6 }}>
          <SmallTag label={roleText(item.role)} />
          <SmallTag label={statusText(item.status)} />
          {profileTags(item.notification_profile)
            .slice(0, 4)
            .map((tag) => (
              <SmallTag key={tag} label={tag} />
            ))}
        </Flex>
        {item.inviter_email ? (
          <Typography.Text
            ellipsis={true}
            style={{
              display: "block",
              margin: 0,
              color: "var(--goodhr-text-secondary)",
              fontSize: 11,
            }}
            title={item.inviter_email}
          >
            邀请：{item.inviter_email}
          </Typography.Text>
        ) : null}
      </Flex>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: 8,
          marginTop: 12,
        }}
      >
        <MobileInfoBlock
          title="会员"
          lines={[
            memberLabel,
            item.subscription?.member_name ||
              membershipName(item.subscription?.member_type),
            formatDate(item.subscription?.expires_at),
          ]}
          strong={item.subscription?.active}
        />
        <MobileInfoBlock
          title="注册 / 登录"
          lines={[
            `注册：${formatDate(item.created_at)}`,
            `登录：${formatDate(item.last_login_at) || "暂无"}`,
          ]}
        />
        <MobileInfoBlock
          title="AI余额"
          lines={[`￥${item.ai_balance || "0.00"}`]}
          strong={Number(item.ai_balance_units || 0) > 0}
        />
        <MobileInfoBlock
          title="本地程序"
          lines={[agentLabel, item.agent?.agent_version || "暂无版本"]}
          strong={Boolean(item.agent?.machine_id)}
        />
        <MobileInfoBlock
          title="流程卡点"
          lines={[flowLabel]}
          strong={item.flow?.state === "completed"}
        />
      </div>
      <div
        style={{
          marginTop: 10,
          paddingTop: 8,
          borderTop: "1px solid",
          borderColor: "var(--goodhr-border)",
        }}
      >
        <UserActions
          item={item}
          openAdjust={openAdjust}
          openBalanceAdjust={openBalanceAdjust}
          unbind={unbind}
        />
      </div>
    </div>
  );
}
/** MobileInfoBlock 展示移动端卡片里的简短信息块。 */
function MobileInfoBlock({
  title,
  lines,
  strong,
}: {
  title: string;
  lines: (string | undefined)[];
  strong?: boolean;
}) {
  return (
    <div
      style={{
        padding: 8,
        borderRadius: "8px",
        backgroundColor: "var(--goodhr-hover)",
        minWidth: 0,
      }}
    >
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginBottom: 4,
          fontSize: 11,
          color: "var(--goodhr-text-secondary)",
        }}
      >
        {title}
      </Typography.Text>
      <Flex vertical={true} gap={0.25 * 8}>
        {lines.filter(Boolean).map((line, index) => (
          <Typography.Text
            ellipsis={true}
            style={{
              display: "block",
              margin: 0,
              fontSize: index === 0 ? 12 : 11,
              fontWeight: index === 0 ? 720 : 400,
              color:
                index === 0 && strong
                  ? "var(--goodhr-primary)"
                  : index === 0
                    ? "var(--goodhr-text)"
                    : "var(--goodhr-text-secondary)",
            }}
            key={`${title}-${index}`}
            title={line}
          >
            {line}
          </Typography.Text>
        ))}
      </Flex>
    </div>
  );
}
/** UserIdentity 展示邮箱、角色和通知画像标签。 */
function UserIdentity({ item }: { item: AdminUserItem }) {
  return (
    <Flex vertical={true} gap={0.75 * 8} style={{ minWidth: 0 }}>
      <Typography.Text
        ellipsis={true}
        style={{
          display: "block",
          margin: 0,
          fontFamily: "monospace",
          fontSize: 13,
          fontWeight: 760,
        }}
        title={item.email}
      >
        {item.email}
      </Typography.Text>
      <Flex gap={0.75 * 8} style={{ flexWrap: "wrap", rowGap: 6 }}>
        <SmallTag label={roleText(item.role)} />
        <SmallTag label={statusText(item.status)} />
        {profileTags(item.notification_profile).map((tag) => (
          <SmallTag key={tag} label={tag} />
        ))}
      </Flex>
      {item.inviter_email ? (
        <Typography.Text
          ellipsis={true}
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 11,
          }}
          title={item.inviter_email}
        >
          邀请：{item.inviter_email}
        </Typography.Text>
      ) : null}
    </Flex>
  );
}
/** SubscriptionInfo 展示会员状态和到期时间。 */
function SubscriptionInfo({ item }: { item: AdminUserItem }) {
  return (
    <Flex vertical={true} gap={0.5 * 8}>
      <Tag
        color={item.subscription?.active ? "success" : undefined}
        style={{ width: "fit-content" }}
      >
        {item.subscription?.active ? "会员有效" : "已过期"}
      </Tag>
      <Typography.Text style={{ display: "block", margin: 0, fontSize: 12 }}>
        {item.subscription?.member_name ||
          membershipName(item.subscription?.member_type)}
      </Typography.Text>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          color: "var(--goodhr-text-secondary)",
          fontSize: 11,
        }}
      >
        {formatDate(item.subscription?.expires_at)}
      </Typography.Text>
    </Flex>
  );
}
/** BalanceInfo 展示用户内置 AI 余额。 */
function BalanceInfo({ item }: { item: AdminUserItem }) {
  return (
    <Link
      style={{
        color: "inherit",
        textDecoration: "none",
        display: "flex",
        flexDirection: "column",
      }}
      href={`/admin/ai-records?email=${encodeURIComponent(item.email)}`}
    >
      <Typography.Text
        style={{ display: "block", margin: 0, fontSize: 12, fontWeight: 720 }}
      >
        AI余额
      </Typography.Text>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          color:
            Number(item.ai_balance_units || 0) > 0
              ? "var(--goodhr-primary)"
              : "var(--goodhr-text-secondary)",
          fontSize: 13,
          fontWeight: 780,
        }}
      >
        ￥{item.ai_balance || "0.00"}
      </Typography.Text>
    </Link>
  );
}
/** TimeInfo 展示注册时间和最近登录时间。 */
function TimeInfo({ item }: { item: AdminUserItem }) {
  return (
    <Flex vertical={true} gap={0.5 * 8}>
      <Typography.Text
        style={{ display: "block", margin: 0, fontSize: 12, fontWeight: 720 }}
      >
        注册 / 最近登录
      </Typography.Text>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          color: "var(--goodhr-text-secondary)",
          fontSize: 11,
        }}
      >
        注册：{formatDate(item.created_at)}
      </Typography.Text>
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          color: "var(--goodhr-text-secondary)",
          fontSize: 11,
        }}
      >
        登录：{formatDate(item.last_login_at) || "暂无"}
      </Typography.Text>
    </Flex>
  );
}
/** AgentInfo 展示本地程序连接状态。 */
function AgentInfo({ item }: { item: AdminUserItem }) {
  return (
    <Flex vertical={true} gap={0.5 * 8}>
      <Typography.Text
        style={{ display: "block", margin: 0, fontSize: 12, fontWeight: 720 }}
      >
        本地程序
      </Typography.Text>
      <Typography.Text
        ellipsis={true}
        style={{ display: "block", margin: 0, fontSize: 12 }}
      >
        {item.agent?.machine_id
          ? String(item.agent.machine_id).slice(0, 14)
          : "未连接"}
      </Typography.Text>
      <Typography.Text
        ellipsis={true}
        style={{
          display: "block",
          margin: 0,
          color: "var(--goodhr-text-secondary)",
          fontSize: 11,
        }}
      >
        {item.agent?.agent_version || "暂无版本"}
      </Typography.Text>
    </Flex>
  );
}
/** FlowInfo 展示用户当前流程卡点。 */
function FlowInfo({ item }: { item: AdminUserItem }) {
  const label = userFlowLabel(item.flow);
  return (
    <Flex vertical={true} gap={0.5 * 8}>
      <Typography.Text
        style={{ display: "block", margin: 0, fontSize: 12, fontWeight: 720 }}
      >
        流程卡点
      </Typography.Text>
      <Tag
        color={
          item.flow?.state === "completed"
            ? "success"
            : item.flow?.state === "blocked"
              ? "error"
              : "warning"
        }
        style={{ width: "fit-content", maxWidth: "100%" }}
      >
        {label}
      </Tag>
    </Flex>
  );
}
/** userFlowLabel 把流程快照转换成管理端可直接判断的短文案。 */
function userFlowLabel(flow?: AdminUserItem["flow"]) {
  if (!flow) return "暂无";
  if (flow.state === "completed") return "核心流程已跑通";
  if (flow.state === "blocked")
    return `卡在：${flow.stage_name || flow.stage || "未知步骤"}${flow.message ? ` · ${flow.message}` : ""}`;
  return `下一步：${flow.stage_name || flow.stage || "启动本地程序"}`;
}
/** UserActions 展示用户管理操作按钮。 */
function UserActions({
  item,
  openAdjust,
  openBalanceAdjust,
  unbind,
}: {
  item: AdminUserItem;
  openAdjust: (item: AdminUserItem, days: number) => void;
  openBalanceAdjust: (item: AdminUserItem) => void;
  unbind: (item: AdminUserItem) => Promise<void>;
}) {
  return (
    <Flex gap={0.5 * 8} style={{ flexWrap: "wrap", rowGap: 6 }}>
      <Button size="small" onClick={() => openAdjust(item, 7)}>
        天数
      </Button>
      <Button size="small" onClick={() => openBalanceAdjust(item)}>
        调余额
      </Button>
      <Button
        size="small"
        href={`/admin/ai-records?email=${encodeURIComponent(item.email)}`}
      >
        记录
      </Button>
      <Button danger={true} size="small" onClick={() => void unbind(item)}>
        解绑设备
      </Button>
    </Flex>
  );
}
/** SmallTag 展示用户画像小标签。 */
function SmallTag({ label }: { label: string }) {
  return (
    <Tag
      variant="filled"
      style={{
        marginInlineEnd: 0,
        border: 0,
        paddingLeft: 6,
        paddingRight: 6,
        paddingTop: 2,
        paddingBottom: 2,
        borderRadius: "6px",
        backgroundColor: "var(--goodhr-primary-soft)",
        color: "var(--goodhr-primary-dark)",
        fontSize: 11,
        fontWeight: "inherit",
        lineHeight: 1.55,
      }}
    >
      {label}
    </Tag>
  );
}
/** profileTags 将通知画像转换为用户标签。 */
function profileTags(profile?: NotificationProfile) {
  if (!profile) return ["未填画像"];
  const tags = [
    userTypeText(profile.user_type),
    genderText(profile.gender),
    profile.os,
    profile.browser,
    ...(profile.platforms || []),
  ].filter(Boolean) as string[];
  return tags.length ? tags.slice(0, 8) : ["未填画像"];
}
/** roleText 将后端角色转换为中文。 */
function roleText(role: string) {
  return role === "super_admin" ? "超管" : role === "admin" ? "管理员" : "成员";
}
/** statusText 将用户状态转换为中文。 */
function statusText(status: string) {
  return status === "pending"
    ? "待激活"
    : status === "disabled"
      ? "已停用"
      : "正常";
}
/** userTypeText 将用户身份画像转换为中文。 */
function userTypeText(value?: string) {
  return value === "headhunter"
    ? "猎头"
    : value === "hr"
      ? "企业HR"
      : value === "recruiting_manager"
        ? "招聘负责人"
        : value === "owner"
          ? "老板/管理者"
          : "";
}
/** genderText 将性别画像转换为中文。 */
function genderText(value?: string) {
  return value === "female"
    ? "女"
    : value === "male"
      ? "男"
      : value === "unknown"
        ? "不方便说"
        : "";
}
