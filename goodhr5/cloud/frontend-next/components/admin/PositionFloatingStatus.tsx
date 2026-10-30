/** 本文件负责在浏览器原生置顶小窗中重点展示 AI、关键词和任务运行状态。 */
"use client";

import { StyleProvider, createCache } from "@ant-design/cssinjs";
import { ConfigProvider, Flex, Spin, Statistic, Tag, Typography } from "antd";
import { useEffect, useMemo } from "react";
import { createPortal } from "react-dom";

export type PositionFloatingStatusValue =
  | "running"
  | "completed"
  | "stopped"
  | "failed";

export type PositionAnalysisStatus = {
  kind: "ai" | "keyword" | "auto_reply";
  phase: "loading" | "result" | "error";
  stage?: "preview" | "final";
  terminal: boolean;
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
  /** 自动回复复用同一小窗，但使用会话和回复统计文案。 */
  autoReply?: boolean;
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

const STATUS_PRESENTATION: Record<
  PositionFloatingStatusValue,
  { background: string; label: string; step: string }
> = {
  running: {
    background: "#2f7d54",
    label: "运行中",
    step: "正在准备下一步",
  },
  completed: {
    background: "#3f6f5b",
    label: "已完成",
    step: "这轮候选人已经处理完成",
  },
  stopped: {
    background: "#b34343",
    label: "已停止",
    step: "任务已按你的要求停止",
  },
  failed: {
    background: "#8f2f2f",
    label: "运行失败",
    step: "这次没跑顺，请回岗位日志看看原因",
  },
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
      width: 340,
      height: 250,
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
      background: "#f5f5f2",
    });
    return pipWindow;
  } catch {
    return null;
  }
}

/** PositionFloatingStatus 渲染全平台共用的分析结果、状态和本次统计。 */
export default function PositionFloatingStatus({
  autoReply = false,
  pipWindow,
  status,
  currentStep,
  analysis,
  scannedCount,
  greetedCount,
  skippedCount,
  onClosed,
}: PositionFloatingStatusProps) {
  // 每个独立窗口使用自己的样式缓存，不能复用主页面已注入的样式记录。
  const styleCache = useMemo(() => createCache(), [pipWindow]);
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
  const presentation = STATUS_PRESENTATION[status];
  const background = presentation.background;
  const step = isRunning ? currentStep || presentation.step : presentation.step;
  const acceptedLabel =
    analysis?.accepted === undefined ? "" : analysis.accepted ? "通过" : "跳过";
  const statusLabel = acceptedLabel
    ? `${presentation.label} · ${acceptedLabel}`
    : presentation.label;
  const matchedKeywords = analysis?.matched_keywords || [];
  const matchedExcludes = analysis?.matched_excludes || [];
  const keywords = analysis?.keywords || [];
  const excludeKeywords = analysis?.exclude_keywords || [];

  return createPortal(
    <StyleProvider cache={styleCache} container={pipWindow.document.head}>
      <ConfigProvider
        // 独立前缀避免复用主页面服务端样式缓存，否则小窗可能只注入主题而漏掉组件样式。
        prefixCls="ant-pip"
        componentSize="small"
        theme={{
          inherit: false,
          cssVar: { prefix: "ant-pip", key: "goodhr-floating-window" },
          token: {
            colorPrimary: "#ffffff",
            colorText: "#ffffff",
            colorTextSecondary: "rgba(255, 255, 255, 0.82)",
            colorBgContainer: background,
            colorBorder: "rgba(255, 255, 255, 0.24)",
            fontSize: 12,
            lineHeight: 1.35,
            borderRadius: 7,
            motion: true,
            fontFamily:
              '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
          },
          components: {
            Tag: {
              defaultBg: "rgba(255, 255, 255, 0.17)",
              defaultColor: "#ffffff",
            },
            Statistic: { contentFontSize: 24 },
          },
        }}
      >
        <Flex
          component="main"
          style={{
            boxSizing: "border-box",
            display: "flex",
            width: "100vw",
            height: "100vh",
            minHeight: 180,
            padding: 7,
            background,
            color: "#ffffff",
            fontFamily:
              '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
          }}
        >
          <Flex
            component="section"
            vertical
            gap={7}
            aria-live="polite"
            style={{
              boxSizing: "border-box",
              display: "flex",
              flex: 1,
              minWidth: 0,
              padding: "9px 11px",
              overflow: "hidden",
              border: "1px solid rgba(255, 255, 255, 0.24)",
              borderRadius: 8,
              background,
              boxShadow: "0 8px 24px rgba(30, 35, 32, 0.18)",
            }}
          >
            <Flex
              align="center"
              justify="space-between"
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                fontSize: 12,
              }}
            >
              <Typography.Text style={{ opacity: 0.8 }}>
                GoodHR · 招聘小助手
              </Typography.Text>
              <Tag
                variant="filled"
                style={{
                  padding: "3px 7px",
                  borderRadius: 6,
                  background: "rgba(255, 255, 255, 0.17)",
                  fontSize: 12,
                  lineHeight: 1.35,
                  marginInlineEnd: 0,
                  fontWeight: 700,
                  flexShrink: 0,
                }}
              >
                {statusLabel}
              </Tag>
            </Flex>

            <Flex
              vertical
              gap={6}
              style={{
                display: "flex",
                minHeight: 0,
                flex: "1 1 auto",
                padding: "9px 10px",
                overflow: "auto",
                borderRadius: 7,
                background: "rgba(255, 255, 255, 0.14)",
              }}
            >
              <Flex
                align="center"
                justify="space-between"
                gap={8}
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  gap: 8,
                  flexShrink: 0,
                }}
              >
                <Typography.Text
                  strong
                  title={analysis?.candidate_name || ""}
                  style={{
                    minWidth: 0,
                    overflow: "hidden",
                    fontSize: 18,
                    lineHeight: 1.35,
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  {analysis?.candidate_name || "等待候选人"}
                </Typography.Text>
                {analysis?.phase === "loading" ? (
                  <Typography.Text
                    style={{
                      flexShrink: 0,
                      fontSize: 12,
                      opacity: 0.82,
                    }}
                  >
                    我正在认真请求 AI
                  </Typography.Text>
                ) : null}
              </Flex>

              {analysis?.kind === "ai" &&
              analysis.phase === "result" &&
              typeof analysis.score === "number" ? (
                <Flex align="baseline" gap={8} style={{ flexShrink: 0 }}>
                  <Statistic
                    aria-label="AI 分数"
                    value={analysis.score}
                    precision={1}
                    styles={{ content: { fontWeight: 700, lineHeight: 1 } }}
                  />
                  <Typography.Text style={{ fontSize: 12, opacity: 0.82 }}>
                    分
                    {typeof analysis.threshold === "number"
                      ? ` · 阈值 ${analysis.threshold.toFixed(1)}`
                      : ""}
                  </Typography.Text>
                </Flex>
              ) : null}

              {analysis?.phase === "loading" ? (
                <Flex
                  align="center"
                  gap={9}
                  role="status"
                  aria-label="AI 正在分析候选人"
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 9,
                    fontSize: 15,
                    fontWeight: 700,
                    flexShrink: 0,
                  }}
                >
                  <Spin size="small" />
                  <Typography.Text strong style={{ fontSize: 15 }}>
                    {autoReply ? "正在整理回复" : "正在分析候选人"}
                  </Typography.Text>
                </Flex>
              ) : null}

              {analysis?.kind === "keyword" ? (
                <Flex
                  vertical
                  gap={4}
                  style={{
                    display: "flex",
                    fontSize: 12,
                    lineHeight: 1.35,
                    flexShrink: 0,
                  }}
                >
                  {keywords.length ? (
                    <Typography.Text
                      style={{ opacity: 0.82, overflowWrap: "anywhere" }}
                    >
                      需要：{keywords.join("、")}
                    </Typography.Text>
                  ) : null}
                  {excludeKeywords.length ? (
                    <Typography.Text
                      style={{ opacity: 0.82, overflowWrap: "anywhere" }}
                    >
                      排除：{excludeKeywords.join("、")}
                    </Typography.Text>
                  ) : null}
                  <Typography.Text style={{ overflowWrap: "anywhere" }}>
                    命中：
                    {matchedKeywords.length
                      ? matchedKeywords.join("、")
                      : "暂无"}
                  </Typography.Text>
                  {matchedExcludes.length ? (
                    <Typography.Text style={{ overflowWrap: "anywhere" }}>
                      命中排除：{matchedExcludes.join("、")}
                    </Typography.Text>
                  ) : null}
                </Flex>
              ) : null}

              <Typography.Paragraph
                title={analysis?.reason || ""}
                style={{
                  overflow: "auto",
                  maxHeight: 78,
                  fontSize: 13,
                  lineHeight: 1.45,
                  opacity: 0.96,
                  margin: 0,
                  flexShrink: 0,
                  overflowWrap: "anywhere",
                }}
              >
                {analysis?.reason ||
                  "分析结果会留在这里，不会被普通操作日志挤走。"}
              </Typography.Paragraph>
            </Flex>

            <Flex
              justify="space-between"
              gap={8}
              aria-label="本次任务统计"
              style={{
                display: "flex",
                justifyContent: "space-between",
                gap: 8,
                padding: "0 2px",
                fontSize: 12,
                opacity: 0.86,
                flexShrink: 0,
              }}
            >
              <Typography.Text>
                {autoReply ? "会话" : "扫描"} {Math.max(0, scannedCount || 0)}
              </Typography.Text>
              <Typography.Text>
                {autoReply ? "回复" : "打招呼"} {Math.max(0, greetedCount || 0)}
              </Typography.Text>
              <Typography.Text>
                跳过 {Math.max(0, skippedCount || 0)}
              </Typography.Text>
            </Flex>
            <Typography.Text
              title={step}
              style={{
                overflow: "hidden",
                fontSize: 13,
                fontWeight: 650,
                lineHeight: 1.35,
                opacity: 0.86,
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                flexShrink: 0,
              }}
            >
              当前：{step}
            </Typography.Text>
          </Flex>
        </Flex>
      </ConfigProvider>
    </StyleProvider>,
    pipWindow.document.body,
  );
}
