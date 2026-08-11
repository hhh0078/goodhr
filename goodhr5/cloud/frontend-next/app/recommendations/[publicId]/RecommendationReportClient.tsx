/** 本文件负责渲染公开候选人推荐报告，并提供复制、打印和按需查看沟通记录能力。 */
"use client";

import { useState } from "react";
import type { CandidateRecommendation, RecommendationConversation, RecommendationExperience, RecommendationPoint } from "@/lib/recommendation";
import { normalizeRecommendationConversations, recommendationCopyText } from "@/lib/recommendation";
import dialogStyles from "./recommendation-dialog.module.css";
import styles from "./recommendation.module.css";

type RecommendationReportClientProps = {
  recommendation: CandidateRecommendation;
  apiBaseURL: string;
};

/** RecommendationReportClient 展示面试官需要的完整推荐快照。 */
export default function RecommendationReportClient({ recommendation, apiBaseURL }: RecommendationReportClientProps) {
  const [notice, setNotice] = useState("");
  const [conversationOpen, setConversationOpen] = useState(false);
  const [conversationLoading, setConversationLoading] = useState(false);
  const [conversationLoaded, setConversationLoaded] = useState(false);
  const [conversationError, setConversationError] = useState("");
  const [conversations, setConversations] = useState<RecommendationConversation[]>([]);
  const { candidate, position } = recommendation.report;

  /** copyDetails 把候选人基础信息和当前公共链接复制到剪贴板。 */
  async function copyDetails() {
    try {
      await navigator.clipboard.writeText(recommendationCopyText(recommendation, window.location.href));
      setNotice("详情和链接已经复制，可以直接发给面试官。");
    } catch {
      setNotice("复制没有成功，你可以手动复制当前页面地址。");
    }
  }

  /** openConversations 打开沟通记录，并只在首次查看时请求云端。 */
  async function openConversations() {
    setConversationOpen(true);
    if (conversationLoaded || conversationLoading) return;
    setConversationLoading(true);
    setConversationError("");
    try {
      const response = await fetch(`${apiBaseURL}/api/public/recommendations/${encodeURIComponent(recommendation.publicID)}/conversations`, { cache: "no-store" });
      if (!response.ok) throw new Error("沟通记录暂时没读出来");
      const payload = await response.json() as { conversations?: unknown };
      setConversations(normalizeRecommendationConversations(payload.conversations));
      setConversationLoaded(true);
    } catch (error) {
      setConversationError(error instanceof Error ? error.message : "沟通记录暂时没读出来");
    } finally {
      setConversationLoading(false);
    }
  }

  return <main className={styles.pageShell}>
    <nav className={styles.toolbar} aria-label="推荐报告操作">
      <div><strong>GoodHR 候选人推荐</strong><span>{notice}</span></div>
      <div className={styles.toolbarActions}>
        <button type="button" onClick={copyDetails}>复制详情</button>
        <button type="button" onClick={openConversations}>沟通记录</button>
        <button type="button" className={styles.primaryButton} onClick={() => window.print()}>打印 / 导出 PDF</button>
      </div>
    </nav>

    <article className={styles.paper}>
      <header className={styles.hero}>
        <div className={styles.identity}>
          {candidate.avatarURL ? <img src={candidate.avatarURL} alt={`${candidate.name || "候选人"}头像`} /> : <div className={styles.avatarFallback}>{(candidate.name || "候").slice(0, 1)}</div>}
          <div><p className={styles.eyebrow}>候选人推荐报告</p><h1>{candidate.name || "未命名候选人"}</h1><p>{candidateFacts(recommendation)}</p></div>
        </div>
        <div className={styles.scoreCard}><strong>{Math.round(recommendation.matchScore)}</strong><span>岗位匹配分</span><em>{recommendation.recommendationLevel || "建议面试核验"}</em></div>
      </header>

      <section className={styles.positionBand}>
        <div><span>推荐岗位</span><strong>{position.name || "岗位暂未填写"}</strong></div>
        <div><span>公司</span><strong>{position.companyName || "公司暂未填写"}</strong></div>
        <div><span>报告生成</span><strong>{formatDate(recommendation.generatedAt)}</strong></div>
      </section>

      <ReportSection title="推荐结论" lead>
        <p className={styles.summary}>{recommendation.report.executiveSummary || recommendation.summary || "暂时没有推荐摘要。"}</p>
        <div className={styles.summaryGrid}>
          <SummaryItem label="求职意向" value={recommendation.report.jobIntentSummary} />
          <SummaryItem label="稳定性判断" value={recommendation.report.stabilitySummary} />
          <SummaryItem label="沟通表现" value={recommendation.report.communicationSummary} />
        </div>
      </ReportSection>

      <ReportSection title="岗位与公司信息">
        <div className={styles.factGrid}>
          <Fact label="岗位名称" value={position.name} />
          <Fact label="招聘平台" value={platformText(position.platformID)} />
          <Fact label="公司名称" value={position.companyName} />
          <Fact label="工作地址" value={position.address} />
          <Fact label="联系方式" value={position.contact} />
          <Fact label="岗位编号" value={position.id} />
        </div>
        {position.description ? <ResumeBlock title="岗位说明"><p className={styles.preLine}>{position.description}</p></ResumeBlock> : null}
        {position.overview ? <ResumeBlock title="公司概况"><p className={styles.preLine}>{position.overview}</p></ResumeBlock> : null}
        {position.extraInfo ? <ResumeBlock title="其他公司信息"><p className={styles.preLine}>{position.extraInfo}</p></ResumeBlock> : null}
      </ReportSection>

      <ReportSection title="联系方式">
        <div className={styles.factGrid}>
          <Fact label="手机" value={candidate.phone} />
          <Fact label="邮箱" value={candidate.email} />
          <Fact label="微信" value={candidate.wechat} />
          <Fact label="当前地区" value={candidate.workRegion} />
          <Fact label="期望岗位" value={candidate.expectedPosition} />
          <Fact label="期望薪资" value={salaryText(candidate.expectedSalaryMin, candidate.expectedSalaryMax)} />
        </div>
      </ReportSection>

      <ReportSection title="岗位条件确认">
        <div className={styles.conditionList}>{recommendation.report.conditions.length ? recommendation.report.conditions.map((item) => <div key={item.id || item.content} className={styles.conditionItem}><span className={styles[item.status === "matched" ? "matched" : item.status === "unmatched" ? "unmatched" : "pending"]}>{conditionStatus(item.status)}</span><div><strong>{item.content}</strong><p>{item.statusReason || item.evidence.join("；") || "暂时没有补充依据"}</p></div></div>) : <EmptyText text="这份报告没有岗位条件记录。" />}</div>
      </ReportSection>

      <div className={styles.twoColumns}>
        <ReportSection title="主要优势"><PointList items={recommendation.report.strengths} empty="暂时没有单独归纳优势。" /></ReportSection>
        <ReportSection title="风险与不足"><PointList items={recommendation.report.risks} empty="暂时没有发现明确风险。" /></ReportSection>
      </div>

      {recommendation.report.bonusItems.length || recommendation.report.unconfirmedItems.length ? <div className={styles.twoColumns}>
        <ReportSection title="加分项"><PointList items={recommendation.report.bonusItems} empty="暂无加分项。" /></ReportSection>
        <ReportSection title="还需确认"><PointList items={recommendation.report.unconfirmedItems} empty="暂无未确认事项。" /></ReportSection>
      </div> : null}

      <ReportSection title="完整简历">
        <div className={styles.factGrid}>
          <Fact label="性别" value={candidate.gender} /><Fact label="出生年月" value={candidate.birthYm} />
          <Fact label="最高学历" value={candidate.educationLevel} /><Fact label="工作年限" value={candidate.workYears} />
          <Fact label="求职状态" value={candidate.workStatus} /><Fact label="在线状态" value={candidate.onlineStatus} />
        </div>
        {candidate.basicInfo ? <ResumeBlock title="基础信息"><p className={styles.preLine}>{candidate.basicInfo}</p></ResumeBlock> : null}
        {candidate.personalDescription ? <ResumeBlock title="个人简介"><p className={styles.preLine}>{candidate.personalDescription}</p></ResumeBlock> : null}
        <ResumeTimeline title="工作经历" items={candidate.workExperiences} />
        <ResumeTimeline title="项目经历" items={candidate.projectExperiences} />
        <ResumeTimeline title="教育经历" items={candidate.educations} />
        {candidate.certificates.length ? <ResumeBlock title="证书"><ul className={styles.simpleList}>{candidate.certificates.map((item, index) => <li key={`${item.certificateName}-${index}`}><strong>{item.certificateName || "未命名证书"}</strong>{[item.issuedBy, item.issuedYm].filter(Boolean).join(" · ") ? <span>{[item.issuedBy, item.issuedYm].filter(Boolean).join(" · ")}</span> : null}</li>)}</ul></ResumeBlock> : null}
        {candidate.honors.length ? <ResumeBlock title="荣誉"><ul className={styles.simpleList}>{candidate.honors.map((item, index) => <li key={`${item.honorName}-${index}`}><strong>{item.honorName || "未命名荣誉"}</strong><span>{[item.issuedBy, item.issuedYm, item.description].filter(Boolean).join(" · ")}</span></li>)}</ul></ResumeBlock> : null}
      </ReportSection>

      <div className={styles.twoColumns}>
        <ReportSection title="面试关注点"><PointList items={recommendation.report.interviewFocus} empty="暂无额外面试关注点。" /></ReportSection>
        <ReportSection title="建议提问"><ol className={styles.questionList}>{recommendation.report.suggestedQuestions.length ? recommendation.report.suggestedQuestions.map((item) => <li key={item}>{item}</li>) : <li>可以围绕岗位条件和实际经历继续核验。</li>}</ol></ReportSection>
      </div>

      <footer className={styles.reportFooter}>本报告依据截至 {formatDate(recommendation.sourceCutoffAt)} 的简历、岗位条件和真实沟通记录生成。请在面试中复核关键事实。</footer>
    </article>

    {conversationOpen ? <ConversationDialog conversations={conversations} loading={conversationLoading} error={conversationError} onRetry={openConversations} onClose={() => setConversationOpen(false)} /> : null}
  </main>;
}

/** ReportSection 渲染报告中的统一章节。 */
function ReportSection({ title, children, lead = false }: { title: string; children: React.ReactNode; lead?: boolean }) {
  return <section className={`${styles.section} ${lead ? styles.leadSection : ""}`}><h2>{title}</h2>{children}</section>;
}

/** SummaryItem 展示推荐结论中的一项短摘要。 */
function SummaryItem({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><p>{value || "暂时没有明确结论"}</p></div>;
}

/** Fact 展示简历中的一个标签和值。 */
function Fact({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><strong>{value || "未填写"}</strong></div>;
}

/** PointList 展示优势、风险或面试关注点。 */
function PointList({ items, empty }: { items: RecommendationPoint[]; empty: string }) {
  if (!items.length) return <EmptyText text={empty} />;
  return <div className={styles.pointList}>{items.map((item, index) => <div key={`${item.title}-${index}`}><strong>{item.title || "需要关注"}</strong><p>{item.detail}</p>{item.evidence.length ? <small>依据：{item.evidence.join("；")}</small> : null}</div>)}</div>;
}

/** EmptyText 展示没有结构化内容时的简短说明。 */
function EmptyText({ text }: { text: string }) {
  return <p className={styles.emptyText}>{text}</p>;
}

/** ResumeBlock 为完整简历中的子模块提供统一标题。 */
function ResumeBlock({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className={styles.resumeBlock}><h3>{title}</h3>{children}</div>;
}

/** ResumeTimeline 展示工作、项目和教育经历。 */
function ResumeTimeline({ title, items }: { title: string; items: RecommendationExperience[] }) {
  if (!items.length) return null;
  return <ResumeBlock title={title}><div className={styles.timeline}>{items.map((item, index) => {
    const name = item.companyName || item.projectName || item.schoolName || "名称暂未填写";
    const role = item.positionName || item.roleName || [item.majorName, item.educationLevel].filter(Boolean).join(" · ");
    return <div key={`${name}-${index}`}><header><strong>{name}</strong><span>{periodText(item)}</span></header>{role ? <b>{role}</b> : null}{item.content ? <p className={styles.preLine}>{item.content}</p> : null}</div>;
  })}</div></ResumeBlock>;
}

/** ConversationDialog 按会话展示候选人与 HR 的真实沟通记录。 */
function ConversationDialog({ conversations, loading, error, onRetry, onClose }: { conversations: RecommendationConversation[]; loading: boolean; error: string; onRetry: () => void; onClose: () => void }) {
  return <div className={dialogStyles.modalBackdrop} role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section className={dialogStyles.modal} role="dialog" aria-modal="true" aria-labelledby="conversation-title">
      <header><div><p>按需加载</p><h2 id="conversation-title">候选人沟通记录</h2></div><button type="button" aria-label="关闭沟通记录" onClick={onClose}>×</button></header>
      <div className={dialogStyles.modalBody}>
        {loading ? <EmptyText text="正在读取沟通记录，我尽量快一点。" /> : null}
        {!loading && error ? <div className={dialogStyles.loadError}><p>{error}</p><button type="button" onClick={onRetry}>重新读取</button></div> : null}
        {!loading && !error && !conversations.length ? <EmptyText text="这份报告暂时没有可公开的沟通记录。" /> : null}
        {!loading && !error ? conversations.map((conversation) => <details key={conversation.id} className={dialogStyles.conversation} open={conversations.length === 1}><summary>{platformText(conversation.platformID)} · {conversation.positionText || "岗位未识别"}<span>{conversation.messages.length} 条</span></summary><div>{conversation.messages.map((message) => <div key={message.id} className={message.direction === "self" ? dialogStyles.hrMessage : dialogStyles.candidateMessage}><strong>{message.direction === "self" ? "HR" : "候选人"}</strong><p>{message.textContent || `[${message.messageType || "非文本消息"}]`}</p><time>{formatDate(message.platformSentAt || message.createdAt)}</time></div>)}</div></details>) : null}
      </div>
    </section>
  </div>;
}

/** candidateFacts 返回候选人标题下方的紧凑基础信息。 */
function candidateFacts(item: CandidateRecommendation) {
  const candidate = item.report.candidate;
  return [candidate.gender, ageText(candidate.birthYm), candidate.educationLevel, candidate.workYears, candidate.workRegion].filter(Boolean).join(" · ") || "基础信息暂未填写";
}

/** ageText 根据出生年月给出当前粗略年龄。 */
function ageText(birthYM: string) {
  const year = Number(birthYM.slice(0, 4));
  return Number.isFinite(year) && year > 1900 ? `${new Date().getFullYear() - year}岁` : "";
}

/** salaryText 把薪资上下限整理为易读文字。 */
function salaryText(minimum: number | null, maximum: number | null) {
  if (minimum != null && maximum != null) return `${minimum}-${maximum}K`;
  if (minimum != null) return `${minimum}K 起`;
  if (maximum != null) return `${maximum}K 以内`;
  return "";
}

/** periodText 返回经历的起止年月。 */
function periodText(item: RecommendationExperience) {
  if (!item.startYm && !item.endYm) return "";
  return `${item.startYm || ""} - ${item.endYm || "至今"}`;
}

/** conditionStatus 返回三种条件状态的中文文案。 */
function conditionStatus(status: string) {
  return ({ matched: "已满足", unmatched: "未满足", pending: "未确认" } as Record<string, string>)[status] || "未确认";
}

/** platformText 返回招聘平台中文名称。 */
function platformText(value: string) {
  return ({ liepin: "猎聘企业", hliepin: "猎聘猎头", zhaopin: "智联招聘", boss: "BOSS直聘" } as Record<string, string>)[value] || value || "招聘平台";
}

/** formatDate 把接口时间整理为中国时区的简短时间。 */
function formatDate(value: string) {
  const date = new Date(value);
  return value && Number.isFinite(date.getTime()) ? date.toLocaleString("zh-CN", { hour12: false }) : "时间暂未记录";
}
