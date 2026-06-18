/** 本文件负责新版后台控制台概览。 */
"use client";

import ArticleRoundedIcon from "@mui/icons-material/ArticleRounded";
import BadgeRoundedIcon from "@mui/icons-material/BadgeRounded";
import TaskAltRoundedIcon from "@mui/icons-material/TaskAltRounded";
import WorkRoundedIcon from "@mui/icons-material/WorkRounded";
import { Box, Button, CircularProgress, Stack, Typography } from "@mui/material";
import Link from "next/link";
import { useEffect, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

const cards = [
  ["positions", "岗位模板", WorkRoundedIcon, "/admin/positions"], ["accounts", "平台账号", BadgeRoundedIcon, "/admin/accounts"], ["tasks", "招聘任务", TaskAltRoundedIcon, "/admin/tasks"], ["resumes", "简历总数", ArticleRoundedIcon, "/admin/resumes"],
] as const;

/** DashboardPage 展示账号、岗位、任务和简历的概览。 */
export default function DashboardPage() {
  const { agentBase } = useAdmin();
  const [loading, setLoading] = useState(true);
  const [counts, setCounts] = useState<Record<string, number>>({});

  useEffect(() => {
    Promise.allSettled([cloudRequest("/api/positions"), cloudRequest("/api/platform-accounts"), cloudRequest("/api/tasks"), cloudRequest("/api/candidates?page=1&page_size=1")]).then((results) => {
      const value = (index: number, key: string) => results[index].status === "fulfilled" ? results[index].value : {};
      setCounts({ positions: (value(0, "positions").positions || []).length, accounts: (value(1, "accounts").accounts || []).length, tasks: (value(2, "tasks").tasks || []).length, resumes: Number(value(3, "resumes").total || 0) });
    }).finally(() => setLoading(false));
  }, []);

  return <><PageHeader title="控制台" description="查看当前数据和本地程序连接状态。" /><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", lg: "repeat(4, 1fr)" }, gap: 2 }}>{cards.map(([key, label, Icon, href]) => <SectionPanel key={key}><Stack direction="row" sx={{ alignItems: "center", justifyContent: "space-between" }}><Icon color="primary" /><Typography color="text.secondary" sx={{ fontSize: 13 }}>{label}</Typography></Stack><Typography sx={{ mt: 2, fontSize: 34, fontWeight: 800 }}>{loading ? <CircularProgress size={24} /> : counts[key] || 0}</Typography><Button component={Link} href={href} size="small" sx={{ mt: 1, px: 0 }}>查看详情</Button></SectionPanel>)}</Box><SectionPanel sx={{ mt: 2 }}><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>开始使用</Typography><Typography sx={{ mt: 1, color: "text.secondary" }}>先创建平台账号和岗位模板，再到任务列表建立招聘任务。</Typography><Stack direction={{ xs: "column", sm: "row" }} spacing={1.5} sx={{ mt: 2.5 }}><Button component={Link} href="/admin/accounts" variant="outlined">管理平台账号</Button><Button component={Link} href="/admin/positions" variant="outlined">创建岗位模板</Button><Button component={Link} href="/admin/tasks" variant="contained">开始招聘任务</Button></Stack><Typography sx={{ mt: 2.5, color: agentBase ? "primary.main" : "error.main", fontWeight: 700 }}>{agentBase ? `本地程序已连接：${agentBase}` : "尚未检测到本地程序，启动任务前请先打开 GoodHR。"}</Typography></SectionPanel></>;
}
