/** 文件作用说明：集中查看和维护猎头推荐，复用候选人详情推进历史，保留关闭岗位记录。 */
"use client";
import { Alert, Button, Drawer, Flex, Input, Pagination, Select, Table, Tag, Typography } from "antd";
import { PlusOutlined, SearchOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import AdminDialog from "@/components/admin/AdminDialog";
import { useAdmin } from "@/components/admin/AdminApp";
import ResumeWorkspace from "@/components/admin/resumes/ResumeWorkspace";
import RecommendationCreateDialog from "@/components/admin/resumes/RecommendationCreateDialog";
import { deleteRecommendationRecord, loadRecommendationRecords, loadResumePositions } from "@/lib/resume-api";
import { latestRecommendationEvent, recommendationStages, recordTime, stageLabel, type PositionOption, type RecommendationRecord } from "@/lib/resume-types";
import styles from "./page.module.css";

/** RecommendationsPage 用分页列表管理已有推荐，候选人详情和阶段推进沿用同一工作区。 */
export default function RecommendationsPage() {
  const { notify } = useAdmin();
  const params = useSearchParams(); const router = useRouter(); const queryText = params.toString();
  const [q, setQ] = useState(params.get("q") || ""); const [stage, setStage] = useState(params.get("stage") || "");
  const [items, setItems] = useState<RecommendationRecord[]>([]); const [total, setTotal] = useState(0);
  const [positions, setPositions] = useState<PositionOption[]>([]);
  const [loading, setLoading] = useState(false); const [error, setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false); const [deleting, setDeleting] = useState<RecommendationRecord | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false); const [deleteError, setDeleteError] = useState("");
  const [detail, setDetail] = useState<{ record: RecommendationRecord; tab: string } | null>(null);
  const generation = useRef(0); const deleteInProgress = useRef(false);
  const requestedPage = Number(params.get("page")); const page = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1;
  const pageSize = [10, 20, 50, 100].includes(Number(params.get("page_size"))) ? Number(params.get("page_size")) : 20;
  /** loadList 读取当前地址对应的筛选分页，晚到的旧查询不会替换当前列表。 */
  async function loadList() {
    const current = ++generation.current; setLoading(true); setError("");
    const query = new URLSearchParams(queryText); query.set("page", String(page)); query.set("page_size", String(pageSize));
    try { const data = await loadRecommendationRecords(query); if (current === generation.current) { setItems(data.items); setTotal(data.total); } }
    catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "推荐记录没读出来，请重试。"); }
    finally { if (current === generation.current) setLoading(false); }
  }
  /** reloadPositions 刷新开启岗位，关闭后的推荐历史仍独立保留在列表。 */
  async function reloadPositions() {
    try { setPositions(await loadResumePositions()); }
    catch (cause) { notify(cause instanceof Error ? cause.message : "岗位没读出来，请刷新后再试。", "warning"); }
  }
  useEffect(() => { void reloadPositions(); }, []);
  useEffect(() => { setQ(params.get("q") || ""); setStage(params.get("stage") || ""); void loadList(); return () => { generation.current += 1; }; }, [queryText]);
  /** navigateFilters 写回搜索和分页，返回列表时保持当前筛选。 */
  function navigateFilters(nextQ = q, nextStage = stage, nextPage = 1, nextSize = pageSize) {
    const query = new URLSearchParams(queryText); query.delete("q"); query.delete("stage");
    if (nextQ.trim()) query.set("q", nextQ.trim()); if (nextStage) query.set("stage", nextStage);
    query.set("page", String(nextPage)); query.set("page_size", String(nextSize));
    if (query.toString() === queryText) void loadList(); else router.replace(`/admin/recommendations?${query}`, { scroll: false });
  }
  /** openRecord 打开指定候选人及推荐岗位，阶段和历史共用原详情工作区。 */
  function openRecord(record: RecommendationRecord, tab = "recommendations") { setDetail({ record, tab }); void reloadPositions(); }
  /** removeRecord 只删除当前推荐及跟进历史，不调用候选人或岗位删除接口。 */
  async function removeRecord() {
    if (!deleting || deleteInProgress.current) return;
    deleteInProgress.current = true; setDeleteBusy(true); setDeleteError("");
    try { await deleteRecommendationRecord(deleting.id); setDeleting(null); notify("推荐记录删除了，简历和岗位都还在。", "success"); void loadList(); }
    catch (cause) { setDeleteError(cause instanceof Error ? cause.message : "推荐记录没删成功，请重试。"); }
    finally { deleteInProgress.current = false; setDeleteBusy(false); }
  }
  return <>
    <PageHeader title="猎头推荐" description="候选人、岗位和跟进放一起，少翻几次页面。" actions={<><Button type="primary" icon={<PlusOutlined aria-hidden="true" />} onClick={() => setCreateOpen(true)}>新增推荐</Button><RefreshButton loading={loading} onClick={() => { void loadList(); void reloadPositions(); }} /></>} />
    <Flex gap={8} wrap className={styles.filters}>
      <Input aria-label="搜索猎头推荐" prefix={<SearchOutlined aria-hidden="true" />} placeholder="候选人、手机号或岗位" value={q} onChange={(event) => setQ(event.target.value)} onPressEnter={() => navigateFilters()} className={styles.keyword} />
      <Select aria-label="筛选推荐阶段" allowClear showSearch optionFilterProp="label" placeholder="全部阶段" value={stage || undefined} onChange={(value) => setStage(value || "")} options={recommendationStages} className={styles.stage} />
      <Button aria-label="查询" type="primary" loading={loading} onClick={() => navigateFilters()}>查询</Button><Button onClick={() => navigateFilters("", "")}>重置</Button>
    </Flex>
    {error ? <Alert type="warning" showIcon title={error} action={<Button size="small" onClick={() => void loadList()}>重试</Button>} style={{ marginBottom: 10 }} /> : null}
    <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
      <Table<RecommendationRecord> size="small" rowKey="id" loading={loading} dataSource={items} pagination={false} scroll={{ x: 1120 }} className={styles.records}
        onRow={(record) => ({ title: "双击打开候选人简历", onDoubleClick: (event) => { if (event.target instanceof Element && event.target.closest("button,a,input,textarea,select,[role='button'],[role='combobox']")) return; openRecord(record, "notes"); } })}
        locale={{ emptyText: "这里暂时没有推荐记录，先选一位候选人吧。" }} columns={[
          { title: "候选人", width: 180, fixed: "left", render: (_, record) => <Flex vertical gap={2}><Button type="link" className={styles.inline} onClick={() => openRecord(record, "notes")}>{record.candidate_name || "未命名候选人"}</Button><Typography.Text type="secondary" className={styles.meta}>{record.phone || "手机号待补充"}</Typography.Text></Flex> },
          { title: "岗位", width: 180, render: (_, record) => <Flex vertical gap={2}><Typography.Text>{record.position_name || "历史岗位"}</Typography.Text>{record.position_is_open === false ? <Typography.Text type="secondary" className={styles.meta}>已关闭 · 历史保留</Typography.Text> : null}</Flex> },
          { title: "阶段", width: 95, render: (_, record) => <Tag>{stageLabel(record.stage)}</Tag> },
          { title: "最新跟进", width: 250, render: (_, record) => { const latest = latestRecommendationEvent(record.events); return <Typography.Paragraph ellipsis={{ rows: 2, tooltip: true }} className={styles.note}>{latest?.content || latest?.note || record.note || "还没备注"}</Typography.Paragraph>; } },
          { title: "跟进时间", width: 155, render: (_, record) => { const latest = latestRecommendationEvent(record.events); return <Typography.Text className={styles.meta}>{recordTime(latest?.occurred_at || latest?.created_at || record.updated_at || record.recommended_at || record.created_at)}</Typography.Text>; } },
          { title: "推荐人", width: 135, render: (_, record) => <Typography.Text ellipsis={{ tooltip: true }} className={styles.meta}>{record.author_name || record.author_email || "未记录"}</Typography.Text> },
          { title: "操作", width: 125, fixed: "right", render: (_, record) => <Flex gap={4}><Button type="link" size="small" onClick={() => openRecord(record)}>{record.can_edit ? "管理" : "查看"}</Button>{record.can_delete ? <Button type="link" size="small" danger onClick={() => { setDeleting(record); setDeleteError(""); }}>删除</Button> : null}</Flex> },
        ]} />
      <Flex justify="space-between" align="center" gap={10} wrap className={styles.pagination}><Typography.Text type="secondary">共 {total} 条推荐</Typography.Text><Pagination current={page} pageSize={pageSize} total={total} showSizeChanger pageSizeOptions={[10, 20, 50, 100]} onChange={(next, size) => navigateFilters(params.get("q") || "", params.get("stage") || "", size !== pageSize ? 1 : next, size)} /></Flex>
    </SectionPanel>
    <RecommendationCreateDialog open={createOpen} onClose={() => setCreateOpen(false)} onSaved={() => { void loadList(); void reloadPositions(); }} />
    <AdminDialog open={Boolean(deleting)} title="删除推荐记录" maxWidth="xs" loading={deleteBusy} onClose={() => setDeleting(null)} onConfirm={() => void removeRecord()} confirmText="删除记录">
      {deleteError ? <Alert type="warning" showIcon title={deleteError} style={{ marginBottom: 10 }} /> : null}
      <Typography.Paragraph>删除“{deleting?.candidate_name} → {deleting?.position_name}”的推荐及跟进记录？简历和岗位会保留。</Typography.Paragraph>
    </AdminDialog>
    <Drawer open={Boolean(detail)} onClose={() => setDetail(null)} title={detail?.record.candidate_name || "候选人详情"} destroyOnHidden styles={{ wrapper: { width: "min(1280px, 96vw)" } }}>
      {detail ? <ResumeWorkspace key={`${detail.record.id}:${detail.tab}`} candidateID={detail.record.candidate_id} initialPositionID={detail.record.position_id} initialTab={detail.tab} positions={positions} onChanged={() => void loadList()} /> : null}
    </Drawer>
  </>;
}
