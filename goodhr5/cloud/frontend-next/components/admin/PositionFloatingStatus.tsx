/** 本文件负责在浏览器原生置顶小窗中展示岗位运行状态。 */
"use client";

import { useEffect } from "react";
import { createPortal } from "react-dom";

export type PositionFloatingStatusValue = "running" | "stopped";

type PositionFloatingStatusProps = {
  pipWindow: Window | null;
  positionName: string;
  status: PositionFloatingStatusValue;
  currentStep: string;
  scannedCount: number;
  greetedCount: number;
  skippedCount: number;
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
      width: 360,
      height: 230,
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

/** PositionFloatingStatus 渲染全平台共用的岗位状态、当前步骤和本次统计。 */
export default function PositionFloatingStatus({
  pipWindow,
  positionName,
  status,
  currentStep,
  scannedCount,
  greetedCount,
  skippedCount,
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
  const background = isRunning ? "#2f7d54" : "#b34343";
  const step = isRunning
    ? currentStep || "正在准备下一步"
    : "任务已停止";
  const stats = [
    { label: "扫描", value: scannedCount },
    { label: "打招呼", value: greetedCount },
    { label: "跳过", value: skippedCount },
  ];
  return createPortal(
    <main
      style={{
        boxSizing: "border-box",
        display: "flex",
        width: "100vw",
        minHeight: "100vh",
        padding: 10,
        background,
        color: "#ffffff",
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
          gap: 10,
          padding: "14px 16px",
          overflow: "hidden",
          border: "1px solid rgba(255, 255, 255, 0.26)",
          borderRadius: 8,
          background,
          boxShadow: "0 8px 24px rgba(30, 35, 32, 0.18)",
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            fontSize: 12,
          }}
        >
          <span style={{ opacity: 0.78 }}>GoodHR · 任务状态</span>
          <strong
            style={{
              padding: "4px 8px",
              borderRadius: 6,
              background: "rgba(255, 255, 255, 0.17)",
              fontSize: 13,
            }}
          >
            {isRunning ? "运行中" : "已停止"}
          </strong>
        </div>
        <div
          title={positionName}
          style={{
            maxWidth: "100%",
            overflow: "hidden",
            fontSize: 17,
            fontWeight: 720,
            lineHeight: 1.35,
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {positionName || "当前岗位"}
        </div>
        <div
          title={step}
          style={{
            minHeight: 40,
            padding: "8px 10px",
            overflow: "hidden",
            borderRadius: 6,
            background: "rgba(255, 255, 255, 0.13)",
            fontSize: 13,
            lineHeight: 1.55,
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          {step}
        </div>
        <div
          aria-label="本次任务统计"
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
            gap: 8,
          }}
        >
          {stats.map((item) => (
            <div
              key={item.label}
              style={{
                padding: "6px 8px",
                borderRadius: 6,
                background: "rgba(255, 255, 255, 0.11)",
                textAlign: "center",
              }}
            >
              <strong style={{ display: "block", fontSize: 18 }}>
                {Math.max(0, item.value || 0)}
              </strong>
              <span style={{ fontSize: 11, opacity: 0.78 }}>
                本次{item.label}
              </span>
            </div>
          ))}
        </div>
      </section>
    </main>,
    pipWindow.document.body,
  );
}
