/** 本文件负责展示团队成员在指定时间周期内的招聘统计。 */
"use client";

import RefreshRoundedIcon from "@mui/icons-material/RefreshRounded";
import { Box, Button, MenuItem, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useMemo, useState } from "react";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest } from "@/lib/admin-api";

const periodOptions = [
  { value: "month", label: "本月" },
  { value: "today", label: "今天" },
  { value: "week", label: "本周" },
  { value: "last_month", label: "上月" },
  { value: "custom", label: "自定义" },
];

const metricFields = [
  ["greeted_count", "打招呼"],
  ["resume_count", "新增简历"],
  ["scanned_count", "扫描人数"],
  ["detail_count", "获取详情"],
  ["skipped_count", "跳过"],
  ["failed_count", "失败"],
  ["task_count", "任务数"],
];

/** TeamStatsPage 展示团队招聘统计。 */
export default function TeamStatsPage() {
  const { notify } = useAdmin();
  const [period, setPeriod] = useState("month");
  const [startDate, setStartDate] = useState("");
  const [endDate, setEndDate] = useState("");
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any>({ totals: {}, members: [] });
  const members = useMemo(() => Array.isArray(data.members) ? data.members : [], [data.members]);

  /** load 读取团队统计数据。 */
  async function load() {
    setLoading(true);
    try {
      const query = new URLSearchParams({ period });
      if (period === "custom") {
        if (startDate) query.set("start_date", startDate);
        if (endDate) query.set("end_date", endDate);
      }
      const next = await cloudRequest(`/api/team/stats?${query}`);
      setData(next);
      if (!startDate) setStartDate(next.start_date || "");
      if (!endDate) setEndDate(next.end_date || "");
    } catch (error) {
      notify(error instanceof Error ? error.message : "统计读取失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, [period]);

  return <>
    <PageHeader title="团队统计" description={`${data.start_date || ""} 至 ${data.end_date || ""}，我先把大家的关键数字摆齐。`} actions={<Button variant="outlined" startIcon={<RefreshRoundedIcon />} disabled={loading} onClick={() => void load()}>{loading ? "刷新中" : "刷新"}</Button>} />
    <SectionPanel sx={{ mb: 1.5 }}>
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: period === "custom" ? "180px 180px 180px auto" : "180px auto" }, gap: 1.25, alignItems: "center" }}>
        <TextField select size="small" label="时间周期" value={period} onChange={(event) => setPeriod(event.target.value)}>{periodOptions.map((item) => <MenuItem key={item.value} value={item.value}>{item.label}</MenuItem>)}</TextField>
        {period === "custom" ? <>
          <TextField size="small" label="开始日期" type="date" value={startDate} onChange={(event) => setStartDate(event.target.value)} slotProps={{ inputLabel: { shrink: true } }} />
          <TextField size="small" label="结束日期" type="date" value={endDate} onChange={(event) => setEndDate(event.target.value)} slotProps={{ inputLabel: { shrink: true } }} />
        </> : null}
        <Button variant="contained" disabled={loading} onClick={() => void load()} sx={{ width: { xs: "100%", md: "auto" }, justifySelf: { md: "start" } }}>查询</Button>
      </Box>
    </SectionPanel>
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", md: "repeat(7, minmax(0, 1fr))" }, gap: 1.25, mb: 1.5 }}>
      {metricFields.map(([key, label]) => <MetricCard key={key} label={label} value={Number(data.totals?.[key] || 0)} />)}
    </Box>
    <SectionPanel sx={{ p: 0, overflow: "hidden" }}>
      {members.length ? <>
        <Box sx={{ display: { xs: "none", md: "grid" }, gridTemplateColumns: "minmax(180px,1.4fr) repeat(7, minmax(86px, .8fr))", gap: 1, px: 2, py: 1.4, bgcolor: "#f6faf7", borderBottom: "1px solid", borderColor: "divider", "& p": { fontWeight: 820, fontSize: 13 } }}>
          <Typography>员工</Typography>{metricFields.map(([, label]) => <Typography key={label} sx={{ textAlign: "right" }}>{label}</Typography>)}
        </Box>
        <Stack>{members.map((member: any) => <MemberRow key={member.email} member={member} />)}</Stack>
      </> : <EmptyState text={loading ? "正在统计，打工小助手正在扒拉算盘" : "这里暂时空空的，等团队跑起来我再认真记账"} />}
    </SectionPanel>
  </>;
}

/** MetricCard 展示单个统计数字。 */
function MetricCard({ label, value }: { label: string; value: number }) {
  return <Box sx={{ p: 1.5, borderRadius: "8px", border: "1px solid", borderColor: "divider", bgcolor: "#fbfdfc", minHeight: 82 }}><Typography sx={{ color: "text.secondary", fontSize: 13 }}>{label}</Typography><Typography sx={{ mt: 0.6, fontSize: 26, fontWeight: 840 }}>{value}</Typography></Box>;
}

/** MemberRow 展示单个员工的统计行。 */
function MemberRow({ member }: { member: any }) {
  return <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "minmax(180px,1.4fr) repeat(7, minmax(86px, .8fr))" }, gap: { xs: 0.7, md: 1 }, px: 2, py: 1.6, borderBottom: "1px solid", borderColor: "divider", alignItems: "center" }}>
    <Typography sx={{ fontWeight: 780, overflowWrap: "anywhere" }}>{member.email || "未命名员工"}</Typography>
    {metricFields.map(([key, label]) => <Box key={key} sx={{ display: "flex", justifyContent: "space-between", gap: 1 }}><Typography sx={{ display: { md: "none" }, color: "text.secondary", fontSize: 13 }}>{label}</Typography><Typography sx={{ ml: "auto", fontWeight: 760 }}>{Number(member[key] || 0)}</Typography></Box>)}
  </Box>;
}
