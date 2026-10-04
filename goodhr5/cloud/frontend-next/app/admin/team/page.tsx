/** 本文件负责团队成员、待确认邀请、今日招呼统计和成员编辑操作。 */
"use client";

import EditRoundedIcon from "@mui/icons-material/EditRounded";
import MailOutlineRoundedIcon from "@mui/icons-material/MailOutlineRounded";
import PersonAddRoundedIcon from "@mui/icons-material/PersonAddRounded";
import {
  Box,
  Button,
  Chip,
  Divider,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
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
    if (!editingMember) return;
    setEditLoading(true);
    try {
      const path = editingMember.status === "pending"
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
    if (!editingMember?.invitation_id) return;
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
    if (!editingMember?.invitation_id) return;
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
    if (!editingMember) return;
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
                variant="contained"
                startIcon={<PersonAddRoundedIcon />}
                onClick={() => setInviteOpen(true)}
              >
                邀请成员
              </Button>
            ) : null}
            <RefreshButton loading={loading} onClick={() => void load()} />
          </>
        }
      />
      <Box
        sx={{
          mb: 1.5,
          px: { xs: 1.75, md: 2 },
          py: 1.4,
          display: "flex",
          alignItems: "baseline",
          gap: 1,
          border: "1px solid",
          borderColor: "divider",
          borderRadius: "8px",
          bgcolor: "action.hover",
        }}
      >
        <Typography sx={{ color: "text.secondary", fontSize: 14 }}>
          今日团队打招呼
        </Typography>
        <Typography sx={{ fontSize: 24, fontWeight: 820, color: "#2f6b45" }}>
          {todayGreetedCount}
        </Typography>
        <Typography sx={{ color: "text.secondary", fontSize: 13 }}>次</Typography>
      </Box>
      <SectionPanel sx={{ p: 0, overflow: "hidden", bgcolor: "background.paper" }}>
        {members.length ? (
          <>
            <Box
              sx={{
                display: { xs: "none", md: "grid" },
                gridTemplateColumns: "minmax(260px, 1fr) 140px 140px 90px",
                gap: 2,
                px: 2,
                py: 1.35,
                bgcolor: "action.hover",
                borderBottom: "1px solid",
                borderColor: "divider",
                "& p": { color: "text.secondary", fontSize: 12, fontWeight: 760 },
              }}
            >
              <Typography>成员账号</Typography>
              <Typography>状态</Typography>
              <Typography sx={{ textAlign: "right" }}>今日打招呼</Typography>
              <Typography sx={{ textAlign: "right" }}>操作</Typography>
            </Box>
            <Stack divider={<Divider flexItem />}>
              {members.map((member) => (
                <MemberRow
                  key={member.invitation_id || member.email}
                  member={member}
                  canManage={canManage}
                  onEdit={() => openEditor(member)}
                />
              ))}
            </Stack>
          </>
        ) : (
          <EmptyState
            text={loading ? "正在读团队名单，我尽量不把人看漏" : "这里暂时只有你，邀请同事后我再认真排队"}
          />
        )}
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
        <Stack spacing={2.5}>
          <TextField
            label="成员邮箱"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            fullWidth
          />
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
        </Stack>
      </AdminDialog>
      <AdminDialog
        open={Boolean(editingMember)}
        title={editingMember?.status === "pending" ? "编辑待确认邀请" : "编辑团队成员"}
        description={editingMember?.email || ""}
        confirmText="保存身份"
        loading={editLoading}
        loadingText="处理中"
        onClose={() => setEditingMember(null)}
        onConfirm={() => void saveMemberRole()}
        extraActions={
          editingMember?.status === "pending" ? (
            <Stack direction="row" spacing={1}>
              <Button color="error" onClick={() => void cancelInvitation()}>
                取消邀请
              </Button>
              <Button
                startIcon={<MailOutlineRoundedIcon />}
                onClick={() => void resendInvitation()}
              >
                重发邮件
              </Button>
            </Stack>
          ) : editingMember && !editingMember.is_owner ? (
            <Button color="error" onClick={() => void removeMember()}>
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

/** MemberRow 展示成员邮箱、邀请状态和今日打招呼数量。 */
function MemberRow({
  member,
  canManage,
  onEdit,
}: {
  member: TeamMember;
  canManage: boolean;
  onEdit: () => void;
}) {
  const pending = member.status === "pending";
  const statusLabel = pending
    ? member.registered
      ? "已注册，等待确认"
      : "尚未注册，等待登录"
    : member.is_owner
      ? "团队所有者"
      : "已加入";
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", md: "minmax(260px, 1fr) 140px 140px 90px" },
        gap: { xs: 1.25, md: 2 },
        px: 2,
        py: 1.65,
        alignItems: "center",
      }}
    >
      <Box sx={{ minWidth: 0 }}>
        <Typography sx={{ fontWeight: 760, overflowWrap: "anywhere" }}>
          {member.email || "邮箱暂时没读出来"}
        </Typography>
        <Typography sx={{ mt: 0.45, color: "text.secondary", fontSize: 12 }}>
          {pending ? "邀请时间" : "加入时间"}：{formatDate(member.created_at) || "--"}
        </Typography>
      </Box>
      <Box>
        <Chip
          size="small"
          label={statusLabel}
          color={pending ? "warning" : "success"}
          variant="outlined"
          sx={{ maxWidth: "100%" }}
        />
      </Box>
      <Stack direction="row" spacing={0.75} sx={{ justifyContent: { md: "flex-end" }, alignItems: "baseline" }}>
        <Typography sx={{ display: { md: "none" }, color: "text.secondary", fontSize: 13 }}>
          今日打招呼
        </Typography>
        <Typography sx={{ fontSize: 20, fontWeight: 800 }}>
          {Math.max(0, Number(member.today_greeted_count) || 0)}
        </Typography>
        <Typography sx={{ color: "text.secondary", fontSize: 12 }}>次</Typography>
      </Stack>
      <Box sx={{ textAlign: { md: "right" } }}>
        {canManage && !member.is_owner ? (
          <Button startIcon={<EditRoundedIcon />} onClick={onEdit}>
            编辑
          </Button>
        ) : (
          <Typography sx={{ color: "text.disabled", fontSize: 12 }}>无需操作</Typography>
        )}
      </Box>
    </Box>
  );
}
