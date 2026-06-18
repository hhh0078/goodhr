/** 本文件提供系统配置使用的 JSON 编辑、校验和树形预览组件。 */
"use client";

import { json } from "@codemirror/lang-json";
import CodeMirror from "@uiw/react-codemirror";
import { Alert, Box, Button, Stack } from "@mui/material";
import { useMemo, useState } from "react";
import JsonTree from "./JsonTree";

/** JsonEditor 支持源码编辑和树形预览两种查看方式。 */
export default function JsonEditor({ value, onChange, minHeight = 500 }: { value: string; onChange: (value: string) => void; minHeight?: number }) {
  const [mode, setMode] = useState<"code" | "tree">("code");
  const parsed = useMemo(() => { try { return { value: JSON.parse(value || "{}"), error: "" }; } catch (error) { return { value: null, error: error instanceof Error ? error.message : "JSON 格式错误" }; } }, [value]);
  return <Box><Stack direction="row" spacing={1} sx={{ mb: 1.5 }}><Button size="small" variant={mode === "code" ? "contained" : "outlined"} onClick={() => setMode("code")}>源码编辑</Button><Button size="small" variant={mode === "tree" ? "contained" : "outlined"} disabled={Boolean(parsed.error)} onClick={() => setMode("tree")}>结构预览</Button></Stack>{parsed.error ? <Alert severity="error" sx={{ mb: 1.5 }}>JSON 语法错误：{parsed.error}</Alert> : null}{mode === "code" ? <Box sx={{ overflow: "hidden", border: "1px solid", borderColor: "divider", borderRadius: "8px", "& .cm-editor": { minHeight }, "& .cm-scroller": { fontFamily: "SFMono-Regular, Consolas, monospace" } }}><CodeMirror value={value} height={`${minHeight}px`} extensions={[json()]} onChange={onChange} basicSetup={{ foldGutter: true, lineNumbers: true, highlightActiveLine: true }} /></Box> : <Box sx={{ minHeight, maxHeight: 640, overflow: "auto", p: 2, bgcolor: "#f7faf8", borderRadius: "8px" }}><JsonTree value={parsed.value} /></Box>}</Box>;
}
