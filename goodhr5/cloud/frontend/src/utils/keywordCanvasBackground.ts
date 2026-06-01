// 本文件负责渲染 GoodHR 登录页和官网共用的高性能关键词动态背景。

import type { Application, Container, Text } from "pixi.js";

export type KeywordCanvasBackground = {
  destroy: () => void;
};

type KeywordCanvasOptions = {
  rows?: string[][];
  rowCount?: number;
  speed?: number;
  minFontSize?: number;
  maxFontSize?: number;
  fontScale?: number;
  opacity?: number;
};

const DEFAULT_ROWS = [
  ["招聘", "候选人", "简历", "打招呼", "沟通", "面试", "筛选", "匹配"],
  ["Boss直聘", "猎聘", "智联", "58同城", "HR", "岗位模板", "AI评分"],
  ["自动筛选", "自动打招呼", "人才库", "回复率", "复聊", "跟进", "Offer"],
  ["薪资", "经验", "学历", "城市", "活跃候选人", "高匹配", "已沟通"],
  ["今日打招呼", "跳过原因", "查看详情", "推荐列表", "招聘效率", "沟通记录"],
  ["AI判断", "匹配分", "已扫描", "已跳过", "已回复", "待跟进", "高意向"],
  ["成都招聘", "销售", "客服", "运营", "老师", "开发", "人事"],
  ["自动化", "批量沟通", "精准筛选", "快速开聊", "职位匹配", "人才发现"],
];

/**
 * 创建关键词动态背景。
 *
 * @param host - 承载 canvas 的 HTML 元素。
 * @param options - 背景密度、速度和字号配置。
 * @returns 背景销毁句柄。
 */
export async function createKeywordCanvasBackground(
  host: HTMLElement,
  options: KeywordCanvasOptions = {},
): Promise<KeywordCanvasBackground | null> {
  if (!host) return null;

  let disposed = false;
  let resizeTimer = 0;
  const pixi = await import("pixi.js");
  if (disposed || !host.isConnected) return null;

  const app = new pixi.Application();
  await app.init({
    resizeTo: host,
    backgroundAlpha: 0,
    antialias: true,
    autoDensity: true,
    resolution: Math.min(window.devicePixelRatio || 1, 2),
    powerPreference: "high-performance",
  });
  if (!host.isConnected) {
    app.destroy(true);
    return null;
  }

  const config = normalizeOptions(options);
  const stage = buildKeywordRows(pixi, config);
  app.canvas.className = "keyword-canvas";
  app.stage.addChild(stage);
  host.appendChild(app.canvas);

  const layout = () => layoutKeywordRows(app, stage, config);
  const tick = (ticker: { deltaTime: number }) => moveKeywordRows(app, stage, config, ticker);
  const scheduleLayout = () => {
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(layout, 120);
  };

  layout();
  app.ticker.add(tick);
  window.addEventListener("resize", scheduleLayout);

  return {
    destroy: () => {
      disposed = true;
      window.clearTimeout(resizeTimer);
      window.removeEventListener("resize", scheduleLayout);
      app.ticker.remove(tick);
      app.destroy(true);
    },
  };
}

/**
 * 初始化页面中声明式配置的关键词背景。
 *
 * @param selector - 需要初始化的背景容器选择器。
 */
export function mountKeywordCanvasBackgrounds(selector = "[data-keyword-canvas]") {
  document.querySelectorAll<HTMLElement>(selector).forEach((host) => {
    createKeywordCanvasBackground(host);
  });
}

/**
 * 合并关键词背景默认配置。
 *
 * @param options - 外部传入配置。
 * @returns 完整配置。
 */
function normalizeOptions(options: KeywordCanvasOptions) {
  return {
    rows: options.rows?.length ? options.rows : DEFAULT_ROWS,
    rowCount: options.rowCount || 15,
    speed: options.speed || 1.28,
    minFontSize: options.minFontSize || 44,
    maxFontSize: options.maxFontSize || 106,
    fontScale: options.fontScale || 0.078,
    opacity: options.opacity || 1,
  };
}

/**
 * 构建 Pixi 关键词文本行。
 *
 * @param pixi - PixiJS 运行时模块。
 * @param config - 背景完整配置。
 * @returns Pixi 容器。
 */
function buildKeywordRows(pixi: typeof import("pixi.js"), config: ReturnType<typeof normalizeOptions>) {
  const stage = new pixi.Container();
  for (let index = 0; index < config.rowCount; index += 1) {
    const row = config.rows[index % config.rows.length];
    const text = Array.from({ length: 5 }, () => row.join("   ")).join("      ");
    const line = new pixi.Text({
      text,
      style: {
        fill: index % 2 === 0 ? "#174a17" : "#2a332a",
        fontFamily: "Arial, Helvetica, sans-serif",
        fontSize: 72,
        fontWeight: "700",
        letterSpacing: 0,
      },
    });
    line.alpha = (index % 2 === 0 ? 0.38 : 0.32) * config.opacity;
    line.rotation = -0.14;
    line.eventMode = "none";
    stage.addChild(line);
  }
  return stage;
}

/**
 * 按容器尺寸重新排列关键词行。
 *
 * @param app - Pixi 应用实例。
 * @param stage - Pixi 关键词行容器。
 * @param config - 背景完整配置。
 */
function layoutKeywordRows(
  app: Application,
  stage: Container,
  config: ReturnType<typeof normalizeOptions>,
) {
  const width = app.screen.width;
  const height = app.screen.height;
  const fontSize = Math.max(config.minFontSize, Math.min(config.maxFontSize, width * config.fontScale));
  const gap = Math.max(44, height / Math.max(stage.children.length - 2, 1));

  stage.children.forEach((child, index) => {
    const line = child as Text;
    line.style.fontSize = fontSize;
    line.x = index % 2 === 0 ? width * 0.16 : -width * 0.6;
    line.y = -height * 0.36 + index * gap;
  });
}

/**
 * 推动关键词行逐帧滚动。
 *
 * @param app - Pixi 应用实例。
 * @param stage - Pixi 关键词行容器。
 * @param config - 背景完整配置。
 * @param ticker - Pixi 当前帧信息。
 */
function moveKeywordRows(
  app: Application,
  stage: Container,
  config: ReturnType<typeof normalizeOptions>,
  ticker: { deltaTime: number },
) {
  const width = app.screen.width;
  stage.children.forEach((child, index) => {
    const line = child as Text;
    const direction = index % 2 === 0 ? -1 : 1;
    const speed = (config.speed + index * 0.035) * ticker.deltaTime;
    line.x += speed * direction;
    if (direction < 0 && line.x < -line.width * 0.58) {
      line.x = width * 0.2;
    }
    if (direction > 0 && line.x > width * 0.22) {
      line.x = -line.width * 0.58;
    }
  });
}
