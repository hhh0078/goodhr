/** 本文件负责超级管理员按类别、账号、岗位和时间查阅全站运行及异常退出日志。 */
"use client";

import { Alert, Box, Button, Chip, MenuItem, Pagination, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { useAdmin } from "@/components/admin/AdminApp";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { cloudRequest, formatDate } from "@/lib/admin-api";

type LogEvent = {
  message?: string;
  error_message?: string;
  exit_reason?: string;
  last_action?: string;
  page_url?: string;
  action?: string;
  step?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
};

type LogItem = {
  id: string;
  account: string;
  position_name: string;
  position_id: string;
  category: string;
  message: string;
  created_at: string;
  event?: LogEvent;
};

type Filters = { category: string; account: string; position: string; from: string; to: string };

const emptyFilters: Filters = { category: "", account: "", position: "", from: "", to: "" };

/** toISOString 将本地输入的时间转换为云端筛选使用的时间。 */
function toISOString(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}

/** readableMessage 从结构化日志中提取最方便阅读的步骤与错误。 */
function readableMessage(item: LogItem): string {
  const event = item.event;
  return String(event?.message || event?.error_message || event?.exit_reason || event?.last_action || item.message || "运行记录");
}

/** LogsPage 显示跨账号日志和异常退出详细信息。 */
export default function LogsPage() {
  const { user, notify } = useAdmin();
  const [filters, setFilters] = useState<Filters>(emptyFilters);
  const [applied, setApplied] = useState<Filters>(emptyFilters);
  const [items, setItems] = useState<LogItem[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  /** load 从云端读取指定页的全账号日志。 */
  async function load(nextPage: number, nextFilters: Filters) {
    setLoading(true);
    try {
      const params = new URLSearchParams({ page: String(nextPage), page_size: "50" });
      if (nextFilters.category) params.set("category", nextFilters.category);
      if (nextFilters.account.trim()) params.set("account", nextFilters.account.trim());
      if (nextFilters.position.trim()) params.set("position", nextFilters.position.trim());
      if (nextFilters.from) params.set("from", toISOString(nextFilters.from));
      if (nextFilters.to) params.set("to", toISOString(nextFilters.to));
      const data = await cloudRequest(`/api/admin/logs?${params}`);
      setItems(Array.isArray(data.logs) ? data.logs : []);
      setTotal(Number(data.total || 0));
      setPage(nextPage);
    } catch (error) {
      notify(error instanceof Error ? error.message : "日志没读出来，请稍后再试", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (user?.role === "super_admin") void load(1, emptyFilters);
  }, [user?.role]);

  /** applyFilters 应用顶部筛选并回到第一页。 */
  function applyFilters() {
    setApplied(filters);
    void load(1, filters);
  }

  /** resetFilters 清空筛选条件并重新读取日志。 */
  function resetFilters() {
    setFilters(emptyFilters);
    setApplied(emptyFilters);
    void load(1, emptyFilters);
  }

  if (user?.role !== "super_admin") return <Alert severity="warning">这里只给管理员看日志。</Alert>;

  return (
    <Stack spacing={2.5}>
      <PageHeader title="运行日志" description="按账号和岗位看看程序当时在忙什么，闪退原因也放在这里。" />
      <SectionPanel>
        <Typography variant="h6" sx={{ mb: 2 }}>筛选日志</Typography>
        <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} sx={{ flexWrap: "wrap" }} useFlexGap>
          <TextField select label="日志类型" value={filters.category} onChange={(event) => setFilters({ ...filters, category: event.target.value })} sx={{ minWidth: 155 }} size="small">
            <MenuItem value="">全部</MenuItem><MenuItem value="运行">运行</MenuItem><MenuItem value="错误">错误</MenuItem><MenuItem value="异常退出">异常退出</MenuItem>
          </TextField>
          <TextField label="账号" placeholder="邮箱关键词" value={filters.account} onChange={(event) => setFilters({ ...filters, account: event.target.value })} size="small" />
          <TextField label="岗位名称" value={filters.position} onChange={(event) => setFilters({ ...filters, position: event.target.value })} size="small" />
          <TextField label="开始时间" type="datetime-local" value={filters.from} onChange={(event) => setFilters({ ...filters, from: event.target.value })} size="small" slotProps={{ inputLabel: { shrink: true } }} />
          <TextField label="结束时间" type="datetime-local" value={filters.to} onChange={(event) => setFilters({ ...filters, to: event.target.value })} size="small" slotProps={{ inputLabel: { shrink: true } }} />
          <Button variant="contained" onClick={applyFilters} disabled={loading}>查询</Button>
          <Button variant="text" onClick={resetFilters} disabled={loading}>清空</Button>
        </Stack>
      </SectionPanel>
      <SectionPanel>
        <Typography variant="h6" sx={{ mb: 2 }}>日志记录（{total}）</Typography>
        {items.length === 0 ? <EmptyState text={loading ? "正在读取日志，我马上整理好" : "这里暂时空空的，换个筛选条件再看看。"} /> : (
          <Stack spacing={1.3}>
            {items.map((item) => {
              const event = item.event;
              const isError = item.category !== "运行";
              return (
                <Box key={item.id} sx={{ border: "1px solid", borderColor: isError ? "error.light" : "divider", borderRadius: 2, p: 1.8, bgcolor: "background.paper" }}>
                  <Stack direction={{ xs: "column", sm: "row" }} spacing={1} sx={{ alignItems: { sm: "center" }, flexWrap: "wrap" }} useFlexGap>
                    <Chip size="small" color={isError ? "error" : "success"} label={item.category} />
                    <Typography variant="body2" color="text.secondary">{formatDate(item.created_at)}</Typography>
                    <Typography variant="body2" sx={{ fontWeight: 700 }}>{item.account}</Typography>
                    <Typography variant="body2" color="text.secondary">{item.position_name || "无岗位"}</Typography>
                  </Stack>
                  <Typography sx={{ mt: 1, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{readableMessage(item)}</Typography>
                  {event?.error_message && event.error_message !== readableMessage(item) && <Typography color="error.main" sx={{ mt: 0.8, overflowWrap: "anywhere" }}>{event.error_message}</Typography>}
                  {event?.last_action && <Typography variant="body2" sx={{ mt: 0.8 }}>退出前：{event.last_action}</Typography>}
                  {event?.page_url && <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: "anywhere" }}>页面：{event.page_url}</Typography>}
                  {(event?.input || event?.output) && <Box component="details" sx={{ mt: 1 }}><Box component="summary" sx={{ cursor: "pointer", color: "text.secondary", fontSize: 13 }}>查看入参和出参</Box><Box component="pre" sx={{ overflowX: "auto", whiteSpace: "pre-wrap", fontSize: 12 }}>{JSON.stringify({ input: event.input || {}, output: event.output || {} }, null, 2)}</Box></Box>}
                </Box>
              );
            })}
          </Stack>
        )}
        {total > 50 && <Pagination sx={{ mt: 2 }} count={Math.ceil(total / 50)} page={page} onChange={(_, nextPage) => void load(nextPage, applied)} />}
      </SectionPanel>
    </Stack>
  );
}
