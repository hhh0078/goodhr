/** 文件作用说明：候选人维护时搜索选择已有标签，陌生名称失焦后明确确认创建，默认仅个人可见。 */
"use client";
import { Alert, Button, Flex, Radio, Select, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { loadResumeTags, saveResumeTag } from "@/lib/resume-api";
import type { ResumeTag } from "@/lib/resume-types";

/** ResumeTagSelect 只把真实标签编号交给表单，输入名称须明确确认后才能创建。 */
export default function ResumeTagSelect({ id, value = [], onChange, active, disabled, initialTags = [], onPendingChange, onTagsLoaded }: {
  id?: string;
  value?: string[]; onChange?: (value: string[]) => void; active: boolean; disabled?: boolean;
  initialTags?: ResumeTag[]; onPendingChange?: (pending: boolean) => void; onTagsLoaded?: (tags: ResumeTag[]) => void;
}) {
  const [tags, setTags] = useState<ResumeTag[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [creating, setCreating] = useState("");
  const [scope, setScope] = useState<ResumeTag["scope"]>("personal");
  const [busy, setBusy] = useState(false);
  const searchRef = useRef("");
  const known = tags.some((tag) => tag.name.trim().toLocaleLowerCase() === search.trim().toLocaleLowerCase());
  useEffect(() => { onPendingChange?.(Boolean(creating || busy || (search.trim() && !known))); }, [creating, busy, search, known, onPendingChange]);
  useEffect(() => { onTagsLoaded?.(tags); }, [tags, onTagsLoaded]);
  /** reloadTags 加载当前账号可选择的标签，失败时保留重试入口。 */
  async function reloadTags() {
    setLoading(true); setError("");
    try { setTags(await loadResumeTags()); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "标签没读出来，请重试。"); }
    finally { setLoading(false); }
  }
  useEffect(() => {
    if (active) { void reloadTags(); }
    else { setSearch(""); searchRef.current = ""; setCreating(""); onPendingChange?.(false); }
  }, [active]);
  /** changeSearch 保留真实输入，已有标签由选择列表操作，避免自由创建。 */
  function changeSearch(text: string) { setSearch(text); searchRef.current = text; }
  /** confirmUnknown 失焦时检查名称，只有不存在的标签才询问是否创建。 */
  function confirmUnknown() {
    const name = searchRef.current.trim();
    if (disabled || loading || !name) return;
    const matched = tags.find((tag) => tag.name.trim().toLocaleLowerCase() === name.toLocaleLowerCase());
    if (matched) { onChange?.([...new Set([...value, matched.id])]); changeSearch(""); return; }
    setCreating(name); setScope("personal"); setError("");
  }
  /** closeCreation 放弃新名称，只保留此前选择的真实标签。 */
  function closeCreation() { if (!busy) { setCreating(""); changeSearch(""); setError(""); } }
  /** createTag 保存明确确认的范围后再选中标签，失败保持名称供重试。 */
  async function createTag() {
    if (!creating || busy) return;
    setBusy(true); setError("");
    try {
      const tag = await saveResumeTag("", creating, scope);
      setTags((current) => [...current.filter((item) => item.id !== tag.id), tag]);
      onChange?.([...new Set([...value, tag.id])]); setCreating(""); changeSearch("");
    } catch (cause) { setError(cause instanceof Error ? cause.message : "标签没创建成功，请重试。"); }
    finally { setBusy(false); }
  }
  const options = [...tags, ...initialTags.filter((tag) => value.includes(tag.id) && !tags.some((current) => current.id === tag.id))];
  return <>
    <Select id={id} mode="multiple" showSearch optionFilterProp="label" value={value} onChange={(next) => { onChange?.(next); changeSearch(""); }}
      searchValue={search} onSearch={changeSearch} onBlur={confirmUnknown} disabled={disabled || loading || Boolean(error && !creating)} loading={loading}
      options={options.map((tag) => ({ value: tag.id, label: `${tag.name} · ${tag.scope === "team" ? "团队" : "个人"}` }))}
      placeholder="搜索选择；新名称失焦后确认创建" notFoundContent={search.trim() ? "暂未找到，离开输入框后可确认创建。" : "还没有标签，可以输入名字创建。"} />
    {error && !creating ? <Flex align="center" gap={6} style={{ marginTop: 4 }}><Typography.Text type="danger" style={{ fontSize: 12 }}>{error}</Typography.Text><Button size="small" onClick={() => void reloadTags()}>重试</Button></Flex> : null}
    <AdminDialog open={active && Boolean(creating)} title="创建新标签" maxWidth="xs" onClose={closeCreation} onConfirm={() => void createTag()} confirmText="创建并选择" loading={busy}>
      {error ? <Alert type="warning" showIcon title={error} style={{ marginBottom: 10 }} /> : null}
      <Typography.Paragraph>“{creating}”这个标签还不存在，要创建吗？</Typography.Paragraph>
      <Radio.Group value={scope} onChange={(event) => setScope(event.target.value)} options={[{ value: "personal", label: "个人标签" }, { value: "team", label: "团队标签" }]} />
      <Typography.Paragraph type="secondary" style={{ marginTop: 10, marginBottom: 0 }}>{scope === "team" ? "团队所有成员可见、可选择。" : "默认仅自己可见，别人看不到这个标签。"}</Typography.Paragraph>
    </AdminDialog>
  </>;
}
