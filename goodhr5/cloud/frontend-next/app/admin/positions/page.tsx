/** 本文件负责新版后台岗位模板的新增、编辑和删除。 */
"use client";

import AddRoundedIcon from "@mui/icons-material/AddRounded";
import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
import EditRoundedIcon from "@mui/icons-material/EditRounded";
import { Box, Button, FormControlLabel, MenuItem, Stack, Switch, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

const emptyForm = { id: "", name: "", platform_id: "boss", mode_default: "keyword", detail_mode: "ocr", keywords: "", exclude_keywords: "", is_and_mode: false, position_requirement: "", open_detail_prompt: "", filter_prompt: "", review_prompt: "", detail_score_threshold: 60, greet_score_threshold: 70 };

/** PositionsPage 管理用于任务筛选的岗位模板。 */
export default function PositionsPage() {
  const { notify, confirm } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ ...emptyForm });
  const [defaults, setDefaults] = useState<any>({});

  /** load 读取岗位模板和系统默认提示词。 */
  async function load() {
    setLoading(true);
    try {
      const [positions, prompts] = await Promise.all([cloudRequest("/api/positions"), cloudRequest("/api/system/default-prompts")]);
      setItems(positions.positions || []); setDefaults(prompts.prompts || prompts || {});
    } catch (error) { notify(error instanceof Error ? error.message : "岗位模板读取失败", "error"); }
    finally { setLoading(false); }
  }

  useEffect(() => { void load(); }, []);

  /** save 保存岗位模板并兼容旧后端字段结构。 */
  async function save() {
    if (!form.name.trim()) return notify("请填写岗位名称", "warning");
    setLoading(true);
    try {
      await cloudRequest("/api/positions", { method: "POST", body: { id: form.id, platform_id: form.platform_id, name: form.name.trim(), keywords: splitKeywords(form.keywords), exclude_keywords: splitKeywords(form.exclude_keywords), description: "", greet_message: "", is_and_mode: form.is_and_mode, common_config: { mode_default: form.mode_default, detail_mode: form.detail_mode }, ai_config: { position_requirement: form.position_requirement, filter_prompt: form.filter_prompt || defaults.filter_prompt || "", greet_prompt: form.filter_prompt || defaults.filter_prompt || "", click_prompt: form.filter_prompt || defaults.filter_prompt || "", open_detail_prompt: form.open_detail_prompt || defaults.open_detail_prompt || "", review_prompt: form.review_prompt || defaults.review_prompt || "", detail_score_threshold: Number(form.detail_score_threshold || 60), greet_score_threshold: Number(form.greet_score_threshold || 70) }, keyword_config: {} } });
      notify(form.id ? "岗位模板已更新" : "岗位模板已创建", "success"); setForm({ ...emptyForm }); setShowForm(false); await load();
    } catch (error) { notify(error instanceof Error ? error.message : "保存岗位失败", "error"); }
    finally { setLoading(false); }
  }

  /** edit 将岗位模板数据填入编辑表单。 */
  function edit(item: any) {
    const common = item.common_config || {}; const ai = item.ai_config || {};
    setForm({ id: item.id || "", name: item.name || "", platform_id: item.platform_id || "boss", mode_default: common.mode_default || "keyword", detail_mode: common.detail_mode || "ocr", keywords: (item.keywords || []).join(" "), exclude_keywords: (item.exclude_keywords || []).join(" "), is_and_mode: Boolean(item.is_and_mode), position_requirement: ai.position_requirement || "", open_detail_prompt: ai.open_detail_prompt || "", filter_prompt: ai.greet_prompt || ai.filter_prompt || "", review_prompt: ai.review_prompt || "", detail_score_threshold: Number(ai.detail_score_threshold ?? 60), greet_score_threshold: Number(ai.greet_score_threshold ?? 70) }); setShowForm(true); window.scrollTo({ top: 0, behavior: "smooth" });
  }

  /** remove 删除指定岗位模板。 */
  async function remove(item: any) {
    if (!(await confirm("删除岗位模板", `确认删除“${item.name}”吗？`))) return;
    try { await cloudRequest(`/api/positions/${item.id}`, { method: "DELETE" }); notify("岗位模板已删除", "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "删除失败", "error"); }
  }

  return <><PageHeader title="岗位管理" description="岗位模板决定候选人筛选模式、详情读取方式和评分标准。" actions={<><Button variant="contained" startIcon={<AddRoundedIcon />} onClick={() => { setForm({ ...emptyForm }); setShowForm((value) => !value); }}>{showForm ? "收起" : "新建岗位"}</Button><RefreshButton loading={loading} onClick={() => void load()} /></>} />{showForm ? <SectionPanel sx={{ mb: 2 }}><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)" }, gap: 2 }}><TextField label="岗位名称" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /><TextField select label="招聘平台" value={form.platform_id} onChange={(event) => setForm({ ...form, platform_id: event.target.value })}><MenuItem value="boss">Boss直聘</MenuItem><MenuItem value="zhaopin">智联招聘</MenuItem><MenuItem value="liepin">猎聘</MenuItem></TextField><TextField select label="首次筛选模式" value={form.mode_default} onChange={(event) => setForm({ ...form, mode_default: event.target.value })}><MenuItem value="keyword">关键词筛选</MenuItem><MenuItem value="ai">AI 筛选（会员功能）</MenuItem></TextField><TextField select label="详情读取模式" value={form.detail_mode} onChange={(event) => setForm({ ...form, detail_mode: event.target.value })}><MenuItem value="ocr">OCR 识别</MenuItem><MenuItem value="dom" disabled={form.platform_id === "boss"}>DOM 结构</MenuItem><MenuItem value="ai">AI 识别（会员功能）</MenuItem></TextField><TextField label="包含关键词" value={form.keywords} onChange={(event) => setForm({ ...form, keywords: event.target.value })} multiline minRows={3} helperText="可用空格、中文逗号、英文逗号或换行分隔多个关键词" /><TextField label="排除关键词" value={form.exclude_keywords} onChange={(event) => setForm({ ...form, exclude_keywords: event.target.value })} multiline minRows={3} /><FormControlLabel control={<Switch checked={form.is_and_mode} onChange={(event) => setForm({ ...form, is_and_mode: event.target.checked })} />} label="包含关键词必须全部命中" />{form.mode_default === "ai" || form.detail_mode === "ai" ? <><TextField label="岗位要求" value={form.position_requirement} onChange={(event) => setForm({ ...form, position_requirement: event.target.value })} multiline minRows={5} sx={{ gridColumn: "1 / -1" }} /><TextField label="打开详情提示词" value={form.open_detail_prompt} onChange={(event) => setForm({ ...form, open_detail_prompt: event.target.value })} multiline minRows={5} /><TextField label="最终筛选提示词" value={form.filter_prompt} onChange={(event) => setForm({ ...form, filter_prompt: event.target.value })} multiline minRows={5} /><TextField label="看详情阈值" type="number" value={form.detail_score_threshold} onChange={(event) => setForm({ ...form, detail_score_threshold: Number(event.target.value) })} /><TextField label="打招呼阈值" type="number" value={form.greet_score_threshold} onChange={(event) => setForm({ ...form, greet_score_threshold: Number(event.target.value) })} /><TextField label="复核提示词（可选）" value={form.review_prompt} onChange={(event) => setForm({ ...form, review_prompt: event.target.value })} multiline minRows={4} sx={{ gridColumn: "1 / -1" }} /></> : null}</Box><Stack direction="row" spacing={1} sx={{ mt: 3, justifyContent: "flex-end" }}><Button color="secondary" onClick={() => setShowForm(false)}>取消</Button><Button variant="contained" disabled={loading} onClick={() => void save()}>保存岗位</Button></Stack></SectionPanel> : null}<SectionPanel>{items.length ? <Stack spacing={0}>{items.map((item) => <Stack key={item.id} direction={{ xs: "column", md: "row" }} spacing={2} sx={{ py: 2, borderBottom: "1px solid", borderColor: "divider", alignItems: { md: "center" } }}><Box sx={{ flex: 1 }}><Typography sx={{ fontWeight: 760 }}>{item.name}</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}>{item.platform_id || "boss"} · {item.common_config?.mode_default === "ai" ? "AI 筛选" : "关键词筛选"} · 详情：{String(item.common_config?.detail_mode || "ocr").toUpperCase()}</Typography></Box><Stack direction="row" spacing={1}><Button startIcon={<EditRoundedIcon />} onClick={() => edit(item)}>编辑</Button><Button color="error" startIcon={<DeleteOutlineRoundedIcon />} onClick={() => void remove(item)}>删除</Button></Stack></Stack>)}</Stack> : <EmptyState text="暂无岗位模板" />}</SectionPanel></>;
}

/** splitKeywords 将多种分隔符输入转换为去重关键词数组。 */
function splitKeywords(value: string) {
  return Array.from(new Set(String(value || "").split(/[,\s，、；;]+/).map((item) => item.trim()).filter(Boolean)));
}
