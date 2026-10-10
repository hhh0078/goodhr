/** 文件作用说明：为候选人提供仅针对本次邀请的面试查看、确认参加、申请改期与婉拒页面。 */
"use client";
import { Alert, Button, Card, Descriptions, Flex, Input, Spin, Tag, Typography } from "antd";
import { useParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import { recruitmentStatusLabel } from "@/lib/recruitment-api";

type PublicInvitation = { candidate_name: string; position_name: string; company_name?: string; starts_at: string; duration_minutes: number; mode: string; location: string; meeting_url: string; confirmation: string; expires_at: string };
/** InterviewInvitationPage 无需登录即可回应随机令牌邀请，不读取团队、简历或内部评价。 */
export default function InterviewInvitationPage() {

  const params = useParams<{ token: string }>();
  const token = String(params.token || "");
  const [invitation, setInvitation] = useState<PublicInvitation | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [note, setNote] = useState("");
  const saving = useRef(false);
  const generation = useRef(0);
  /** load 只访问公共邀请接口，失效时不继续提供操作入口。 */

  async function load() {

    const current = ++generation.current;
    setLoading(true);
    setError("");
    try {

      const data = await cloudRequest(`/api/public/recruitment/invitations/${encodeURIComponent(token)}`, { auth: false });
      if (current === generation.current) setInvitation(data.invitation || null);
    } catch (cause) {

      if (current === generation.current) {

        setInvitation(null);
        setError(cause instanceof Error ? cause.message : "这份邀请暂时没读出来，可以联系邮件里的招聘联系人。");
      }
    } finally {

      if (current === generation.current) setLoading(false);
    }
  }

  useEffect(() => {

    void load();
    return () => {

      generation.current++;
    };
  }, [token]);
  /** respond 明确提交本次邀请的答复，改期只产生申请，不自动改变已有面试时间。 */

  async function respond(action: "confirm" | "reschedule" | "decline") {

    if (!invitation || saving.current) return;
    if (action === "reschedule" && !note.trim()) {

      setError("告诉我们方便的时间，招聘联系人会再和你确认。");
      return;
    }
    saving.current = true;
    setBusy(true);
    setError("");
    setMessage("");
    try {

      const data = await cloudRequest(`/api/public/recruitment/invitations/${encodeURIComponent(token)}`, { method: "POST", auth: false, body: { action, note: note.trim() } });
      setInvitation((current) => current ? { ...current, confirmation: data.confirmation } : current);
      setMessage(data.message || "你的答复已送到招聘联系人。");
    } catch (cause) {

      setError(cause instanceof Error ? cause.message : "答复暂时没送到，请稍后重试。");
      await load();
    } finally {

      saving.current = false;
      setBusy(false);
    }
  }

  return <main style={{ maxWidth: 680, margin: "48px auto", padding: "0 16px" }}>
    <Typography.Text type="secondary">GoodHR ·
      {invitation?.company_name || "面试邀请"}
    </Typography.Text>
    <Card style={{ marginTop: 12 }}>
      <Typography.Title level={2} style={{ marginTop: 0 }}>面试安排</Typography.Title>
      {loading ? <Flex justify="center" style={{ padding: 32 }}>
        <Spin />
      </Flex> : null}
      {error ? <Alert type="warning" title={error} style={{ marginBottom: 16 }} /> : null}
      {message ? <Alert type="success" title={message} style={{ marginBottom: 16 }} /> : null}
      {!loading && invitation ? <><Typography.Paragraph>
        {invitation.candidate_name}，你好，以下是本次面试安排。
      </Typography.Paragraph><Descriptions
          column={1}
          items={[
            { key: "position", label: "岗位", children: invitation.position_name },
            { key: "time", label: "时间", children: formatDate(invitation.starts_at) },
            { key: "duration", label: "时长", children: `${invitation.duration_minutes} 分钟` },
            {

              key: "mode",
              label: "方式",
              children: invitation.mode === "onsite" ? "现场面试" : invitation.mode === "phone" ? "电话面试" : "视频面试"
            },
            {

              key: "location",
              label: invitation.mode === "video" ? "会议链接" : "地址",
              children: invitation.mode === "video" && /^https?:\/\//i.test(invitation.meeting_url || "") ? <a href={invitation.meeting_url} target="_blank" rel="noreferrer">进入视频会议</a> : invitation.location || "招聘联系人会另行说明"
            },
            {

              key: "confirmation",
              label: "你的答复",
              children: <Tag>
                {recruitmentStatusLabel(invitation.confirmation)}
              </Tag>
            }
          ]} /><Typography.Paragraph type="secondary" style={{ marginTop: 20 }}>时间合适就确认参加。如需改期，写下方便的时间，招聘联系人会再与你确认。</Typography.Paragraph><Input.TextArea
          aria-label="面试答复说明"
          placeholder="需要改期时，请写下方便的时间；也可以留下其他说明。"
          value={note}
          maxLength={1000}
          disabled={busy}
          onChange={(event) => setNote(event.target.value)}
          autoSize={{ minRows: 3, maxRows: 6 }} /><Flex gap={8} wrap justify="flex-end" style={{ marginTop: 16 }}>
          <Button disabled={busy} onClick={() => void respond("decline")}>这次不参加</Button>
          <Button disabled={busy} onClick={() => void respond("reschedule")}>申请改期</Button>
          <Button type="primary" loading={busy} onClick={() => void respond("confirm")}>确认参加</Button>
        </Flex></> : null}
      {!loading && !invitation ? <Typography.Paragraph type="secondary">邀请可能已过期、取消或改期。请联系原邮件中的招聘联系人，获取最新安排。</Typography.Paragraph> : null}
    </Card>
  </main>;
}
