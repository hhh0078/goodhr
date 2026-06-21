/** 本文件负责新版后台个人 AI 接口、操作节奏和模拟休息配置。 */
"use client";

import ApiRoundedIcon from "@mui/icons-material/ApiRounded";
import ArrowOutwardRoundedIcon from "@mui/icons-material/ArrowOutwardRounded";
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

const defaults = { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions", model: "qwen3.7-plus", api_key: "", click_frequency: 80, detail_open_probability: 80, detail_open_delay_min: 1, detail_open_delay_max: 2, detail_close_delay_min: 0, detail_close_delay_max: 0, greet_before_delay_min: 1, greet_before_delay_max: 2, rest_after_candidates_min: 40, rest_after_candidates_max: 70, rest_times_min: 2, rest_times_max: 3, rest_duration_min: 2, rest_duration_max: 7 };

/** PersonalConfigPage 管理 AI 接口和模拟人工操作参数。 */
export default function PersonalConfigPage() {
  const { notify } = useAdmin();
  const [form, setForm] = useState({ ...defaults });
  const [keySet, setKeySet] = useState(false);
  const [loading, setLoading] = useState(false);

  /** load 读取个人 AI 配置和操作偏好。 */
  async function load() {
    setLoading(true);
    try {
      const [aiData, preferenceData] = await Promise.all([cloudRequest("/api/config/user-ai"), cloudRequest("/api/config/user-preferences")]);
      const ai = aiData.config || {};
      const preference = preferenceData.config || {};
      setKeySet(Boolean(ai.api_key_set));
      setForm({ ...defaults, ...preference, base_url: ai.base_url || defaults.base_url, model: ai.model || preference.ai_model || defaults.model, api_key: "" });
    } catch (error) {
      notify(error instanceof Error ? error.message : "个人配置读取失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, []);

  /** testAI 通过云端代理验证当前填写的 AI 接口。 */
  async function testAI() {
    if (!form.api_key.trim()) return notify(keySet ? "测试时需要重新填写 AI Key" : "测试前请填写 AI Key", "warning");
    setLoading(true);
    try {
      await cloudRequest("/api/config/test-ai", { method: "POST", body: { base_url: form.base_url.trim(), model: form.model.trim(), api_key: form.api_key.trim(), temperature: 0, enabled: true } });
      notify("AI 接口测试成功", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "AI 接口测试失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** save 保存 AI 配置和操作偏好。 */
  async function save() {
    if (!form.base_url.trim() || !form.model.trim()) return notify("请填写 AI 地址和模型", "warning");
    if (!keySet && !form.api_key.trim()) return notify("请填写 AI Key", "warning");
    setLoading(true);
    try {
      await cloudRequest("/api/config/user-ai", { method: "PUT", body: { base_url: form.base_url.trim(), model: form.model.trim(), api_key: form.api_key.trim(), temperature: 0, prompt_template: "", enabled: true } });
      const { base_url: _baseURL, model, api_key: _key, ...preference } = form;
      await cloudRequest("/api/config/user-preferences", { method: "PUT", body: { ...preference, ai_model: model } });
      setKeySet(true);
      setForm((value) => ({ ...value, api_key: "" }));
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
    <PageHeader title="个人配置" description="设置 AI 接口和任务操作节奏，保存后会用于本地任务运行。" actions={<><Button variant="outlined" startIcon={<ScienceRoundedIcon />} disabled={loading} onClick={() => void testAI()}>测试 AI</Button><Button variant="contained" startIcon={<SaveRoundedIcon />} disabled={loading} onClick={() => void save()}>{loading ? "处理中" : "保存配置"}</Button></>} />

    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(2, minmax(0, 1fr))" }, gap: 1.5, mb: 2 }}>
      <QuickLink href="https://www.qianwenai.com/" external icon={<PsychologyAltRoundedIcon />} title="获取 AI 接口" description="前往千问平台申请多模态模型和 API Key" />
      <QuickLink href="/videos" icon={<PlayCircleOutlineRoundedIcon />} title="查看配置教程" description="跟随视频完成 AI 接口申请、填写和测试" />
    </Box>

    <Alert severity="info" icon={<ApiRoundedIcon />} sx={{ mb: 2, border: "1px solid #cbded4", bgcolor: "#f3f8f5", color: "#244d3b", "& .MuiAlert-icon": { color: "#1e6545" } }}>
      可接入兼容 OpenAI 格式的多模态模型，例如千问、硅基流动和 OpenAI。模型必须支持图片识别；DeepSeek 当前不支持图片输入，请不要用于详情 AI 识别。
    </Alert>

    <SectionPanel sx={{ mb: 2 }}>
      <SectionTitle icon={<ApiRoundedIcon />} title="AI 接口" description="API Key 仅用于你的任务调用，已经配置时留空不会覆盖原值。" />
      <Box sx={{ mt: 2.5, display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(0, 1.55fr) minmax(220px, .65fr)" }, gap: 2 }}>
        <TextField label="API 地址" value={form.base_url} onChange={(event) => setForm({ ...form, base_url: event.target.value })} helperText="默认使用千问兼容 OpenAI 的 Chat Completions 地址。" />
        <TextField label="模型名称" value={form.model} onChange={(event) => setForm({ ...form, model: event.target.value })} helperText="例如 qwen3.7-plus" />
        <TextField label="API Key" type="password" value={form.api_key} onChange={(event) => setForm({ ...form, api_key: event.target.value })} placeholder={keySet ? "已配置，留空表示不修改" : "请输入 API Key"} helperText={keySet ? "当前已有可用 Key；只有填写新值时才会更新。" : "请先从 AI 平台创建可用的 API Key。"} sx={{ gridColumn: { lg: "1 / -1" }, maxWidth: 760 }} />
      </Box>
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
function QuickLink({ href, icon, title, description, external = false }: { href: string; icon: React.ReactNode; title: string; description: string; external?: boolean }) {
  const content = <Stack direction="row" spacing={1.5} sx={{ p: 2, height: "100%", alignItems: "center", border: "1px solid #d8e4dc", borderRadius: "8px", bgcolor: "#fbfdfc", color: "text.primary", transition: "150ms ease", "&:hover": { borderColor: "#82a891", bgcolor: "#f4f8f5", transform: "translateY(-1px)" } }}><Box sx={{ width: 42, height: 42, borderRadius: "8px", display: "grid", placeItems: "center", bgcolor: "#e7f1ea", color: "#1e6545", flexShrink: 0 }}>{icon}</Box><Box sx={{ flex: 1 }}><Typography sx={{ fontWeight: 760 }}>{title}</Typography><Typography sx={{ mt: 0.25, color: "text.secondary", fontSize: 13 }}>{description}</Typography></Box><ArrowOutwardRoundedIcon sx={{ color: "text.secondary", fontSize: 20 }} /></Stack>;
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
