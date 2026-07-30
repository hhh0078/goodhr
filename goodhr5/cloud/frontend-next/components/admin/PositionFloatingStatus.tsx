/** 本文件负责在浏览器原生置顶小窗中展示岗位运行状态。 */
"use client";

import { useEffect } from "react";
import { createPortal } from "react-dom";

export type PositionFloatingStatusValue = "running" | "stopped";

type PositionFloatingStatusProps = {
  pipWindow: Window | null;
  positionName: string;
  status: PositionFloatingStatusValue;
  onClosed: () => void;
};

type DocumentPictureInPictureAPI = {
  window: Window | null;
  requestWindow: (options?: {
    width?: number;
    height?: number;
  }) => Promise<Window>;
};

/**
 * openPositionFloatingWindow 创建或复用浏览器原生置顶状态窗。
 * 浏览器不支持该能力或用户关闭授权时返回 null，不影响岗位正常启动。
 */
export async function openPositionFloatingWindow() {
  const browserWindow = window as Window & {
    documentPictureInPicture?: DocumentPictureInPictureAPI;
  };
  const pictureInPicture = browserWindow.documentPictureInPicture;
  if (!pictureInPicture) return null;

  const existingWindow = pictureInPicture.window;
  if (existingWindow && !existingWindow.closed) {
    existingWindow.focus();
    return existingWindow;
  }

  try {
    const pipWindow = await pictureInPicture.requestWindow({
      width: 320,
      height: 180,
    });
    const viewport = pipWindow.document.createElement("meta");
    viewport.name = "viewport";
    viewport.content = "width=device-width, initial-scale=1";
    pipWindow.document.head.replaceChildren(viewport);
    pipWindow.document.title = "GoodHR 任务状态";
    pipWindow.document.documentElement.lang = "zh-CN";
    pipWindow.document.body.replaceChildren();
    Object.assign(pipWindow.document.body.style, {
      margin: "0",
      minWidth: "240px",
      minHeight: "140px",
      background: "#edf5f0",
    });
    return pipWindow;
  } catch {
    return null;
  }
}

/** PositionFloatingStatus 渲染岗位名称以及运行中、已停止两种状态。 */
export default function PositionFloatingStatus({
  pipWindow,
  positionName,
  status,
  onClosed,
}: PositionFloatingStatusProps) {
  useEffect(() => {
    if (!pipWindow) return undefined;

    /** handleWindowClosed 在用户关闭置顶小窗后清理主页面中的窗口引用。 */
    function handleWindowClosed() {
      onClosed();
    }

    pipWindow.addEventListener("pagehide", handleWindowClosed);
    return () => pipWindow.removeEventListener("pagehide", handleWindowClosed);
  }, [onClosed, pipWindow]);

  if (!pipWindow || pipWindow.closed) return null;

  const isRunning = status === "running";
  return createPortal(
    <main
      style={{
        boxSizing: "border-box",
        display: "flex",
        width: "100vw",
        minHeight: "100vh",
        padding: 12,
        background: isRunning ? "#edf5f0" : "#f1f3f2",
        color: "#20352a",
        fontFamily:
          '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
      }}
    >
      <section
        aria-live="polite"
        style={{
          boxSizing: "border-box",
          display: "flex",
          flex: 1,
          minWidth: 0,
          flexDirection: "column",
          justifyContent: "space-between",
          padding: "16px 18px",
          overflow: "hidden",
          border: `1px solid ${isRunning ? "#bfd8c9" : "#d2d9d5"}`,
          borderRadius: 8,
          background: "#ffffff",
          boxShadow: "0 8px 24px rgba(34, 67, 49, 0.10)",
        }}
      >
        <div
          style={{
            color: "#718078",
            fontSize: 12,
            fontWeight: 650,
            letterSpacing: "0.04em",
          }}
        >
          GoodHR · 任务状态
        </div>
        <div
          title={positionName}
          style={{
            maxWidth: "100%",
            overflow: "hidden",
            color: "#53635a",
            fontSize: 14,
            lineHeight: 1.5,
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {positionName || "当前岗位"}
        </div>
        <strong
          style={{
            color: isRunning ? "#2f7d54" : "#5d6962",
            fontSize: 34,
            fontWeight: 780,
            lineHeight: 1,
            letterSpacing: "-0.04em",
          }}
        >
          {isRunning ? "运行中" : "已停止"}
        </strong>
      </section>
    </main>,
    pipWindow.document.body,
  );
}
