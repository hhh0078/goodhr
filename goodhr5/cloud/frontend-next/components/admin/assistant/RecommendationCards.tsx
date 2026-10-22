/** 文件作用说明：把服务端校验后的候选人推荐展示为证据卡，明确优势、缺口、未知项与对应原简历。 */
"use client";
import { Alert, Button, Collapse, Flex, Tag, Typography } from "antd";
import { isAssistantRecord } from "@/lib/assistant-protocol";
import JsonTree from "../JsonTree";

/** factList 只展示模型合同中的可读事实，未知对象不转成误导性的字符串。 */
function factList(value: unknown): string[] {
  return Array.isArray(value) ? value.flatMap((item) => typeof item === "string" ? [item] : isAssistantRecord(item) && typeof item.content === "string" ? [item.content] : []) : [];
}

/** Facts 仅在存在内容时显示同类推荐依据，不补写模型未给出的结论。 */
function Facts({ title, value }: { title: string; value: unknown }) {
  const facts = factList(value);
  return facts.length ? <div><Typography.Text strong>{title}</Typography.Text><ul style={{ margin: "4px 0 8px", paddingLeft: 20 }}>{facts.map((text, index) => <li key={index}>{text}</li>)}</ul></div> : null;
}

/** candidateRows 仅接受带候选人编号的结构化条目，不把无归属数据做成推荐。 */
function candidateRows(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.filter((item): item is Record<string, unknown> => isAssistantRecord(item) && typeof item.candidate_id === "string" && Boolean(item.candidate_id)) : [];
}

/** CandidateEvidence 优先展示可读的原文引用和来源路径，完整逐项数据仍可继续展开。 */
function CandidateEvidence({ item }: { item: Record<string, unknown> }) {
  const refs = Array.isArray(item.evidence_refs) ? item.evidence_refs.filter((value) => isAssistantRecord(value) && (typeof value.quote === "string" || typeof value.path === "string" || typeof value.field_path === "string")) : [];
  return <div style={{ maxHeight: 360, overflow: "auto" }}>
    {refs.length ? <Flex vertical gap={10}>{refs.map((value, index) => {
      const evidence = value as Record<string, unknown>;
      const path = typeof evidence.path === "string" ? evidence.path : typeof evidence.field_path === "string" ? evidence.field_path : "";
      return <div key={`${String(evidence.chunk_id || path)}:${index}`} style={{ paddingLeft: 10, borderLeft: "2px solid var(--ant-color-border)" }}>
        {typeof evidence.quote === "string" && evidence.quote ? <Typography.Paragraph style={{ margin: "0 0 4px", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{evidence.quote}</Typography.Paragraph> : null}
        {path ? <Typography.Text type="secondary" style={{ fontSize: 12, overflowWrap: "anywhere" }}>来源：{path}</Typography.Text> : null}
      </div>;
    })}</Flex> : <Typography.Paragraph type="secondary">这位人选暂无可展示的原文引用，需继续核实。</Typography.Paragraph>}
    <Collapse size="small" ghost style={{ marginTop: 8 }} items={[{ key: "checks", label: "查看逐项结构化详情", children: <JsonTree value={{ resume_version: item.resume_version, requirements_version: item.requirements_version, requirement_checks: item.requirement_checks || item.checks || [], evidence_refs: item.evidence_refs || [] }} /> }]} />
  </div>;
}

/** CandidateCards 独立展示一组人选；待确认组始终明确标注，不能冒充必要条件已满足。 */
function CandidateCards({ candidates, pending = false }: { candidates: Record<string, unknown>[]; pending?: boolean }) {
  return <Flex vertical gap={12}>{candidates.map((item, index) => {
    const status = String(item.recommendation_status || "");
    const label = pending ? "条件待确认" : ({ priority: "建议优先沟通", recommended: "建议优先沟通", conditional: "条件待确认", not_recommended: "暂不推荐" } as Record<string, string>)[status] || status;
    return <div key={`${item.candidate_id}:${index}`} style={{ padding: 12, border: "1px solid var(--ant-color-border-secondary)", borderRadius: 8 }}>
      <Flex gap={8} align="center" justify="space-between" wrap><Typography.Text strong>{String(item.candidate_name || item.name || `候选人 ${index + 1}`)}</Typography.Text>{label ? <Tag color={pending ? "warning" : undefined}>{label}</Tag> : null}<Button type="link" href={`/admin/resumes/detail?candidate_id=${encodeURIComponent(String(item.candidate_id))}`} target="_blank" rel="noopener noreferrer">查看原简历</Button></Flex>
      <Facts title="相关经历与优势" value={item.strengths} /><Facts title="明确缺口" value={item.gaps} /><Facts title="待确认" value={item.questions_to_verify} />
      <Collapse size="small" items={[{ key: "evidence", label: "查看逐项核对和原文证据", children: <CandidateEvidence item={item} /> }]} />
    </div>;
  })}</Flex>;
}

/** RecommendationCards 区分已核实推荐、待确认人选和部分分析失败，即使无正文或推荐也明确展示状态。 */
export default function RecommendationCards({ result }: { result: unknown }) {
  if (!isAssistantRecord(result)) return null;
  const recommendations = candidateRows(result.recommendations);
  const pending = candidateRows(result.pending_verification);
  const partial = result.partial_failure === true;
  if (!recommendations.length && !pending.length && !partial && !result.message) return null;
  return <Flex vertical gap={12} style={{ marginTop: 12 }} data-testid="assistant-recommendations">
    {partial ? <Alert type="warning" showIcon title="部分分析未完成，已核实结果见下" /> : null}
    {typeof result.message === "string" && result.message ? <Typography.Paragraph type="secondary" style={{ margin: 0 }}>{result.message}</Typography.Paragraph> : null}
    {recommendations.length ? <section aria-label="已核实推荐"><Typography.Paragraph strong style={{ marginBottom: 8 }}>已核实推荐</Typography.Paragraph><CandidateCards candidates={recommendations} /></section> : null}
    {pending.length ? <section aria-label="待确认人选"><Typography.Paragraph strong style={{ marginBottom: 4 }}>待确认人选</Typography.Paragraph><Typography.Paragraph type="secondary">这些人选的关键条件或相关经历仍缺少明确证据，需要核实后再判断。</Typography.Paragraph><CandidateCards candidates={pending} pending /></section> : null}
  </Flex>;
}
