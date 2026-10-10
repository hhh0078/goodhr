/** 文件作用说明：共用简历详情工作区，展示结构化简历、通用备注、人工推荐及自动任务记录。 */
"use client";
import { Alert, Button, Collapse, Empty, Flex, Input, Radio, Select, Skeleton, Space, Tag, Timeline, Typography } from "antd";
import { EditOutlined, PlusOutlined, ShareAltOutlined } from "@ant-design/icons";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { useAdmin } from "../AdminApp";
import { addRecommendation, addResumeNote, loadRecommendations, loadResume, loadResumeNotes } from "@/lib/resume-api";
import { formatDate } from "@/lib/admin-api";
import { recommendationStages, recordTime, sourceLabel, stageLabel, type PositionOption, type Recommendation, type Resume, type ResumeNote } from "@/lib/resume-types";
import ResumeEditor from "./ResumeEditor";
import ResumeSummaryDialog from "./ResumeSummaryDialog";
import ResumeProfile from "./ResumeProfile";
import ResumeShareDialog from "./ResumeShareDialog";
import styles from "./resumes.module.css";

/** ResumeWorkspace 在详情地址与抽屉间复用同一候选人记录和维护入口。 */
export default function ResumeWorkspace({ candidateID, engagementID = "", positions, initialTab = "notes", onChanged }: {
  candidateID: string; engagementID?: string; positions: PositionOption[]; initialTab?: string; onChanged?: () => void;
}) {
  const { notify, user } = useAdmin();
  const accountIdentity = String(user?.id || user?.email || "").trim();
  const recommendationPreferenceKey = accountIdentity ? `goodhr5_resume_recommendations_visible:${accountIdentity}` : "";
  const recommendationEntryKey = JSON.stringify([accountIdentity, candidateID, engagementID]);
  const recommendationContentID = useId();
  const [candidate, setCandidate] = useState<Resume | null>(null);
  const [notes, setNotes] = useState<ResumeNote[]>([]);
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [summaryOpen, setSummaryOpen] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [note, setNote] = useState("");
  const [noteBusy, setNoteBusy] = useState(false);
  const [positionID, setPositionID] = useState<string>();
  const [stage, setStage] = useState("draft");
  const [recommendationNote, setRecommendationNote] = useState("");
  const [recommendationBusy, setRecommendationBusy] = useState(false);
  const [recommendationPreference, setRecommendationPreference] = useState({ account: "", visible: true });
  const [recommendationEntryScope, setRecommendationEntryScope] = useState<string | null>(null);
  const noteKey = useRef("");
  const recommendationKey = useRef("");
  const generation = useRef(0);
  const candidateScope = useRef(0);
  const noteSaving = useRef(false);
  const recommendationSaving = useRef(false);
  const pendingSection = useRef(initialTab);
  const lastEntrySection = useRef(initialTab);
  const notesSection = useRef<HTMLElement>(null);
  const recommendationsSection = useRef<HTMLElement>(null);
  const automationSection = useRef<HTMLElement>(null);
  const recommendationsVisible = (recommendationPreference.account === accountIdentity ? recommendationPreference.visible : true) || recommendationEntryScope === recommendationEntryKey;
  // 初始化只读偏好，写入仅由明确切换按钮触发，避免首次渲染用默认值覆盖已保存的隐藏设置。
  useEffect(() => {
    let visible = true;
    try { if (recommendationPreferenceKey) visible = localStorage.getItem(recommendationPreferenceKey) !== "0"; }
    catch { /* 当前浏览器不允许读取缓存时，默认展示且不影响简历操作。 */ }
    setRecommendationPreference({ account: accountIdentity, visible });
  }, [accountIdentity, recommendationPreferenceKey]);
  // 推荐入口只在本次候选人浏览中临时展开，普通入口及主动隐藏继续尊重已存偏好。
  useEffect(() => {
    setRecommendationEntryScope(initialTab === "recommendations" ? recommendationEntryKey : null);
  }, [recommendationEntryKey, initialTab]);
  /** reload 分别读取简历、备注和推荐，某项失败时保留其它已读结果。 */
  async function reload() {
    const current = ++generation.current;
    setLoading(true); setError("");
    const responses = await Promise.allSettled([loadResume(candidateID, engagementID), loadResumeNotes(candidateID), loadRecommendations(candidateID)]);
    if (current !== generation.current) return;
    if (responses[0].status === "fulfilled") setCandidate(responses[0].value);
    if (responses[1].status === "fulfilled") setNotes(responses[1].value);
    if (responses[2].status === "fulfilled") setRecommendations(responses[2].value);
    const failed = responses.filter((item) => item.status === "rejected");
    if (failed.length) setError(failed.map((item) => item.status === "rejected" ? item.reason?.message || "部分记录没读出来" : "").join("；"));
    setLoading(false);
  }
  useEffect(() => {
    candidateScope.current += 1;
    setCandidate(null); setNotes([]); setRecommendations([]); setNote(""); setEditing(false); setSummaryOpen(false); setShareOpen(false);
    setPositionID(undefined); setStage("draft"); setRecommendationNote("");
    noteSaving.current = false; recommendationSaving.current = false; setNoteBusy(false); setRecommendationBusy(false);
    noteKey.current = crypto.randomUUID(); recommendationKey.current = crypto.randomUUID();
    void reload();
    return () => { generation.current += 1; candidateScope.current += 1; };
  }, [candidateID, engagementID, accountIdentity]);
  useEffect(() => {
    pendingSection.current = initialTab === "notes" && lastEntrySection.current === initialTab ? "" : initialTab;
    lastEntrySection.current = initialTab;
  }, [candidateID, engagementID, initialTab, accountIdentity]);
  // 仅响应候选人或入口变化；保存后的读取不重新滚动，也不影响默认打开时阅读姓名和正文。
  useEffect(() => {
    if (loading || candidate?.id !== candidateID || !pendingSection.current) return;
    const target = pendingSection.current;
    pendingSection.current = "";
    if (target === "recommendations") recommendationsSection.current?.scrollIntoView({ block: "start" });
    else if (target === "automation") automationSection.current?.scrollIntoView({ block: "start" });
    else if (target === "notes") notesSection.current?.scrollIntoView({ block: "start" });
  }, [candidate, loading, candidateID, engagementID, initialTab]);
  /** saveNote 只新增通用备注，失败时保留正文供重试。 */
  async function saveNote() {
    if (!note.trim() || noteSaving.current) return;
    const currentScope = candidateScope.current;
    noteSaving.current = true;
    setNoteBusy(true);
    try {
      const saved = await addResumeNote(candidateID, note.trim(), noteKey.current);
      if (currentScope !== candidateScope.current) return;
      setNotes((current) => [saved, ...current.filter((item) => item.id !== saved.id)]);
      setNote(""); noteKey.current = crypto.randomUUID(); notify("备注记好了，打工小本本更新了。", "success"); onChanged?.();
    } catch (cause) { if (currentScope === candidateScope.current) notify(cause instanceof Error ? cause.message : "备注没记上，再试一次就好。", "error"); }
    finally { if (currentScope === candidateScope.current) { noteSaving.current = false; setNoteBusy(false); } }
  }
  /** saveRecommendation 明确提交当前阶段和该岗位备注，不影响自动任务状态。 */
  async function saveRecommendation() {
    const previous = recommendations.find((item) => item.position_id === positionID);
    if (!positionID || recommendationSaving.current || (previous?.stage === stage && !recommendationNote.trim())) return;
    const currentScope = candidateScope.current;
    recommendationSaving.current = true;
    setRecommendationBusy(true);
    try {
      const saved = await addRecommendation(candidateID, positionID, stage, recommendationNote.trim(), recommendationKey.current);
      if (currentScope !== candidateScope.current) return;
      // 保存回包已包含完整推进历史，直接更新关系，避免后续读取失败被误认为没有保存。
      setRecommendations((current) => [saved, ...current.filter((item) => item.position_id !== saved.position_id)]);
      setRecommendationNote(""); recommendationKey.current = crypto.randomUUID();
      notify(stage === "draft" ? "岗位已关联，等你明确推荐时再记推荐动作。" : "推荐和跟进记录已记好。", "success"); onChanged?.();
    } catch (cause) { if (currentScope === candidateScope.current) notify(cause instanceof Error ? cause.message : "推荐没记上，请重试。", "error"); }
    finally { if (currentScope === candidateScope.current) { recommendationSaving.current = false; setRecommendationBusy(false); } }
  }
  /** selectRecommendationPosition 切换岗位时只带入已存阶段，清掉其它岗位的未提交备注并更换请求编号。 */
  function selectRecommendationPosition(value: string) {
    if (recommendationSaving.current || value === positionID) return;
    setPositionID(value); setStage(recommendations.find((item) => item.position_id === value)?.stage || "draft");
    setRecommendationNote(""); recommendationKey.current = crypto.randomUUID();
  }
  /** editRecommendation 选择已有关系，保留当前岗位草稿，不复制旧备注或自动提交。 */
  function editRecommendation(item: Recommendation) {
    selectRecommendationPosition(item.position_id);
  }
  /** toggleRecommendations 仅记住当前账号主动选择的显示状态，不清理草稿或触发业务保存。 */
  function toggleRecommendations() {
    const visible = !recommendationsVisible;
    setRecommendationEntryScope(null);
    setRecommendationPreference({ account: accountIdentity, visible });
    try { if (recommendationPreferenceKey) localStorage.setItem(recommendationPreferenceKey, visible ? "1" : "0"); }
    catch { notify("这次显示设置没记住，当前窗口照常使用。", "info"); }
  }
  if (loading && !candidate) return <Skeleton active paragraph={{ rows: 8 }} />;
  if (!candidate) return <Alert type="warning" showIcon title={error || "这份简历暂时没读出来。"} action={<Button onClick={() => void reload()}>重试</Button>} />;
  const autoEvents = (candidate.events || []).filter((event) => event.event_type !== "manual_note");
  const currentOptions = [...positions];
  for (const item of recommendations) if (!currentOptions.some((option) => option.id === item.position_id)) currentOptions.push({ id: item.position_id, name: `${item.position_name || "已删除岗位"}（历史记录）` });
  const selectedRecommendation = recommendations.find((item) => item.position_id === positionID);
  const historyRecommendation = selectedRecommendation || (!positionID ? recommendations[0] : undefined);
  const recommendationChanged = Boolean(positionID && (!selectedRecommendation || selectedRecommendation.stage !== stage || recommendationNote.trim()));
  return <>
    {error ? <Alert type="warning" title={error} action={<Button size="small" onClick={() => void reload()}>重试读取</Button>} style={{ marginBottom: 12 }} /> : null}
    <div className={styles.workspace} data-testid="resume-detail-layout">
      <div className={styles.resumeBody} data-testid="resume-detail-main">
        <ResumeProfile candidate={candidate} showTags headerActions={<Space size={4} wrap><Button icon={<ShareAltOutlined aria-hidden="true" />} onClick={() => setShareOpen(true)}>分享</Button><Button icon={<EditOutlined aria-hidden="true" />} onClick={() => setEditing(true)}>编辑</Button></Space>} />
        <section className={styles.resumeSection}><Flex gap={8} justify="space-between" align="center" wrap><Typography.Title level={5} style={{ margin: 0 }}>AI 简历摘要</Typography.Title><Button size="small" onClick={() => setSummaryOpen(true)}>生成摘要</Button></Flex>{candidate.ai_summary ? <Typography.Paragraph className={styles.preWrap} style={{ marginTop: 6 }}>{candidate.ai_summary}</Typography.Paragraph> : <Typography.Text type="secondary">还没有摘要，点一下让本地AI帮忙整理。</Typography.Text>}</section>
        {candidate.raw_text ? <Collapse size="small" items={[{ key: "raw", label: "查看完整简历正文", children: <Typography.Paragraph className={styles.preWrap}>{candidate.raw_text}</Typography.Paragraph> }]} style={{ marginTop: 10 }} /> : null}
        <section className={styles.resumeSection}><Typography.Text type="secondary">{sourceLabel(candidate.source_type, candidate.platform_id)} · 入库人：{candidate.creator_name || candidate.user_email || "暂时没记上"} · {candidate.created_at ? formatDate(candidate.created_at) : "入库时间待补充"}</Typography.Text>
          {(candidate.sources || []).map((source, index) => <div key={`${source.platform_id}-${index}`}><Typography.Text type="secondary">{source.source_type ? sourceLabel(source.source_type, source.platform_id) : sourceLabel(source.platform_id)}{source.source_url ? " · " : ""}</Typography.Text>{source.source_url && /^https?:\/\//.test(source.source_url) ? <a href={source.source_url} target="_blank" rel="noopener noreferrer">查看原页面</a> : null}</div>)}
        </section>
      </div>
      <aside className={styles.recordAside} data-testid="resume-detail-aside">
        <section ref={notesSection} className={styles.recordSection} data-testid="resume-notes-section" aria-label="备注">
          <Typography.Title level={5}>备注{notes.length ? ` · ${notes.length}` : ""}</Typography.Title>
          <Flex vertical gap={8}>
          <Input.TextArea value={note} onChange={(event) => { setNote(event.target.value); noteKey.current = crypto.randomUUID(); }} maxLength={1000} showCount disabled={noteBusy} autoSize={{ minRows: 2, maxRows: 6 }} placeholder="比如：意向不错，周五再约一次。" />
          {/* 字数计数浮在输入框下方，预留高度避免压住右侧按钮。 */}
          <Flex justify="flex-end" style={{ marginTop: 16 }}><Button size="small" type="primary" icon={<PlusOutlined aria-hidden="true" />} loading={noteBusy} disabled={!note.trim()} onClick={() => void saveNote()}>加备注</Button></Flex>
          <div className={styles.recordHistory}>{notes.length ? notes.map((item) => <div key={item.id} className={styles.record}><Typography.Paragraph className={styles.preWrap}>{item.content}</Typography.Paragraph><Typography.Text type="secondary" className={styles.meta} title={item.created_at ? `保存于 ${recordTime(item.created_at)}` : undefined}>{item.author_name || item.author_email || "未记录操作人"} · {(item.occurred_at || item.created_at) ? recordTime(item.occurred_at || item.created_at) : ""}</Typography.Text></div>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这里还没备注，先记一条也行。" />}</div>
          </Flex>
        </section>
        <section ref={recommendationsSection} className={styles.recordSection} data-testid="resume-recommendations-section" aria-label="推荐">
          <Flex gap={8} align="center" justify="space-between" className={styles.recommendationHeading}>
            <Typography.Title level={5} style={{ margin: 0 }}>推荐</Typography.Title>
            <Button size="small" type="text" onClick={toggleRecommendations} aria-expanded={recommendationsVisible} aria-controls={recommendationContentID}>{recommendationsVisible ? "隐藏推荐" : "显示推荐"}</Button>
          </Flex>
          <div id={recommendationContentID} data-testid="resume-recommendation-content" hidden={!recommendationsVisible} className={styles.recommendationContent}>
          <Flex vertical gap={8}>
          <Select aria-label="选择推荐岗位" showSearch optionFilterProp="label" placeholder="选择岗位" value={positionID} disabled={recommendationBusy} onChange={selectRecommendationPosition} options={currentOptions.map((item) => ({ value: item.id, label: item.name }))} />
          <Radio.Group aria-label="推荐阶段" value={stage} disabled={recommendationBusy} className={styles.recommendationStages} onChange={(event) => { if (recommendationSaving.current) return; setStage(event.target.value); recommendationKey.current = crypto.randomUUID(); }} options={recommendationStages} />
          <Input.TextArea aria-label="新增岗位跟进备注" value={recommendationNote} disabled={recommendationBusy} maxLength={1000} autoSize={{ minRows: 2, maxRows: 5 }} onChange={(event) => { if (recommendationSaving.current) return; setRecommendationNote(event.target.value); recommendationKey.current = crypto.randomUUID(); }} placeholder="只记这个岗位的推荐、面试和跟进情况。" />
          <Flex justify="flex-end"><Button size="small" type="primary" loading={recommendationBusy} disabled={!recommendationChanged} onClick={() => void saveRecommendation()}>{stage === "draft" ? "保存关联" : stage === "recommended" ? "记录推荐" : "记录跟进"}</Button></Flex>
          {recommendations.length ? <div className={styles.recommendationPicker} data-testid="resume-recommendation-picker" role="group" aria-label="已关联岗位">{recommendations.map((item) => <RecommendationCard key={item.id} item={item} selected={historyRecommendation?.position_id === item.position_id} busy={recommendationBusy} onSelect={() => editRecommendation(item)} />)}</div> : null}
          <div className={styles.recommendationHistory} data-testid="resume-recommendation-history">{historyRecommendation ? <RecommendationHistory item={historyRecommendation} /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={positionID ? "这个岗位还没记录，保存后我再认真记账。" : "还没关联岗位，选一个就能开始记记录。"} />}</div>
          </Flex>
          </div>
        </section>
        <section ref={automationSection} className={styles.recordSection} data-testid="resume-auto-tasks-section" aria-label="自动任务">
          <Typography.Title level={5}>自动任务</Typography.Title>
          <Flex vertical gap={8}>
          {candidate.engagement_id ? <><Typography.Text>岗位：{candidate.position_name || "未记录岗位"}</Typography.Text><Typography.Text type="secondary">任务状态：{candidate.engagement_status || "未记录"}</Typography.Text></> : null}
          <AIResult title="详情分析" value={candidate.ai?.detail} /><AIResult title="招呼分析" value={candidate.ai?.greet} />
          {autoEvents.length ? <Timeline items={autoEvents.map((event) => ({ children: <><Typography.Text>{String(event.message_text || event.event_type || "任务记录")}</Typography.Text><div className={styles.meta}>{event.created_at ? formatDate(String(event.created_at)) : ""}</div></> }))} /> : <Typography.Text type="secondary">暂时没有更多自动任务记录。</Typography.Text>}
          </Flex>
        </section>
      </aside>
    </div>
    <ResumeEditor open={editing} candidate={candidate} positions={positions} onClose={() => setEditing(false)} onSaved={(saved) => { setEditing(false); setCandidate(saved); void reload(); onChanged?.(); }} />
    <ResumeSummaryDialog open={summaryOpen} candidate={candidate} onClose={() => setSummaryOpen(false)} onSaved={(saved) => { setSummaryOpen(false); setCandidate(saved); void reload(); onChanged?.(); }} />
    <ResumeShareDialog key={`${accountIdentity}:${candidateID}`} open={shareOpen} candidateID={candidateID} onClose={() => setShareOpen(false)} />
  </>;
}
/** RecommendationCard 在横向岗位条中切换当前历史，选择仅填表，不复制旧备注或触发保存。 */
function RecommendationCard({ item, selected, busy, onSelect }: { item: Recommendation; selected: boolean; busy: boolean; onSelect: () => void }) {
  /** selectByKeyboard 让回车和空格与点击使用同一选择规则，保存中忽略键盘操作。 */
  function selectByKeyboard(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    if (!busy) onSelect();
  }
  return <div role="button" aria-label={`选择推荐记录 ${item.position_name || "已删除岗位"}`} aria-pressed={selected} aria-disabled={busy} tabIndex={busy ? -1 : 0} className={`${styles.recommendationCard}${selected ? ` ${styles.recommendationCardSelected}` : ""}`} onClick={() => { if (!busy) onSelect(); }} onKeyDown={selectByKeyboard}>
    <Typography.Text ellipsis={{ tooltip: item.position_name || "已删除岗位" }}>{item.position_name || "已删除岗位"}</Typography.Text>
  </div>;
}
/** RecommendationHistory 直接展示当前岗位的完整历史，每条只出现一次阶段、作者时间与备注。 */
function RecommendationHistory({ item }: { item: Recommendation }) {
  const events = item.events || [];
  const legacyNote = item.note?.trim();
  const showLegacyNote = legacyNote && !events.some((event) => (event.content || event.note || "").trim() === legacyNote);
  return <>
    {events.map((event) => <article key={event.id} className={styles.recommendationEvent}>
      <Flex gap={6} align="start" justify="space-between" wrap>
        <Tag>{event.from_stage && event.from_stage !== (event.to_stage || item.stage) ? `${stageLabel(event.from_stage)} → ${stageLabel(event.to_stage || item.stage)}` : stageLabel(event.to_stage || item.stage)}</Tag>
        <Typography.Text type="secondary" className={styles.meta} title={event.created_at ? `保存于 ${recordTime(event.created_at)}` : undefined}>{event.author_name || event.author_email || "未记录操作人"} · {(event.occurred_at || event.created_at) ? recordTime(event.occurred_at || event.created_at) : "时间待补充"}</Typography.Text>
      </Flex>
      {(event.content || event.note) ? <Typography.Paragraph className={styles.preWrap}>{event.content || event.note}</Typography.Paragraph> : null}
    </article>)}
    {showLegacyNote || !events.length ? <article className={styles.recommendationEvent}>
      <Flex gap={6} align="start" justify="space-between" wrap><Tag>{stageLabel(item.stage)}</Tag><Typography.Text type="secondary" className={styles.meta}>{item.author_name || item.author_email || "未记录操作人"} · {(item.created_at || item.recommended_at) ? recordTime(item.created_at || item.recommended_at) : "时间待补充"}</Typography.Text></Flex>
      {legacyNote ? <Typography.Paragraph className={styles.preWrap}>{legacyNote}</Typography.Paragraph> : null}
    </article> : null}
  </>;
}
/** AIResult 显示任务分析，缺失分数保留“未分析”而非零分。 */
function AIResult({ title, value }: { title: string; value?: { score?: number | null; reason?: string } }) {
  return <div className={styles.record}><Typography.Text strong>{title} · {value?.score == null ? "未分析" : `${value.score}分`}</Typography.Text>{value?.reason ? <Typography.Paragraph className={styles.preWrap}>{value.reason}</Typography.Paragraph> : null}</div>;
}
