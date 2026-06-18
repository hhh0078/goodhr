/** 本文件负责超级管理员分类查看、校验和编辑云端系统 JSON 配置。 */
"use client";

import RestartAltRoundedIcon from "@mui/icons-material/RestartAltRounded";
import SaveRoundedIcon from "@mui/icons-material/SaveRounded";
import { Alert, Box, Button, Chip, Stack, Tab, Tabs, Typography } from "@mui/material";
import { useEffect, useMemo, useState } from "react";
import JsonEditor from "@/components/admin/JsonEditor";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest } from "@/lib/admin-api";

const categories = ["全部", "AI 配置", "基础配置", "订阅支付", "本地组件", "邀请帮助"] as const;

/** SystemConfigPage 管理系统配置并按用途分类。 */
export default function SystemConfigPage() {
  const { user, notify, confirm } = useAdmin();
  const [configs, setConfigs] = useState<any[]>([]);
  const [category, setCategory] = useState<(typeof categories)[number]>("全部");
  const [activeKey, setActiveKey] = useState("");
  const [draft, setDraft] = useState("{}");
  const [original, setOriginal] = useState("{}");
  const [loading, setLoading] = useState(false);

  /** load 读取全部系统配置并保持当前选中项。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/admin/system/configs/");
      const nextConfigs = data.configs || [];
      setConfigs(nextConfigs);
      const selected = nextConfigs.find((item: any) => configKey(item) === activeKey) || nextConfigs[0];
      if (selected) selectDirect(selected);
    } catch (error) { notify(error instanceof Error ? error.message : "系统配置读取失败", "error"); }
    finally { setLoading(false); }
  }

  useEffect(() => { if (user?.role === "super_admin") void load(); }, [user]);
  const filtered = useMemo(() => configs.filter((item) => category === "全部" || configCategory(configKey(item)) === category), [configs, category]);
  const active = configs.find((item) => configKey(item) === activeKey) || null;
  const dirty = draft !== original;
  const jsonError = validateJSON(draft);

  /** selectDirect 不经确认直接切换当前配置。 */
  function selectDirect(item: any) {
    const value = prettyJSON(item.config_value ?? item.value);
    setActiveKey(configKey(item));
    setDraft(value);
    setOriginal(value);
  }

  /** selectConfig 在存在未保存修改时确认是否放弃。 */
  async function selectConfig(item: any) {
    if (configKey(item) === activeKey) return;
    if (dirty && !(await confirm("放弃未保存修改", "当前配置有未保存修改，确认切换到其他配置吗？"))) return;
    selectDirect(item);
  }

  /** save 校验并保存当前系统配置。 */
  async function save() {
    if (!active || jsonError) return notify(jsonError || "请选择系统配置", "warning");
    setLoading(true);
    try { const formatted = prettyJSON(draft); await cloudRequest(`/api/admin/system/configs/${encodeURIComponent(activeKey)}`, { method: "PUT", body: { config_value: formatted } }); setDraft(formatted); setOriginal(formatted); notify(`${configTitle(activeKey)}已保存`, "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "保存失败", "error"); } finally { setLoading(false); }
  }

  /** resetDraft 恢复当前配置的服务端内容。 */
  function resetDraft() { setDraft(original); }

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;
  return <><PageHeader title="系统配置" description="配置按业务用途分组。保存前会校验 JSON，错误内容不会提交。" actions={<><RefreshButton loading={loading} onClick={() => void load()} /><Button variant="outlined" startIcon={<RestartAltRoundedIcon />} disabled={!dirty || loading} onClick={resetDraft}>撤销修改</Button><Button variant="contained" startIcon={<SaveRoundedIcon />} disabled={!active || !dirty || Boolean(jsonError) || loading} onClick={() => void save()}>保存当前配置</Button></>} />
    <Tabs value={category} onChange={(_, value) => setCategory(value)} variant="scrollable" scrollButtons="auto" sx={{ mb: 2, borderBottom: "1px solid", borderColor: "divider" }}>{categories.map((item) => <Tab key={item} value={item} label={item} />)}</Tabs>
    {configs.length ? <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "260px minmax(0, 1fr)" }, gap: 2 }}><Stack spacing={1}>{filtered.map((item) => { const key = configKey(item); return <Button key={key} color={key === activeKey ? "primary" : "secondary"} variant={key === activeKey ? "contained" : "outlined"} onClick={() => void selectConfig(item)} sx={{ display: "block", minHeight: 74, p: 1.5, borderRadius: "8px", textAlign: "left" }}><Typography sx={{ fontWeight: 760 }}>{configTitle(key)}</Typography><Typography sx={{ mt: 0.25, opacity: 0.72, fontSize: 11.5 }}>{item.description || key}</Typography></Button>; })}</Stack><SectionPanel>{active ? <><Stack direction={{ xs: "column", sm: "row" }} spacing={1.5} sx={{ mb: 2, justifyContent: "space-between", alignItems: { sm: "flex-start" } }}><Box><Typography component="h2" sx={{ fontSize: 20, fontWeight: 780 }}>{configTitle(activeKey)}</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}>{active.description || activeKey}</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontFamily: "monospace", fontSize: 11 }}>{activeKey}</Typography></Box><Stack direction="row" spacing={1}><Chip size="small" color={active.enabled === false ? "default" : "success"} label={active.enabled === false ? "已停用" : "已启用"} />{dirty ? <Chip size="small" color="warning" label="有未保存修改" /> : null}</Stack></Stack>{activeKey === "ai.default_prompts" ? <Alert severity="info" sx={{ mb: 2 }}>这里分别保存首次筛选、打开详情和最终复核使用的系统默认提示词。岗位模板留空时会读取这些值。</Alert> : null}{jsonError ? <Alert severity="error" sx={{ mb: 2 }}>{jsonError}</Alert> : null}<JsonEditor value={draft} onChange={setDraft} /></> : <EmptyState text="请选择一项配置" />}</SectionPanel></Box> : <SectionPanel><EmptyState text="暂无系统配置" /></SectionPanel>}
  </>;
}

/** configKey 返回配置记录的统一键名。 */
function configKey(item: any) { return String(item?.config_key || item?.key || ""); }

/** configCategory 根据配置键返回业务分类。 */
function configCategory(key: string): (typeof categories)[number] {
  if (key.startsWith("ai.") || key.includes("prompt")) return "AI 配置";
  if (key.includes("subscription") || key.includes("payment")) return "订阅支付";
  if (key.includes("onboarding") || key.includes("runtime") || key.includes("agent")) return "本地组件";
  if (key.includes("invite") || key.includes("guide") || key.includes("help")) return "邀请帮助";
  return "基础配置";
}

/** configTitle 返回系统配置的中文标题。 */
function configTitle(key: string) { return ({ "ai.default_prompts": "AI 默认提示词", "system.app_config": "公共系统配置", "system.subscription_plans": "订阅套餐", "system.onboarding_config": "本地程序与组件", "system.invite_config": "邀请奖励", "system.guide": "帮助中心" } as Record<string, string>)[key] || key; }

/** prettyJSON 将任意配置值格式化为缩进 JSON。 */
function prettyJSON(value: unknown) { try { const parsed = typeof value === "string" ? JSON.parse(value) : value; return JSON.stringify(parsed ?? {}, null, 2); } catch { return String(value || ""); } }

/** validateJSON 校验 JSON 文本并返回中文错误。 */
function validateJSON(value: string) { try { JSON.parse(value || "{}"); return ""; } catch (error) { return `JSON 语法错误：${error instanceof Error ? error.message : "格式不正确"}`; } }
