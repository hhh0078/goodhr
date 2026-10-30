/** 文件作用说明：共用简历详情工作区，展示结构化简历、通用备注、人工推荐及自动任务记录。 */
"use client";
import { Alert, Button, Collapse, Empty, Flex, Input, Radio, Select, Skeleton, Space, Steps, Tabs, Tag, Timeline, Typography } from "antd";
import { EditOutlined, PlusOutlined, ShareAltOutlined } from "@ant-design/icons";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { useAdmin } from "../AdminApp";
import { addRecommendation, addResumeNote, loadRecommendations, loadResume, loadResumeNotes } from "@/lib/resume-api";
import { formatDate } from "@/lib/admin-api";
import { isReadOnlyResume, latestRecommendationEvent, recommendationStages, recordTime, sourceLabel, stageLabel, type PositionOption, type Recommendation, type Resume, type ResumeNote } from "@/lib/resume-types";
import ResumeEditor from "./ResumeEditor";
import ResumeSummaryDialog from "./ResumeSummaryDialog";
import ResumeRecruitmentPanel from "../recruitment/ResumeRecruitmentPanel";
import ResumeProfile, { ResumeCandidateHeader } from "./ResumeProfile";
import ResumeShareDialog from "./ResumeShareDialog";
import ResumeAutoReplyReports from "./ResumeAutoReplyReports";
import styles from "./resumes.module.css";
import detailStyles from "./resume-detail.module.css";

/** entryTab 把历史入口映射到详情标签，备注入口始终保留右侧备注区。 */
function entryTab(value: string) {
  return ["recommendations", "automation", "recruitment", "auto_reply"].includes(value) ? value : "resume";
}

/** ResumeWorkspace 在详情地址与抽屉间复用同一候选人记录和维护入口。 */
export default function ResumeWorkspace({ candidateID, engagementID = "", positions, initialTab = "resume", initialPositionID = "", onChanged }: {
  candidateID: string; engagementID?: string; positions: PositionOption[]; initialTab?: string; initialPositionID?: string; onChanged?: () => void;
}) {
  const { notify, user } = useAdmin();
  const accountIdentity = String(user?.id || user?.email || "").trim();
  const recommendationPreferenceKey = accountIdentity ? `goodhr5_resume_recommendations_visible:${accountIdentity}` : "";
  const recommendationEntryKey = JSON.stringify([accountIdentity, candidateID, engagementID]);
  const recommendationContentID = useId();
  const [candidate, setCandidate] = useState<Resume | null>(null);
  const readOnly = isReadOnlyResume(candidate || candidateID);
  const [notes, setNotes] = useState<ResumeNote[]>([]);
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState(entryTab(initialTab));
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [summaryOpen, setSummaryOpen] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [note, setNote] = useState("");
  const [noteBusy, setNoteBusy] = useState(false);
  const [positionID, setPositionID] = useState<string>();
  const pendingInitialPosition = useRef(initialPositionID);
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
  const notesSection = useRef<HTMLElement>(null);
  const pendingNotesEntry = useRef(initialTab === "notes");
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
    pendingInitialPosition.current = initialPositionID;
    setPositionID(initialPositionID || undefined); setStage("draft"); setRecommendationNote("");
    noteSaving.current = false; recommendationSaving.current = false; setNoteBusy(false); setRecommendationBusy(false);
    noteKey.current = crypto.randomUUID(); recommendationKey.current = crypto.randomUUID();
    void reload();
    return () => { generation.current += 1; candidateScope.current += 1; };
  }, [candidateID, engagementID, accountIdentity, initialPositionID]);
  // 独立推荐页入口只在首次读到历史后恢复指定岗位阶段，保存及再次读取不会覆盖未提交草稿。
  useEffect(() => {
    if (loading || !pendingInitialPosition.current) return;
    const initial = pendingInitialPosition.current; pendingInitialPosition.current = "";
    setStage(recommendations.find((item) => item.position_id === initial)?.stage || "draft");
  }, [loading, recommendations]);
  useEffect(() => {
    setActiveTab(entryTab(initialTab));
    pendingNotesEntry.current = initialTab === "notes";
  }, [candidateID, engagementID, initialTab, accountIdentity]);
  // 备注入口在手机上直接定位到备注卡；保存和重新读取后不打断当前阅读位置。
  useEffect(() => {
    if (loading || candidate?.id !== candidateID || !pendingNotesEntry.current) return;
    pendingNotesEntry.current = false;
    notesSection.current?.scrollIntoView({ block: "start" });
  }, [loading, candidate, candidateID, initialTab]);
  /** saveNote 只新增通用备注，失败时保留正文供重试。 */
  async function saveNote() {
    if (readOnly || !note.trim() || noteSaving.current) return;
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
    if (readOnly || !positionID || !positions.some((item) => item.id === positionID) || recommendationSaving.current || (previous?.stage === stage && !recommendationNote.trim())) return;
    const currentScope = candidateScope.current;
    recommendationSaving.current = true;
    setRecommendationBusy(true);
    try {
      const saved = await addRecommendation(candidateID, positionID, stage, recommendationNote.trim(), recommendationKey.current);
      if (currentScope !== candidateScope.current) return;
      // 保存回包已包含完整推进历史，直接更新关系，避免后续读取失败被误认为没有保存。
      setRecommendations((current) => [saved, ...current.filter((item) => item.position_id !== saved.position_id)]);
      setRecommendationNote(""); recommendationKey.current = crypto.randomUUID();
      setActiveTab("recommendations");
      notify(stage === "draft" ? "岗位已关联，等你明确推荐时再记推荐动作。" : "推荐和跟进记录已记好。", "success"); onChanged?.();
    } catch (cause) { if (currentScope === candidateScope.current) notify(cause instanceof Error ? cause.message : "推荐没记上，请重试。", "error"); }
    finally { if (currentScope === candidateScope.current) { recommendationSaving.current = false; setRecommendationBusy(false); } }
  }
  /** selectRecommendationPosition 切换岗位时只带入已存阶段，清掉其它岗位的未提交备注并更换请求编号。 */
  function selectRecommendationPosition(value: string) {
    if (readOnly || recommendationSaving.current || value === positionID) return;
    setPositionID(value); setStage(recommendations.find((item) => item.position_id === value)?.stage || "draft");
    setActiveTab("recommendations");
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
  const positionUnavailable = Boolean(positionID && !currentOptions.some((option) => option.id === positionID));
  const selectedRecommendation = recommendations.find((item) => item.position_id === positionID);
  const historyRecommendation = selectedRecommendation || (!positionID ? recommendations[0] : undefined);
  const recommendationChanged = Boolean(positionID && (!selectedRecommendation || selectedRecommendation.stage !== stage || recommendationNote.trim()));
  const timelineStage = historyRecommendation?.stage || "draft";
  const progressStages = recommendationStages.filter((item) => item.value !== "paused");
  const progressStage = timelineStage === "paused" ? latestRecommendationEvent(historyRecommendation?.events)?.from_stage || "draft" : timelineStage;
  const currentStage = Math.max(0, progressStages.findIndex((item) => item.value === progressStage));
  return <>
    <div className={detailStyles.workspace} data-testid="resume-detail-layout">
      <main className={detailStyles.main} data-testid="resume-detail-main">
        {readOnly ? <Alert type="info" showIcon title="管理员内置的只读示例，资料仅供演示，不能修改或删除。" data-testid="resume-builtin-notice" /> : null}
        {error ? <Alert type="warning" title={error} action={<Button onClick={() => void reload()}>重试读取</Button>} /> : null}
        <div className={detailStyles.candidateTop}>
          <Flex align="center" justify="space-between" gap={8} wrap className={detailStyles.context}>
            <Space size={6} wrap><Tag>{sourceLabel(candidate.source_type, candidate.platform_id)}</Tag>{historyRecommendation ? <Tag>{historyRecommendation.position_name || "历史岗位"}</Tag> : null}</Space>
            <Typography.Text type="secondary" className={styles.meta}>{candidate.created_at ? "入库于 " + formatDate(candidate.created_at) : "入库时间待补充"}</Typography.Text>
          </Flex>
          <ResumeCandidateHeader candidate={candidate} showTags headerActions={readOnly ? null : <Button type="text" icon={<EditOutlined aria-hidden="true" />} onClick={() => setEditing(true)}>编辑</Button>} />
        </div>
        <Tabs activeKey={activeTab} onChange={setActiveTab} className={detailStyles.tabs} items={[
          { key: "resume", label: "简历", children: <>
            <ResumeProfile candidate={candidate} hideHeader />
            <section className={styles.resumeSection}><Flex gap={8} justify="space-between" align="center" wrap><Typography.Title level={5} style={{ margin: 0 }}>AI 简历摘要</Typography.Title><Button disabled={readOnly} onClick={() => { if (!readOnly) setSummaryOpen(true); }}>生成摘要</Button></Flex>{candidate.ai_summary ? <Typography.Paragraph className={styles.preWrap} style={{ marginTop: 8 }}>{candidate.ai_summary}</Typography.Paragraph> : <Typography.Text type="secondary">{readOnly ? "内置示例不调用AI生成摘要。" : "还没有摘要，点一下让本地AI帮忙整理。"}</Typography.Text>}</section>
            {candidate.raw_text ? <Collapse items={[{ key: "raw", label: "查看完整简历正文", children: <Typography.Paragraph className={styles.preWrap}>{candidate.raw_text}</Typography.Paragraph> }]} style={{ marginTop: 16 }} /> : null}
            <section className={styles.resumeSection}><Typography.Text type="secondary">入库人：{candidate.creator_name || candidate.user_email || "暂时没记上"}</Typography.Text>
              {(candidate.sources || []).map((source, index) => <div key={String(source.platform_id) + index}><Typography.Text type="secondary">{source.source_type ? sourceLabel(source.source_type, source.platform_id) : sourceLabel(source.platform_id)}{source.source_url ? " · " : ""}</Typography.Text>{source.source_url && /^https?:\/\//.test(source.source_url) ? <a href={source.source_url} target="_blank" rel="noopener noreferrer">查看原页面</a> : null}</div>)}
            </section>
          </> },
          { key: "recommendations", label: "推荐 " + recommendations.length, children: <section aria-label="推荐跟进记录">
            <Flex justify="space-between" align="center" gap={8} wrap className={detailStyles.sectionHeading}><Typography.Title level={5}>推荐与跟进</Typography.Title><Typography.Text type="secondary">按岗位分别记录</Typography.Text></Flex>
            {recommendationsVisible ? <>
              {recommendations.length ? <div className={styles.recommendationPicker} data-testid="resume-recommendation-picker" role="group" aria-label="已关联岗位">{recommendations.map((item) => <RecommendationCard key={item.id} item={item} selected={historyRecommendation?.position_id === item.position_id} busy={recommendationBusy} onSelect={() => editRecommendation(item)} />)}</div> : null}
              <div className={detailStyles.recommendationHistory} data-testid="resume-recommendation-history">{historyRecommendation ? <RecommendationHistory item={historyRecommendation} /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={positionID ? "这个岗位还没记录，保存后我再认真记账。" : "还没关联岗位，右侧选一个就能开始。"} />}</div>
            </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="推荐记录已收起，随时可以展开。"><Button onClick={toggleRecommendations}>展开推荐记录</Button></Empty>}
          </section> },
          { key: "recruitment", label: "面试 / 招聘", disabled: readOnly, children: !readOnly ? <ResumeRecruitmentPanel key={candidate.id} candidate={candidate} positions={positions} positionID={positionID || historyRecommendation?.position_id || ""} initiallyExpanded /> : null },
          { key: "auto_reply", label: "自动回复", disabled: readOnly, children: !readOnly ? <ResumeAutoReplyReports key={candidate.id} candidateID={candidate.id} /> : null },
          { key: "automation", label: "任务记录", children: <section className={styles.recordSection} data-testid="resume-auto-tasks-section" aria-label="自动任务">
            <Typography.Title level={5}>自动任务</Typography.Title><Flex vertical gap={12}>
              {candidate.engagement_id ? <><Typography.Text>岗位：{candidate.position_name || "未记录岗位"}</Typography.Text><Typography.Text type="secondary">任务状态：{candidate.engagement_status || "未记录"}</Typography.Text></> : null}
              <AIResult title="详情分析" value={candidate.ai?.detail} /><AIResult title="招呼分析" value={candidate.ai?.greet} />
              {autoEvents.length ? <Timeline items={autoEvents.map((event, index) => ({ key: String(event.id || index), children: <><Typography.Text>{String(event.message_text || event.event_type || "任务记录")}</Typography.Text><div className={styles.meta}>{event.created_at ? formatDate(String(event.created_at)) : ""}</div></> }))} /> : <Typography.Text type="secondary">暂时没有更多自动任务记录。</Typography.Text>}
            </Flex>
          </section> },
        ]} />
      </main>
      <aside className={detailStyles.aside} data-testid="resume-detail-aside">
        <section className={detailStyles.sideCard}>
          {historyRecommendation ? <div className={detailStyles.progress} aria-label="当前推荐进度">
            <Steps size="small" responsive={false} current={currentStage} status={timelineStage === "paused" ? "error" : timelineStage === "hired" ? "finish" : "process"} items={progressStages.map((item) => ({ title: item.label }))} />
            <Flex justify="space-between" gap={8}><Typography.Text strong>{stageLabel(timelineStage)}</Typography.Text><Typography.Text type="secondary">{historyRecommendation.position_name || "历史岗位"}</Typography.Text></Flex>
          </div> : <Typography.Title level={5} className={detailStyles.cardTitle}>操作</Typography.Title>}
          <Button type="primary" block disabled={readOnly} onClick={() => { if (readOnly) return; setRecommendationEntryScope(recommendationEntryKey); setActiveTab("recommendations"); }}>推荐到岗位</Button>
          <div className={detailStyles.actionGrid}><Button icon={<ShareAltOutlined aria-hidden="true" />} disabled={readOnly} onClick={() => { if (!readOnly) setShareOpen(true); }}>分享简历</Button><Button disabled={readOnly} onClick={() => { if (!readOnly) setSummaryOpen(true); }}>AI 摘要</Button></div>
        </section>
        <section className={detailStyles.sideCard} data-testid="resume-recommendations-section" aria-label="推荐">
          <Flex gap={8} align="center" justify="space-between" className={styles.recommendationHeading}><Typography.Title level={5} className={detailStyles.cardTitle}>推荐跟进</Typography.Title><Button type="text" onClick={toggleRecommendations} aria-expanded={recommendationsVisible} aria-controls={recommendationContentID}>{recommendationsVisible ? "隐藏推荐" : "显示推荐"}</Button></Flex>
          <div id={recommendationContentID} data-testid="resume-recommendation-content" hidden={!recommendationsVisible} className={styles.recommendationContent}>
            <Flex vertical gap={12}>
              <Select aria-label="选择推荐岗位" showSearch optionFilterProp="label" placeholder="选择开启的岗位" value={positionUnavailable ? undefined : positionID} disabled={readOnly || recommendationBusy} onChange={selectRecommendationPosition} options={currentOptions.map((item) => ({ value: item.id, label: item.name }))} />
              {positionUnavailable ? <Typography.Text type="secondary" className={styles.meta}>这个岗位当前不可推荐，历史记录还在。</Typography.Text> : null}
              <Radio.Group aria-label="推荐阶段" value={stage} disabled={readOnly || recommendationBusy || positionUnavailable} className={styles.recommendationStages} onChange={(event) => { if (readOnly || recommendationSaving.current || positionUnavailable) return; setStage(event.target.value); recommendationKey.current = crypto.randomUUID(); }} options={recommendationStages} />
              <Input.TextArea aria-label="新增岗位跟进备注" value={recommendationNote} disabled={readOnly || recommendationBusy || positionUnavailable} maxLength={1000} autoSize={{ minRows: 3, maxRows: 6 }} onChange={(event) => { if (readOnly || recommendationSaving.current || positionUnavailable) return; setRecommendationNote(event.target.value); recommendationKey.current = crypto.randomUUID(); }} placeholder="只记这个岗位的推荐、面试和跟进情况。" />
              <Button block type="primary" loading={recommendationBusy} disabled={readOnly || positionUnavailable || !recommendationChanged} onClick={() => void saveRecommendation()}>{stage === "draft" ? "保存关联" : stage === "recommended" ? "记录推荐" : "记录跟进"}</Button>
            </Flex>
          </div>
        </section>
        <section ref={notesSection} className={detailStyles.sideCard} data-testid="resume-notes-section" aria-label="备注">
          <Typography.Title level={5} className={detailStyles.cardTitle}>备注{notes.length ? " · " + notes.length : ""}</Typography.Title>
          <Flex vertical gap={12}>
            <Input.TextArea aria-label="新增候选人备注" value={note} onChange={(event) => { setNote(event.target.value); noteKey.current = crypto.randomUUID(); }} maxLength={1000} showCount disabled={readOnly || noteBusy} autoSize={{ minRows: 3, maxRows: 5 }} placeholder={readOnly ? "内置示例备注仅供阅读" : "比如：意向不错，周五再约一次。"} />
            <Flex justify="flex-end"><Button type="primary" icon={<PlusOutlined aria-hidden="true" />} loading={noteBusy} disabled={readOnly || !note.trim()} onClick={() => void saveNote()}>加备注</Button></Flex>
            <div className={styles.recordHistory} data-testid="resume-notes-history" role="region" aria-label="历史备注" tabIndex={0}>{notes.length ? notes.map((item) => <div key={item.id} className={styles.record}><Typography.Paragraph className={styles.preWrap}>{item.content}</Typography.Paragraph><Typography.Text type="secondary" className={styles.meta} title={item.created_at ? "保存于 " + recordTime(item.created_at) : undefined}>{item.author_name || item.author_email || "未记录操作人"} · {(item.occurred_at || item.created_at) ? recordTime(item.occurred_at || item.created_at) : ""}</Typography.Text></div>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这里还没备注，先记一条也行。" />}</div>
          </Flex>
        </section>
      </aside>
    </div>
    <ResumeEditor open={editing} candidate={candidate} positions={positions} onClose={() => setEditing(false)} onFoldersChanged={onChanged} onSaved={(saved) => { setEditing(false); setCandidate(saved); void reload(); onChanged?.(); }} />
    <ResumeSummaryDialog open={summaryOpen} candidate={candidate} onClose={() => setSummaryOpen(false)} onSaved={(saved) => { setSummaryOpen(false); setCandidate(saved); void reload(); onChanged?.(); }} />
    {!readOnly ? <ResumeShareDialog key={`${accountIdentity}:${candidateID}`} open={shareOpen} candidateID={candidateID} onClose={() => setShareOpen(false)} /> : null}
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
  const events = [...(item.events || [])].sort((left, right) => (Date.parse(right.occurred_at || right.created_at || "") || 0) - (Date.parse(left.occurred_at || left.created_at || "") || 0));
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
