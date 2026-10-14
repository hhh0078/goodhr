/** 文件作用说明：通过本地程序上传和批量解析简历，恢复导入任务并让用户逐项补号、查重与确认入库。 */
"use client";
import { Alert, Button, Flex, Input, Progress, Space, Table, Tag, Typography, Upload, type UploadFile } from "antd";
import { ArrowLeftOutlined, InboxOutlined, ReloadOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState, type ReactNode } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { getToken } from "@/lib/admin-api";
import { checkResumeDuplicates, loadResume } from "@/lib/resume-api";
import { normalizePhone, validPhone, type PositionOption, type Resume, type ResumeDraft } from "@/lib/resume-types";
import ResumeEditor, { type ResumeDraftSubmission } from "./ResumeEditor";
import styles from "./resumes.module.css";

type ImportItem = { id: string; file_name: string; status: string; candidate?: ResumeDraft; error?: string; candidate_id?: string };
type ImportTask = { id: string; status: string; items: ImportItem[]; total: number; processed: number };
const importStatus: Record<string, string> = { queued: "排队中", parsing: "解析中", ready: "待确认", needs_phone: "待补手机号", failed: "失败", saved: "已入库", partial_success: "简历已保存，备注 / 岗位待补齐" };
type Props = {
  open: boolean; initialTaskID?: string; positions: PositionOption[]; onClose: () => void; onChanged: () => void;
  /** 添加标签内直接展示导入内容，保留独立导入入口的原有弹框。 */
  embedded?: boolean;
  active?: boolean;
  onBusyChange?: (busy: boolean) => void;
};

/** ResumeImportDialog 仅连接本地 AI 导入，不把原文件或截图上传云端。 */
export default function ResumeImportDialog({ open, initialTaskID = "", positions, onClose, onChanged, embedded = false, active = true, onBusyChange }: Props) {
  const { agentBase, refreshAgent, notify, user } = useAdmin();
  const [files, setFiles] = useState<UploadFile[]>([]);
  const [text, setText] = useState("");
  const [task, setTask] = useState<ImportTask | null>(null);
  const [busy, setBusy] = useState(false);
  const [editorBusy, setEditorBusy] = useState(false);
  const [error, setError] = useState("");
  const [editingItem, setEditingItem] = useState<ImportItem | null>(null);
  const [duplicateHints, setDuplicateHints] = useState<Record<string, string>>({});
  const generation = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const taskCache = `goodhr5_resume_import:${user?.id || user?.email || "anonymous"}`;
  useEffect(() => {
    onBusyChange?.(busy || editorBusy);
    return () => onBusyChange?.(false);
  }, [busy, editorBusy, onBusyChange]);
  /** requestTask 访问本地任务 API，并传送当前 GoodHR 会话用于账号隔离。 */
  async function requestTask(path: string, method = "GET", body?: unknown, upload?: FormData): Promise<ImportTask> {
    if (!agentBase) throw new Error("本地程序还没连接，打开后点重新连接就好。手动添加仍可用。");
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 60000);
    try {
      const response = await fetch(`${agentBase}/api/v1/resume-import/tasks${path}`, {
        method, signal: controller.signal, cache: "no-store",
        headers: { Authorization: `Bearer ${getToken()}`, ...(!upload ? { "Content-Type": "application/json" } : {}) },
        body: upload || (body == null ? undefined : JSON.stringify(body)),
      });
      // 旧版本地程序返回纯文本404，先识别响应格式，避免将JSON解析异常直接展示给用户。
      const payload = await response.text();
      let data: any;
      try { data = JSON.parse(payload); }
      catch {
        if (response.status === 404) throw new Error("当前本地程序还不支持简历批量导入，请更新到 6.2.10.3 或更新版本。手动添加和浏览器插件仍可用。");
        throw new Error(response.ok ? "本地程序返回的导入结果没读完整，请重新读取任务。" : `本地导入暂时没响应（${response.status}），请重新连接后重试。`);
      }
      if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error("本地程序返回的导入结果没读完整，请重新读取任务。");
      if (!response.ok || data.ok === false) throw new Error(data.error?.message || data.message || (typeof data.error === "string" ? data.error : "导入请求没成功，请重试。"));
      const result = data.data?.task || data.task || (Array.isArray(data.data?.tasks) ? data.data.tasks.find((item: ImportTask) => Array.isArray(item?.items) && item.items.some((row) => row.status !== "saved")) : undefined);
      if (!result && path === "" && method === "GET" && Array.isArray(data.data?.tasks)) return { id: "", status: "completed", total: 0, processed: 0, items: [] };
      if (!result?.id || !Array.isArray(result.items)) throw new Error("本地程序还不支持新版简历导入，请先更新本地程序。");
      return result;
    } catch (cause) {
      if (cause instanceof Error && cause.name === "AbortError") throw new Error("本地程序响应有点慢，请重试读取任务，已完成的结果会保留。");
      throw cause instanceof Error ? cause : new Error("本地程序没连接上，请重新连接后重试。");
    } finally { clearTimeout(timeout); }
  }
  /** rememberTask 只缓存任务编号，本地文件和候选人草稿仍留在本地程序。 */
  function rememberTask(next: ImportTask) {
    setTask(next); localStorage.setItem(taskCache, next.id);
  }
  /** readTask 串行读取任务状态，不叠加慢请求。 */
  async function readTask(id: string, current = generation.current) {
    try {
      const next = await requestTask(id ? `/${encodeURIComponent(id)}` : "");
      if (current !== generation.current) return;
      if (!next.id) return;
      rememberTask(next); setError("");
      if (next.status === "running") timer.current = setTimeout(() => void readTask(id, current), 1600);
    } catch (cause) {
      if (current === generation.current) setError(cause instanceof Error ? cause.message : "任务没读出来，请重试。");
    }
  }
  useEffect(() => {
    if (!open || !agentBase) return;
    const current = ++generation.current;
    const cached = initialTaskID || localStorage.getItem(taskCache);
    void readTask(cached || "", current);
    return () => { generation.current += 1; if (timer.current) clearTimeout(timer.current); };
  }, [open, agentBase, taskCache, initialTaskID]);
  useEffect(() => { setTask(null); setEditingItem(null); setFiles([]); setText(""); setDuplicateHints({}); setError(""); }, [taskCache]);
  /** startImport 校验文件范围后创建本地持久化解析任务。 */
  async function startImport() {
    if (!files.length && !text.trim()) { setError("先选几份简历，或粘贴一份文本，我再开始。 "); return; }
    if (files.length + (text.trim() ? 1 : 0) > 20) { setError("每批先放 20 份以内，处理完再来一批。 "); return; }
    setBusy(true); setError("");
    try {
      const form = new FormData();
      let totalBytes = 0;
      for (const item of files) {
        const file = item.originFileObj;
        if (!file || file.size > 10 * 1024 * 1024) throw new Error(`${item.name} 超过 10MB 或未读取成功，请换一个文件。`);
        totalBytes += file.size;
        if (!/\.(pdf|doc|docx|txt|png|jpe?g)$/i.test(file.name)) throw new Error(`${file.name} 暂时不支持，先用 PDF、DOC/DOCX、TXT 或图片。`);
        form.append("files", file);
      }
      if (totalBytes > 60 * 1024 * 1024) throw new Error("这批文件超过 60MB，分成两批更稳妥。");
      if (text.trim()) form.append("text", text.trim());
      const next = await requestTask("", "POST", undefined, form);
      generation.current += 1; if (timer.current) clearTimeout(timer.current);
      rememberTask(next); setDuplicateHints({});
      if (next.status === "running") void readTask(next.id);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "导入没开始，请重试。"); }
    finally { setBusy(false); }
  }
  /** taskAction 取消或只重试失败文件，已入库的结果不重新解析。 */
  async function taskAction(action: "cancel" | "retry") {
    if (!task) return;
    setBusy(true); setError("");
    try {
      generation.current += 1; if (timer.current) clearTimeout(timer.current);
      const next = await requestTask(`/${encodeURIComponent(task.id)}/${action}`, "POST", action === "retry" ? { item_ids: task.items.filter((item) => ["failed", "queued"].includes(item.status)).map((item) => item.id) } : {});
      rememberTask(next); if (next.status === "running") void readTask(next.id);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "没处理成功，请重试。"); }
    finally { setBusy(false); }
  }
  /** confirmOne 由本地程序保存已核对的草稿并持久化入库状态。 */
  async function confirmOne(submission: ResumeDraftSubmission): Promise<Resume> {
    if (!task || !editingItem) throw new Error("导入草稿没找到，请重新读取任务。");
    generation.current += 1; if (timer.current) clearTimeout(timer.current);
    const persisted = task.items.find((item) => item.id === editingItem.id);
    const next = await requestTask(`/${encodeURIComponent(task.id)}/confirm`, "POST", { items: [persisted?.candidate_id ? { id: editingItem.id } : { id: editingItem.id, ...submission }] });
    rememberTask(next);
    if (next.status === "running") void readTask(next.id);
    const item = next.items.find((current) => current.id === editingItem.id);
    if (item?.status !== "saved" || !item.candidate_id) throw new Error(item?.error || "这份简历还没保存成功，请检查后重试。");
    return loadResume(item.candidate_id);
  }
  /** finishPartial 用本地保存的原确认内容补齐剩余记录，既不修改简历也不重新调用 AI。 */
  async function finishPartial(item: ImportItem) {
    if (!task) return;
    setBusy(true); setError(""); generation.current += 1; if (timer.current) clearTimeout(timer.current);
    try {
      const next = await requestTask(`/${encodeURIComponent(task.id)}/confirm`, "POST", { items: [{ id: item.id }] });
      rememberTask(next); onChanged(); if (next.status === "running") void readTask(next.id);
      const saved = next.items.find((current) => current.id === item.id);
      if (saved?.status !== "saved") throw new Error(saved?.error || "简历主体已保存，剩余记录还没记全，请继续重试。");
      notify("剩余备注和岗位记录都记好了。", "success");
    } catch (cause) { setError(cause instanceof Error ? cause.message : "剩余记录没补全，请重试。"); }
    finally { setBusy(false); }
  }
  /** confirmReady 查重后逐项确认完整草稿，避免批次正文超限，并随每项响应保留可恢复的结果。 */
  async function confirmReady() {
    if (!task) return;
    const taskID = task.id;
    let saved = 0;
    setBusy(true); setError("");
    try {
      const eligible = task.items.filter((item) => item.status === "ready" && item.candidate?.candidate_name && validPhone(item.candidate.phone));
      const safe: ImportItem[] = []; const hints: Record<string, string> = {};
      const phones = new Set<string>();
      for (const item of eligible) {
        if (!item.candidate) continue;
        const duplicate = await checkResumeDuplicates(item.candidate);
        const phone = normalizePhone(item.candidate.phone);
        if (duplicate.exact.length || duplicate.possible.length || duplicate.hidden_match || phones.has(phone)) hints[item.id] = "可能重复，先逐份核对";
        else { safe.push(item); phones.add(phone); }
      }
      setDuplicateHints(hints);
      if (!safe.length) { setError("这批草稿需要逐份补充或核对，点“预览 / 补充”就能继续。 "); return; }
      generation.current += 1; if (timer.current) clearTimeout(timer.current);
      let latest = task;
      const failed: string[] = [];
      for (const item of safe) {
        latest = await requestTask(`/${encodeURIComponent(taskID)}/confirm`, "POST", { items: [{ id: item.id, candidate: item.candidate }] });
        rememberTask(latest);
        const current = latest.items.find((row) => row.id === item.id);
        if (current?.status === "saved") saved += 1;
        else failed.push(`${item.file_name}：${current?.error || "还没保存成功，请逐份核对"}`);
      }
      onChanged();
      if (latest.status === "running") void readTask(latest.id);
      if (failed.length) setError(failed.join("；"));
      notify(`已入库 ${saved} 份${Object.keys(hints).length ? "，其余草稿请逐份核对。" : "。"}`, "success");
    } catch (cause) {
      if (saved) onChanged();
      setError(`批量确认暂停了，已入库 ${saved} 份；未完成的草稿仍保留。${cause instanceof Error ? cause.message : "请重试读取任务后继续。"}`);
    }
    finally { setBusy(false); }
  }
  /** newBatch 清除任务界面指针，已完成的本地任务记录仍保留。 */
  function newBatch() { generation.current += 1; if (timer.current) clearTimeout(timer.current); setTask(null); setFiles([]); setText(""); setDuplicateHints({}); localStorage.removeItem(taskCache); setError(""); }
  const itemEditor = <ResumeEditor open={open && Boolean(editingItem)} embedded={embedded} active={active} draft={editingItem?.candidate} persistedID={task?.items.find((item) => item.id === editingItem?.id)?.candidate_id} positions={positions} onBusyChange={setEditorBusy} onFoldersChanged={onChanged} onClose={() => setEditingItem(null)} onConfirmDraft={confirmOne} onSaved={() => { setEditingItem(null); onChanged(); }} />;
  return <>
    <ResumeImportFrame embedded={embedded} open={open} busy={busy || editorBusy} onClose={onClose}>
      {embedded && editingItem ? <>
        <Flex align="center" gap={10} className={styles.importEditorHeading}>
          <Button size="small" icon={<ArrowLeftOutlined />} disabled={editorBusy} onClick={() => setEditingItem(null)}>返回解析结果</Button>
          <Typography.Text strong>{editingItem.file_name}</Typography.Text>
        </Flex>
        {itemEditor}
      </> : null}
      <div hidden={embedded && Boolean(editingItem)}>
      <Flex vertical gap={14}>
        <Alert showIcon type={agentBase ? "info" : "warning"} title={agentBase ? "本地程序已连接。解析会调用 AI，按现有规则计费；确认前只保存草稿。" : "先打开本地程序，再连接一下，就能用 AI 解析。手动添加照常可用。"} action={!agentBase ? <Button onClick={() => void refreshAgent()}>重新连接</Button> : undefined} />
        {error ? <Alert showIcon type="warning" title={error} action={/请(?:先)?更新/.test(error) ? <Button size="small" href="/download#local-agent" target="_blank" rel="noopener noreferrer">去下载更新</Button> : undefined} /> : null}
        {!task ? <>
          <Upload.Dragger multiple accept=".pdf,.doc,.docx,.txt,.png,.jpg,.jpeg" maxCount={20} fileList={files} beforeUpload={() => false} onChange={({ fileList }) => setFiles(fileList)} disabled={busy || !agentBase}>
            <InboxOutlined style={{ fontSize: 28, color: "var(--goodhr-primary)" }} /><Typography.Paragraph style={{ margin: "8px 0" }}>拖入简历，或点击选择文件</Typography.Paragraph><Typography.Text type="secondary">PDF、DOC/DOCX、TXT、PNG/JPEG；每批 20 份，单份 10MB、总计 60MB 以内。</Typography.Text>
          </Upload.Dragger>
          <Input.TextArea value={text} onChange={(event) => setText(event.target.value)} disabled={busy} autoSize={{ minRows: 3, maxRows: 8 }} placeholder="也可以粘贴一份简历文本。" />
          <Flex justify="flex-end"><Button type="primary" disabled={!agentBase || (!files.length && !text.trim())} loading={busy} onClick={() => void startImport()}>开始解析</Button></Flex>
        </> : <>
          <Flex align="center" justify="space-between" gap={12} wrap><Typography.Text strong>{task.status === "running" ? "正在认真读简历" : task.status === "cancelled" ? "已暂停解析，完成结果仍保留" : "解析结果等你确认"} · {task.processed || 0}/{task.total || task.items.length}</Typography.Text><Button icon={<ReloadOutlined />} disabled={!agentBase} onClick={() => { generation.current += 1; if (timer.current) clearTimeout(timer.current); void readTask(task.id); }}>重新读取</Button></Flex>
          <Progress percent={task.total ? Math.round((task.processed || 0) / task.total * 100) : 0} status={task.status === "running" ? "active" : "normal"} />
          <Table<ImportItem> size="small" className={styles.importTable} rowKey="id" dataSource={task.items} pagination={false} scroll={{ x: 560 }} columns={[
            { title: "简历文件", dataIndex: "file_name", width: 180 },
            { title: "识别结果", render: (_, item) => <Space direction="vertical" size={0}><Typography.Text>{item.candidate?.candidate_name || "待识别"}</Typography.Text><Typography.Text type="secondary">{item.candidate?.phone || "手机号待补充"}</Typography.Text></Space> },
            { title: "状态", width: 180, render: (_, item) => <><Tag>{duplicateHints[item.id] || importStatus[item.status] || item.status}</Tag>{item.error ? <div className={styles.meta}>{item.error}</div> : null}</> },
            { title: "操作", width: 116, render: (_, item) => item.status === "saved" ? <Typography.Text type="secondary">已入库</Typography.Text> : item.status === "partial_success" ? <Button size="small" loading={busy} onClick={() => void finishPartial(item)}>继续完成记录</Button> : <Button size="small" disabled={!item.candidate || ["queued", "parsing"].includes(item.status) || busy} onClick={() => setEditingItem(item)}>预览 / 补充</Button> },
          ]} />
          <Flex justify="flex-end" gap={8} wrap>
            {task.status === "running" ? <Button disabled={busy} onClick={() => void taskAction("cancel")}>暂停解析</Button> : <Button disabled={busy} onClick={newBatch}>新的一批</Button>}
            {task.items.some((item) => item.status === "failed" || (task.status !== "running" && item.status === "queued")) ? <Button loading={busy} onClick={() => void taskAction("retry")}>重试未解析项</Button> : null}
            <Button type="primary" loading={busy} disabled={!agentBase || !task.items.some((item) => item.status === "ready")} onClick={() => void confirmReady()}>确认无重复的完整草稿</Button>
          </Flex>
        </>}
      </Flex>
      </div>
    </ResumeImportFrame>
    {!embedded ? itemEditor : null}
  </>;
}
/** ResumeImportFrame 根据入口选择内嵌内容或独立导入弹框，正在提交时禁止关闭。 */
function ResumeImportFrame({ embedded, open, busy, onClose, children }: { embedded: boolean; open: boolean; busy: boolean; onClose: () => void; children: ReactNode }) {
  return embedded ? <div className={styles.embeddedImport}>{children}</div> : <AdminDialog open={open} title="AI 批量解析简历" maxWidth="md" loading={busy} onClose={onClose}>{children}</AdminDialog>;
}
