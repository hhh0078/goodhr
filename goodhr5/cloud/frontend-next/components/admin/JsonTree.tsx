/** 本文件递归展示 JSON 对象、数组和基础值，供后台配置预览复用。 */
"use client";

import { Box, Typography } from "@mui/material";

/** JsonTree 以可折叠树形结构展示 JSON 数据。 */
export default function JsonTree({ value, label = "", depth = 0 }: { value: unknown; label?: string; depth?: number }) {
  if (value !== null && typeof value === "object") {
    const entries = Array.isArray(value) ? value.map((item, index) => [String(index), item] as const) : Object.entries(value as Record<string, unknown>);
    return <Box component="details" open={depth < 2} sx={{ ml: depth ? 1.5 : 0, "& > summary": { cursor: "pointer", py: 0.25 } }}><Box component="summary"><Typography component="span" sx={{ color: "#0f754a", fontFamily: "monospace", fontSize: 13 }}>{label ? `${label}: ` : ""}{Array.isArray(value) ? `[${entries.length} 项]` : `{${entries.length} 个字段}`}</Typography></Box><Box sx={{ ml: 1.25, pl: 1.5, borderLeft: "1px solid", borderColor: "divider" }}>{entries.map(([key, item]) => <JsonTree key={key} label={key} value={item} depth={depth + 1} />)}</Box></Box>;
  }
  return <Typography sx={{ ml: depth ? 3 : 0, py: 0.2, fontFamily: "monospace", fontSize: 13, color: leafColor(value), overflowWrap: "anywhere" }}><Box component="span" sx={{ color: "#0f754a" }}>{label ? `${label}: ` : ""}</Box>{formatLeaf(value)}</Typography>;
}

/** formatLeaf 将 JSON 基础值转换为可读文本。 */
function formatLeaf(value: unknown) {
  if (value === null) return "null";
  if (typeof value === "string") return `"${value}"`;
  return String(value);
}

/** leafColor 返回不同 JSON 数据类型的展示颜色。 */
function leafColor(value: unknown) {
  if (value === null) return "#c83f49";
  if (typeof value === "number") return "#9a5b10";
  if (typeof value === "boolean") return "#7553a6";
  return "#46524c";
}
