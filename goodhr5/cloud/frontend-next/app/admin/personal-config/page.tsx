/** 本文件负责新版后台个人 AI 配置和操作节奏偏好。 */
"use client";

import ScienceRoundedIcon from "@mui/icons-material/ScienceRounded";
import SaveRoundedIcon from "@mui/icons-material/SaveRounded";
import { Box, Button, InputAdornment, Stack, TextField, Typography } from "@mui/material";
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
      const ai = aiData.config || {}; const preference = preferenceData.config || {};
      setKeySet(Boolean(ai.api_key_set)); setForm({ ...defaults, ...preference, base_url: ai.base_url || defaults.base_url, model: ai.model || preference.ai_model || defaults.model, api_key: "" });
    } catch (error) { notify(error instanceof Error ? error.message : "个人配置读取失败", "error"); }
    finally { setLoading(false); }
  }

  useEffect(() => { void load(); }, []);

  /** testAI 通过云端代理验证当前填写的 AI 接口。 */
  async function testAI() {
    if (!form.api_key.trim()) return notify("测试前请填写 AI Key", "warning");
    setLoading(true);
    try { await cloudRequest("/api/config/test-ai", { method: "POST", body: { base_url: form.base_url.trim(), model: form.model.trim(), api_key: form.api_key.trim(), temperature: 0, enabled: true } }); notify("AI 接口测试成功", "success"); } catch (error) { notify(error instanceof Error ? error.message : "AI 接口测试失败", "error"); } finally { setLoading(false); }
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
      setKeySet(true); setForm((value) => ({ ...value, api_key: "" })); notify("个人配置已保存", "success");
    } catch (error) { notify(error instanceof Error ? error.message : "保存配置失败", "error"); }
    finally { setLoading(false); }
  }

  /** setNumber 更新数字配置字段。 */
  function setNumber(key: keyof typeof defaults, value: string) { setForm((current) => ({ ...current, [key]: Number(value || 0) })); }

  return <><PageHeader title="个人配置" description="配置 AI 接口以及本地任务模拟人工操作的节奏。" actions={<><Button variant="outlined" startIcon={<ScienceRoundedIcon />} disabled={loading} onClick={() => void testAI()}>测试 AI</Button><Button variant="contained" startIcon={<SaveRoundedIcon />} disabled={loading} onClick={() => void save()}>保存配置</Button></>} /><SectionPanel sx={{ mb: 2 }}><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>AI 接口</Typography><Box sx={{ mt: 2, display: "grid", gridTemplateColumns: { xs: "1fr", md: "2fr 1fr" }, gap: 2 }}><TextField label="AI API 地址" value={form.base_url} onChange={(event) => setForm({ ...form, base_url: event.target.value })} /><TextField label="模型" value={form.model} onChange={(event) => setForm({ ...form, model: event.target.value })} /><TextField label="AI Key" type="password" value={form.api_key} onChange={(event) => setForm({ ...form, api_key: event.target.value })} placeholder={keySet ? "已配置，留空表示不修改" : "请输入 API Key"} sx={{ gridColumn: { md: "1 / -1" } }} /></Box></SectionPanel><SectionPanel><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>操作节奏</Typography><Typography sx={{ mt: 0.75, color: "text.secondary" }}>任务运行时会在最小值和最大值之间随机选择，避免固定节奏。</Typography><Box sx={{ mt: 2.5, display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)" }, gap: 2 }}><NumberRange label="打开详情前延时" unit="秒" min={form.detail_open_delay_min} max={form.detail_open_delay_max} onMin={(value) => setNumber("detail_open_delay_min", value)} onMax={(value) => setNumber("detail_open_delay_max", value)} /><NumberRange label="关闭详情前延时" unit="秒" min={form.detail_close_delay_min} max={form.detail_close_delay_max} onMin={(value) => setNumber("detail_close_delay_min", value)} onMax={(value) => setNumber("detail_close_delay_max", value)} /><NumberRange label="打招呼前延时" unit="秒" min={form.greet_before_delay_min} max={form.greet_before_delay_max} onMin={(value) => setNumber("greet_before_delay_min", value)} onMax={(value) => setNumber("greet_before_delay_max", value)} /><NumberRange label="处理多少人后休息" unit="人" min={form.rest_after_candidates_min} max={form.rest_after_candidates_max} onMin={(value) => setNumber("rest_after_candidates_min", value)} onMax={(value) => setNumber("rest_after_candidates_max", value)} /><NumberRange label="单次任务休息次数" unit="次" min={form.rest_times_min} max={form.rest_times_max} onMin={(value) => setNumber("rest_times_min", value)} onMax={(value) => setNumber("rest_times_max", value)} /><NumberRange label="每次休息时长" unit="分钟" min={form.rest_duration_min} max={form.rest_duration_max} onMin={(value) => setNumber("rest_duration_min", value)} onMax={(value) => setNumber("rest_duration_max", value)} /><TextField label="点击频率" type="number" value={form.click_frequency} onChange={(event) => setNumber("click_frequency", event.target.value)} slotProps={{ input: { endAdornment: <InputAdornment position="end">%</InputAdornment> } }} /><TextField label="打开详情概率" type="number" value={form.detail_open_probability} onChange={(event) => setNumber("detail_open_probability", event.target.value)} slotProps={{ input: { endAdornment: <InputAdornment position="end">%</InputAdornment> } }} /></Box></SectionPanel></>;
}

/** NumberRange 展示一组最小值和最大值输入。 */
function NumberRange({ label, unit, min, max, onMin, onMax }: { label: string; unit: string; min: number; max: number; onMin: (value: string) => void; onMax: (value: string) => void }) { return <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><TextField label={`${label}最小值`} type="number" value={min} onChange={(event) => onMin(event.target.value)} fullWidth /><Typography color="text.secondary">到</Typography><TextField label="最大值" type="number" value={max} onChange={(event) => onMax(event.target.value)} fullWidth slotProps={{ input: { endAdornment: <InputAdornment position="end">{unit}</InputAdornment> } }} /></Stack>; }
