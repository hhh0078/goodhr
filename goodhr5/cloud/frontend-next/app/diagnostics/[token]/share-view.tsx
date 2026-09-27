/** 本文件负责只读排障链接的错误置顶、搜索筛选和结构化入参出参展示。 */
"use client";

import { Alert, Box, Button, CircularProgress, Container, Paper, Stack, TextField, Typography } from "@mui/material";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";

type DiagnosticEvent = {
  timestamp: string;
  category: string;
  is_error: boolean;
  error_message: string;
  input: Record<string, unknown>;
  output: Record<string, unknown>;
  message: string;
  task_id?: string;
  action?: string;
  step?: string;
  status?: string;
  duration_ms?: number;
};

type SharedLog = {
  id: string;
  level: string;
  message: string;
  created_at: string;
  event?: DiagnosticEvent;
};

/** DiagnosticShareView 加载固定快照，并优先显示错误与退出原因。 */
export default function DiagnosticShareView() {
  const params = useParams<{ token: string }>();
  const token = String(params?.token || "");
  const [logs, setLogs] = useState<SharedLog[]>([]);
  const [expiresAt, setExpiresAt] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [onlyErrors, setOnlyErrors] = useState(false);
  const [query, setQuery] = useState("");
  const [visibleCount, setVisibleCount] = useState(100);

  useEffect(() => {
    if (!/^[a-f0-9]{64}$/.test(token)) { setError("这个排障链接不完整，可以请对方重新复制。"); setLoading(false); return; }
    let active = true;
    cloudRequest(`/api/public/position-log-shares/${token}`, { auth: false })
      .then((data) => { if (active) { setLogs(Array.isArray(data.logs) ? data.logs : []); setExpiresAt(String(data.expires_at || "")); } })
      .catch(() => { if (active) setError("链接已过期或被新链接替换，请对方重新复制。"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [token]);

  const ordered = useMemo(() => [...logs].sort((left, right) =>
    Number(Boolean(right.event?.is_error || right.level === "error")) - Number(Boolean(left.event?.is_error || left.level === "error"))
    || Date.parse(right.created_at) - Date.parse(left.created_at)), [logs]);
  const errors = useMemo(() => ordered.filter((item) => item.event?.is_error || item.level === "error"), [ordered]);
  const filtered = useMemo(() => (onlyErrors ? errors : ordered).filter((item) => {
    const text = [item.event?.message, item.event?.error_message, item.event?.action, item.event?.step, item.message].join(" ").toLowerCase();
    return text.includes(query.trim().toLowerCase());
  }), [onlyErrors, errors, ordered, query]);

  return <Container maxWidth="lg" sx={{ py: { xs: 3, md: 6 } }}>
    <Stack spacing={3}>
      <Box>
        <Typography component="h1" variant="h4" sx={{ fontWeight: 760 }}>岗位排障日志</Typography>
        <Typography sx={{ mt: 1, color: "text.secondary" }}>这是创建链接时保存的只读日志快照。错误信息排在前面，入参和出参可按需展开。</Typography>
      </Box>
      {loading ? <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}><CircularProgress size={20} /><Typography>正在读取日志</Typography></Stack> : null}
      {error ? <Alert severity="warning">{error}</Alert> : null}
      {!loading && !error ? <>
        <Paper variant="outlined" sx={{ p: 2, borderRadius: "8px" }}>
          <Stack direction={{ xs: "column", md: "row" }} spacing={2} sx={{ alignItems: { md: "center" }, justifyContent: "space-between" }}>
            <Typography>共 {ordered.length} 条，错误 {errors.length} 条。链接有效至 {formatTime(expiresAt)}。</Typography>
            <Stack direction="row" spacing={1}>
              <Button variant={onlyErrors ? "outlined" : "contained"} onClick={() => { setOnlyErrors(false); setVisibleCount(100); }}>全部日志</Button>
              <Button variant={onlyErrors ? "contained" : "outlined"} color="error" onClick={() => { setOnlyErrors(true); setVisibleCount(100); }}>只看错误</Button>
            </Stack>
          </Stack>
          <TextField size="small" fullWidth value={query} onChange={(event) => { setQuery(event.target.value); setVisibleCount(100); }} placeholder="搜索错误、方法或步骤" sx={{ mt: 2 }} />
        </Paper>
        {errors.length > 0 && !onlyErrors && !query ? <Alert severity="error">最近一条错误：{errors[0].event?.error_message || errors[0].event?.message || errors[0].message}</Alert> : null}
        <Stack spacing={1.5}>
          {filtered.slice(0, visibleCount).map((item, index) => <DiagnosticLine key={`${item.id || item.created_at}-${index}`} item={item} />)}
          {filtered.length === 0 ? <Typography color="text.secondary">这里没有符合条件的日志。</Typography> : null}
          {filtered.length > visibleCount ? <Button variant="outlined" onClick={() => setVisibleCount((count) => count + 100)}>再显示 100 条（还剩 {filtered.length - visibleCount} 条）</Button> : null}
        </Stack>
      </> : null}
    </Stack>
  </Container>;
}

/** DiagnosticLine 显示单条日志，错误正文直接可见，较长的参数默认折叠。 */
function DiagnosticLine({ item }: { item: SharedLog }) {
  const event = item.event;
  const isError = Boolean(event?.is_error || item.level === "error");
  return <Paper variant="outlined" sx={{ p: 2, borderRadius: "8px", borderColor: isError ? "error.main" : "divider" }}>
    <Stack direction={{ xs: "column", md: "row" }} spacing={1} sx={{ alignItems: { md: "center" } }}>
      <Typography sx={{ fontSize: 12, color: "text.secondary", minWidth: 205 }}>{formatTime(event?.timestamp || item.created_at)}</Typography>
      <Typography sx={{ fontSize: 12, fontWeight: 700, color: isError ? "error.main" : "text.secondary" }}>{isError ? "错误" : "正常"}</Typography>
      <Typography sx={{ fontSize: 12, color: "text.secondary" }}>{event?.category || "旧日志"} · {event?.action || event?.step || "岗位流程"}{event?.duration_ms != null ? ` · ${event.duration_ms}ms` : ""}</Typography>
    </Stack>
    <Typography sx={{ mt: 1, whiteSpace: "pre-wrap", wordBreak: "break-word", fontWeight: isError ? 650 : 400 }}>{event?.message || item.message}</Typography>
    {isError && event?.error_message && event.error_message !== event.message ? <Alert severity="error" sx={{ mt: 1 }}>{event.error_message}</Alert> : null}
    {event ? <Box component="details" sx={{ mt: 1.5, fontSize: 13 }}>
      <Box component="summary" sx={{ cursor: "pointer", color: "primary.main" }}>查看入参、出参和任务编号</Box>
      <Stack spacing={1} sx={{ mt: 1 }}>
        <DataBlock label="入参" value={event.input} />
        <DataBlock label="出参" value={event.output} />
        {event.task_id ? <Typography sx={{ fontSize: 12, color: "text.secondary" }}>任务编号：{event.task_id}</Typography> : null}
      </Stack>
    </Box> : null}
  </Paper>;
}

/** DataBlock 格式化显示已脱敏的 JSON 摘要。 */
function DataBlock({ label, value }: { label: string; value: Record<string, unknown> }) {
  return <Box><Typography sx={{ fontWeight: 700, fontSize: 12 }}>{label}</Typography><Box component="pre" sx={{ m: 0, p: 1, bgcolor: "action.hover", overflowX: "auto", fontSize: 12 }}>{JSON.stringify(value || {}, null, 2)}</Box></Box>;
}

/** formatTime 把 ISO 时间显示为本地可读时间。 */
function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间未知" : date.toLocaleString("zh-CN", { hour12: false });
}
