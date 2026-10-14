/** 文件作用说明：候选人维护时搜索选择已有收藏夹，陌生名称失焦后明确确认创建，默认仅个人可见。 */
"use client";
import { Alert, Button, Flex, Radio, Select, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { loadResumeFolders, saveResumeFolder } from "@/lib/resume-api";
import type { ResumeFolder } from "@/lib/resume-types";

/** ResumeFolderSelect 只把真实收藏夹编号交给表单，输入名称须明确确认后才能创建。 */
export default function ResumeFolderSelect({ id, value = [], onChange, active, disabled, initialFolders = [], onPendingChange, onFoldersLoaded, onCreated }: {
  id?: string;
  value?: string[]; onChange?: (value: string[]) => void; active: boolean; disabled?: boolean;
  initialFolders?: ResumeFolder[]; onPendingChange?: (pending: boolean) => void; onFoldersLoaded?: (folders: ResumeFolder[]) => void; onCreated?: () => void;
}) {
  const [folders, setFolders] = useState<ResumeFolder[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [creating, setCreating] = useState("");
  const [scope, setScope] = useState<ResumeFolder["scope"]>("personal");
  const [busy, setBusy] = useState(false);
  const searchRef = useRef("");
  const known = folders.some((folder) => folder.name.trim().toLocaleLowerCase() === search.trim().toLocaleLowerCase());
  useEffect(() => { onPendingChange?.(Boolean(creating || busy || (search.trim() && !known))); }, [creating, busy, search, known, onPendingChange]);
  useEffect(() => { onFoldersLoaded?.(folders); }, [folders, onFoldersLoaded]);
  /** reloadFolders 加载当前账号可选择的收藏夹，失败时保留重试入口。 */
  async function reloadFolders() {
    setLoading(true); setError("");
    try { setFolders(await loadResumeFolders()); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "收藏夹没读出来，请重试。"); }
    finally { setLoading(false); }
  }
  useEffect(() => {
    if (active) { void reloadFolders(); }
    else { setSearch(""); searchRef.current = ""; setCreating(""); onPendingChange?.(false); }
  }, [active]);
  /** changeSearch 保留真实输入，已有收藏夹由选择列表操作，避免自由创建。 */
  function changeSearch(text: string) { setSearch(text); searchRef.current = text; }
  /** confirmUnknown 失焦时检查名称，只有不存在的收藏夹才询问是否创建。 */
  function confirmUnknown() {
    const name = searchRef.current.trim();
    if (disabled || loading || !name) return;
    const matched = folders.find((folder) => folder.name.trim().toLocaleLowerCase() === name.toLocaleLowerCase());
    if (matched) { onChange?.([...new Set([...value, matched.id])]); changeSearch(""); return; }
    setCreating(name); setScope("personal"); setError("");
  }
  /** closeCreation 放弃新名称，只保留此前选择的真实收藏夹。 */
  function closeCreation() { if (!busy) { setCreating(""); changeSearch(""); setError(""); } }
  /** createFolder 保存明确确认的范围后再选中收藏夹，失败保持名称供重试。 */
  async function createFolder() {
    if (!creating || busy) return;
    setBusy(true); setError("");
    try {
      const folder = await saveResumeFolder("", creating, scope);
      setFolders((current) => [...current.filter((item) => item.id !== folder.id), folder]);
      onChange?.([...new Set([...value, folder.id])]); setCreating(""); changeSearch("");
      onCreated?.();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "收藏夹没创建成功，请重试。"); }
    finally { setBusy(false); }
  }
  const options = [...folders, ...initialFolders.filter((folder) => value.includes(folder.id) && !folders.some((current) => current.id === folder.id))];
  return <>
    <Select id={id} mode="multiple" showSearch optionFilterProp="label" value={value} onChange={(next) => { onChange?.(next); changeSearch(""); }}
      searchValue={search} onSearch={changeSearch} onBlur={confirmUnknown} disabled={disabled || loading || Boolean(error && !creating)} loading={loading}
      options={options.map((folder) => ({ value: folder.id, label: `${folder.name} · ${folder.scope === "team" ? "团队" : "个人"}` }))}
      placeholder="搜索选择；新名称失焦后确认创建" notFoundContent={search.trim() ? "暂未找到，离开输入框后可确认创建。" : "还没有收藏夹，可以输入名字创建。"} />
    {error && !creating ? <Flex align="center" gap={6} style={{ marginTop: 4 }}><Typography.Text type="danger" style={{ fontSize: 12 }}>{error}</Typography.Text><Button size="small" onClick={() => void reloadFolders()}>重试</Button></Flex> : null}
    <AdminDialog open={active && Boolean(creating)} title="创建新收藏夹" maxWidth="xs" onClose={closeCreation} onConfirm={() => void createFolder()} confirmText="创建并选择" loading={busy}>
      {error ? <Alert type="warning" showIcon title={error} style={{ marginBottom: 10 }} /> : null}
      <Typography.Paragraph>“{creating}”这个收藏夹还不存在，要创建吗？</Typography.Paragraph>
      <Radio.Group value={scope} onChange={(event) => setScope(event.target.value)} options={[{ value: "personal", label: "个人收藏夹" }, { value: "team", label: "团队收藏夹" }]} />
      <Typography.Paragraph type="secondary" style={{ marginTop: 10, marginBottom: 0 }}>{scope === "team" ? "团队所有成员可见、可选择。" : "默认仅自己可见，别人看不到这个收藏夹。"}</Typography.Paragraph>
    </AdminDialog>
  </>;
}
