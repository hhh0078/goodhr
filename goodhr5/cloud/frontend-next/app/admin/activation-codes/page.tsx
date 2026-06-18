/** 本文件负责超级管理员生成、查看和复制会员激活码。 */
"use client";

import ContentCopyRoundedIcon from "@mui/icons-material/ContentCopyRounded";
import { Button, Chip, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** ActivationCodesPage 管理会员激活码。 */
export default function ActivationCodesPage() {
  const { user, notify } = useAdmin();
  const [codes, setCodes] = useState<any[]>([]);
  const [generated, setGenerated] = useState<any[]>([]);
  const [form, setForm] = useState({ days: 30, count: 1, remark: "" });
  const [loading, setLoading] = useState(false);

  /** load 读取全部会员激活码。 */
  async function load() { setLoading(true); try { const data = await cloudRequest("/api/admin/activation-codes"); setCodes(data.codes || []); } catch (error) { notify(error instanceof Error ? error.message : "激活码读取失败", "error"); } finally { setLoading(false); } }
  useEffect(() => { if (user?.role === "super_admin") void load(); }, [user]);

  /** createCodes 批量生成会员激活码。 */
  async function createCodes() { try { const data = await cloudRequest("/api/admin/activation-codes", { method: "POST", body: form }); setGenerated(data.codes || []); notify(`已生成 ${(data.codes || []).length} 个激活码`, "success"); await load(); } catch (error) { notify(error instanceof Error ? error.message : "生成激活码失败", "error"); } }

  /** copyGenerated 复制本次生成的全部激活码。 */
  async function copyGenerated() { try { await navigator.clipboard.writeText(generated.map((item) => item.code).join("\n")); notify("激活码已复制", "success"); } catch { notify("复制失败", "error"); } }

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以访问此页面" /></SectionPanel>;
  return <><PageHeader title="激活码管理" actions={<RefreshButton loading={loading} onClick={() => void load()} />} /><SectionPanel sx={{ mb: 2 }}><Stack direction={{ xs: "column", md: "row" }} spacing={1.5}><TextField label="会员天数" type="number" value={form.days} onChange={(event) => setForm({ ...form, days: Number(event.target.value) })} /><TextField label="生成数量" type="number" value={form.count} onChange={(event) => setForm({ ...form, count: Number(event.target.value) })} /><TextField label="备注" value={form.remark} onChange={(event) => setForm({ ...form, remark: event.target.value })} fullWidth /><Button variant="contained" onClick={() => void createCodes()}>生成激活码</Button></Stack>{generated.length ? <Stack direction="row" spacing={1} sx={{ mt: 2, alignItems: "center" }}><Typography color="text.secondary">本次生成：{generated.map((item) => item.code).join("、")}</Typography><Button startIcon={<ContentCopyRoundedIcon />} onClick={() => void copyGenerated()}>复制全部</Button></Stack> : null}</SectionPanel><SectionPanel>{codes.length ? <Stack>{codes.map((item) => <Stack key={item.id || item.code} direction={{ xs: "column", md: "row" }} spacing={2} sx={{ py: 1.5, borderBottom: "1px solid", borderColor: "divider", justifyContent: "space-between" }}><Typography sx={{ fontFamily: "monospace", fontWeight: 760 }}>{item.code}</Typography><Typography>{item.days} 天</Typography><Chip size="small" color={item.status === "used" ? "default" : "success"} label={item.status === "used" ? "已使用" : "未使用"} /><Typography color="text.secondary">{item.used_by_email || item.remark || "--"}</Typography><Typography color="text.secondary">{formatDate(item.used_at || item.created_at)}</Typography></Stack>)}</Stack> : <EmptyState text="暂无激活码" />}</SectionPanel></>;
}
