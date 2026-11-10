/** 文件作用说明：用同一紧凑卡片展示当前授权会话的在线基础资料及资料交换进度，不冒充完整简历附件。 */
"use client";

import { Card, Descriptions, Flex, Tag, Typography } from "antd";
import { replyHistoryStatus, type ReplyThreadSnapshot } from "@/lib/resume-reply-history";
import styles from "./resume-reply-history.module.css";

/** confirmedText 只展示服务器明确给出的文字，缺少信息时不猜测或回填。 */
function confirmedText(value?: string) {
  return value?.trim() || "暂未确认";
}

/** confirmedNumber 展示明确的年龄或年份，空值和无效数字不被当作零岁或零年。 */
function confirmedNumber(value: number | null | undefined, suffix: string) {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value + suffix : "暂未确认";
}

/** ResumeReplyProfile 优先说明资料完整度和临时状态，再展示联系方式及三类原生索要结果。 */
export default function ResumeReplyProfile({ snapshot, formatTime }: { snapshot: ReplyThreadSnapshot; formatTime: (value?: string) => string }) {
  const profile = snapshot.online_profile;
  const facts = profile ? [
    { key: "name", label: "姓名", children: confirmedText(profile.candidate_name) },
    { key: "age", label: "年龄", children: confirmedNumber(profile.age, "岁") },
    { key: "education", label: "最高学历", children: confirmedText(profile.education_level) },
    { key: "company", label: "最早公司", children: confirmedText(profile.earliest_company_name) },
    { key: "company_year", label: "开始年份", children: confirmedNumber(profile.earliest_company_year, "年") },
    { key: "school", label: "最早学校", children: confirmedText(profile.earliest_school_name) },
    { key: "school_year", label: "入学年份", children: confirmedNumber(profile.earliest_school_year, "年") },
  ] : [];
  const confirmed = facts.filter(item => item.children !== "暂未确认").length;
  const mode = profile?.mode === "ai" ? "AI 识别" : profile?.mode === "dom" ? "页面读取" : "识别方式待确认";
  const match = ({ matched: "已关联档案", temporary: "独立建档", ambiguous: "同人待核实" } as Record<string, string>)[profile?.match_status || ""] || "关联待确认";
  return <Card size="small" className={styles.profileCard}>
    <section aria-label="在线基础资料">
      <Flex justify="space-between" align="center" gap={8} wrap className={styles.profileHeading}>
        <Typography.Text strong>在线基础资料</Typography.Text>
        <Flex gap={8} wrap>{profile ? <><Tag color={confirmed === 7 ? "success" : "default"}>已确认 {confirmed} / 7 项</Tag><Tag>{profile.is_temporary ? "在线临时资料" : "已入库资料"}</Tag></> : <Tag>暂未确认</Tag>}</Flex>
      </Flex>
      {profile ? <>
        <Flex gap={8} wrap className={styles.profileMeta}><span>{mode}</span><span>{match}</span>{profile.updated_at && <time dateTime={profile.updated_at}>{formatTime(profile.updated_at)}</time>}</Flex>
        <Descriptions size="small" layout="vertical" column={{ xs: 2, sm: 3 }} items={facts} className={styles.profileFacts} />
        <Typography.Text type="secondary" className={styles.profileNote}>这是在线基础资料，不代表已收到简历附件。{profile.is_temporary ? "手机号待补充，按最近资料或真实沟通保留30天。" : ""}</Typography.Text>
      </> : <Typography.Text type="secondary" className={styles.profileNote}>在线资料暂未确认，已有聊天和沟通目标仍可查看。</Typography.Text>}
    </section>
    <section className={styles.contactSection} aria-label="联系方式和资料索要进度">
      <Flex gap={8} wrap className={styles.contactValues}>
        <span>手机号：<Typography.Text copyable={snapshot.contacts?.phone ? { text: snapshot.contacts.phone } : false}>{confirmedText(snapshot.contacts?.phone)}</Typography.Text></span>
        <span>微信：<Typography.Text copyable={snapshot.contacts?.wechat ? { text: snapshot.contacts.wechat } : false}>{confirmedText(snapshot.contacts?.wechat)}</Typography.Text></span>
      </Flex>
      <Flex gap={8} wrap>{(["resume", "phone", "wechat"] as const).map(kind => {
        const request = snapshot.info_requests?.find(item => item.kind === kind);
        const [label, color] = request ? replyHistoryStatus(request.status, request.status_label) : ["暂未确认", "default"];
        const name = { resume: "简历", phone: "手机号", wechat: "微信" }[kind];
        return <Tag key={kind} color={color} title={request?.updated_at ? "更新于 " + formatTime(request.updated_at) : undefined}>{name} · {label}</Tag>;
      })}</Flex>
    </section>
  </Card>;
}
