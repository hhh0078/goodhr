/** 文件作用说明：为管理员和岗位设置提供同一套分类、场景、标题及回复正文编辑表单。 */
"use client";

import { Alert, AutoComplete, Form, Input } from "antd";
import { useId, useState } from "react";
import type { ReplyPhrase } from "@/lib/auto-reply";
import AdminDialog from "./AdminDialog";

type Props = {
  phrase: ReplyPhrase;
  categories: string[];
  onSave: (phrase: ReplyPhrase) => void;
  onClose: () => void;
};

/** ReplyPhraseEditor 先完整校验一条话术，再交给所属配置的草稿。 */
export default function ReplyPhraseEditor({ phrase, categories, onSave, onClose }: Props) {
  const [draft, setDraft] = useState(phrase);
  const [error, setError] = useState("");
  const prefix = useId();
  // 老岗位的索要简历话术可超过新长度；编辑其他字段时原文仍可保存。
  const contentLimit = phrase.id === "request_resume" ? Math.max(200, Math.min(500, Array.from(phrase.content).length)) : 200;
  /** updateField 保留同一话术的其他草稿字段。 */
  function updateField(field: keyof Omit<ReplyPhrase, "id">, value: string) {
    setDraft(current => ({ ...current, [field]: value }));
    setError("");
  }
  /** apply 检查所有文本字段并移除首尾空白。 */
  function apply() {
    const result = { id: phrase.id, category: draft.category.trim(), title: draft.title.trim(), scene: draft.scene.trim(), content: draft.content.trim() };
    if (!result.title || !result.category || !result.scene) {
      setError("请补齐标题、分类和使用场景。");
      return;
    }
    if (Array.from(result.category).length > 24 || Array.from(result.title).length > 60 || Array.from(result.scene).length > 200 || Array.from(result.content).length > contentLimit) {
      setError("文字有点长，请缩短后再保存。");
      return;
    }
    onSave(result);
  }
  return <AdminDialog open title={phrase.title ? "编辑话术" : "新增话术"} onClose={onClose} onConfirm={apply} confirmText="确定" maxWidth="sm">
    <Form layout="vertical" component="div">
      <Form.Item label="标题" htmlFor={prefix + "title"}><Input id={prefix + "title"} autoFocus value={draft.title} maxLength={60}
        placeholder="例如：收到手机号" onChange={event => updateField("title", event.target.value)} /></Form.Item>
      <Form.Item label="分类" htmlFor={prefix + "category"}><AutoComplete id={prefix + "category"} value={draft.category}
        placeholder="选择或输入分类" options={categories.map(value => ({ value }))} onChange={value => updateField("category", value)} /></Form.Item>
      <Form.Item label="使用场景" htmlFor={prefix + "scene"}><Input.TextArea id={prefix + "scene"} value={draft.scene} maxLength={200} autoSize={{ minRows: 1, maxRows: 4 }}
        placeholder="例如：候选人刚刚提供了手机号" onChange={event => updateField("scene", event.target.value)} /></Form.Item>
      <Form.Item label="回复内容" htmlFor={prefix + "content"} extra="留空不发文字；原生资料请求仍按现有规则处理。"><Input.TextArea id={prefix + "content"} value={draft.content} maxLength={contentLimit} autoSize={{ minRows: 3, maxRows: 8 }}
        placeholder="留空时，这个场景不发文字" onChange={event => updateField("content", event.target.value)} /></Form.Item>
      {error && <Alert type="warning" showIcon title={error} />}
    </Form>
  </AdminDialog>;
}
