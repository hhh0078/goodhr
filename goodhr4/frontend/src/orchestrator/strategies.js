/**
 * 策略对象定义
 *
 * 每个策略对象定义了筛选行为的四个决策点：
 * - coarseFilter：粗筛（看卡片基本信息，决定是否打开详情）
 * - fineFilter：精筛（看完整信息，决定是否打招呼）
 * - fallbackFilter：粗筛未通过时的兜底筛选
 * - needsDetailPage：是否需要打开详情页
 *
 * 主流程根据策略驱动，平台差异全部封装在策略内，流程中不出现 if(platform)
 */

import { requestAIDecision } from "./ai.js";

/**
 * 默认策略：标准两阶段筛选（粗筛+精筛都过AI）
 */
const DEFAULT_STRATEGY = {
  /**
   * 粗筛：基于卡片基本信息，决定是否值得打开详情
   * @param {object} ctx - 流程管理器实例
   * @param {string} candidateInfo - 候选人基本信息文本
   * @returns {Promise<{pass: boolean, reason: string}>}
   */
  async coarseFilter(ctx, candidateInfo) {
    const result = await requestAIDecision(ctx, candidateInfo);
    return {
      pass: result.isok,
      reason: result.msg + `(-￥${result.cost})`,
    };
  },

  /**
   * 精筛：打开详情后，基于完整信息决定是否打招呼
   * @param {object} ctx - 流程管理器实例
   * @param {string} candidateInfo - 候选人完整信息文本
   * @returns {Promise<{pass: boolean, reason: string}>}
   */
  async fineFilter(ctx, candidateInfo) {
    const result = await requestAIDecision(ctx, candidateInfo);
    return {
      pass: result.isok,
      reason: result.msg + `(-￥${result.cost})`,
    };
  },

  /**
   * 粗筛未通过时的兜底决策
   * @returns {Promise<boolean>}
   */
  async fallbackFilter() {
    return false;
  },

  /**
   * 是否需要打开详情页
   * @returns {boolean}
   */
  needsDetailPage() {
    return true;
  },
};

/**
 * 免费模式策略：概率粗筛 + 关键词精筛
 */
const FREE_STRATEGY = {
  async coarseFilter(ctx, candidateInfo) {
    const result = await ctx._sendCommand({ action: "SHOULD_CLICK" });
    const pass = result?.shouldClick || false;
    return { pass, reason: pass ? "概率通过" : "概率未通过" };
  },

  async fineFilter(ctx, candidateInfo) {
    const result = await ctx._sendCommand({
      action: "FILTER_CANDIDATE",
      data: { candidateInfo },
    });
    return {
      pass: result?.pass || false,
      reason: result?.pass ? "关键词匹配" : "关键词不匹配",
    };
  },

  async fallbackFilter(ctx, candidateInfo) {
    const result = await ctx._sendCommand({
      action: "FILTER_CANDIDATE",
      data: { candidateInfo },
    });
    return result?.pass || false;
  },

  needsDetailPage() {
    return true;
  },
};

/**
 * Boss(AI)策略：AI粗筛后直接打招呼，跳过精筛
 * Boss通过API拦截已获取完整信息，不需要二次AI
 */
const BOSS_AI_STRATEGY = {
  async coarseFilter(ctx, candidateInfo) {
    const result = await requestAIDecision(ctx, candidateInfo);
    return {
      pass: result.isok,
      reason: result.msg + `(-￥${result.cost})`,
    };
  },

  async fineFilter() {
    return { pass: true, reason: "Boss信息充足，跳过精筛" };
  },

  async fallbackFilter() {
    return false;
  },

  needsDetailPage() {
    return false;
  },
};

/**
 * 58同城策略：不做筛选，直接打招呼
 */
const EMPLOYER58_STRATEGY = {
  async coarseFilter() {
    return { pass: true, reason: "58无法筛选" };
  },

  async fineFilter() {
    return { pass: true, reason: "58无法筛选" };
  },

  async fallbackFilter() {
    return true;
  },

  needsDetailPage() {
    return false;
  },
};

/**
 * 根据平台名和模式选择策略
 * @param {string} parserName - 平台标识
 * @param {boolean} aiMode - 是否AI模式
 * @returns {object} 策略对象
 */
export function resolveStrategy(parserName, aiMode) {
  if (parserName === "employer58") return EMPLOYER58_STRATEGY;
  if (parserName === "boos" && aiMode) return BOSS_AI_STRATEGY;
  if (aiMode) return DEFAULT_STRATEGY;
  return FREE_STRATEGY;
}
