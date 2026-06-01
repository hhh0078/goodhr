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

type KeywordLine = Container & {
  direction: number;
  speed: number;
  wordGap: number;
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
export function mountKeywordCanvasBackgrounds(selector = "[data-keyword-canvas]", options: KeywordCanvasOptions = {}) {
  document.querySelectorAll<HTMLElement>(selector).forEach((host) => {
    createKeywordCanvasBackground(host, options);
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
    const line = new pixi.Container() as KeywordLine;
    line.alpha = (index % 2 === 0 ? 0.38 : 0.32) * config.opacity;
    line.rotation = -0.14;
    line.eventMode = "none";
    line.direction = index % 2 === 0 ? -1 : 1;
    line.speed = config.speed + index * 0.035;
    line.wordGap = 34;
    buildKeywordLineWords(pixi, line, row, index);
    stage.addChild(line);
  }
  return stage;
}

/**
 * 为单行创建多个短词文本，避免生成超宽纹理。
 *
 * @param pixi - PixiJS 运行时模块。
 * @param line - 当前关键词行容器。
 * @param row - 当前行关键词。
 * @param index - 当前行序号。
 */
function buildKeywordLineWords(
  pixi: typeof import("pixi.js"),
  line: KeywordLine,
  row: string[],
  index: number,
) {
  const words = Array.from({ length: 8 }, () => row).flat();
  words.forEach((word) => {
    const item = new pixi.Text({
      text: word,
      style: {
        fill: index % 2 === 0 ? "#174a17" : "#2a332a",
        fontFamily: "Arial, Helvetica, sans-serif",
        fontSize: 72,
        fontWeight: "700",
        letterSpacing: 0,
      },
    });
    item.eventMode = "none";
    line.addChild(item);
  });
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
  const verticalPadding = fontSize * 1.1;
  const gap = (height + verticalPadding * 2) / Math.max(stage.children.length - 1, 1);

  stage.children.forEach((child, index) => {
    const line = child as KeywordLine;
    line.wordGap = Math.max(30, fontSize * 0.42);
    layoutKeywordLineWords(line, fontSize, line.wordGap, width);
    line.x = 0;
    line.y = -verticalPadding + index * gap;
  });
}

/**
 * 重新排列单行里的每个词。
 *
 * @param line - 当前关键词行容器。
 * @param fontSize - 当前字号。
 * @param wordGap - 词语之间的间距。
 * @param viewportWidth - 当前画布宽度。
 */
function layoutKeywordLineWords(line: KeywordLine, fontSize: number, wordGap: number, viewportWidth: number) {
  const words = line.children as Text[];
  if (words.length === 0) return;

  let cursor = line.direction < 0 ? -viewportWidth * 0.08 : viewportWidth * 1.08;
  line.children.forEach((child) => {
    const item = child as Text;
    item.style.fontSize = fontSize;
    item.x = cursor;
    item.y = 0;
    cursor += line.direction < 0 ? item.width + wordGap : -(item.width + wordGap);
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
  stage.children.forEach((child) => {
    const line = child as KeywordLine;
    const speed = line.speed * ticker.deltaTime;
    recycleKeywordLineWords(line, width, speed);
  });
}

/**
 * 循环回收单行里的词，避免整行跑完后出现空白。
 *
 * @param line - 当前关键词行容器。
 * @param viewportWidth - 当前画布宽度。
 * @param speed - 当前帧移动距离。
 */
function recycleKeywordLineWords(line: KeywordLine, viewportWidth: number, speed: number) {
  const words = line.children as Text[];
  if (words.length === 0) return;

  words.forEach((word) => {
    word.x += speed * line.direction;
  });

  if (line.direction < 0) {
    let rightMost = Math.max(...words.map((word) => word.x + word.width));
    words.forEach((word) => {
      if (word.x + word.width < -viewportWidth * 0.18) {
        word.x = rightMost + line.wordGap;
        rightMost = word.x + word.width;
      }
    });
    return;
  }

  let leftMost = Math.min(...words.map((word) => word.x));
  words.forEach((word) => {
    if (word.x > viewportWidth * 1.18) {
      word.x = leftMost - word.width - line.wordGap;
      leftMost = word.x;
    }
  });
}
