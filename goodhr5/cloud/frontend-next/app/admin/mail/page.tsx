/** 本文件负责超级管理员富文本邮件群发、图片上传、发送进度和查看记录。 */
"use client";

import SendRoundedIcon from "@mui/icons-material/SendRounded";
import { Box, Button, Chip, LinearProgress, MenuItem, Stack, TextField, ToggleButton, ToggleButtonGroup, Typography } from "@mui/material";
import type { IDomEditor, IEditorConfig, IToolbarConfig } from "@wangeditor/editor";
import "@wangeditor/editor/dist/css/style.css";
import { Editor, Toolbar } from "@wangeditor/editor-for-react";
import { useEffect, useMemo, useState } from "react";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { CLOUD_API_BASE, cloudRequest, formatDate, getToken } from "@/lib/admin-api";

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
  ["local_agent", "未连接本地程序"],
  ["position", "未创建岗位"],
  ["greet_success", "未打招呼成功"],
  ["paid", "未支付"],
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

/** AdminMailPage 展示超管邮件群发工作台。 */
export default function AdminMailPage() {
  const { user, notify } = useAdmin();
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
  const [editor, setEditor] = useState<IDomEditor | null>(null);
  const progress = activeBatch?.total_count ? Math.round(((activeBatch.sent_count + activeBatch.failed_count) / activeBatch.total_count) * 100) : 0;

  const toolbarConfig: Partial<IToolbarConfig> = useMemo(() => ({}), []);
  const editorConfig: Partial<IEditorConfig> = useMemo(() => ({
    placeholder: "写邮件正文，图片请用编辑器里的上传图片按钮。",
    MENU_CONF: {
      uploadImage: {
        server: `${CLOUD_API_BASE}/api/admin/emails/upload-image`,
        fieldName: "file",
        headers: { Authorization: `Bearer ${getToken()}` },
        maxFileSize: 5 * 1024 * 1024,
        allowedFileTypes: ["image/*"],
        customInsert(response: any, insertFn: (url: string, alt?: string, href?: string) => void) {
          const url = response?.absolute_url || response?.url;
          if (url) insertFn(url);
        },
      },
    },
  }), []);

  useEffect(() => {
    if (user?.role === "super_admin") void load();
  }, [user?.role]);

  useEffect(() => () => {
    editor?.destroy();
  }, [editor]);

  useEffect(() => {
    if (!activeBatch?.id || progress >= 100) return;
    const timer = window.setInterval(() => void loadBatch(activeBatch.id), 1500);
    return () => window.clearInterval(timer);
  }, [activeBatch?.id, progress]);

  /** load 读取最近邮件批次。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/admin/emails");
      setBatches(data.batches || []);
    } catch (error) {
      notify(error instanceof Error ? error.message : "邮件记录读取失败", "error");
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
    if (!mailHtml.replace(/<[^>]+>/g, "").trim() && !mailHtml.includes("<img")) return notify("正文还空着，我发不出去", "warning");
    setSending(true);
    try {
      const data = await cloudRequest("/api/admin/emails", {
        method: "POST",
        body: {
          subject,
          html: mailHtml,
          mode,
          emails: emails.split(/[\n,，]/).map((item) => item.trim()).filter(Boolean),
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

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;

  return <>
    <PageHeader title="邮件群发" description="给用户发送富文本邮件，图片会保存到服务器，不塞 base64。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} />
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(0, 1fr) 380px" }, gap: 2 }}>
      <SectionPanel>
        <Stack spacing={2}>
          <TextField label="邮件标题" value={subject} onChange={(event) => setSubject(event.target.value)} fullWidth />
          <TextField select label="发送对象" value={mode} onChange={(event) => setMode(event.target.value)} fullWidth>
            <MenuItem value="filter">按邮箱 / 标记 / 流程卡点</MenuItem>
            <MenuItem value="all">全部用户</MenuItem>
          </TextField>
          {mode !== "all" ? <>
            <TextField label="指定邮箱" value={emails} onChange={(event) => setEmails(event.target.value)} multiline minRows={3} helperText="一行一个，也支持逗号分隔。" />
            <TextField
              label="最近未登录天数"
              type="number"
              value={lastLoginBeforeDays}
              onChange={(event) => setLastLoginBeforeDays(event.target.value)}
              slotProps={{ htmlInput: { min: 1, step: 1 } }}
              helperText="比如填 5，就是筛选至少 5 天没上线的用户。"
              fullWidth
            />
            <OptionGroup title="用户标记" value={tags} options={profileOptions} onChange={setTags} />
            <OptionGroup title="流程卡点" value={flows} options={flowOptions} onChange={setFlows} />
          </> : null}
          <Box sx={{ border: "1px solid", borderColor: "divider", borderRadius: "8px", overflow: "hidden", "& .w-e-text-container": { minHeight: "260px !important" }, "& img": { maxWidth: "100%", height: "auto" } }}>
            <Toolbar editor={editor} defaultConfig={toolbarConfig} mode="default" style={{ borderBottom: "1px solid #eee" }} />
            <Editor defaultConfig={editorConfig} value={mailHtml} onCreated={setEditor} onChange={(nextEditor) => setMailHtml(nextEditor.getHtml())} mode="default" style={{ height: 320, overflowY: "hidden" }} />
          </Box>
          <Button variant="contained" size="large" startIcon={<SendRoundedIcon />} disabled={sending} onClick={() => void send()}>{sending ? "正在创建批次" : "发送邮件"}</Button>
        </Stack>
      </SectionPanel>

      <Stack spacing={2}>
        <SectionPanel>
          <Typography component="h2" sx={{ fontSize: 18, fontWeight: 780 }}>发送进度</Typography>
          {activeBatch ? <Box sx={{ mt: 1.5 }}>
            <Typography sx={{ fontWeight: 720 }}>{activeBatch.subject}</Typography>
            <LinearProgress variant="determinate" value={progress} sx={{ mt: 1.25, height: 8, borderRadius: 999 }} />
            <Stack direction="row" spacing={1} sx={{ mt: 1, flexWrap: "wrap", rowGap: 1 }}>
              <Chip size="small" label={`总数 ${activeBatch.total_count || 0}`} />
              <Chip size="small" color="success" label={`成功 ${activeBatch.sent_count || 0}`} />
              <Chip size="small" color="error" label={`失败 ${activeBatch.failed_count || 0}`} />
              <Chip size="small" color="info" label={`查看 ${activeBatch.opened_count || 0}`} />
            </Stack>
            <Box sx={{ mt: 1.5, maxHeight: 220, overflow: "auto" }}>
              {recipients.map((item) => <Typography key={item.id} sx={{ py: 0.5, fontSize: 12, borderBottom: "1px solid", borderColor: "divider" }}>{item.email} · {statusText(item.status)} · {item.opened ? "已查看" : "未查看"}</Typography>)}
            </Box>
          </Box> : <EmptyState text="发送后这里会显示进度" />}
        </SectionPanel>
        <SectionPanel>
          <Typography component="h2" sx={{ fontSize: 18, fontWeight: 780 }}>最近批次</Typography>
          {batches.length ? <Stack sx={{ mt: 1 }}>
            {batches.map((batch) => <Button key={batch.id} color="secondary" onClick={() => void loadBatch(batch.id)} sx={{ justifyContent: "flex-start", textAlign: "left", py: 1.25 }}>
              <Box sx={{ minWidth: 0 }}>
                <Typography noWrap sx={{ fontWeight: 720 }}>{batch.subject}</Typography>
                <Typography sx={{ color: "text.secondary", fontSize: 12 }}>{batch.sent_count}/{batch.total_count} 成功 · 查看 {batch.opened_count || 0} · {formatDate(batch.created_at)}</Typography>
              </Box>
            </Button>)}
          </Stack> : <EmptyState text="暂无邮件批次" />}
        </SectionPanel>
      </Stack>
    </Box>
  </>;
}

/** OptionGroup 展示可多选的筛选标签。 */
function OptionGroup({ title, value, options, onChange }: { title: string; value: string[]; options: string[][]; onChange: (value: string[]) => void }) {
  return <Box><Typography sx={{ mb: 0.75, fontSize: 13, fontWeight: 720 }}>{title}</Typography><ToggleButtonGroup size="small" value={value} onChange={(_, next) => onChange(next)} sx={{ flexWrap: "wrap", gap: 0.75, "& .MuiToggleButtonGroup-grouped": { border: "1px solid", borderColor: "divider", borderRadius: "8px !important" } }}>
    {options.map(([key, label]) => <ToggleButton key={key} value={key}>{label}</ToggleButton>)}
  </ToggleButtonGroup></Box>;
}

/** statusText 返回邮件收件人的发送状态文案。 */
function statusText(value: string) {
  return value === "sent" ? "已发送" : value === "failed" ? "失败" : "等待中";
}
