/** 本文件负责新版后台简历库的搜索、分页、详情跳转和清空。 */
"use client";

import DeleteSweepRoundedIcon from "@mui/icons-material/DeleteSweepRounded";
import SearchRoundedIcon from "@mui/icons-material/SearchRounded";
import { Avatar, Box, Button, InputAdornment, MenuItem, Pagination, Stack, TextField, Typography } from "@mui/material";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { experienceLine, normalizeCandidate, scoreText, type NormalizedCandidate } from "@/lib/candidate-normalize";

/** ResumesPage 展示云端保存的候选人简历列表。 */
export default function ResumesPage() {
  const params = useSearchParams();
  const { notify, confirm } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [keyword, setKeyword] = useState("");
  const [tasks, setTasks] = useState<any[]>([]);
  const [positions, setPositions] = useState<any[]>([]);
  const [selectedTask, setSelectedTask] = useState(params.get("task_id") || "");
  const [selectedPosition, setSelectedPosition] = useState("");
  const [pageSize, setPageSize] = useState(20);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const taskID = params.get("task_id") || "";
  const candidates = useMemo(() => items.map(normalizeCandidate), [items]);

  /** load 按任务、关键词和页码读取简历。 */
  async function load(nextPage = page) {
    setLoading(true);
    try {
      const query = new URLSearchParams({ page: String(nextPage), page_size: String(pageSize) });
      if (selectedTask || taskID) query.set("task_id", selectedTask || taskID);
      if (selectedPosition) query.set("position_id", selectedPosition);
      if (keyword.trim()) query.set("q", keyword.trim());
      const data = await cloudRequest(`/api/candidates?${query}`);
      setItems(data.candidates || data.items || []);
      setTotal(Number(data.total || 0));
      setPage(Number(data.page || nextPage));
    } catch (error) {
      notify(error instanceof Error ? error.message : "简历读取失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** loadFilters 读取任务和岗位筛选项。 */
  async function loadFilters() {
    const [taskData, positionData] = await Promise.allSettled([cloudRequest("/api/tasks"), cloudRequest("/api/positions")]);
    if (taskData.status === "fulfilled") setTasks(taskData.value.tasks || []);
    if (positionData.status === "fulfilled") setPositions(positionData.value.positions || []);
  }

  useEffect(() => { void loadFilters(); }, []);
  useEffect(() => { void load(1); }, [selectedTask, selectedPosition, pageSize]);

  /** resetFilters 清空简历筛选条件。 */
  function resetFilters() {
    setKeyword("");
    setSelectedTask(taskID);
    setSelectedPosition("");
    void load(1);
  }

  /** clearAll 清空当前团队简历库。 */
  async function clearAll() {
    if (!(await confirm("清空简历库", "我小声确认一下，清空后这些简历记录就找不回来了。继续吗？"))) return;
    try {
      const data = await cloudRequest("/api/candidates", { method: "DELETE" });
      notify(`已删除 ${Number(data.deleted || 0)} 份简历`, "success");
      await load(1);
    } catch (error) {
      notify(error instanceof Error ? error.message : "清空失败", "error");
    }
  }

  return <>
    <PageHeader title="简历库" description={selectedTask ? "当前显示指定任务产生的简历。" : "按任务、岗位和关键词筛选结构化简历。"} actions={<Button color="error" startIcon={<DeleteSweepRoundedIcon />} onClick={() => void clearAll()}>清空简历库</Button>} />
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(240px,1fr) 220px 220px 100px auto auto" }, gap: 1.25, mb: 1.5 }}>
      <TextField size="small" value={keyword} onChange={(event) => setKeyword(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void load(1); }} placeholder="搜索姓名、岗位、公司或关键词" slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchRoundedIcon /></InputAdornment> } }} />
      <TextField select size="small" label="任务" value={selectedTask} onChange={(event) => setSelectedTask(event.target.value)}><MenuItem value="">全部任务</MenuItem>{tasks.map((item) => <MenuItem key={item.id} value={item.id}>{item.name}</MenuItem>)}</TextField>
      <TextField select size="small" label="岗位" value={selectedPosition} onChange={(event) => setSelectedPosition(event.target.value)}><MenuItem value="">全部岗位</MenuItem>{positions.map((item) => <MenuItem key={item.id} value={item.id}>{item.name}</MenuItem>)}</TextField>
      <TextField select size="small" label="每页" value={pageSize} onChange={(event) => setPageSize(Number(event.target.value))}><MenuItem value={20}>20</MenuItem><MenuItem value={50}>50</MenuItem><MenuItem value={100}>100</MenuItem></TextField>
      <Button variant="contained" disabled={loading} onClick={() => void load(1)}>查询</Button>
      <Button color="secondary" onClick={resetFilters}>重置</Button>
    </Box>
    <SectionPanel sx={{ p: 0, overflow: "hidden" }}>
      {candidates.length ? <>
        <Box sx={{ display: { xs: "none", md: "grid" }, gridTemplateColumns: "1.1fr 1.6fr .8fr", px: 2, py: 1.5, bgcolor: "#fafbfa", borderBottom: "1px solid", borderColor: "divider", "& p": { fontWeight: 800 } }}>
          <Typography>候选人</Typography><Typography>经历</Typography><Typography>AI分析</Typography>
        </Box>
        <Stack>{candidates.map((item) => <ResumeRow key={`${item.id}-${item.engagementId}`} item={item} />)}</Stack>
      </> : <EmptyState text={loading ? "正在读取简历" : "暂无简历"} />}
      <Stack direction={{ xs: "column", sm: "row" }} spacing={2} sx={{ p: 2, justifyContent: "space-between", alignItems: "center", borderTop: "1px solid", borderColor: "divider" }}>
        <Typography color="text.secondary">共 {total} 份简历</Typography>
        <Pagination page={page} count={Math.max(1, Math.ceil(total / pageSize))} onChange={(_, value) => void load(value)} color="primary" />
      </Stack>
    </SectionPanel>
  </>;
}

/** ResumeRow 展示一行简历库候选人。 */
function ResumeRow({ item }: { item: NormalizedCandidate }) {
  const href = `/admin/resumes/detail?candidate_id=${encodeURIComponent(item.id)}${item.engagementId ? `&engagement_id=${encodeURIComponent(item.engagementId)}` : ""}`;
  const facts = [item.workRegion, item.age ? `${item.age}岁` : "", item.gender, item.workYears, item.educationLevel].filter(Boolean).join(" / ");
  const ownerLine = [item.creatorEmail ? `创建人：${item.creatorEmail}` : "", item.createdAt ? `创建时间：${formatDate(item.createdAt)}` : ""].filter(Boolean).join("  ");
  const experiences = [...item.workExperiences, ...item.educations].map(experienceLine).filter(Boolean).slice(0, 3);
  return <Button component={Link} href={href} color="secondary" sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1.1fr 1.6fr .8fr" }, gap: { xs: 1.25, md: 2 }, alignItems: "center", width: "100%", px: 2, py: 2, textAlign: "left", borderRadius: 0, borderBottom: "1px solid", borderColor: "divider" }}>
    <Stack direction="row" spacing={1.5} sx={{ minWidth: 0, alignItems: "center" }}>
      <Avatar src={item.avatarUrl}>{item.name.slice(0, 1)}</Avatar>
      <Box sx={{ minWidth: 0 }}><Typography noWrap sx={{ fontWeight: 820 }}>{item.name}</Typography><Typography noWrap sx={{ mt: 0.4, color: "text.secondary", fontSize: 13 }}>{facts || "暂无基础信息"}</Typography><Typography noWrap sx={{ mt: 0.6 }}>{item.expectedPosition || "暂无期望职位"}</Typography>{ownerLine ? <Typography noWrap sx={{ mt: 0.5, color: "text.secondary", fontSize: 12 }}>{ownerLine}</Typography> : null}</Box>
    </Stack>
    <Stack spacing={0.6} sx={{ minWidth: 0 }}>{experiences.length ? experiences.map((line) => <Typography key={line} noWrap sx={{ fontSize: 14 }}>{line}</Typography>) : <Typography color="text.secondary">暂无经历</Typography>}</Stack>
    <Stack spacing={0.8} sx={{ minWidth: 0 }}><AIText label="第一次" score={item.aiFirstAnalysis.score} reason={item.aiFirstAnalysis.reason} /><AIText label="第二次" score={item.aiSecondAnalysis.score} reason={item.aiSecondAnalysis.reason} /></Stack>
  </Button>;
}

/** AIText 展示一次 AI 判断结果。 */
function AIText({ label, score, reason }: { label: string; score: unknown; reason: string }) {
  if (!reason && scoreText(score) === "无") return null;
  const text = `${label} ${scoreText(score)}${reason ? `：${reason}` : ""}`;
  return <Typography title={text} sx={{ color: "text.secondary", display: "-webkit-box", fontSize: 12, lineHeight: 1.6, overflow: "hidden", overflowWrap: "anywhere", WebkitBoxOrient: "vertical", WebkitLineClamp: 3, whiteSpace: "normal" }}><Box component="span" sx={{ color: "#16724c", fontWeight: 800 }}>{label} {scoreText(score)}</Box>{reason ? `：${reason}` : ""}</Typography>;
}
