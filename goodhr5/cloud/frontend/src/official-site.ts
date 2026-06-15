// 本文件负责官网静态页面的前端增强逻辑。

import { cloudApiBase } from "./services/apiClient";
import { mountKeywordCanvasBackgrounds } from "./utils/keywordCanvasBackground";

mountKeywordCanvasBackgrounds("[data-keyword-canvas]", {
  rowCount: 16,
  speed: 1.46,
  minFontSize: 46,
  maxFontSize: 112,
  fontScale: 0.082,
  opacity: 0.92,
});

loadPublicTodayStats();

/**
 * 加载官网首页公开统计数据。
 * @returns {Promise<void>} 无返回值。
 */
async function loadPublicTodayStats() {
  const root = document.querySelector<HTMLElement>("[data-public-stats]");
  if (!root) return;
  try {
    const res = await fetch(`${cloudApiBase()}/api/public/stats/today`, {
      cache: "no-store",
    });
    if (!res.ok) return;
    const data = await res.json();
    setStatText(root, "today_greeted_count", data.today_greeted_count);
    setStatText(root, "today_registered_count", data.today_registered_count);
  } catch {
    // 官网统计不影响首页主体展示。
  }
}

/**
 * 设置官网统计数字。
 * @param {HTMLElement} root - 统计根元素。
 * @param {string} name - 统计字段名。
 * @param {unknown} value - 后端返回的统计值。
 * @returns {void} 无返回值。
 */
function setStatText(root: HTMLElement, name: string, value: unknown) {
  const target = root.querySelector<HTMLElement>(`[data-stat-field="${name}"]`);
  if (!target) return;
  const numberValue = Number(value || 0);
  target.textContent = Number.isFinite(numberValue)
    ? numberValue.toLocaleString("zh-CN")
    : "--";
}
