/** 本文件负责在岗位、管理员和分享日志中按需展开 AI 失败的完整实际返回及解析原因。 */
"use client";

import { Collapse, Flex, Typography } from "antd";

type AIResponseDiagnostic = {
  model: string;
  response: string;
  response_kind: string;
  response_body?: string;
  parse_error: string;
  parse_field?: string;
  parse_offset?: number;
  response_complete: boolean;
  request_attempt?: number;
  final_request_error?: string;
};

/** aiResponseDiagnosticFromOutput 校验未知日志出参，兼容旧记录、空值及无 AI 诊断的日志。 */
export function aiResponseDiagnosticFromOutput(
  output: unknown,
): AIResponseDiagnostic | null {
  if (!output || typeof output !== "object" || Array.isArray(output))
    return null;
  const value = (output as Record<string, unknown>).ai_response_diagnostic;
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  if (typeof record.response !== "string") return null;
  return {
    model: typeof record.model === "string" ? record.model : "",
    response: record.response,
    response_kind:
      typeof record.response_kind === "string" ? record.response_kind : "",
    response_body:
      typeof record.response_body === "string"
        ? record.response_body
        : undefined,
    parse_error:
      typeof record.parse_error === "string" ? record.parse_error : "",
    parse_field:
      typeof record.parse_field === "string" ? record.parse_field : undefined,
    parse_offset:
      typeof record.parse_offset === "number" ? record.parse_offset : undefined,
    response_complete: record.response_complete === true,
    request_attempt:
      typeof record.request_attempt === "number"
        ? record.request_attempt
        : undefined,
    final_request_error:
      typeof record.final_request_error === "string"
        ? record.final_request_error
        : undefined,
  };
}

/** ordinaryDiagnosticOutput 去掉已独立展示的完整 AI 回复，避免同一份长原文在详情中重复展示。 */
export function ordinaryDiagnosticOutput(
  output: unknown,
): Record<string, unknown> {
  if (!output || typeof output !== "object" || Array.isArray(output)) return {};
  const fields = { ...(output as Record<string, unknown>) };
  delete fields.ai_response_diagnostic;
  return fields;
}

/** AIResponseDiagnosticDetails 默认折叠长回复，展开后完整显示模型、解析位置和实际收到的内容。 */
export default function AIResponseDiagnosticDetails({
  output,
}: {
  output: unknown;
}) {
  const diagnostic = aiResponseDiagnosticFromOutput(output);
  if (!diagnostic) return null;
  return (
    <Collapse
      ghost
      size="small"
      style={{ minWidth: 0, fontSize: 13 }}
      items={[
        {
          key: "ai-response",
          label: (
            <Typography.Text
              style={{ color: "var(--goodhr-primary, #159a62)", fontSize: 13 }}
            >
              查看 AI 实际返回
              {diagnostic.model ? `（${diagnostic.model}）` : ""}
            </Typography.Text>
          ),
          children: (
            <Flex vertical gap={8} style={{ minWidth: 0 }}>
              <Typography.Text style={{ fontSize: 12 }}>
                模型：{diagnostic.model || "未提供"} ·{" "}
                {diagnostic.response_complete
                  ? "回复已接收完"
                  : "连接中断，以下是已收到的全部内容"}
              </Typography.Text>
              {diagnostic.parse_error ? (
                <Typography.Text
                  type="danger"
                  style={{
                    fontSize: 12,
                    whiteSpace: "pre-wrap",
                    overflowWrap: "anywhere",
                  }}
                >
                  具体原因：{diagnostic.parse_error}
                </Typography.Text>
              ) : null}
              {diagnostic.request_attempt ? (
                <Typography.Text style={{ fontSize: 12 }}>
                  原文来自第 {diagnostic.request_attempt} 次请求
                </Typography.Text>
              ) : null}
              {diagnostic.final_request_error ? (
                <Typography.Text
                  type="danger"
                  style={{ fontSize: 12, overflowWrap: "anywhere" }}
                >
                  后续重试结果：{diagnostic.final_request_error}
                </Typography.Text>
              ) : null}
              {diagnostic.parse_field || diagnostic.parse_offset != null ? (
                <Typography.Text style={{ fontSize: 12 }}>
                  解析位置：
                  {diagnostic.parse_field
                    ? `字段 ${diagnostic.parse_field}`
                    : ""}
                  {diagnostic.parse_offset != null
                    ? ` · 第 ${diagnostic.parse_offset} 字节`
                    : ""}
                </Typography.Text>
              ) : null}
              <AIResponseText
                label={
                  diagnostic.response_kind === "model_content"
                    ? "AI 原始回复"
                    : "接口原始返回"
                }
                value={diagnostic.response}
              />
              {diagnostic.response_body &&
              diagnostic.response_body !== diagnostic.response ? (
                <AIResponseText
                  label="接口事件流原文"
                  value={diagnostic.response_body}
                />
              ) : null}
            </Flex>
          ),
        },
      ]}
    />
  );
}

/** AIResponseText 保留原文换行与完整长度，通过滚动控制详情高度。 */
function AIResponseText({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ minWidth: 0 }}>
      <Typography.Text strong style={{ fontSize: 12 }}>
        {label}
      </Typography.Text>
      <pre
        style={{
          margin: "4px 0 0",
          padding: 8,
          maxHeight: 360,
          overflow: "auto",
          whiteSpace: "pre-wrap",
          overflowWrap: "anywhere",
          fontSize: 12,
          background: "var(--goodhr-hover)",
        }}
      >
        {value || "接口没有返回正文"}
      </pre>
    </div>
  );
}
