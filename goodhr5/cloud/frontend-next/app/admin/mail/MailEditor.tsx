/** 本文件负责仅在浏览器运行的邮件富文本编辑器，隔离实例并清理切换模式时的迟到事件。 */
"use client";

import {
  DomEditor,
  type IDomEditor,
  type IEditorConfig,
  type IToolbarConfig,
} from "@wangeditor/editor";
import { Editor, Toolbar } from "@wangeditor/editor-for-react";
import { useEffect, useState } from "react";
import styles from "./page.module.css";

/** destroyMailEditor 取消 wangEditor 尚未执行的选区回调，再销毁当前实例及工具栏。 */
function destroyMailEditor(editor: IDomEditor | null) {
  if (!editor || editor.isDestroyed) return;
  // wangEditor 5.1.23 / core 1.1.19 未在销毁时取消此私有 100ms 节流任务；升级编辑器后需复验此兼容清理。
  const selectionChange = (
    DomEditor.getTextarea(editor) as unknown as {
      onDOMSelectionChange?: unknown;
    }
  ).onDOMSelectionChange;
  if (typeof selectionChange === "function") {
    const handler = selectionChange as EventListener & {
      cancel?: () => void;
    };
    DomEditor.getWindow(editor).document.removeEventListener(
      "selectionchange",
      handler,
    );
    if (typeof handler.cancel === "function") handler.cancel();
  }
  editor.destroy();
}

/** MailEditor 管理本次挂载的编辑器实例，切换模式时清理实例，正文草稿由父页面保留。 */
export default function MailEditor({
  html,
  editorConfig,
  toolbarConfig,
  onChange,
}: {
  html: string;
  editorConfig: Partial<IEditorConfig>;
  toolbarConfig: Partial<IToolbarConfig>;
  onChange: (html: string) => void;
}) {
  const [editor, setEditor] = useState<IDomEditor | null>(null);
  useEffect(() => () => destroyMailEditor(editor), [editor]);
  return (
    <div
      className={styles.layout4}
      style={{
        border: "1px solid",
        borderColor: "var(--goodhr-border)",
        borderRadius: "8px",
        overflow: "hidden",
      }}
    >
      {/* 工具栏先以空实例挂载，避免 StrictMode 首次重放效果时重复创建。 */}
      <Toolbar
        style={{ borderBottom: "1px solid #eee" }}
        editor={editor}
        defaultConfig={toolbarConfig}
        mode="default"
      />
      <Editor
        style={{ height: 320, overflowY: "hidden" }}
        defaultConfig={editorConfig}
        value={html}
        onCreated={setEditor}
        onChange={(nextEditor) => onChange(nextEditor.getHtml())}
        mode="default"
      />
    </div>
  );
}
