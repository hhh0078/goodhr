/** 本文件负责超级管理员查看和编辑云端系统原始配置。 */
"use client";

import SaveRoundedIcon from "@mui/icons-material/SaveRounded";
import { Button, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** SystemConfigPage 管理系统配置 JSON。 */
export default function SystemConfigPage() {
  const { user, notify } = useAdmin();
  const [configs, setConfigs] = useState<any[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);

  /** load 读取全部系统配置并格式化 JSON。 */
  async function load() { setLoading(true); try { const data = await cloudRequest("/api/admin/system/configs/"); const nextConfigs = data.configs || []; setConfigs(nextConfigs); setValues(Object.fromEntries(nextConfigs.map((item: any) => [item.config_key || item.key, prettyJSON(item.config_value ?? item.value)]))); } catch (error) { notify(error instanceof Error ? error.message : "系统配置读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { if (user?.role === "super_admin") void load(); }, [user]);

  /** save 校验并保存指定系统配置 JSON。 */
  async function save(key: string) { const text = values[key] || ""; try { JSON.parse(text); await cloudRequest(`/api/admin/system/configs/${encodeURIComponent(key)}`, { method: "PUT", body: { config_value: text } }); notify(`${key} 已保存`, "success"); await load(); } catch (error) { notify(error instanceof SyntaxError ? "配置不是有效 JSON" : error instanceof Error ? error.message : "保存失败", "error"); } }

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;
  return <><PageHeader title="系统配置" description="这里直接编辑云端系统 JSON，请确认格式正确后保存。" actions={<RefreshButton loading={loading} onClick={() => void load()} />} /><Stack spacing={2}>{configs.length ? configs.map((item) => { const key = item.config_key || item.key; return <SectionPanel key={key}><Stack direction="row" sx={{ mb: 1.5, justifyContent: "space-between", alignItems: "center" }}><Typography component="h2" sx={{ fontSize: 18, fontWeight: 760 }}>{key}</Typography><Button variant="contained" startIcon={<SaveRoundedIcon />} onClick={() => void save(key)}>保存</Button></Stack><TextField value={values[key] || ""} onChange={(event) => setValues((current) => ({ ...current, [key]: event.target.value }))} multiline minRows={10} fullWidth slotProps={{ input: { sx: { fontFamily: "monospace", fontSize: 13 } } }} /></SectionPanel>; }) : <SectionPanel><EmptyState text="暂无系统配置" /></SectionPanel>}</Stack></>;
}

/** prettyJSON 将系统配置值格式化为易读 JSON。 */
function prettyJSON(value: unknown) { try { const parsed = typeof value === "string" ? JSON.parse(value) : value; return JSON.stringify(parsed ?? {}, null, 2); } catch { return String(value || ""); } }
