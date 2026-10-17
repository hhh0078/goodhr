// 文件作用说明：统一鼠标速度档位和原生轨迹总时长区间，供启动校验与浏览器内部偏好复用。

import type { MouseSpeed } from "../../contracts/actions.js";
export type { MouseSpeed } from "../../contracts/actions.js";

/** MOUSE_SPEED_DURATIONS 保存完整原生轨迹的计划耗时，单位为毫秒。 */
export const MOUSE_SPEED_DURATIONS: Readonly<Record<MouseSpeed, readonly [number, number]>> = {
  ultra_fast: [90, 210],
  fast: [180, 360],
  standard: [350, 650],
  relaxed: [650, 1100],
  slow: [1100, 1800],
};

/** isMouseSpeed 判断外部值是否为支持的鼠标速度档位。 */
export function isMouseSpeed(value: unknown): value is MouseSpeed {
  return typeof value === "string" && Object.prototype.hasOwnProperty.call(MOUSE_SPEED_DURATIONS, value);
}

/** normalizeMouseSpeed 为旧版请求和缺失配置补齐更快的默认档位。 */
export function normalizeMouseSpeed(value: unknown): MouseSpeed {
  return isMouseSpeed(value) ? value : "fast";
}

/** mouseSpeedPreferences 返回浏览器内部原生输入补丁使用的上下限，不接触招聘页面。 */
export function mouseSpeedPreferences(speed: MouseSpeed): Record<string, number> {
  const [minimum, maximum] = MOUSE_SPEED_DURATIONS[speed];
  return { "goodhr.mouse.min_ms": minimum, "goodhr.mouse.max_ms": maximum };
}
