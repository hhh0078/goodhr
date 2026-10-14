/** 本文件负责 团队成员管理 的 Ant Design 页面。 */
"use client";
import { Button, Flex, Form, Input, Table, Tag, Typography } from "antd";
import { EditOutlined, MailOutlined, UserAddOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  EmptyState,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";
type TeamMember = {
  invitation_id?: string;
  email: string;
  role: "admin" | "user";
  status: "active" | "pending";
  invited_by?: string;
  created_at?: string;
  today_greeted_count?: number;
  registered?: boolean;
  is_owner?: boolean;
};
/** TeamPage 管理当前团队成员并展示今日招呼数量。 */
export default function TeamPage() {
  const { notify, confirm } = useAdmin();
  const [members, setMembers] = useState<TeamMember[]>([]);
  const [todayGreetedCount, setTodayGreetedCount] = useState(0);
  const [canManage, setCanManage] = useState(false);
  const [loading, setLoading] = useState(false);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState("user");
  const [inviteOpen, setInviteOpen] = useState(false);
  const [inviteLoading, setInviteLoading] = useState(false);
  const [editingMember, setEditingMember] = useState<TeamMember | null>(null);
  const [editingRole, setEditingRole] = useState("user");
  const [editLoading, setEditLoading] = useState(false);
  /** load 读取团队成员、待确认邀请和今日招呼汇总。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/tenants/members");
      setMembers(Array.isArray(data.members) ? data.members : []);
      setTodayGreetedCount(Math.max(0, Number(data.today_greeted_count) || 0));
      setCanManage(Boolean(data.can_manage));
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "团队成员读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  /** invite 创建或重新发送团队邀请。 */
  async function invite() {
    if (!email.trim()) return notify("请填写成员邮箱", "warning");
    setInviteLoading(true);
    try {
      const data = await cloudRequest("/api/tenants/invite", {
        method: "POST",
        body: { email: email.trim(), role },
      });
      setEmail("");
      setInviteOpen(false);
      notify(
        data.resent
          ? "邀请重新发出去了，这次我再认真敲一次门。"
          : "邀请已经发出，等对方本人点头就能加入。",
        "success",
      );
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "邀请失败", "error");
      await load();
    } finally {
      setInviteLoading(false);
    }
  }
  /** openEditor 打开成员或待确认邀请的编辑弹框。 */
  function openEditor(member: TeamMember) {
    setEditingMember(member);
    setEditingRole(member.role || "user");
  }
  /** saveMemberRole 保存成员或待确认邀请的角色。 */
  async function saveMemberRole() {
    if (!editingMember || editLoading) return;
    setEditLoading(true);
    try {
      const path =
        editingMember.status === "pending"
          ? `/api/tenants/invitations/${encodeURIComponent(editingMember.invitation_id || "")}`
          : `/api/tenants/members/${encodeURIComponent(editingMember.email)}`;
      await cloudRequest(path, { method: "PUT", body: { role: editingRole } });
      notify("成员身份已经更新，我记清楚了。", "success");
      setEditingMember(null);
      await load();
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "成员身份更新失败",
        "error",
      );
    } finally {
      setEditLoading(false);
    }
  }
  /** resendInvitation 重新发送当前待确认邀请邮件。 */
  async function resendInvitation() {
    if (!editingMember?.invitation_id || editLoading) return;
    setEditLoading(true);
    try {
      await cloudRequest(
        `/api/tenants/invitations/${encodeURIComponent(editingMember.invitation_id)}/resend`,
        { method: "POST" },
      );
      notify("邀请重新发出去了，我这次又礼貌敲了下门。", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "邀请重发失败", "error");
    } finally {
      setEditLoading(false);
    }
  }
  /** cancelInvitation 取消待确认邀请，之后仍然可以重新邀请。 */
  async function cancelInvitation() {
    if (!editingMember?.invitation_id || editLoading) return;
    const accepted = await confirm(
      "取消这次邀请",
      `要取消发给 ${editingMember.email} 的邀请吗？以后想起来还能重新邀请，我不会把门焊死。`,
    );
    if (!accepted) return;
    setEditLoading(true);
    try {
      await cloudRequest(
        `/api/tenants/invitations/${encodeURIComponent(editingMember.invitation_id)}`,
        { method: "DELETE" },
      );
      notify("这次邀请已取消，后面仍然可以重新发。", "success");
      setEditingMember(null);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "取消邀请失败", "error");
    } finally {
      setEditLoading(false);
    }
  }
  /** removeMember 把成员移出团队，但保留其账号和个人配置。 */
  async function removeMember() {
    if (!editingMember || editLoading) return;
    const accepted = await confirm(
      "我小声确认一下",
      `真的要把 ${editingMember.email} 移出团队吗？账号不会删除，团队里已经产生的简历也会留下。`,
    );
    if (!accepted) return;
    setEditLoading(true);
    try {
      await cloudRequest(
        `/api/tenants/members/${encodeURIComponent(editingMember.email)}`,
        { method: "DELETE" },
      );
      notify("成员已移出，账号和个人配置都还好好的。", "success");
      setEditingMember(null);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "移出成员失败", "error");
    } finally {
      setEditLoading(false);
    }
  }
  return (
    <>
      <PageHeader
        title="团队管理"
        description="成员加入需要本人确认，谁都不会被我偷偷搬进来。"
        actions={
          <>
            {canManage ? (
              <Button
                type={"primary"}
                icon={<UserAddOutlined aria-hidden="true" />}
                onClick={() => setInviteOpen(true)}
              >
                邀请成员
              </Button>
            ) : null}

          </>
        }
      />
      <div
        className={styles.layout4}
        style={{
          marginBottom: 12,
          paddingTop: 11.2,
          paddingBottom: 11.2,
          display: "flex",
          alignItems: "baseline",
          gap: 8,
          border: "1px solid",
          borderColor: "var(--goodhr-border)",
          borderRadius: "8px",
          backgroundColor: "var(--goodhr-primary-soft)",
        }}
      >
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 14,
          }}
        >
          今日团队打招呼
        </Typography.Text>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            fontSize: 24,
            fontWeight: 820,
            color: "var(--goodhr-primary)",
          }}
        >
          {todayGreetedCount}
        </Typography.Text>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 13,
          }}
        >
          次
        </Typography.Text>
      </div>
      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        <Table<TeamMember>
          rowKey={(member) => member.invitation_id || member.email}
          dataSource={members}
          loading={loading}
          pagination={false}
          size="small"
          scroll={{ x: 760 }}
          locale={{
            emptyText: (
              <EmptyState text="这里暂时只有你，邀请同事后我再认真排队" />
            ),
          }}
          columns={[
            {
              key: "email",
              title: "成员账号",
              width: 300,
              render: (_, member) => (
                <Flex vertical gap={4}>
                  <Typography.Text strong>
                    {member.email || "邮箱暂时没读出来"}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {member.status === "pending" ? "邀请时间" : "加入时间"}：
                    {formatDate(member.created_at) || "--"}
                  </Typography.Text>
                </Flex>
              ),
            },
            {
              key: "status",
              title: "状态",
              width: 180,
              render: (_, member) => (
                <Tag
                  color={member.status === "pending" ? "warning" : "success"}
                >
                  {member.status === "pending"
                    ? member.registered
                      ? "已注册，等待确认"
                      : "尚未注册，等待登录"
                    : member.is_owner
                      ? "团队所有者"
                      : "已加入"}
                </Tag>
              ),
            },
            {
              key: "greeted",
              title: "今日打招呼",
              align: "right",
              width: 140,
              render: (_, member) =>
                Math.max(0, Number(member.today_greeted_count) || 0),
            },
            {
              key: "actions",
              title: "操作",
              align: "right",
              width: 100,
              render: (_, member) =>
                canManage && !member.is_owner ? (
                  <Button
                    icon={<EditOutlined aria-hidden="true" />}
                    onClick={() => openEditor(member)}
                  >
                    编辑
                  </Button>
                ) : (
                  <Typography.Text type="secondary">无需操作</Typography.Text>
                ),
            },
          ]}
        />
      </SectionPanel>
      <AdminDialog
        open={inviteOpen}
        title="邀请团队成员"
        description="对方登录后还要本人确认，数据不会提前搬走。"
        confirmText="发送邀请"
        loading={inviteLoading}
        loadingText="发送中"
        confirmDisabled={!email.trim()}
        onClose={() => setInviteOpen(false)}
        onConfirm={() => void invite()}
      >
        <Flex vertical={true} gap={2.5 * 8}>
          <Form.Item
            htmlFor="goodhr-team-field-1"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"成员邮箱"}
          >
            <Input
              id="goodhr-team-field-1"
              style={{ width: "100%" }}
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </Form.Item>
          <ChoiceCards
            label="加入后的身份"
            value={role}
            onChange={(value) => setRole(String(value))}
            options={[
              {
                value: "user",
                label: "普通成员",
                description: "管理自己的岗位和简历，数据归入当前团队。",
              },
              {
                value: "admin",
                label: "团队管理员",
                description: "还可以邀请成员和管理团队设置。",
              },
            ]}
          />
        </Flex>
      </AdminDialog>
      <AdminDialog
        open={Boolean(editingMember)}
        title={
          editingMember?.status === "pending"
            ? "编辑待确认邀请"
            : "编辑团队成员"
        }
        description={editingMember?.email || ""}
        confirmText="保存身份"
        loading={editLoading}
        loadingText="处理中"
        onClose={() => setEditingMember(null)}
        onConfirm={() => void saveMemberRole()}
        extraActions={
          editingMember?.status === "pending" ? (
            <Flex gap={1 * 8}>
              <Button
                danger={true}
                disabled={editLoading}
                onClick={() => void cancelInvitation()}
              >
                取消邀请
              </Button>
              <Button
                disabled={editLoading}
                icon={<MailOutlined aria-hidden="true" />}
                onClick={() => void resendInvitation()}
              >
                重发邮件
              </Button>
            </Flex>
          ) : editingMember && !editingMember.is_owner ? (
            <Button
              danger={true}
              disabled={editLoading}
              onClick={() => void removeMember()}
            >
              移出团队
            </Button>
          ) : null
        }
      >
        <ChoiceCards
          label="成员身份"
          value={editingRole}
          onChange={(value) => setEditingRole(String(value))}
          options={[
            {
              value: "user",
              label: "普通成员",
              description: "管理自己的岗位和简历，数据归入当前团队。",
            },
            {
              value: "admin",
              label: "团队管理员",
              description: "可以邀请、编辑和移出团队成员。",
            },
          ]}
        />
      </AdminDialog>
    </>
  );
}
