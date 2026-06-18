/** 本文件负责新版后台简历库的搜索、分页、详情跳转和清空。 */
"use client";

import DeleteSweepRoundedIcon from "@mui/icons-material/DeleteSweepRounded";
import SearchRoundedIcon from "@mui/icons-material/SearchRounded";
import { Avatar, Box, Button, Chip, InputAdornment, Pagination, Stack, TextField, Typography } from "@mui/material";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** ResumesPage 展示云端保存的候选人简历列表。 */
export default function ResumesPage() {
  const params = useSearchParams();
  const { notify, confirm } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const taskID = params.get("task_id") || "";

  /** load 按任务、关键词和页码读取简历。 */
  async function load(nextPage = page) {
    setLoading(true);
    try {
      const query = new URLSearchParams({ page: String(nextPage), page_size: "20" });
      if (taskID) query.set("task_id", taskID);
      if (keyword.trim()) query.set("keyword", keyword.trim());
      const data = await cloudRequest(`/api/candidates?${query.toString()}`);
      setItems(data.candidates || []); setTotal(Number(data.total || 0)); setPage(Number(data.page || nextPage));
    } catch (error) { notify(error instanceof Error ? error.message : "简历读取失败", "error"); }
    finally { setLoading(false); }
  }

  useEffect(() => { void load(1); }, [taskID]);

  /** clearAll 清空当前团队的全部简历。 */
  async function clearAll() {
    if (!(await confirm("清空简历库", "该操作会删除当前团队全部简历，确认继续吗？"))) return;
    try { const data = await cloudRequest("/api/candidates", { method: "DELETE" }); notify(`已删除 ${Number(data.deleted || 0)} 份简历`, "success"); await load(1); } catch (error) { notify(error instanceof Error ? error.message : "清空失败", "error"); }
  }

  return <><PageHeader title="简历库" description={taskID ? "当前仅显示指定任务产生的简历。" : "搜索并查看已同步到云端的结构化简历。"} actions={<Button color="error" startIcon={<DeleteSweepRoundedIcon />} onClick={() => void clearAll()}>清空简历库</Button>} /><SectionPanel><Stack direction={{ xs: "column", sm: "row" }} spacing={1.5} sx={{ mb: 2 }}><TextField value={keyword} onChange={(event) => setKeyword(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void load(1); }} placeholder="搜索姓名、岗位、公司或关键词" fullWidth slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchRoundedIcon /></InputAdornment> } }} /><Button variant="contained" disabled={loading} onClick={() => void load(1)}>搜索</Button></Stack>{items.length ? <Stack>{items.map((item) => <Button key={item.id} component={Link} href={`/admin/resumes/detail?candidate_id=${encodeURIComponent(item.id)}${item.engagement_id ? `&engagement_id=${encodeURIComponent(item.engagement_id)}` : ""}`} color="secondary" sx={{ display: "grid", gridTemplateColumns: { xs: "48px 1fr", md: "48px minmax(180px, .8fr) minmax(180px, 1fr) 110px 150px" }, gap: 1.5, py: 1.75, px: 1, justifyContent: "stretch", textAlign: "left", borderRadius: "8px", borderBottom: "1px solid", borderColor: "divider" }}><Avatar src={item.avatar_url || ""}>{String(item.name || "?").slice(0, 1)}</Avatar><Box><Typography sx={{ fontWeight: 760 }}>{item.name || "未命名候选人"}</Typography><Typography sx={{ color: "text.secondary", fontSize: 12 }}>{[item.age ? `${item.age}岁` : "", item.education, item.experience].filter(Boolean).join(" · ") || "暂无基础信息"}</Typography></Box><Typography sx={{ display: { xs: "none", md: "block" } }}>{item.position_name || item.expected_position || "--"}</Typography><Chip size="small" color={Number(item.score || item.analysis_score || 0) >= 60 ? "success" : "default"} label={`${Number(item.score || item.analysis_score || 0)} 分`} sx={{ display: { xs: "none", md: "inline-flex" }, justifySelf: "start" }} /><Typography sx={{ display: { xs: "none", md: "block" }, color: "text.secondary", fontSize: 12 }}>{formatDate(item.created_at || item.updated_at)}</Typography></Button>)}</Stack> : <EmptyState text={loading ? "正在读取简历" : "暂无简历"} />}<Stack sx={{ mt: 3, alignItems: "center" }}><Pagination page={page} count={Math.max(1, Math.ceil(total / 20))} onChange={(_, value) => void load(value)} color="primary" /></Stack></SectionPanel></>;
}
