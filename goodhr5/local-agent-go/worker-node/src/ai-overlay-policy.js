// 本文件负责统一 AI 浮层的候选人身份识别策略。

/**
 * aiOverlayMatchKey 返回 AI 浮层复用卡片时使用的候选人标识。
 * @param {string} title - 浮层标题。
 * @param {string} subtitle - 候选人名称。
 * @returns {string} 浮层匹配标识。
 */
export function aiOverlayMatchKey(title, subtitle) {
  const candidateName = String(subtitle || "").trim();
  return candidateName || String(title || "").trim();
}

/**
 * normalizeAIOverlayMaxAge 返回 AI 或休息浮层允许保留的安全时长。
 * @param {unknown} value - 调用方传入的毫秒数。
 * @returns {number} 介于 3 秒和 12 小时之间的毫秒数。
 */
export function normalizeAIOverlayMaxAge(value) {
  const parsed = Number(value || 15000);
  if (!Number.isFinite(parsed)) return 15000;
  return Math.max(3000, Math.min(12 * 60 * 60 * 1000, parsed));
}
