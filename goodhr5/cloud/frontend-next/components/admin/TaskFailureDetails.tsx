/** 文件作用：在岗位日志和分享记录中展示已确认的故障来源、等待阶段和实际影响。 */
import { Flex, Typography } from "antd";

type FailureRow = { title: string; impact: string; facts: string };
const measures = {
  elapsed_ms: "已等待", limit_ms: "时限", headers_ms: "收到响应头", first_content_ms: "收到首段内容",
  attempt: "尝试次数", received_bytes: "已接收", http_status: "HTTP",
} as const;

/** textField 只使用可展示的文本，不把异常数据转换成占位文案。 */
function textField(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** failureRows 提取新日志里的各层证据，兼容没有这些字段的历史记录。 */
function failureRows(output: unknown): FailureRow[] {
  if (!output || typeof output !== "object" || !("failure_diagnostics" in output) ||
      !Array.isArray(output.failure_diagnostics)) return [];
  return output.failure_diagnostics.slice(0, 8).flatMap((value: unknown) => {
    if (!value || typeof value !== "object" || Array.isArray(value)) return [];
    const detail = value as Record<string, unknown>;
    const source = textField(detail.source), reason = textField(detail.reason);
    if (!source || !reason) return [];
    const where = [...new Set([source, textField(detail.operation), textField(detail.phase)].filter(Boolean))].join(" · ");
    const facts = Object.entries(measures).flatMap(([key, label]) => {
      const n = detail[key];
      if (typeof n !== "number" || !Number.isFinite(n) || n < 0) return [];
      let formatted = String(n);
      if (key.endsWith("_ms")) formatted = n >= 1000 ? `${n / 1000} 秒` : `${n} ms`;
      if (key === "received_bytes") formatted += " 字节";
      return [`${label} ${formatted}`];
    }).join(" · ");
    return [{ title: `${where}：${reason}`, impact: textField(detail.impact), facts }];
  });
}

/** taskFailureText 让复制的岗位日志与分享页搜索包含相同的故障证据。 */
export function taskFailureText(output: unknown): string {
  return failureRows(output).map(row => [row.title, row.impact, row.facts].filter(Boolean).join("\n")).join("\n");
}

/** TaskFailureDetails 直接显示各环节的原因与影响，仅展示实际记录到的时限和耗时。 */
export default function TaskFailureDetails({ output }: { output: unknown }) {
  const rows = failureRows(output);
  if (rows.length === 0) return null;
  return <Flex vertical gap={6} style={{ minWidth: 0, overflowWrap: "anywhere" }}>
    {rows.map((row, index) => <Flex key={index} vertical gap={2}>
      <Typography.Text strong>{row.title}</Typography.Text>
      {row.impact && <Typography.Text>{row.impact}</Typography.Text>}
      {row.facts && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{row.facts}</Typography.Text>}
    </Flex>)}
  </Flex>;
}
