/** 本文件负责超级管理员按类别、账号、岗位和时间查阅全站运行及异常退出日志。 */
"use client";

import ExpandMoreRoundedIcon from "@mui/icons-material/ExpandMoreRounded";
import { Alert, Box, Button, Chip, IconButton, MenuItem, Pagination, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, TextField, Typography } from "@mui/material";
import { Fragment, useEffect, useState } from "react";
import { useAdmin } from "@/components/admin/AdminApp";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { cloudRequest } from "@/lib/admin-api";
import AIResponseDiagnosticDetails, { ordinaryDiagnosticOutput } from "@/components/admin/AIResponseDiagnosticDetails";

type LogEvent = {
  message?: string;
  error_message?: string;
  exit_reason?: string;
  last_action?: string;
  page_url?: string;
  action?: string;
  step?: string;
  status?: string;
  trace_id?: string;
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
const logActionLabels: Record<string, string> = {
  "browser.start": "启动浏览器",
  "browser.stop": "关闭浏览器",
  "browser.exit": "浏览器异常退出",
  "worker.exit": "浏览器工具异常退出",
  "agent.start": "启动本地程序",
  "agent.exit": "退出本地程序",
  "agent.previous_exit": "上次本地程序异常退出",
  start_browser: "启动浏览器",
  stop_browser: "关闭浏览器",
};
const ellipsisSx = { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };

/** toISOString 将本地输入的时间转换为云端筛选使用的时间。 */
function toISOString(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}

/** logText 只读取文本字段，避免把空值或对象显示为日志正文。 */
function logText(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** logEvent 兼容已解析的事件和旧接口返回的 JSON 字符串。 */
function logEvent(item: LogItem): LogEvent | undefined {
  if (item.event && typeof item.event === "object" && !Array.isArray(item.event)) return item.event;
  try {
    const parsed: unknown = JSON.parse(item.message);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return parsed as LogEvent;
  } catch {
    // 普通文本日志直接作为正文展示，无需按 JSON 解析。
  }
  return undefined;
}

/** readableMessage 优先展示错误或流程短句，将浏览器生命周期事件翻译成中文。 */
function readableMessage(item: LogItem, event?: LogEvent): string {
  const error = logText(event?.error_message);
  const message = logText(event?.message);
  if (item.category !== "运行" && error) return error;
  if (message) return message;
  if (error || logText(event?.exit_reason)) return error || logText(event?.exit_reason);
  const action = logActionLabels[logText(event?.action)] || logActionLabels[logText(event?.step)];
  if (action) {
    const status = logText(event?.status) || logText(event?.output?.status);
    if (status === "success") return `${action}成功`;
    if (status === "failed" || status === "error") return `${action}失败`;
    return action;
  }
  if (logText(event?.last_action)) return `最后操作：${logText(event?.last_action)}`;
  return event ? "运行记录，展开查看详情" : logText(item.message) || "运行记录";
}

/** formatLogTime 显示电脑本地时区下的完整日期和时间，精确到秒。 */
function formatLogTime(value: string): string {
  const date = new Date(value);
  if (!value || Number.isNaN(date.getTime())) return "--";
  return date.toLocaleString("zh-CN", {
    year: "numeric", month: "2-digit", day: "2-digit",
    hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
  });
}

/** LogRow 用单行摘要展示日志，展开后才显示完整错误、上下文及入参出参。 */
function LogRow({ item }: { item: LogItem }) {
  const [expanded, setExpanded] = useState(false);
  const event = logEvent(item);
  const message = readableMessage(item, event);
  const isError = item.category !== "运行";
  const detailID = `log-detail-${item.id}`;

  return (
    <Fragment>
      <TableRow hover onClick={() => setExpanded(!expanded)} sx={{ cursor: "pointer", height: 42, bgcolor: expanded ? "action.selected" : "background.paper" }}>
        <TableCell sx={{ whiteSpace: "nowrap", color: "text.secondary", fontVariantNumeric: "tabular-nums" }}>{formatLogTime(item.created_at)}</TableCell>
        <TableCell><Chip size="small" variant="outlined" color={isError ? "error" : "success"} label={item.category || "运行"} sx={{ height: 22, fontSize: 12 }} /></TableCell>
        <TableCell><Box title={item.account} sx={ellipsisSx}>{item.account || "--"}</Box></TableCell>
        <TableCell><Box title={item.position_name} sx={ellipsisSx}>{item.position_name || "无岗位"}</Box></TableCell>
        <TableCell><Box title={message} sx={{ ...ellipsisSx, color: isError ? "error.main" : "text.primary" }}>{message}</Box></TableCell>
        <TableCell align="center">
          <IconButton size="small" aria-label={expanded ? "收起日志详情" : "展开日志详情"} aria-expanded={expanded} aria-controls={expanded ? detailID : undefined} onClick={(click) => { click.stopPropagation(); setExpanded(!expanded); }}>
            <ExpandMoreRoundedIcon fontSize="small" sx={{ transform: expanded ? "rotate(180deg)" : "none" }} />
          </IconButton>
        </TableCell>
      </TableRow>
      {expanded && <TableRow>
        <TableCell colSpan={6} sx={{ p: "0 !important" }}>
          <Stack id={detailID} spacing={1.25} sx={{ px: 2, py: 1.5, bgcolor: "action.hover", overflowWrap: "anywhere" }}>
            <Typography variant="body2" sx={{ whiteSpace: "pre-wrap" }}>日志内容：{message}</Typography>
            <Typography variant="body2" color="text.secondary">账号：{item.account || "--"}　岗位：{item.position_name || "无岗位"}</Typography>
            {logText(event?.error_message) && <Alert severity="error" sx={{ whiteSpace: "pre-wrap", py: 0 }}>错误信息：{logText(event?.error_message)}</Alert>}
            {logText(event?.exit_reason) && <Typography variant="body2">退出原因：{logText(event?.exit_reason)}</Typography>}
            {logText(event?.last_action) && <Typography variant="body2">退出前操作：{logText(event?.last_action)}</Typography>}
            {logText(event?.page_url) && <Typography variant="body2">页面：{logText(event?.page_url)}</Typography>}
            {(logText(event?.action) || logText(event?.step)) && <Typography variant="body2" color="text.secondary">动作：{logText(event?.action) || "--"}　步骤：{logText(event?.step) || "--"}</Typography>}
            {logText(event?.trace_id) && <Typography variant="body2" color="text.secondary">跟踪编号：{logText(event?.trace_id)}</Typography>}
            <AIResponseDiagnosticDetails output={event?.output} />
            {(event?.input || event?.output) && <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 2 }}>
              {(["input", "output"] as const).map((field) => <Box key={field} sx={{ minWidth: 0 }}>
                <Typography variant="body2" sx={{ fontWeight: 600 }}>{field === "input" ? "入参" : "出参"}</Typography>
                <Box component="pre" sx={{ m: 0, mt: 0.5, p: 1.25, maxHeight: 260, overflow: "auto", whiteSpace: "pre-wrap", overflowWrap: "anywhere", fontSize: 12, bgcolor: "background.paper", borderRadius: 1 }}>{JSON.stringify(field === "output" ? ordinaryDiagnosticOutput(event.output) : event.input || {}, null, 2)}</Box>
              </Box>)}
            </Box>}
            {event && <Box component="details">
              <Box component="summary" sx={{ cursor: "pointer", fontSize: 13, color: "text.secondary" }}>查看原始日志</Box>
              <Box component="pre" sx={{ maxHeight: 320, overflow: "auto", whiteSpace: "pre-wrap", fontSize: 12 }}>{JSON.stringify(event, null, 2)}</Box>
            </Box>}
          </Stack>
        </TableCell>
      </TableRow>}
    </Fragment>
  );
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
        <Stack direction="row" sx={{ alignItems: "center", justifyContent: "space-between", mb: 1.5 }}>
          <Typography variant="h6">日志记录（{total}）</Typography>
          <Typography variant="body2" color="text.secondary">点击一行查看详情</Typography>
        </Stack>
        {items.length === 0 ? <EmptyState text={loading ? "正在读取日志，我马上整理好" : "这里暂时空空的，换个筛选条件再看看。"} /> : (
          <TableContainer>
            <Table size="small" aria-label="运行日志列表" sx={{ tableLayout: "fixed", minWidth: 960, "& .MuiTableCell-root": { px: 1.25, py: 0.65, fontSize: 13 }, "& .MuiTableCell-head": { py: 1, bgcolor: "action.hover", fontWeight: 600 } }}>
              <TableHead><TableRow>
                <TableCell sx={{ width: 176 }}>时间</TableCell>
                <TableCell sx={{ width: 90 }}>类型</TableCell>
                <TableCell sx={{ width: 200 }}>账号</TableCell>
                <TableCell sx={{ width: 172 }}>岗位</TableCell>
                <TableCell>日志内容</TableCell>
                <TableCell align="center" sx={{ width: 54 }}>详情</TableCell>
              </TableRow></TableHead>
              <TableBody>{items.map((item) => <LogRow key={item.id} item={item} />)}</TableBody>
            </Table>
          </TableContainer>
        )}
        {total > 50 && <Pagination sx={{ mt: 2 }} count={Math.ceil(total / 50)} page={page} onChange={(_, nextPage) => void load(nextPage, applied)} />}
      </SectionPanel>
    </Stack>
  );
}
