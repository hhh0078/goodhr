/** 本文件使用 Ant Design Tree 展示 JSON 对象、数组和基础值。 */
"use client";
import { Tree, Typography } from "antd";
import type { DataNode } from "antd/es/tree";
/** buildJsonNode 把 JSON 递归转换为可折叠节点，使用路径防止重名键冲突。 */
function buildJsonNode(value: unknown, label: string, path: string): DataNode {
  const object = value !== null && typeof value === "object";
  const entries = object
    ? Object.entries(value as Record<string, unknown>)
    : [];
  const text = object
    ? Array.isArray(value)
      ? `[${entries.length} 项]`
      : `{${entries.length} 个字段}`
    : (JSON.stringify(value) ?? String(value));
  return {
    key: path,
    title: (
      <Typography.Text
        style={{
          fontFamily: "monospace",
          fontSize: 13,
          overflowWrap: "anywhere",
          whiteSpace: "normal",
        }}
      >
        {label ? `${label}: ` : ""}
        {text}
      </Typography.Text>
    ),
    children: object
      ? entries.map(([key, item]) => buildJsonNode(item, key, `${path}/${key}`))
      : undefined,
    isLeaf: !object,
  };
}
/** JsonTree 渲染能够展开和收起的 JSON 结构预览。 */
export default function JsonTree({
  value,
  label = "",
}: {
  value: unknown;
  label?: string;
  depth?: number;
}) {
  const root = buildJsonNode(value, label, "root");
  return (
    <Tree
      showLine
      selectable={false}
      defaultExpandedKeys={[
        "root",
        ...(root.children || []).map((node) => node.key),
      ]}
      treeData={[root]}
      style={{ background: "transparent" }}
    />
  );
}
