/** 文件作用说明：统一岗位固定详情策略，旧OCR草稿按平台兼容，不再向用户展示识别方式选择。 */

/** positionDetailMode 返回BOSS固定AI、其它平台固定DOM的详情读取方式。 */
export function positionDetailMode(platformId: unknown): "ai" | "dom" {
  return String(platformId || "boss").trim().toLowerCase() === "boss" ? "ai" : "dom";
}
