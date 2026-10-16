/** 本文件在现有 AI 默认提示词管理中提供 BOSS 附件基础识别编辑框，更新时保留其它提示词和未知字段。 */
"use client";
import { Flex, Form, Input, Typography } from "antd";

/** parsePromptConfig 只接受 JSON 对象，格式错误时禁止快捷编辑覆盖原有草稿。 */
function parsePromptConfig(value: string): Record<string, unknown> | null {
  try {
    const config = JSON.parse(value);
    return config && typeof config === "object" && !Array.isArray(config)
      ? config
      : null;
  } catch {
    return null;
  }
}

/** validateBossAttachmentPrompt 与后端保持文字类型和两万字上限，缺失或留空沿用系统默认值。 */
export function validateBossAttachmentPrompt(value: string) {
  const config = parsePromptConfig(value);
  if (!config) return "AI 默认提示词需要使用 JSON 对象";
  if (!("boss_attachment_basic_prompt" in config)) return "";
  const prompt = config.boss_attachment_basic_prompt;
  return typeof prompt === "string" && Array.from(prompt).length <= 20000
    ? ""
    : "BOSS 附件基础识别提示词请填写文字，最多2万字；留空使用系统默认值";
}

/** BossAttachmentPromptField 编辑插件首次识别提示词，不调整完整解析、用户 AI 配置或计费规则。 */
export default function BossAttachmentPromptField({
  value,
  defaultValue,
  onChange,
  disabled = false,
}: {
  value: string;
  defaultValue: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const config = parsePromptConfig(value);
  const current = config?.boss_attachment_basic_prompt;
  /** changePrompt 只合并专用字段，保留首次筛选、详情、复核和未来新增配置。 */
  function changePrompt(prompt: string) {
    if (!config) return;
    onChange(JSON.stringify({ ...config, boss_attachment_basic_prompt: prompt }, null, 2));
  }
  return (
    <Flex vertical gap={4} style={{ marginBottom: 16 }}>
      <Form layout="vertical">
        <Form.Item label="BOSS 附件基础识别提示词" htmlFor="boss-attachment-basic-prompt" style={{ marginBottom: 0 }}>
          <Input.TextArea
            id="boss-attachment-basic-prompt"
            value={current === undefined ? defaultValue : typeof current === "string" ? current : ""}
            disabled={disabled || !config}
            autoSize={{ minRows: 6, maxRows: 14 }}
            placeholder="留空使用系统默认提示词"
            onChange={(event) => changePrompt(event.target.value)}
          />
        </Form.Item>
      </Form>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        只用于插件首次读取姓名、手机号、原文明示性别及时间最早的工作和学校。保存后，下次新识别立即使用；留空使用系统默认值。
      </Typography.Text>
    </Flex>
  );
}
