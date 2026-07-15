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
