/** 本文件提供系统 JSON 配置的语法编辑、校验和 Ant Design 树形预览。 */
"use client";
import { json } from "@codemirror/lang-json";
import CodeMirror from "@uiw/react-codemirror";
import { Alert, Card, Segmented } from "antd";
import { useMemo, useState } from "react";
import JsonTree from "./JsonTree";
/** JsonEditor 切换源码与结构预览，格式无效时提示原因。 */
export default function JsonEditor({
  value,
  onChange,
  minHeight = 500,
}: {
  value: string;
  onChange: (value: string) => void;
  minHeight?: number;
}) {
  const [mode, setMode] = useState<"code" | "tree">("code");
  const parsed = useMemo(() => {
    try {
      return { value: JSON.parse(value || "{}"), error: "" };
    } catch (error) {
      return {
        value: null,
        error: error instanceof Error ? error.message : "JSON 格式错误",
      };
    }
  }, [value]);
  return (
    <div>
      <Segmented
        value={mode}
        onChange={(next) => setMode(next as "code" | "tree")}
        options={[
          { value: "code", label: "源码编辑" },
          { value: "tree", label: "结构预览", disabled: Boolean(parsed.error) },
        ]}
        style={{ marginBottom: 12 }}
      />
      {parsed.error ? (
        <Alert
          type="error"
          title={`JSON 语法错误：${parsed.error}`}
          showIcon
          style={{ marginBottom: 12 }}
        />
      ) : null}
      {mode === "code" ? (
        <Card styles={{ body: { padding: 0, overflow: "hidden" } }}>
          <CodeMirror
            value={value}
            height={`${minHeight}px`}
            extensions={[json()]}
            onChange={onChange}
            basicSetup={{
              foldGutter: true,
              lineNumbers: true,
              highlightActiveLine: true,
            }}
          />
        </Card>
      ) : (
        <Card
          styles={{ body: { minHeight, maxHeight: 640, overflow: "auto" } }}
        >
          <JsonTree value={parsed.value} />
        </Card>
      )}
    </div>
  );
}
