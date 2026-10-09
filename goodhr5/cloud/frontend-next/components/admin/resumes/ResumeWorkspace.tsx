/** 文件作用说明：共用简历详情工作区，展示结构化简历、通用备注、人工推荐及自动任务记录。 */
"use client";
import { Alert, Avatar, Button, Collapse, Descriptions, Empty, Flex, Input, Select, Skeleton, Space, Tabs, Tag, Timeline, Typography } from "antd";
import { EditOutlined, PlusOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import { useAdmin } from "../AdminApp";
import { addRecommendation, addResumeNote, loadRecommendations, loadResume, loadResumeNotes } from "@/lib/resume-api";
import { formatDate } from "@/lib/admin-api";
import { experienceLabel, recommendationStages, resumeAge, salaryLabel, sourceLabel, stageLabel, type PositionOption, type Recommendation, type Resume, type ResumeExperience, type ResumeNote } from "@/lib/resume-types";
import ResumeEditor from "./ResumeEditor";
import styles from "./resumes.module.css";

/** ResumeWorkspace 在详情地址与抽屉间复用同一候选人记录和维护入口。 */
export default function ResumeWorkspace({ candidateID, engagementID = "", positions, initialTab = "notes", onChanged }: {
  candidateID: string; engagementID?: string; positions: PositionOption[]; initialTab?: string; onChanged?: () => void;
}) {
  const { notify } = useAdmin();
  const [candidate, setCandidate] = useState<Resume | null>(null);
  const [notes, setNotes] = useState<ResumeNote[]>([]);
  const [recommendations, setRecommendations] = useState<Recommendation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState(false);
  const [activeTab, setActiveTab] = useState(initialTab);
  const [note, setNote] = useState("");
  const [noteBusy, setNoteBusy] = useState(false);
  const [positionID, setPositionID] = useState<string>();
  const [stage, setStage] = useState("draft");
  const [recommendationNote, setRecommendationNote] = useState("");
  const [recommendationBusy, setRecommendationBusy] = useState(false);
  const noteKey = useRef("");
  const recommendationKey = useRef("");
  const generation = useRef(0);
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
    setCandidate(null); setNotes([]); setRecommendations([]); setNote(""); setActiveTab(initialTab);
    setPositionID(undefined); setStage("draft"); setRecommendationNote("");
    noteKey.current = crypto.randomUUID(); recommendationKey.current = crypto.randomUUID();
    void reload();
    return () => { generation.current += 1; };
  }, [candidateID, engagementID]);
  useEffect(() => { setActiveTab(initialTab); }, [initialTab]);
  /** saveNote 只新增通用备注，失败时保留正文供重试。 */
  async function saveNote() {
    if (!note.trim()) return;
    setNoteBusy(true);
    try {
      const saved = await addResumeNote(candidateID, note.trim(), noteKey.current);
      setNotes((current) => [saved, ...current.filter((item) => item.id !== saved.id)]);
      setNote(""); noteKey.current = crypto.randomUUID(); notify("备注记好了，打工小本本更新了。", "success"); onChanged?.();
    } catch (cause) { notify(cause instanceof Error ? cause.message : "备注没记上，再试一次就好。", "error"); }
    finally { setNoteBusy(false); }
  }
  /** saveRecommendation 明确提交当前阶段和该岗位备注，不影响自动任务状态。 */
  async function saveRecommendation() {
    if (!positionID) return;
    setRecommendationBusy(true);
    try {
      await addRecommendation(candidateID, positionID, stage, recommendationNote.trim(), recommendationKey.current);
      setRecommendations(await loadRecommendations(candidateID));
      setRecommendationNote(""); recommendationKey.current = crypto.randomUUID();
      notify(stage === "draft" ? "岗位已关联，等你明确推荐时再记推荐动作。" : "推荐和跟进记录已记好。", "success"); onChanged?.();
    } catch (cause) { notify(cause instanceof Error ? cause.message : "推荐没记上，请重试。", "error"); }
    finally { setRecommendationBusy(false); }
  }
  /** editRecommendation 将已有关系带入表单，推进前仍需明确点击保存。 */
  function editRecommendation(item: Recommendation) {
    setPositionID(item.position_id); setStage(item.stage); setRecommendationNote(""); recommendationKey.current = crypto.randomUUID();
  }
  if (loading && !candidate) return <Skeleton active paragraph={{ rows: 8 }} />;
  if (!candidate) return <Alert type="warning" showIcon title={error || "这份简历暂时没读出来。"} action={<Button onClick={() => void reload()}>重试</Button>} />;
  const facts = [resumeAge(candidate), candidate.gender, candidate.education_level, candidate.work_years].filter(Boolean);
  const autoEvents = candidate.events || [];
  const currentOptions = [...positions];
  for (const item of recommendations) if (!currentOptions.some((option) => option.id === item.position_id)) currentOptions.push({ id: item.position_id, name: `${item.position_name || "已删除岗位"}（历史记录）` });
  return <>
    {error ? <Alert type="warning" title={error} action={<Button size="small" onClick={() => void reload()}>重试读取</Button>} style={{ marginBottom: 12 }} /> : null}
    <div className={styles.workspace} data-testid="resume-detail-layout">
      <div className={styles.resumeBody} data-testid="resume-detail-main">
        <Flex gap={12} align="start" className={styles.candidateHeader}>
          <Avatar size={52}>{candidate.candidate_name?.slice(0, 1) || "人"}</Avatar>
          <Flex vertical gap={4} style={{ minWidth: 0, flex: 1 }}>
            <Typography.Title level={3} style={{ margin: 0 }}>{candidate.candidate_name || "未命名候选人"}</Typography.Title>
            <Typography.Text type="secondary">{facts.join(" · ") || "基础信息待补充"}</Typography.Text>
            <Typography.Text copyable={candidate.phone ? { text: candidate.phone } : false}>{candidate.phone || "手机号待补充"}{candidate.email ? ` · ${candidate.email}` : ""}</Typography.Text>
          </Flex>
          <Button icon={<EditOutlined />} onClick={() => setEditing(true)}>编辑</Button>
        </Flex>
        <Space wrap style={{ marginTop: 10 }}>{(candidate.tags || []).map((item) => <Tag key={item}>{item}</Tag>)}</Space>
        <section className={styles.resumeSection}><Typography.Title level={5}>求职意向</Typography.Title><Descriptions size="small" column={{ xs: 1, sm: 2 }} items={[
          { key: "current", label: "当前地区", children: candidate.current_region || "未填写" },
          { key: "expected", label: "期望地区", children: candidate.expected_region || candidate.work_region || "未填写" },
          { key: "position", label: "期望职位", children: candidate.expected_position || "未填写" },
          { key: "salary", label: "期望月薪", children: salaryLabel(candidate) },
          { key: "status", label: "求职状态", children: candidate.work_status || "未填写" },
        ]} /></section>
        {(candidate.personal_description || candidate.basic_info) ? <TextSection title="个人优势" text={candidate.personal_description || candidate.basic_info} /> : null}
        <ExperienceSection title="工作经历" items={candidate.work_experiences || []} />
        <ExperienceSection title="项目经历" items={candidate.project_experiences || []} />
        <ExperienceSection title="教育经历" items={candidate.educations || []} />
        {candidate.certificates?.length ? <TextSection title="证书" text={candidate.certificates.map((item) => [item.certificate_name, item.issued_by, item.issued_ym].filter(Boolean).join(" · ")).join("\n")} /> : null}
        {candidate.honors?.length ? <TextSection title="荣誉" text={candidate.honors.map((item) => [item.honor_name, item.issued_by, item.issued_ym, item.description].filter(Boolean).join(" · ")).join("\n")} /> : null}
        {candidate.ai_summary ? <TextSection title="AI 简历摘要" text={candidate.ai_summary} /> : null}
        {candidate.raw_text ? <Collapse items={[{ key: "raw", label: "查看完整简历正文", children: <Typography.Paragraph className={styles.preWrap}>{candidate.raw_text}</Typography.Paragraph> }]} style={{ marginTop: 16 }} /> : null}
        <section className={styles.resumeSection}><Typography.Text type="secondary">{sourceLabel(candidate.source_type)} · 入库人：{candidate.creator_name || candidate.user_email || "暂时没记上"} · {candidate.created_at ? formatDate(candidate.created_at) : "入库时间待补充"}</Typography.Text>
          {(candidate.sources || []).map((source, index) => <div key={`${source.platform_id}-${index}`}><Typography.Text type="secondary">{sourceLabel(source.platform_id || source.source_type)}{source.source_url ? " · " : ""}</Typography.Text>{source.source_url && /^https?:\/\//.test(source.source_url) ? <a href={source.source_url} target="_blank" rel="noopener noreferrer">查看原页面</a> : null}</div>)}
        </section>
      </div>
      <aside className={styles.recordAside} data-testid="resume-detail-aside"><Tabs activeKey={activeTab} onChange={setActiveTab} items={[
        { key: "notes", label: `备注 ${notes.length || ""}`, children: <Flex vertical gap={12}>
          <Input.TextArea value={note} onChange={(event) => { setNote(event.target.value); noteKey.current = crypto.randomUUID(); }} maxLength={1000} showCount disabled={noteBusy} autoSize={{ minRows: 3, maxRows: 8 }} placeholder="比如：意向不错，周五再约一次。" />
          <Button type="primary" icon={<PlusOutlined aria-hidden="true" />} loading={noteBusy} disabled={!note.trim()} onClick={() => void saveNote()}>加备注</Button>
          <div className={styles.recordHistory}>{notes.length ? notes.map((item) => <div key={item.id} className={styles.record}><Typography.Paragraph className={styles.preWrap}>{item.content}</Typography.Paragraph><Typography.Text type="secondary" className={styles.meta}>{item.author_name || item.author_email || "未记录操作人"} · {item.created_at ? formatDate(item.created_at) : ""}</Typography.Text></div>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这里还没备注，先记一条也行。" />}</div>
        </Flex> },
        { key: "recommendations", label: `推荐 ${recommendations.length || ""}`, children: <Flex vertical gap={12}>
          <Select aria-label="选择推荐岗位" showSearch optionFilterProp="label" placeholder="选择岗位" value={positionID} disabled={recommendationBusy} onChange={(value) => { setPositionID(value); const item = recommendations.find((current) => current.position_id === value); setStage(item?.stage || "draft"); recommendationKey.current = crypto.randomUUID(); }} options={currentOptions.map((item) => ({ value: item.id, label: item.name }))} />
          <Select aria-label="推荐阶段" value={stage} disabled={recommendationBusy} onChange={(value) => { setStage(value); recommendationKey.current = crypto.randomUUID(); }} options={recommendationStages} />
          <Input.TextArea value={recommendationNote} disabled={recommendationBusy} maxLength={1000} autoSize={{ minRows: 2, maxRows: 5 }} onChange={(event) => { setRecommendationNote(event.target.value); recommendationKey.current = crypto.randomUUID(); }} placeholder="只记这个岗位的推荐、面试和跟进情况。" />
          <Button type="primary" loading={recommendationBusy} disabled={!positionID} onClick={() => void saveRecommendation()}>{stage === "draft" ? "保存关联" : stage === "recommended" ? "记录推荐" : "记录跟进"}</Button>
          <Typography.Text type="secondary" className={styles.meta}>这里只记人工推荐，不会启动岗位或发送消息。</Typography.Text>
          <div className={styles.recordHistory}>{recommendations.length ? recommendations.map((item) => <div key={item.id} className={styles.record}>
            <Flex gap={8} justify="space-between" align="start"><Typography.Text strong>{item.position_name || "已删除岗位"}</Typography.Text><Tag>{stageLabel(item.stage)}</Tag></Flex>
            <Button type="link" size="small" onClick={() => editRecommendation(item)}>继续跟进</Button>
            <Collapse size="small" items={[{ key: item.id, label: `推进记录 · ${item.events?.length || 0} 条`, children: <Timeline items={(item.events || []).map((event) => ({ children: <><Typography.Text>{stageLabel(event.from_stage)} → {stageLabel(event.to_stage)}</Typography.Text>{event.content ? <Typography.Paragraph className={styles.preWrap}>{event.content}</Typography.Paragraph> : null}<div className={styles.meta}>{event.author_name || event.author_email} · {event.created_at ? formatDate(event.created_at) : ""}</div></> }))} /> }]} />
          </div>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没关联岗位，选一个就能开始记记录。" />}</div>
        </Flex> },
        { key: "automation", label: "自动任务", children: <Flex vertical gap={12}>
          {candidate.engagement_id ? <><Typography.Text>岗位：{candidate.position_name || "未记录岗位"}</Typography.Text><Typography.Text type="secondary">任务状态：{candidate.engagement_status || "未记录"}</Typography.Text></> : null}
          <AIResult title="详情分析" value={candidate.ai?.detail} /><AIResult title="招呼分析" value={candidate.ai?.greet} />
          {autoEvents.length ? <Timeline items={autoEvents.filter((event) => event.event_type !== "manual_note").map((event) => ({ children: <><Typography.Text>{String(event.message_text || event.event_type || "任务记录")}</Typography.Text><div className={styles.meta}>{event.created_at ? formatDate(String(event.created_at)) : ""}</div></> }))} /> : <Typography.Text type="secondary">暂时没有更多自动任务记录。</Typography.Text>}
        </Flex> },
      ]} /></aside>
    </div>
    <ResumeEditor open={editing} candidate={candidate} positions={positions} onClose={() => setEditing(false)} onSaved={(saved) => { setEditing(false); setCandidate(saved); void reload(); onChanged?.(); }} />
  </>;
}
/** AIResult 显示任务分析，缺失分数保留“未分析”而非零分。 */
function AIResult({ title, value }: { title: string; value?: { score?: number | null; reason?: string } }) {
  return <div className={styles.record}><Typography.Text strong>{title} · {value?.score == null ? "未分析" : `${value.score}分`}</Typography.Text>{value?.reason ? <Typography.Paragraph className={styles.preWrap}>{value.reason}</Typography.Paragraph> : null}</div>;
}
/** TextSection 展示完整简历段落并保留原文换行。 */
function TextSection({ title, text }: { title: string; text?: string }) {
  return <section className={styles.resumeSection}><Typography.Title level={5}>{title}</Typography.Title><Typography.Paragraph className={styles.preWrap}>{text}</Typography.Paragraph></section>;
}
/** ExperienceSection 展示结构化经历，保留全部条目和说明。 */
function ExperienceSection({ title, items }: { title: string; items: ResumeExperience[] }) {
  if (!items.length) return null;
  return <section className={styles.resumeSection}><Typography.Title level={5}>{title}</Typography.Title>{items.map((item, index) => <div key={index} className={styles.experience}><Typography.Text strong>{experienceLabel(item) || "未填写名称"}</Typography.Text>{item.content ? <Typography.Paragraph className={styles.preWrap}>{item.content}</Typography.Paragraph> : null}</div>)}</section>;
}
