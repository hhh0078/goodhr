/** 本文件负责新版后台个人 AI 接口、操作节奏和模拟休息配置。 */
"use client";

import ApiRoundedIcon from "@mui/icons-material/ApiRounded";
import ArrowOutwardRoundedIcon from "@mui/icons-material/ArrowOutwardRounded";
import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import NotificationsActiveRoundedIcon from "@mui/icons-material/NotificationsActiveRounded";
import PlayCircleOutlineRoundedIcon from "@mui/icons-material/PlayCircleOutlineRounded";
import PsychologyAltRoundedIcon from "@mui/icons-material/PsychologyAltRounded";
import SaveRoundedIcon from "@mui/icons-material/SaveRounded";
import ScienceRoundedIcon from "@mui/icons-material/ScienceRounded";
import TimerOutlinedIcon from "@mui/icons-material/TimerOutlined";
import { Alert, Box, Button, InputAdornment, Stack, TextField, Typography } from "@mui/material";
import Link from "next/link";
import { useEffect, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import NotificationProfileDialog from "@/components/admin/NotificationProfileDialog";

const defaults = { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", model: "qwen3.7-plus", api_key: "", click_frequency: 80, detail_open_probability: 80, detail_open_delay_min: 1, detail_open_delay_max: 2, detail_close_delay_min: 0, detail_close_delay_max: 0, greet_before_delay_min: 1, greet_before_delay_max: 2, rest_after_candidates_min: 40, rest_after_candidates_max: 70, rest_times_min: 2, rest_times_max: 3, rest_duration_min: 2, rest_duration_max: 7 };

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

  /** load 读取个人 AI 配置和操作偏好。 */
  async function load() {
    setLoading(true);
    try {
      const [aiData, preferenceData] = await Promise.all([cloudRequest("/api/config/user-ai"), cloudRequest("/api/config/user-preferences")]);
      const ai = aiData.config || {};
      const preference = preferenceData.config || {};
      setKeySet(Boolean(ai.api_key_set));
      setForm({ ...defaults, ...preference, base_url: ai.base_url || defaults.base_url, model: ai.model || preference.ai_model || defaults.model, api_key: ai.api_key || "" });
    } catch (error) {
      notify(error instanceof Error ? error.message : "个人配置读取失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, []);

  /** testAI 通过云端代理验证当前填写的 AI 接口。 */
  async function testAI() {
    if (!form.api_key.trim()) return notify("测试前请填写 AI Key", "warning");
    const baseURL = normalizeAIBaseURL(form.base_url);
    if (!baseURL || !form.model.trim()) return notify("请填写 AI 地址和模型", "warning");
    setLoading(true);
    try {
      setForm((current) => ({ ...current, base_url: baseURL }));
      await cloudRequest("/api/config/test-ai", { method: "POST", body: { base_url: baseURL, model: form.model.trim(), api_key: form.api_key.trim(), temperature: 0, enabled: true } });
      notify("AI 接口测试成功", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "AI 接口测试失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** save 保存 AI 配置和操作偏好。 */
  async function save() {
    const baseURL = normalizeAIBaseURL(form.base_url);
    if (!baseURL || !form.model.trim()) return notify("请填写 AI 地址和模型", "warning");
    if (!keySet && !form.api_key.trim()) return notify("请填写 AI Key", "warning");
    setLoading(true);
    try {
      setForm((current) => ({ ...current, base_url: baseURL }));
      await cloudRequest("/api/config/user-ai", { method: "PUT", body: { base_url: baseURL, model: form.model.trim(), api_key: form.api_key.trim(), temperature: 0, prompt_template: "", enabled: true } });
      const { base_url: _baseURL, model, api_key: _key, ...preference } = form;
      await cloudRequest("/api/config/user-preferences", { method: "PUT", body: { ...preference, ai_model: model } });
      setKeySet(true);
      notify("个人配置已保存", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "保存配置失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** setNumber 更新一个数字配置字段。 */
  function setNumber(key: keyof typeof defaults, value: string) {
    setForm((current) => ({ ...current, [key]: Number(value || 0) }));
  }

	return <>
	<NotificationProfileDialog openSignal={profileOpenSignal} />
	<PageHeader title="个人配置" description="设置 AI 接口和任务操作节奏，保存后会用于本地任务运行。" actions={<><Button variant="outlined" startIcon={<NotificationsActiveRoundedIcon />} onClick={() => setProfileOpenSignal((value) => value + 1)}>通知偏好</Button><Button variant="outlined" startIcon={<ScienceRoundedIcon />} disabled={loading} onClick={() => void testAI()}>测试 AI</Button><Button variant="contained" startIcon={<SaveRoundedIcon />} disabled={loading} onClick={() => void save()}>{loading ? "处理中" : "保存配置"}</Button></>} />

    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "1.15fr .85fr" }, gap: 2, mb: 2 }}>
      <QuickLink href="/videos" icon={<PlayCircleOutlineRoundedIcon />} eyebrow="新手推荐" title="查看视频教程" description="按视频一步步完成 AI 平台申请、接口填写、测试和保存。" primary />
      <QuickLink href="https://www.qianwenai.com/" external icon={<PsychologyAltRoundedIcon />} eyebrow="AI 接入" title="获取 AI 接口" description="前往千问平台申请多模态模型和 API Key。" />
    </Box>

    <SectionPanel sx={{ mb: 2, borderColor: "#9fbca9", bgcolor: "#f8fbf8", boxShadow: "0 16px 44px rgba(38, 88, 57, .08)" }}>
      <Stack direction={{ xs: "column", md: "row" }} spacing={2} sx={{ justifyContent: "space-between", alignItems: { md: "flex-start" }, mb: 2 }}>
        <SectionTitle icon={<ApiRoundedIcon />} title="AI 配置" description="这是 AI 筛选和详情识别的核心配置，建议先测试成功再保存。" />
        <Stack direction="row" spacing={1} sx={{ alignItems: "center", px: 1.35, py: 0.8, borderRadius: "999px", border: "1px solid", borderColor: keySet ? "#a9c8b2" : "#e5cda3", bgcolor: keySet ? "#edf6ef" : "#fff8ea", color: keySet ? "#1d6844" : "#8a5a10", fontSize: 13, fontWeight: 760, width: "fit-content" }}>
          <CheckCircleRoundedIcon sx={{ fontSize: 18 }} />
          {keySet ? "已保存 AI Key" : "还未保存 AI Key"}
        </Stack>
      </Stack>
      <Alert severity="info" icon={<ApiRoundedIcon />} sx={{ mb: 2, border: "1px solid #cbded4", bgcolor: "#f3f8f5", color: "#244d3b", "& .MuiAlert-icon": { color: "#1e6545" } }}>
        可接入兼容 OpenAI 格式的多模态模型，例如千问、硅基流动和 OpenAI。模型必须支持图片识别；DeepSeek 当前不支持图片输入，请不要用于详情 AI 识别。
      </Alert>
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(0, 1.55fr) minmax(220px, .65fr)" }, gap: 2 }}>
        <TextField label="API 地址" value={form.base_url} onChange={(event) => setForm({ ...form, base_url: event.target.value })} helperText="默认使用千问兼容 OpenAI 的 Chat Completions 地址。" />
        <TextField label="模型名称" value={form.model} onChange={(event) => setForm({ ...form, model: event.target.value })} helperText="例如 qwen3.7-plus" />
        <TextField label="API Key" value={form.api_key} onChange={(event) => setForm({ ...form, api_key: event.target.value })} placeholder="请输入 API Key" helperText="这里会明文显示当前保存的 Key，方便复制和修改。" sx={{ gridColumn: { lg: "1 / -1" }, maxWidth: 760 }} />
      </Box>
      <Stack direction={{ xs: "column", sm: "row" }} spacing={1.25} sx={{ mt: 2.25, alignItems: { sm: "center" } }}>
        <Button variant="contained" startIcon={<ScienceRoundedIcon />} disabled={loading} onClick={() => void testAI()} sx={{ borderRadius: "999px", px: 2.4 }}>先测试 AI</Button>
        <Typography sx={{ color: "text.secondary", fontSize: 13 }}>测试成功后再点右上角保存，任务运行时就会使用这套配置。</Typography>
      </Stack>
    </SectionPanel>

    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", xl: "repeat(2, minmax(0, 1fr))" }, gap: 2 }}>
      <SectionPanel>
        <SectionTitle icon={<TimerOutlinedIcon />} title="操作节奏" description="在范围内随机等待，让任务操作保持自然。" />
        <Stack spacing={2.25} sx={{ mt: 2.5 }}>
          <CompactNumber label="点击频率" help="控制候选人操作的执行比例。" unit="%" value={form.click_frequency} onChange={(value) => setNumber("click_frequency", value)} />
          <CompactNumber label="详情查看概率" help="关键词模式下，决定是否打开详情继续筛选。" unit="%" value={form.detail_open_probability} onChange={(value) => setNumber("detail_open_probability", value)} />
          <NumberRange label="打开详情前延时" help="点击候选人详情前随机等待。" unit="秒" min={form.detail_open_delay_min} max={form.detail_open_delay_max} onMin={(value) => setNumber("detail_open_delay_min", value)} onMax={(value) => setNumber("detail_open_delay_max", value)} />
          <NumberRange label="关闭详情前延时" help="详情提取完成后、关闭页面前随机等待。" unit="秒" min={form.detail_close_delay_min} max={form.detail_close_delay_max} onMin={(value) => setNumber("detail_close_delay_min", value)} onMax={(value) => setNumber("detail_close_delay_max", value)} />
          <NumberRange label="打招呼前延时" help="候选人通过筛选后、打招呼前随机等待。" unit="秒" min={form.greet_before_delay_min} max={form.greet_before_delay_max} onMin={(value) => setNumber("greet_before_delay_min", value)} onMax={(value) => setNumber("greet_before_delay_max", value)} />
        </Stack>
      </SectionPanel>

      <SectionPanel>
        <SectionTitle icon={<PsychologyAltRoundedIcon />} title="模拟休息" description="任务会按配置间歇休息，避免长时间连续操作。" />
        <Stack spacing={2.25} sx={{ mt: 2.5 }}>
          <NumberRange label="处理多少人后休息" help="例如设置 40 到 70，系统会随机选择人数。" unit="人" min={form.rest_after_candidates_min} max={form.rest_after_candidates_max} onMin={(value) => setNumber("rest_after_candidates_min", value)} onMax={(value) => setNumber("rest_after_candidates_max", value)} />
          <NumberRange label="单次任务休息次数" help="达到本次随机次数后不再休息。" unit="次" min={form.rest_times_min} max={form.rest_times_max} onMin={(value) => setNumber("rest_times_min", value)} onMax={(value) => setNumber("rest_times_max", value)} />
          <NumberRange label="每次休息时长" help="每次休息会在此范围内随机，并写入任务日志。" unit="分钟" min={form.rest_duration_min} max={form.rest_duration_max} onMin={(value) => setNumber("rest_duration_min", value)} onMax={(value) => setNumber("rest_duration_max", value)} />
        </Stack>
      </SectionPanel>
    </Box>
  </>;
}

/** QuickLink 展示个人配置页的外部帮助入口。 */
function QuickLink({ href, icon, eyebrow, title, description, external = false, primary = false }: { href: string; icon: React.ReactNode; eyebrow: string; title: string; description: string; external?: boolean; primary?: boolean }) {
  const content = <Stack direction="row" spacing={1.75} sx={{ p: { xs: 2, md: primary ? 2.5 : 2 }, minHeight: primary ? 140 : 118, height: "100%", alignItems: "center", border: "1px solid", borderColor: primary ? "#9fc7ae" : "#d8e4dc", borderRadius: "8px", bgcolor: primary ? "#f3faf5" : "#fbfdfc", color: "text.primary", boxShadow: primary ? "0 18px 44px rgba(38, 88, 57, .1)" : "none", transition: "150ms ease", "&:hover": { borderColor: "#82a891", bgcolor: primary ? "#eef7f1" : "#f4f8f5", transform: "translateY(-1px)" } }}><Box sx={{ width: primary ? 58 : 46, height: primary ? 58 : 46, borderRadius: "999px", display: "grid", placeItems: "center", bgcolor: primary ? "#1f7048" : "#e7f1ea", color: primary ? "#fff" : "#1e6545", flexShrink: 0, "& .MuiSvgIcon-root": { fontSize: primary ? 31 : 24 } }}>{icon}</Box><Box sx={{ flex: 1, minWidth: 0 }}><Typography sx={{ mb: 0.45, width: "fit-content", px: 1, py: 0.35, borderRadius: "999px", bgcolor: primary ? "#dff0e4" : "#eef4f0", color: "#1e6545", fontSize: 12, fontWeight: 760 }}>{eyebrow}</Typography><Typography sx={{ fontSize: primary ? 22 : 17, fontWeight: 820, lineHeight: 1.2 }}>{title}</Typography><Typography sx={{ mt: 0.75, color: "text.secondary", fontSize: 13.5, lineHeight: 1.65 }}>{description}</Typography></Box><ArrowOutwardRoundedIcon sx={{ color: primary ? "#1e6545" : "text.secondary", fontSize: 22, flexShrink: 0 }} /></Stack>;
  return external ? <Box component="a" href={href} target="_blank" rel="noreferrer" sx={{ textDecoration: "none" }}>{content}</Box> : <Link href={href} style={{ textDecoration: "none" }}>{content}</Link>;
}

/** SectionTitle 展示配置区域标题和说明。 */
function SectionTitle({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return <Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}><Box sx={{ color: "#1e6545", display: "grid", placeItems: "center" }}>{icon}</Box><Box><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>{title}</Typography><Typography sx={{ mt: 0.25, color: "text.secondary", fontSize: 13 }}>{description}</Typography></Box></Stack>;
}

/** CompactNumber 展示一个带单位的紧凑数字配置。 */
function CompactNumber({ label, help, unit, value, onChange }: { label: string; help: string; unit: string; value: number; onChange: (value: string) => void }) {
  return <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "minmax(160px, 1fr) 150px" }, gap: 1.5, alignItems: "center" }}><Box><Typography sx={{ fontWeight: 700 }}>{label}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>{help}</Typography></Box><TextField size="small" type="number" value={value} onChange={(event) => onChange(event.target.value)} slotProps={{ input: { endAdornment: <InputAdornment position="end">{unit}</InputAdornment> } }} /></Box>;
}

/** NumberRange 展示一组带说明的最小值和最大值输入。 */
function NumberRange({ label, help, unit, min, max, onMin, onMax }: { label: string; help: string; unit: string; min: number; max: number; onMin: (value: string) => void; onMax: (value: string) => void }) {
  return <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "minmax(160px, 1fr) 250px" }, gap: 1.5, alignItems: "center" }}><Box><Typography sx={{ fontWeight: 700 }}>{label}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>{help}</Typography></Box><Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><TextField size="small" aria-label={`${label}最小值`} type="number" value={min} onChange={(event) => onMin(event.target.value)} sx={{ minWidth: 0 }} /><Typography color="text.secondary">至</Typography><TextField size="small" aria-label={`${label}最大值`} type="number" value={max} onChange={(event) => onMax(event.target.value)} sx={{ minWidth: 0 }} slotProps={{ input: { endAdornment: <InputAdornment position="end">{unit}</InputAdornment> } }} /></Stack></Box>;
}
