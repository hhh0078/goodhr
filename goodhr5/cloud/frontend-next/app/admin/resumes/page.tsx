/** 文件作用说明：提供紧凑候选人简历库，支持多条件查询、手动维护、本地 AI 导入、备注及人工推荐。 */
"use client";
import { Avatar, Button, Drawer, Dropdown, Empty, Flex, Input, Pagination, Select, Space, Table, Tag, Typography } from "antd";
import { DownloadOutlined, FilterOutlined, MoreOutlined, PlusOutlined, SearchOutlined, SettingOutlined } from "@ant-design/icons";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";
import { SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import ExtensionPairApproval from "@/components/admin/ExtensionPairApproval";
import ResumeEditor from "@/components/admin/resumes/ResumeEditor";
import ResumeWorkspace from "@/components/admin/resumes/ResumeWorkspace";
import ResumeImportDialog from "@/components/admin/resumes/ResumeImportDialog";
import ResumeLibrarySettingsDialog from "@/components/admin/resumes/ResumeLibrarySettingsDialog";
import { cloudRequest } from "@/lib/admin-api";
import { loadResume, loadResumePositions } from "@/lib/resume-api";
import { avatarSource, experienceLabel, genderLabel, isReadOnlyResume, recordTime, resumeAge, salaryLabel, sourceLabel, sourceOptions, stageLabel, workYearsLabel, type PositionOption, type Resume } from "@/lib/resume-types";
import styles from "./page.module.css";

const filterKeys = ["q", "phone", "position_id", "source_type", "region", "education_level", "work_years", "work_status", "tag", "creator", "created_from", "created_to"] as const;
type Filters = Record<(typeof filterKeys)[number], string>;
/** readFilters 恢复 URL 中的搜索条件，返回列表仍保留原查询。 */
function readFilters(params: URLSearchParams): Filters { return Object.fromEntries(filterKeys.map((key) => [key, params.get(key) || ""])) as Filters; }
/** ResumesPage 在列表中维护简历，所有人工操作与自动任务入库分开。 */
export default function ResumesPage() {
  const router = useRouter(); const params = useSearchParams(); const queryText = params.toString();
  const { notify } = useAdmin();
  const [filters, setFilters] = useState<Filters>(() => readFilters(new URLSearchParams(queryText)));
  const [moreFilters, setMoreFilters] = useState(filterKeys.slice(4).some((key) => Boolean(params.get(key))));
  const [items, setItems] = useState<Resume[]>([]);
  const [positions, setPositions] = useState<PositionOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [total, setTotal] = useState(0);
  const [realTotal, setRealTotal] = useState(0); const [builtinTotal, setBuiltinTotal] = useState(0);
  const [page, setPage] = useState(1); const [pageSize, setPageSize] = useState(20);
  const [loadError, setLoadError] = useState("");
  const [chooserOpen, setChooserOpen] = useState(false); const [addMethod, setAddMethod] = useState("manual");
  const [editorOpen, setEditorOpen] = useState(false); const [editing, setEditing] = useState<Resume | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [detail, setDetail] = useState<{ id: string; name: string; engagementID?: string; tab: string } | null>(null);
  const generation = useRef(0);
  /** load 读取当前 URL 对应的分页，并忽略晚返回的旧搜索结果。 */
  async function load(search = queryText) {
    const current = ++generation.current;
    const query = new URLSearchParams(search);
    const requestedPage = Number(query.get("page"));
    const nextPage = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1;
    const nextSize = [10, 20, 50, 100].includes(Number(query.get("page_size"))) ? Number(query.get("page_size")) : 20;
    const request = new URLSearchParams({ page: String(nextPage), page_size: String(nextSize) });
    filterKeys.forEach((key) => { if (query.get(key)?.trim()) request.set(key, query.get(key)!.trim()); });
    setLoading(true); setLoadError("");
    try {
      const data = await cloudRequest(`/api/candidates?${request}`);
      if (current !== generation.current) return;
      setItems(data.candidates || []); setTotal(Number(data.total) || 0); setPage(nextPage); setPageSize(nextSize);
      setBuiltinTotal(Number(data.builtin_total) || 0); setRealTotal(data.real_total == null ? Number(data.total) || 0 : Number(data.real_total) || 0);
    } catch (cause) { if (current === generation.current) setLoadError(cause instanceof Error ? cause.message : "简历没读出来，请再试一次。"); }
    finally { if (current === generation.current) setLoading(false); }
  }
  useEffect(() => { void loadResumePositions().then(setPositions).catch((cause) => notify(cause.message || "岗位没读出来，可稍后刷新。", "warning")); }, []);
  useEffect(() => { setFilters(readFilters(new URLSearchParams(queryText))); void load(queryText); }, [queryText]);
  useEffect(() => { if (params.get("resume_import_id")) setImportOpen(true); }, [queryText]);
  /** updateFilter 只修改输入草稿，点击查询或回车才请求搜索。 */
  function updateFilter(key: keyof Filters, value = "") { setFilters((current) => ({ ...current, [key]: value })); }
  /** navigateQuery 写回查询与分页，同时保留本地端口和插件配对等独立参数。 */
  function navigateQuery(nextFilters: Filters, nextPage = 1, nextSize = pageSize) {
    const query = new URLSearchParams(queryText);
    filterKeys.forEach((key) => { query.delete(key); if (nextFilters[key].trim()) query.set(key, nextFilters[key].trim()); });
    query.set("page", String(nextPage)); query.set("page_size", String(nextSize));
    const next = query.toString();
    if (next === queryText) void load(next); else router.replace(`/admin/resumes?${next}`, { scroll: false });
  }
  /** resetFilters 清空筛选，恢复第一页，不清空任何简历。 */
  function resetFilters() { const empty = readFilters(new URLSearchParams()); setFilters(empty); navigateQuery(empty); }
  /** openDetails 在抽屉中阅读，保留当前查询和分页。 */
  function openDetails(item: Resume, tab = "notes") { setDetail({ id: item.id, name: item.candidate_name, engagementID: item.engagement_id, tab }); }
  /** openEditor 先获取完整候选人，避免列表摘要覆盖已有经历。 */
  async function openEditor(item?: Resume) {
    if (!item) { setEditing(null); setEditorOpen(true); return; }
    if (isReadOnlyResume(item)) { openDetails(item); return; }
    try { setEditing(await loadResume(item.id)); setEditorOpen(true); }
    catch (cause) { notify(cause instanceof Error ? cause.message : "这份简历没读出来，刷新后再试。", "error"); }
  }
  /** chooseAddMethod 从两种入口继续，手动添加不要求本地程序在线。 */
  function chooseAddMethod() { setChooserOpen(false); if (addMethod === "ai") setImportOpen(true); else void openEditor(); }
  /** closeImport 清除消息携带的草稿入口，避免之后切换筛选时重复打开导入弹框。 */
  function closeImport() {
    setImportOpen(false);
    if (params.get("resume_import_id")) { const query = new URLSearchParams(queryText); query.delete("resume_import_id"); router.replace(`/admin/resumes?${query}`, { scroll: false }); }
  }
  const appliedFilters = readFilters(new URLSearchParams(queryText));
  return <>
    <ExtensionPairApproval />
    <div className={styles.toolbar} data-testid="resume-filters">
      <Flex gap={8} wrap className={styles.searchControls}>
        <Input aria-label="简历关键词" prefix={<SearchOutlined />} placeholder="姓名、公司、学校、职位" value={filters.q} onChange={(event) => updateFilter("q", event.target.value)} onPressEnter={() => navigateQuery(filters)} className={styles.keyword} />
        <Input aria-label="手机号搜索" placeholder="手机号 / 手机尾号" value={filters.phone} onChange={(event) => updateFilter("phone", event.target.value)} onPressEnter={() => navigateQuery(filters)} className={styles.phone} />
        <Select aria-label="岗位筛选" allowClear showSearch optionFilterProp="label" placeholder="全部岗位" value={filters.position_id || undefined} onChange={(value) => updateFilter("position_id", value)} options={positions.map((item) => ({ value: item.id, label: item.name }))} className={styles.position} />
        <Select aria-label="来源筛选" allowClear placeholder="全部来源" value={filters.source_type || undefined} onChange={(value) => updateFilter("source_type", value)} options={sourceOptions} className={styles.source} />
        <Button type="primary" loading={loading} onClick={() => navigateQuery(filters)}>查询</Button><Button onClick={resetFilters}>重置</Button>
        <Button icon={<FilterOutlined />} type={moreFilters ? "text" : "default"} onClick={() => setMoreFilters((value) => !value)}>更多筛选</Button>
      </Flex>
      <Flex gap={8} className={styles.toolbarActions}>
        <Button icon={<SettingOutlined aria-hidden="true" />} onClick={() => setSettingsOpen(true)}>设置</Button>
        <Dropdown trigger={["click"]} menu={{ items: [{ key: "chrome", label: <a href="/download?browser=chrome#browser-extension" target="_blank" rel="noopener noreferrer">Chrome 插件</a> }, { key: "firefox", label: <a href="/download?browser=firefox#browser-extension" target="_blank" rel="noopener noreferrer">Firefox 插件</a> }] }}><Button icon={<DownloadOutlined />}>下载浏览器插件</Button></Dropdown>
        <Button type="primary" icon={<PlusOutlined aria-hidden="true" />} onClick={() => setChooserOpen(true)}>添加</Button>
      </Flex>
    </div>
    {moreFilters ? <div className={styles.moreFilters}>
      <Input aria-label="地区筛选" placeholder="当前 / 期望地区" value={filters.region} onChange={(event) => updateFilter("region", event.target.value)} />
      <Select aria-label="学历筛选" allowClear showSearch placeholder="学历" value={filters.education_level || undefined} onChange={(value) => updateFilter("education_level", value)} options={["博士", "硕士", "本科", "大专", "高中", "中专"].map((value) => ({ value, label: value }))} />
      <Input aria-label="工作年限筛选" placeholder="工作年限，如 5年" value={filters.work_years} onChange={(event) => updateFilter("work_years", event.target.value)} />
      <Input aria-label="求职状态筛选" placeholder="求职状态" value={filters.work_status} onChange={(event) => updateFilter("work_status", event.target.value)} />
      <Input aria-label="标签筛选" placeholder="标签" value={filters.tag} onChange={(event) => updateFilter("tag", event.target.value)} />
      <Input aria-label="入库人筛选" placeholder="入库人姓名 / 邮箱" value={filters.creator} onChange={(event) => updateFilter("creator", event.target.value)} />
      <label className={styles.dateField}>入库起始<Input aria-label="入库起始日期" type="date" value={filters.created_from} onChange={(event) => updateFilter("created_from", event.target.value)} /></label>
      <label className={styles.dateField}>入库截止<Input aria-label="入库截止日期" type="date" value={filters.created_to} onChange={(event) => updateFilter("created_to", event.target.value)} /></label>
    </div> : null}
    {loadError ? <Flex gap={8} style={{ marginBottom: 10 }}><Typography.Text type="danger">{loadError}</Typography.Text><Button size="small" onClick={() => void load()}>重试</Button></Flex> : null}
    <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
      <Table<Resume> size="small" rowKey={(item) => item.id + (item.engagement_id || "")} dataSource={items} loading={loading} pagination={false} tableLayout="fixed" className={styles.resumeTable} scroll={{ x: 1290 }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这里暂时空空的，先加一份简历，我来认真记账。"><Button type="primary" onClick={() => setChooserOpen(true)}>添加简历</Button></Empty> }} columns={[
        { title: "候选人", key: "candidate", width: 300, render: (_, item) => {
          const attributes = [resumeAge(item), genderLabel(item.gender), item.education_level, workYearsLabel(item), item.expected_position || "期望职位待补充", item.expected_region || item.work_region || "地区待补充", salaryLabel(item), item.work_status || "求职状态待补充", ...(item.tags || []).slice(0, 3)].filter(Boolean);
          return <Flex gap={8} align="start"><Avatar size={32} src={avatarSource(item.avatar_data_url)} alt={`${item.candidate_name || "候选人"}头像`} style={{ flexShrink: 0 }}>{item.candidate_name?.slice(0, 1) || "人"}</Avatar><Flex vertical gap={2} className={styles.candidateSummary}>
            <Flex gap={8} align="center" className={styles.candidateHeading}><Button type="link" className={styles.nameButton} title={item.candidate_name || "未命名候选人"} onClick={() => openDetails(item)}>{item.candidate_name || "未命名候选人"}</Button><Typography.Text ellipsis={{ tooltip: item.phone }} className={styles.phoneText}>{item.phone || "手机号待补充"}</Typography.Text></Flex>
            <div className={styles.candidateAttributes}>{attributes.map((attribute, index) => <Typography.Text key={index} className={styles.candidateAttribute}>{attribute}</Typography.Text>)}</div>
          </Flex></Flex>;
        } },
        { title: "最近经历", key: "experience", width: 300, render: (_, item) => <Flex vertical gap={4}>{[...(item.work_experiences || []), ...(item.educations || [])].slice(0, 2).map((experience, index) => <Typography.Paragraph key={index} ellipsis={{ rows: 2, tooltip: experienceLabel(experience) }} className={styles.cellText}>{experienceLabel(experience)}</Typography.Paragraph>)}{!item.work_experiences?.length && !item.educations?.length ? <Typography.Text type="secondary">经历待补充</Typography.Text> : null}</Flex> },
        { title: "岗位 / 推荐", key: "recommendation", width: 160, render: (_, item) => <Flex vertical gap={3}><Button type="link" className={styles.inlineButton} onClick={() => openDetails(item, "recommendations")}>{item.recommendation_count || item.recommendations?.length || 0} 个岗位记录</Button>{item.recommendations?.[0] ? <><Typography.Text ellipsis>{item.recommendations[0].position_name || "历史岗位"}</Typography.Text><Typography.Text type="secondary" className={styles.meta}>{stageLabel(item.recommendations[0].stage)}</Typography.Text></> : item.position_name ? <Typography.Text type="secondary" className={styles.meta}>自动任务 · {item.position_name}</Typography.Text> : null}</Flex> },
        { title: "最新备注", key: "note", width: 240, render: (_, item) => { const note = item.latest_note || item.notes?.[0]; return <Button type="text" className={styles.noteButton} onClick={() => openDetails(item, "notes")}><Flex vertical gap={3}><Typography.Paragraph ellipsis={{ rows: 2, tooltip: note?.content }} className={styles.cellText}>{note?.content || "还没备注，点这里记一条"}</Typography.Paragraph>{note ? <Typography.Text type="secondary" className={styles.meta} title={note.created_at ? `保存于 ${recordTime(note.created_at)}` : undefined}>{note.author_name || note.author_email || "未记录操作人"} · {(note.occurred_at || note.created_at) ? recordTime(note.occurred_at || note.created_at) : ""}</Typography.Text> : null}</Flex></Button>; } },
        { title: "创建时间", key: "created_at", width: 190, render: (_, item) => <Flex vertical gap={3}><Typography.Text className={styles.meta}>{recordTime(item.created_at)}</Typography.Text><Space wrap size={[2, 2]}><Tag>{sourceLabel(item.source_type, item.platform_id)}</Tag>{item.platform_id ? <Tag>{sourceLabel(item.platform_id)}</Tag> : null}{(item.sources?.length || 0) > 1 ? <Typography.Text className={styles.meta}>{item.sources!.length} 个来源</Typography.Text> : null}</Space><Typography.Text type="secondary" ellipsis={{ tooltip: item.creator_name || item.user_email }} className={styles.meta}>{item.creator_name || item.user_email || "创建人待补充"}</Typography.Text></Flex> },
        { title: "操作", key: "actions", width: 100, fixed: "right", render: (_, item) => isReadOnlyResume(item) ? <Button type="link" size="small" onClick={() => openDetails(item)}>查看</Button> : <Space size={2}><Button type="link" size="small" onClick={() => void openEditor(item)}>编辑</Button><Dropdown trigger={["click"]} menu={{ items: [{ key: "note", label: "加备注" }, { key: "recommend", label: "推荐到岗位" }, { key: "detail", label: "查看完整简历" }], onClick: ({ key }) => openDetails(item, key === "recommend" ? "recommendations" : "notes") }}><Button type="text" size="small" aria-label={`${item.candidate_name}更多操作`} icon={<MoreOutlined />} /></Dropdown></Space> },
      ]} />
      <Flex wrap gap={12} justify="space-between" align="center" className={styles.pagination}><Typography.Text type="secondary">共 {realTotal} 份简历{builtinTotal ? ` · 另有 ${builtinTotal} 份内置示例` : ""}</Typography.Text><Pagination current={page} total={total} pageSize={pageSize} showSizeChanger pageSizeOptions={[10, 20, 50, 100]} onChange={(next, size) => navigateQuery(appliedFilters, size !== pageSize ? 1 : next, size)} /></Flex>
    </SectionPanel>
    <AdminDialog open={chooserOpen} title="添加简历" onClose={() => setChooserOpen(false)} onConfirm={chooseAddMethod} confirmText="继续">
      <ChoiceCards label="选一种方式，我来帮你记入简历库。" value={addMethod} onChange={(value) => setAddMethod(String(value))} options={[{ value: "ai", label: "AI 批量解析", description: "多份文件一起解析，补齐手机号后确认入库。" }, { value: "manual", label: "手动添加", description: "姓名、手机号先填好，其它信息可以慢慢补。" }]} />
    </AdminDialog>
    <ResumeEditor open={editorOpen} candidate={editing} positions={positions} onClose={() => setEditorOpen(false)} onSaved={(item) => { setEditorOpen(false); void load(); openDetails(item); }} />
    <ResumeImportDialog open={importOpen} initialTaskID={params.get("resume_import_id") || ""} positions={positions} onClose={closeImport} onChanged={() => void load()} />
    <ResumeLibrarySettingsDialog open={settingsOpen} onClose={() => setSettingsOpen(false)} />
    <Drawer open={Boolean(detail)} onClose={() => setDetail(null)} size="large" title={detail?.name || "简历详情"} destroyOnHidden extra={detail ? <Link href={`/admin/resumes/detail?candidate_id=${encodeURIComponent(detail.id)}${detail.engagementID ? `&engagement_id=${encodeURIComponent(detail.engagementID)}` : ""}&return_to=${encodeURIComponent(`/admin/resumes?${queryText}`)}`}>单独打开</Link> : null} styles={{ wrapper: { width: "min(1280px, 96vw)" } }}>
      {detail ? <ResumeWorkspace candidateID={detail.id} engagementID={detail.engagementID} initialTab={detail.tab} positions={positions} onChanged={() => void load()} /> : null}
    </Drawer>
  </>;
}
