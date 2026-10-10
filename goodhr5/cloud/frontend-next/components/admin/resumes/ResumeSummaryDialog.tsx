/** 文件作用说明：让用户主动调用本地AI生成简历摘要，保留本地草稿，并明确只保存摘要及当前版本。 */
"use client";
import { Alert, Button, Flex, Input, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { saveResume } from "@/lib/resume-api";
import { requestResumeSummary, ResumeSummaryRequestError, summarySource, type ResumeSummaryTask } from "@/lib/resume-summary-api";
import type { Resume } from "@/lib/resume-types";
import { isReadOnlyResume } from "@/lib/resume-types";

type SummaryCache = { id?: string; key: string; version?: number; editedSummary?: string; saveKey?: string; source?: ReturnType<typeof summarySource>; pending?: boolean };
/** ResumeSummaryDialog 生成和云端保存分两步，关闭窗口只收起草稿，不偷偷重复调用模型。 */
export default function ResumeSummaryDialog({ open, candidate, onClose, onSaved }: { open: boolean; candidate: Resume; onClose: () => void; onSaved: (candidate: Resume) => void }) {
  const { agentBase, refreshAgent, notify, user } = useAdmin();
  const [task, setTask] = useState<ResumeSummaryTask | null>(null);
  const [summary, setSummary] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState(false);
  const [version, setVersion] = useState(candidate.row_version);
  const generation = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cache = useRef<SummaryCache>({ key: "" });
  const saveKey = useRef("");
  const cacheKey = `goodhr5_resume_summary:${user?.id || user?.email || "anonymous"}:${candidate.id}`;
  const running = task?.status === "queued" || task?.status === "running";
  const readOnly = isReadOnlyResume(candidate);
  const stale = version !== candidate.row_version;
  /** persistCache 将草稿可靠写入浏览器；实际存储失败时交给调用方处理，不能继续发收费请求。 */
  function persistCache() {
    try { localStorage.setItem(cacheKey, JSON.stringify(cache.current)); }
    catch { throw new Error("浏览器草稿没记住，空间可能不够。先释放浏览器存储空间，再试一次。"); }
  }
  /** rememberTask 缓存编号和版本，轮询结果始终保留用户在本浏览器人工修订的摘要。 */
  function rememberTask(next: ResumeSummaryTask) {
    setTask(next);
    if (next.status === "completed") {
      setSummary(typeof cache.current.editedSummary === "string" ? cache.current.editedSummary : next.ai_summary);
      cache.current.saveKey ||= crypto.randomUUID(); saveKey.current = cache.current.saveKey;
    }
    cache.current.id = next.id; cache.current.pending = false; setPending(false);
    delete cache.current.source;
    persistCache();
  }
  /** editSummary 按账号和候选人保留人工摘要及保存幂等键，收起或刷新不会丢掉手动修改。 */
  function editSummary(value: string) {
    setSummary(value); cache.current.editedSummary = value;
    cache.current.saveKey = crypto.randomUUID(); saveKey.current = cache.current.saveKey;
    try { persistCache(); }
    catch { setError("修改已留在当前窗口，但浏览器没记住。先别收起，直接保存或释放存储空间后重试。"); }
  }
  /** readTask 串行轮询本地摘要，不会再次调用模型。 */
  async function readTask(id: string, current = generation.current) {
    try {
      const next = await requestResumeSummary(agentBase, `/${encodeURIComponent(id)}`);
      if (current !== generation.current) return;
      rememberTask(next); setError("");
      if (next.status === "queued" || next.status === "running") timer.current = setTimeout(() => void readTask(id, current), 1200);
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "摘要状态没读出来，请重试读取。"); }
  }
  useEffect(() => {
    if (!open || readOnly) return;
    const current = ++generation.current;
    setTask(null); setSummary(""); setError(""); setBusy(false);
    let stored: SummaryCache | null = null;
    try { stored = JSON.parse(localStorage.getItem(cacheKey) || "null"); } catch { /* 旧缓存不影响重新生成。 */ }
    cache.current = typeof stored?.key === "string" && stored.key ? stored : { key: crypto.randomUUID(), version: candidate.row_version };
    cache.current.saveKey = typeof cache.current.saveKey === "string" && cache.current.saveKey ? cache.current.saveKey : crypto.randomUUID();
    setVersion(cache.current.version); saveKey.current = cache.current.saveKey;
    setPending(Boolean(cache.current.pending));
    if (typeof cache.current.editedSummary === "string") setSummary(cache.current.editedSummary);
    if (agentBase && cache.current.id) void readTask(cache.current.id, current);
    return () => { generation.current += 1; if (timer.current) clearTimeout(timer.current); };
  }, [open, agentBase, cacheKey, readOnly]);
  /** generate 在发请求前冻结职业资料；未知回包必须用同编号和旧资料恢复，不能随新版本重复计费。 */
  async function generate(fresh = false) {
    if (readOnly) return;
    setBusy(true); setError(""); const current = generation.current;
    const recovering = Boolean(cache.current.pending);
    let requestSent = false;
    if (timer.current) clearTimeout(timer.current);
    try {
      if (fresh && !cache.current.pending) {
        cache.current = { key: crypto.randomUUID(), version: candidate.row_version, saveKey: crypto.randomUUID() };
        setTask(null); setSummary(""); saveKey.current = cache.current.saveKey!;
      }
      cache.current.source ||= JSON.parse(JSON.stringify(summarySource(candidate)));
      cache.current.pending = true; setPending(true);
      setVersion(cache.current.version); persistCache();
      requestSent = true;
      const next = await requestResumeSummary(agentBase, "", "POST", { request_key: cache.current.key, candidate: cache.current.source });
      if (current !== generation.current) return;
      rememberTask(next);
      if (next.status === "queued" || next.status === "running") timer.current = setTimeout(() => void readTask(next.id, current), 1200);
    } catch (cause) {
      if (current !== generation.current) return;
      if (!requestSent && !recovering) { cache.current.pending = false; setPending(false); delete cache.current.source; }
      // 只有本地创建前的参数拒绝确认没有AI任务，网络、权限和冲突错误仍按未知请求保留。
      if (cause instanceof ResumeSummaryRequestError && ["SUMMARY_INVALID", "SUMMARY_BODY_INVALID"].includes(cause.code)) {
        cache.current.pending = false; setPending(false); delete cache.current.source;
        try { persistCache(); } catch { setError("请求未启动，浏览器草稿也没记住。先释放存储空间后重试。"); return; }
      }
      setError(cause instanceof Error ? cause.message : "摘要请求没接上，请重试。");
    }
    finally { if (current === generation.current) setBusy(false); }
  }
  /** operateTask 停止或明确重试现有任务，计费不明的失败绝不自动重试。 */
  async function operateTask(action: "cancel" | "retry") {
    if (readOnly) return;
    if (!task) return;
    if (action === "retry" && stale) { setError("简历资料已更新，请按当前资料重新生成摘要。"); return; }
    setBusy(true); setError(""); const current = generation.current;
    if (timer.current) clearTimeout(timer.current);
    try {
      const next = await requestResumeSummary(agentBase, `/${encodeURIComponent(task.id)}/${action}`, "POST", {});
      if (current !== generation.current) return;
      rememberTask(next);
      if (next.status === "queued" || next.status === "running") timer.current = setTimeout(() => void readTask(next.id, current), 1200);
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "摘要操作没完成，请重试读取。"); }
    finally { if (current === generation.current) setBusy(false); }
  }
  /** saveSummary 只更新人工确认的摘要和原版本，不发送其它简历字段、备注或推荐。 */
  async function saveSummary() {
    if (readOnly) return;
    if (!summary.trim() || stale || version == null || task?.status !== "completed") return;
    setBusy(true); setError(""); const current = generation.current;
    try {
      const saved = await saveResume(candidate.id, { ai_summary: summary.trim() }, saveKey.current, version);
      if (current !== generation.current) return;
      localStorage.removeItem(cacheKey); notify("摘要记好了，其它简历内容保留。", "success"); onSaved(saved);
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "摘要没保存，请先核对当前简历版本再重试。"); }
    finally { if (current === generation.current) setBusy(false); }
  }
  if (readOnly) return <AdminDialog open={open} title="AI 简历摘要" maxWidth="sm" onClose={onClose} cancelText="关闭"><Alert type="info" title="这份是管理员内置的只读示例，不调用AI，也不保存摘要。" /></AdminDialog>;
  return <AdminDialog open={open} title="AI 简历摘要" description="调用当前账号设置的模型，可能产生AI费用。先核对摘要，再点保存。" maxWidth="sm" onClose={onClose} cancelText="收起（草稿保留）" loading={busy} onConfirm={() => void saveSummary()} confirmText="保存摘要" confirmDisabled={!summary.trim() || stale || version == null || task?.status !== "completed"}>
    <Flex vertical gap={12}>
      {!agentBase ? <Alert type="warning" title="本地程序还没连接，打开后再继续。" action={<Button onClick={() => void refreshAgent()}>重新连接</Button>} /> : null}
      {error ? <Alert showIcon type="warning" title={error} action={task?.id ? <Button onClick={() => void readTask(task.id)}>重试读取</Button> : undefined} /> : null}
      {pending ? <Alert type="warning" title="上次请求还没确认结果，先恢复原任务，不会另开一次AI调用。" /> : null}
      {stale ? <Alert type="warning" title={pending ? "这份简历已有更新。先恢复上次任务，再决定是否按新资料重新生成。" : "这份简历在生成后有更新。按当前资料重新生成可能再次计费，请核对后继续。"} /> : null}
      {task?.model ? <Typography.Text type="secondary">本次模型：{task.model}</Typography.Text> : null}
      {running ? <Flex gap={12} align="center"><Spin size="small" /><Typography.Text>正在整理职业经历，先让我认真看一眼。</Typography.Text><Button disabled={busy} onClick={() => void operateTask("cancel")}>停止生成</Button></Flex> : null}
      {task?.status === "failed" || task?.status === "cancelled" ? <><Alert type="warning" title={task.error || "摘要没完成，简历还在。"} description={task.possibly_charged ? "可能已计费，不会自动重复调用；核对后可明确重试。" : "调整模型设置或重新连接后，可以再试一次。"} />{stale ? <Button disabled={busy || !agentBase} onClick={() => void generate(true)}>按当前简历重新生成摘要（可能再次计费）</Button> : <Button disabled={busy || !agentBase} onClick={() => void operateTask("retry")}>确认重试摘要</Button>}</> : null}
      {task?.status === "completed" ? <><Input.TextArea aria-label="摘要草稿" value={summary} onChange={(event) => editSummary(event.target.value)} maxLength={2000} showCount autoSize={{ minRows: 6, maxRows: 14 }} /><Typography.Text type="secondary">这里只保存摘要。没有写清楚的事实，可以删掉再存。</Typography.Text></> : null}
      {!running && task?.status !== "failed" && task?.status !== "cancelled" ? <Button disabled={busy || !agentBase} onClick={() => void generate(!pending && Boolean(task || stale))}>{pending ? "恢复上次摘要任务" : task?.status === "completed" ? "重新生成摘要（再次调用模型）" : "开始生成摘要"}</Button> : null}
    </Flex>
  </AdminDialog>;
}
