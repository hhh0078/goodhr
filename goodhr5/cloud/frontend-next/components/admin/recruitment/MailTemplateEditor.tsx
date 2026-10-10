/** 文件作用说明：编辑中文邮件模板，支持点击插入内置占位符并实时展示服务端示例预览。 */
"use client";
import { Alert, Button, Flex, Form, Input, Select, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { recruitmentMailCategories, recruitmentRequest, type RecruitmentMailTemplate, type RecruitmentPlaceholder, type RecruitmentPreview } from "@/lib/recruitment-api";

/** MailTemplateEditor 用现有文本控件编辑模板，不要求用户手写未知变量或代码。 */
export default function MailTemplateEditor({ open, template, placeholders, loading, onClose, onSave }: { open: boolean; template: RecruitmentMailTemplate | null; placeholders: RecruitmentPlaceholder[]; loading: boolean; onClose: () => void; onSave: (payload: Partial<RecruitmentMailTemplate>) => void }) {

  const [name, setName] = useState("");
  const [category, setCategory] = useState("custom");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [preview, setPreview] = useState<RecruitmentPreview | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [previewBusy, setPreviewBusy] = useState(false);

  const activeField = useRef<"subject" | "body">("body");
  const subjectRef = useRef<any>(null);
  const bodyRef = useRef<any>(null);
  const generation = useRef(0);

  useEffect(() => {

    if (!open) return;
    setName(template?.name || "");
    setCategory(template?.category || "custom");
    setSubject(template?.subject || "{公司名称} · {岗位名称}面试邀请");
    setBody(template?.body || "{简历姓名}，你好：\n\n邀请你参加{岗位名称}的面试。\n面试时间：{面试时间}\n\n确认或调整安排：{确认链接}\n{招聘联系人}\n{联系邮箱}");
    setPreview(null);
  }, [open, template]);

  useEffect(() => {

    if (!open) {

      generation.current++;
      return;
    }
    const current = ++generation.current;
    setPreviewBusy(true);
    setPreviewError("");
    const timer = window.setTimeout(() => {

      recruitmentRequest("/mail-preview", "POST", { subject, body, example: true }).then((data) => {

        if (current === generation.current) setPreview(data.preview);
      }).catch((error) => {

        if (current === generation.current) {

          setPreview(null);
          setPreviewError(error.message || "预览暂时没读出来");
        }
      }).finally(() => {

        if (current === generation.current) setPreviewBusy(false);
      });
    }, 350);
    return () => {

      window.clearTimeout(timer);
      generation.current++;
    };
  }, [open, subject, body]);
  /** insertPlaceholder 在最近编辑的输入位置插入中文占位符，并保留后续输入位置。 */

  function insertPlaceholder(key: string) {

    const field = activeField.current;
    const element: HTMLInputElement | HTMLTextAreaElement | undefined = field === "subject" ? subjectRef.current?.input : bodyRef.current?.resizableTextArea?.textArea;
    const text = field === "subject" ? subject : body;
    const start = element?.selectionStart ?? text.length;
    const end = element?.selectionEnd ?? start;
    const token = `{${key}}`;
    const next = text.slice(0, start) + token + text.slice(end);
    if (field === "subject") setSubject(next); else setBody(next);
    window.setTimeout(() => {

      element?.focus();
      element?.setSelectionRange(start + token.length, start + token.length);
    }, 0);
  }

  const invalid = Boolean(preview?.unknown_placeholders?.length);

  return <AdminDialog
    open={open}
    title={template?.id ? "编辑邮件模板" : "新建邮件模板"}
    maxWidth="md"
    loading={loading}
    onClose={onClose}
    onConfirm={() => onSave({ id: template?.id || undefined, name: name.trim(), category, subject, body })}
    confirmDisabled={!name.trim() || !subject.trim() || !body.trim() || invalid}>

    <Flex gap={20} wrap align="start">
      <Form layout="vertical" style={{ flex: "1 1 360px", minWidth: 0 }}>
        <Form.Item label="模板名称" required>
          <Input
            aria-label="邮件模板名称"
            value={name}
            maxLength={100}
            onChange={(event) => setName(event.target.value)} />
        </Form.Item>
        <Form.Item label="使用场景">
          <Select
            aria-label="邮件模板场景"
            showSearch
            optionFilterProp="label"
            value={category}
            options={recruitmentMailCategories}
            onChange={setCategory} />
        </Form.Item>
        <Form.Item label="邮件标题" required>
          <Input
            ref={subjectRef}
            aria-label="邮件模板标题"
            value={subject}
            maxLength={300}
            onFocus={() => {

              activeField.current = "subject";
            }}
            onChange={(event) => setSubject(event.target.value)} />
        </Form.Item>
        <Form.Item label="邮件正文" required>
          <Input.TextArea
            ref={bodyRef}
            aria-label="邮件模板正文"
            autoSize={{ minRows: 8, maxRows: 18 }}
            value={body}
            maxLength={60000}
            onFocus={() => {

              activeField.current = "body";
            }}
            onChange={(event) => setBody(event.target.value)} />
        </Form.Item>
        <Typography.Text type="secondary">点一个变量，插入刚才编辑的位置。发出时会换成真实资料。</Typography.Text>
        <Flex gap={6} wrap style={{ marginTop: 8 }}>
          {placeholders.map((item) => <Button
            key={item.key}
            size="small"
            title={`${item.label || item.key}，示例：${item.example || "待填写"}`}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => insertPlaceholder(item.key)}>
            {`{${item.key}}`}
          </Button>)}
        </Flex>
      </Form>

      <div style={{ flex: "1 1 320px", minWidth: 0 }}>
        <Typography.Title level={5} style={{ marginTop: 0 }}>示例预览
          <Spin spinning={previewBusy} size="small" />
        </Typography.Title>
        <Alert type="info" title="这里使用示例资料。实际发送前，还会检查公司信息和本次面试资料。" style={{ marginBottom: 12 }} />
        {previewError ? <Alert type="warning" title={previewError} /> : null}
        {preview?.unknown_placeholders?.length ? <Alert
          type="error"
          title={`这些变量还不认识：${preview.unknown_placeholders.join("、")}`}
          style={{ marginBottom: 10 }} /> : null}
        <Typography.Text strong>
          {preview?.subject || subject}
        </Typography.Text>
        <Typography.Paragraph
          style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere", marginTop: 16 }}
          data-testid="mail-template-live-preview">
          {preview?.body || "正在整理示例……"}
        </Typography.Paragraph>
      </div>

    </Flex>

  </AdminDialog>;
}
