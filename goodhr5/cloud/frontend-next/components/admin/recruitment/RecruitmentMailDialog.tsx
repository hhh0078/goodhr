/** 文件作用说明：用真实招聘资料预览并发送邮件，保留幂等编号与结果不明确时的人工核实入口。 */
"use client";
import { Alert, Button, Flex, Form, Input, Select, Space, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { APIRequestError, formatDate } from "@/lib/admin-api";
import { recruitmentRequest, type RecruitmentApplication, type RecruitmentDelivery, type RecruitmentMailTemplate, type RecruitmentPreview } from "@/lib/recruitment-api";

/** defaultTemplate 按节点绑定与实际通知场景选模板，避免把面试邀请默认选成 Offer。 */
function defaultTemplate(application: RecruitmentApplication, templates: RecruitmentMailTemplate[], interviewID: string) {
  const node = application.nodes?.[application.current_node];
  const bound = templates.find((item) => item.id === node?.mail_template_id);
  if (bound) return bound.id;
  const interview = application.interviews?.find((item) => item.id === interviewID);
  const category = application.status !== "active" ? "result"
    : interview?.status === "cancelled" ? "cancel"
      : node?.type === "interview" ? (application.nodes.slice(0, application.current_node).some((item) => item.type === "interview") ? "interview_followup" : "interview_first")
        : node?.type === "onboarding" ? "onboarding"
          : node?.type === "offer" || node?.type === "approval" ? "offer" : "result";
  return templates.find((item) => item.category === category)?.id || templates.find((item) => item.category === "custom")?.id || "";
}

/** RecruitmentMailDialog 在发送前检查真实变量，任何失败都保留编辑内容与重试编号。 */
export default function RecruitmentMailDialog({ open, application, templates, history, initialInterviewID = "", onClose, onSent }: { open: boolean; application: RecruitmentApplication; templates: RecruitmentMailTemplate[]; history: RecruitmentDelivery[]; initialInterviewID?: string; onClose: () => void; onSent: () => void }) {

  const { notify, confirm } = useAdmin();
  const [templateID, setTemplateID] = useState("");
  const [interviewID, setInterviewID] = useState("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [preview, setPreview] = useState<RecruitmentPreview | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [previewBusy, setPreviewBusy] = useState(false);
  const [previewRevision, setPreviewRevision] = useState(0);
  const [sendError, setSendError] = useState("");
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [failed, setFailed] = useState(false);
  const requestKey = useRef("");
  const saving = useRef(false);
  const generation = useRef(0);
  /** selectTemplate 把选定模板复制成当前发送草稿，不改团队模板。 */

  function selectTemplate(id: string) {

    const template = templates.find((item) => item.id === id);
    setTemplateID(id);
    setSubject(template?.subject || "");
    setBody(template?.body || "");
    requestKey.current = crypto.randomUUID();
    setUncertain(false);
    setFailed(false);
    setSendError("");
  }

  useEffect(() => {

    if (!open) {

      generation.current++;
      return;
    }
    const selectedInterviewID = initialInterviewID || application.interviews?.filter((item) => item.status !== "cancelled").at(-1)?.id || "";
    selectTemplate(defaultTemplate(application, templates, selectedInterviewID));
    setInterviewID(selectedInterviewID);
  }, [open, application.id]);

  useEffect(() => {

    if (!open || !templateID) return;
    const current = ++generation.current;
    setPreview(null);
    setPreviewBusy(true);
    setPreviewError("");
    const timer = window.setTimeout(() => {

      recruitmentRequest("/mail-preview", "POST", {

        application_id: application.id,
        interview_id: interviewID || undefined,
        template_id: templateID,
        subject,
        body
      }).then((data) => {

        if (current === generation.current) setPreview(data.preview);
      }).catch((error) => {

        if (current === generation.current) setPreviewError(error.message || "邮件预览没读出来");
      }).finally(() => {

        if (current === generation.current) setPreviewBusy(false);
      });
    }, 350);
    return () => {

      window.clearTimeout(timer);
      generation.current++;
    };
  }, [open, application.id, templateID, interviewID, subject, body, previewRevision]);
  /** refreshPreview 重新读取真实资料并等待再次确认，不会自动发送邮件。 */
  function refreshPreview() {
    generation.current++;
    setPreview(null);
    setPreviewBusy(true);
    setPreviewRevision((value) => value + 1);
  }
  /** updateDraft 变更内容时换发送编号，结果不明确的原邮件仍需在历史中核实。 */

  function updateDraft(field: "subject" | "body", value: string) {

    if (field === "subject") setSubject(value); else setBody(value);
    requestKey.current = crypto.randomUUID();
    setFailed(false);
  }

  const related = history.filter((item) => item.template_id === templateID && (item.interview_id || "") === interviewID);
  const unresolved = uncertain || related.some((item) => ["uncertain", "sending"].includes(item.status));
  /** send 使用服务端确定的候选人邮箱与公司资料，重复点击和网络重试复用同一编号。 */

  async function send() {

    if (saving.current || !preview?.can_send || preview.is_example || !preview.context_fingerprint || unresolved) return;
    if (failed) {

      requestKey.current = crypto.randomUUID();
      setFailed(false);
    }
    if (related.some((item) => item.status === "sent") && !await confirm("这份邮件已经发过", "要再发一次吗？候选人会收到一封新的邮件。")) return;
    saving.current = true;
    setBusy(true);
    try {

      const data = await recruitmentRequest("/mail-send", "POST", {

        application_id: application.id,
        interview_id: interviewID || undefined,
        template_id: templateID,
        subject,
        body,
        idempotency_key: requestKey.current,
        context_fingerprint: preview.context_fingerprint
      });
      const status = data.delivery?.status;
      if (status === "sent") {

        notify("邮件已发出，候选人的答复会单独记录。", "success");
        onClose();
      } else if (status === "uncertain" || status === "sending") {

        setUncertain(true);
        notify("发送结果还不明确，先核实邮件记录，避免重复发。", "warning");
      } else {

        setFailed(status === "failed");
        notify(data.delivery?.error_message || "邮件没有发成功，内容已保留。", "error");
      }
      onSent();
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "发送没有完成，可以核实记录后再试。";
      setSendError(message);
      notify(message, "error");
      if (!(cause instanceof APIRequestError) || cause.status >= 500 || (cause.status === 409 && cause.code !== "recruitment_preview_changed")) {
        setUncertain(true);
      }
      refreshPreview();
      onSent();
    } finally {

      saving.current = false;
      setBusy(false);
    }
  }

  const missing = [...(preview?.missing_placeholders || []), ...(preview?.unknown_placeholders || [])];

  return <AdminDialog
    open={open}
    title="预览并发送招聘邮件"
    maxWidth="md"
    loading={busy}
    confirmText={failed ? "重新发送" : "确认发送"}
    confirmDisabled={!preview?.can_send || preview.is_example || !preview.context_fingerprint || previewBusy || unresolved || !templateID}
    onConfirm={() => void send()}
    onClose={onClose}>

    <Alert
      type="info"
      title={`收件人：${preview?.recipient ?? application.candidate?.email ?? "等待预览确认收件人"}。平台邮箱发送，标题带公司名。回复地址：${preview?.reply_to || "等待预览确认"}。`}
      style={{ marginBottom: 12 }} />
    {unresolved ? <Alert type="warning" title="这次发送结果待核实，暂时不重复发送。可以关闭后查看邮件记录。" style={{ marginBottom: 12 }} /> : null}
    {sendError ? <Alert type="warning" title={sendError} description="已重新读取预览。请核对收件人和内容，再决定是否发送。" style={{ marginBottom: 12 }} /> : null}
    <Flex gap={20} wrap align="start">
      <Form layout="vertical" style={{ flex: "1 1 340px", minWidth: 0 }}>
        <Form.Item label="邮件模板">
          <Select
            aria-label="发送邮件模板"
            showSearch
            optionFilterProp="label"
            options={templates.map((item) => ({ value: item.id, label: item.name }))}
            value={templateID || undefined}
            onChange={selectTemplate} />
        </Form.Item>
        <Form.Item label="对应面试">
          <Select
            aria-label="邮件对应面试"
            showSearch
            allowClear
            optionFilterProp="label"
            placeholder="Offer、入职通知可不选面试"
            value={interviewID || undefined}
            onChange={(value) => {

              setInterviewID(value || "");
              requestKey.current = crypto.randomUUID();
              setFailed(false);
            }}
            options={(application.interviews || []).map((item) => ({

              value: item.id,
              label: `${formatDate(item.starts_at)} · ${item.status === "cancelled" ? "已取消" : item.mode === "video" ? "视频面试" : item.mode === "phone" ? "电话面试" : "现场面试"}`
            }))} />
        </Form.Item>
        <Form.Item label="本次邮件标题">
          <Input
            aria-label="本次邮件标题"
            value={subject}
            maxLength={300}
            onChange={(event) => updateDraft("subject", event.target.value)} />
        </Form.Item>
        <Form.Item label="本次邮件正文">
          <Input.TextArea
            aria-label="本次邮件正文"
            value={body}
            autoSize={{ minRows: 7, maxRows: 16 }}
            maxLength={60000}
            onChange={(event) => updateDraft("body", event.target.value)} />
        </Form.Item>
      </Form>
      <div style={{ flex: "1 1 320px", minWidth: 0 }}>
        <Flex justify="space-between" align="center" style={{ marginBottom: 12 }}>
          <Space>
            <Typography.Text strong>真实预览</Typography.Text>
            <Spin size="small" spinning={previewBusy} />
          </Space>
          <Button size="small" disabled={busy || previewBusy} onClick={refreshPreview}>刷新预览</Button>
        </Flex>
        {previewError ? <Alert type="warning" title={previewError} /> : null}
        {missing.length ? <Alert type="warning" title={`先补上：${missing.join("、")}`} style={{ marginBottom: 12 }} /> : null}
        {preview?.warnings?.length ? <Alert type="warning" title={preview.warnings.join("；")} style={{ marginBottom: 12 }} /> : null}
        {preview && !preview.can_send && !missing.length && !preview.warnings?.length ? <Alert type="warning" title="这份邮件暂时不能发送，请检查公司信息、候选人邮箱和面试安排。" style={{ marginBottom: 12 }} /> : null}
        <Typography.Text strong>
          {preview?.subject || "等待预览"}
        </Typography.Text>
        <Typography.Paragraph
          style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere", marginTop: 16 }}
          data-testid="recruitment-mail-preview">
          {preview?.body || "我正在把变量换成真实资料。"}
        </Typography.Paragraph>
      </div>
    </Flex>

  </AdminDialog>;
}
