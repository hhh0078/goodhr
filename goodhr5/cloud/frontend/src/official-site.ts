// 本文件负责官网静态页面的前端增强逻辑。

import { mountKeywordCanvasBackgrounds } from "./utils/keywordCanvasBackground";

mountKeywordCanvasBackgrounds("[data-keyword-canvas]", {
  rowCount: 16,
  speed: 1.46,
  minFontSize: 46,
  maxFontSize: 112,
  fontScale: 0.082,
  opacity: 0.92,
});
