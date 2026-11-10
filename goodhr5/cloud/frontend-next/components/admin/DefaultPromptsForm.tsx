/** 本文件用按用途分组的多行输入框编辑系统 AI 提示词，保留未展示的配置字段。 */
"use client";
import { Alert, Form, Input, Tabs } from "antd";

const groups = [
  { key: "screening", label: "简历筛选", fields: [
    ["filter_prompt", "首次筛选", "岗位未单独填写时使用。"],
    ["open_detail_prompt", "打开简历详情", "用于简历详情识别与分析。"],
    ["review_prompt", "最终复核", "用于候选人复核。"],
    ["boss_attachment_basic_prompt", "BOSS 附件基础识别提示词", "用于手动采集插件首次识别附件。"],
  ] },
  { key: "auto-reply", label: "自动回复", fields: [
    ["auto_reply_dialogue_prompt", "对话与回复决策", "处理候选人问题、沟通条件和话术选择。"],
    ["auto_reply_review_prompt", "简历补齐后的条件复核", "读取新简历后核对已启用的沟通条件。"],
    ["auto_reply_resume_prompt", "自动回复简历整理", "将聊天中收到的简历整理成候选人资料。"],
    ["auto_reply_recommendation_prompt", "自动回复推荐报告", "结合岗位、简历和已确认的信息生成报告。"],
    ["auto_reply_conditions_prompt", "岗位条件生成", "点击“根据 AI 生成”时，从岗位原文提取待勾选条件。"],
    ["auto_reply_supplement_prompt", "HR 回复后的资料补充", "根据 HR 已确认的回答，决定补充到岗位描述或公司信息。"],
  ] },
];

/** validateDefaultPromptsForm 校验所有已配置的提示词均为有界文本。 */
export function validateDefaultPromptsForm(value: string) {
  try {
    const config = JSON.parse(value);
    if (!config || typeof config !== "object" || Array.isArray(config)) return "提示词配置格式暂时没读明白，请重新加载";
    for (const field of groups.flatMap(group => group.fields)) {
      const v = config[field[0]];
      if (v !== undefined && (typeof v !== "string" || Array.from(v).length > 20000)) return field[0] === "boss_attachment_basic_prompt" ? "BOSS 附件基础识别提示词请填写文字，最多2万字；留空使用系统默认值" : `${field[1]}请填写文字，最多两万字`;
    }
    return "";
  } catch { return "提示词配置暂时没读出来，请重新加载"; }
}

/** DefaultPromptsForm 通过普通输入框修改字段，完整保留其它提示词和未知对象。 */
export default function DefaultPromptsForm({ value, defaults, onChange, disabled }: {
  value: string; defaults: Record<string, string>; onChange: (next: string) => void; disabled?: boolean;
}) {
  let config: Record<string, unknown>;
  try { config = JSON.parse(value); } catch { return <Alert type="warning" showIcon title="提示词没读出来，请重新加载后再编辑。" />; }
  if (!config || typeof config !== "object" || Array.isArray(config)) return <Alert type="warning" showIcon title="提示词配置格式暂时没读明白，请重新加载。" />;
  const known = new Set(groups.flatMap(group => group.fields.map(field => field[0])));
  const extra = Object.entries(config).filter(([key, val]) => !known.has(key) && typeof val === "string");
  const sections = extra.length ? [...groups, {key: "other", label: "其他提示词", fields: extra.map(([key]) => [key, key, ""]) }] : groups;
  /** updatePrompt 只替换当前输入字段，保持其他草稿内容。 */
  function updatePrompt(key: string, text: string) {
    onChange(JSON.stringify({...config, [key]: text}, null, 2));
  }
  return <Tabs items={sections.map(group => ({key: group.key, label: group.label, children: <Form layout="vertical" disabled={disabled}>
    {group.fields.map(([key, label, help]) => <Form.Item key={key} label={label} htmlFor={`prompt-${key}`} extra={help}>
      <Input.TextArea id={`prompt-${key}`} aria-label={label} value={typeof config[key] === "string" ? config[key] as string : defaults[key] || ""}
        autoSize={{minRows: 5, maxRows: 14}} style={{overflowWrap:"anywhere", whiteSpace:"pre-wrap"}}
        placeholder="留空使用系统默认提示词" onChange={event => updatePrompt(key, event.target.value)} />
    </Form.Item>)}
  </Form>}))} />;
}
