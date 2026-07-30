/** 本文件负责在浏览器原生置顶小窗中重点展示 AI、关键词和任务运行状态。 */
"use client";

import { useEffect } from "react";
import { createPortal } from "react-dom";

export type PositionFloatingStatusValue = "running" | "stopped";

export type PositionAnalysisStatus = {
  kind: "ai" | "keyword";
  phase: "loading" | "result" | "error";
  candidate_name: string;
  score?: number;
  threshold?: number;
  accepted?: boolean;
  reason: string;
  keywords?: string[];
  matched_keywords?: string[];
  exclude_keywords?: string[];
  matched_excludes?: string[];
  updated_at: string;
};

type PositionFloatingStatusProps = {
  pipWindow: Window | null;
  status: PositionFloatingStatusValue;
  currentStep: string;
  analysis: PositionAnalysisStatus | null;
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
      width: 380,
      height: 320,
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
      minWidth: "260px",
      minHeight: "180px",
      background: "#edf5f0",
    });
    return pipWindow;
  } catch {
    return null;
  }
}

/** analysisTitle 返回分析阶段对应的短标题。 */
function analysisTitle(analysis: PositionAnalysisStatus | null) {
  if (!analysis) return "等待分析";
  if (analysis.kind === "keyword") return "关键词匹配";
  if (analysis.phase === "loading") return "AI 请求中";
  if (analysis.phase === "error") return "AI 没跑顺";
  return "AI 判断结果";
}

/** PositionFloatingStatus 渲染全平台共用的分析结果、状态和本次统计。 */
export default function PositionFloatingStatus({
  pipWindow,
  status,
  currentStep,
  analysis,
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
  const step = isRunning ? currentStep || "正在准备下一步" : "任务已停止";
  const acceptedLabel =
    analysis?.accepted === undefined
      ? ""
      : analysis.accepted
        ? "通过"
        : "跳过";
  const matchedKeywords = analysis?.matched_keywords || [];
  const matchedExcludes = analysis?.matched_excludes || [];
  const keywords = analysis?.keywords || [];

  return createPortal(
    <main
      style={{
        boxSizing: "border-box",
        display: "flex",
        width: "100vw",
        minHeight: "100vh",
        padding: 9,
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
          gap: 8,
          padding: "11px 13px",
          overflow: "hidden",
          border: "1px solid rgba(255, 255, 255, 0.24)",
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
          <span style={{ opacity: 0.8 }}>GoodHR · 招聘小助手</span>
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
          style={{
            display: "flex",
            minHeight: 0,
            flex: 1,
            flexDirection: "column",
            gap: 8,
            padding: "10px 11px",
            overflow: "hidden",
            borderRadius: 7,
            background: "rgba(255, 255, 255, 0.14)",
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 8,
            }}
          >
            <strong style={{ fontSize: 15 }}>{analysisTitle(analysis)}</strong>
            {acceptedLabel ? (
              <span
                style={{
                  flexShrink: 0,
                  padding: "3px 7px",
                  borderRadius: 5,
                  background: analysis?.accepted
                    ? "rgba(218, 255, 232, 0.22)"
                    : "rgba(255, 225, 225, 0.22)",
                  fontSize: 12,
                  fontWeight: 700,
                }}
              >
                {acceptedLabel}
              </span>
            ) : null}
          </div>

          <div
            title={analysis?.candidate_name || ""}
            style={{
              overflow: "hidden",
              fontSize: 12,
              opacity: 0.82,
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {analysis?.candidate_name || "还没轮到候选人，我先安静待命"}
          </div>

          {analysis?.kind === "ai" &&
          analysis.phase === "result" &&
          typeof analysis.score === "number" ? (
            <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
              <strong style={{ fontSize: 30, lineHeight: 1 }}>
                {analysis.score.toFixed(1)}
              </strong>
              <span style={{ fontSize: 12, opacity: 0.82 }}>
                分
                {typeof analysis.threshold === "number"
                  ? ` · 阈值 ${analysis.threshold.toFixed(1)}`
                  : ""}
              </span>
            </div>
          ) : null}

          {analysis?.phase === "loading" ? (
            <div style={{ fontSize: 18, fontWeight: 700 }}>
              正在分析<span style={{ opacity: 0.65 }}> ···</span>
            </div>
          ) : null}

          {analysis?.kind === "keyword" ? (
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                gap: 4,
                fontSize: 12,
                lineHeight: 1.35,
              }}
            >
              <span>
                命中：{matchedKeywords.length ? matchedKeywords.join("、") : "暂无"}
              </span>
              {matchedExcludes.length ? (
                <span>排除词：{matchedExcludes.join("、")}</span>
              ) : null}
              {!matchedKeywords.length && keywords.length ? (
                <span style={{ opacity: 0.78 }}>需要：{keywords.join("、")}</span>
              ) : null}
            </div>
          ) : null}

          <div
            title={analysis?.reason || ""}
            style={{
              overflow: "auto",
              fontSize: 13,
              lineHeight: 1.5,
              opacity: 0.96,
            }}
          >
            {analysis?.reason || "分析结果会留在这里，不会被普通操作日志挤走。"}
          </div>
        </div>

        <div
          aria-label="本次任务统计"
          style={{
            display: "flex",
            justifyContent: "space-between",
            gap: 8,
            padding: "0 2px",
            fontSize: 11,
            opacity: 0.86,
          }}
        >
          <span>扫描 {Math.max(0, scannedCount || 0)}</span>
          <span>打招呼 {Math.max(0, greetedCount || 0)}</span>
          <span>跳过 {Math.max(0, skippedCount || 0)}</span>
        </div>
        <div
          title={step}
          style={{
            overflow: "hidden",
            fontSize: 10,
            lineHeight: 1.3,
            opacity: 0.72,
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          当前：{step}
        </div>
      </section>
    </main>,
    pipWindow.document.body,
  );
}
