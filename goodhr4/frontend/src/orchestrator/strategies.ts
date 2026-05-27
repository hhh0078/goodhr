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
 *
 * 免费模式的关键词筛选在扩展侧完成，不再发消息到注入侧
 */

import { requestAIDecision } from "./ai.js";
import type { AIConfig } from "../constants/defaults.js";

/** 运行参数（与 orchestrator.ts 中 RunData 保持一致） */
export interface RunData {
  matchLimit: number;
  scrollDelayMin: number;
  scrollDelayMax: number;
  clickFrequency: number;
  enableSound: boolean;
  communicationEnabled?: boolean;
  communicationConfig?: any;
  positionName?: string;
  jobDescription?: string;
  aiConfig?: AIConfig;
  keywords?: string[];
  excludeKeywords?: string[];
  isAndMode?: boolean;
}

/** 筛选结果 */
export interface FilterResult {
  pass: boolean;
  reason: string;
}

/** 策略对象接口 */
export interface Strategy {
  coarseFilter: (ctx: any, candidateInfo: string) => Promise<FilterResult>;
  fineFilter: (ctx: any, candidateInfo: string) => Promise<FilterResult>;
  fallbackFilter: (ctx: any, candidateInfo: string) => Promise<boolean>;
  needsDetailPage: () => boolean;
}

/**
 * 关键词筛选（扩展侧）
 * 检查候选人信息是否包含指定关键词
 * @param info - 候选人信息文本
 * @param keywords - 关键词列表
 * @param excludeKeywords - 排除关键词列表
 * @param isAndMode - 是否"与"模式（全部匹配才通过）
 * @param clickFrequency - 点击频率（0-10，决定概率通过率）
 * @returns 筛选结果
 */
function keywordFilter(
  info: string,
  keywords: string[],
  excludeKeywords: string[],
  isAndMode: boolean,
  clickFrequency: number,
): FilterResult {
  const lowerInfo = (info || "").toLowerCase();

  if (excludeKeywords.length > 0) {
    for (const kw of excludeKeywords) {
      if (kw && lowerInfo.includes(kw.toLowerCase())) {
        return { pass: false, reason: `包含排除词"${kw}"` };
      }
    }
  }

  if (keywords.length === 0) {
    const pass = Math.random() * 10 < (clickFrequency || 7);
    return { pass, reason: pass ? "无条件概率通过" : "概率未通过" };
  }

  if (isAndMode) {
    const missed = keywords.filter(
      (kw) => kw && !lowerInfo.includes(kw.toLowerCase()),
    );
    if (missed.length > 0) {
      return { pass: false, reason: `与模式缺少关键词"${missed[0]}"` };
    }
    return { pass: true, reason: "与模式全部匹配" };
  }

  const matched = keywords.filter(
    (kw) => kw && lowerInfo.includes(kw.toLowerCase()),
  );
  if (matched.length === 0) {
    return { pass: false, reason: "或模式无关键词匹配" };
  }
  return { pass: true, reason: `或模式匹配"${matched[0]}"` };
}

/** 默认策略：标准两阶段筛选（粗筛+精筛都过AI） */
const DEFAULT_STRATEGY: Strategy = {
  /**
   * 粗筛：基于卡片基本信息，决定是否值得打开详情
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
   */
  async fallbackFilter() {
    return false;
  },

  /**
   * 是否需要打开详情页
   */
  needsDetailPage() {
    return true;
  },
};

/** 免费模式策略：概率粗筛 + 关键词精筛（扩展侧完成） */
const FREE_STRATEGY: Strategy = {
  async coarseFilter(ctx, candidateInfo) {
    const clickFrequency = ctx.clickFrequency || 7;
    const pass = Math.random() * 10 < clickFrequency;
    return { pass, reason: pass ? "概率通过" : "概率未通过" };
  },

  async fineFilter(ctx, candidateInfo) {
    const data = ctx as RunData;
    const result = keywordFilter(
      candidateInfo,
      data.keywords || [],
      data.excludeKeywords || [],
      data.isAndMode || false,
      data.clickFrequency || 7,
    );
    return result;
  },

  async fallbackFilter(ctx, candidateInfo) {
    const data = ctx as RunData;
    if (!data.keywords || data.keywords.length === 0) return false;
    const lowerInfo = (candidateInfo || "").toLowerCase();
    return data.keywords.some(
      (kw) => kw && lowerInfo.includes(kw.toLowerCase()),
    );
  },

  needsDetailPage() {
    return true;
  },
};

/** Boss(AI)策略：AI粗筛后直接打招呼，跳过精筛 */
const BOSS_AI_STRATEGY: Strategy = {
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

/** 58同城策略：不做筛选，直接打招呼 */
const EMPLOYER58_STRATEGY: Strategy = {
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
 * @param parserName - 平台标识
 * @param aiMode - 是否AI模式
 * @returns 策略对象
 */
export function resolveStrategy(parserName: string, aiMode: boolean): Strategy {
  if (parserName === "employer58") return EMPLOYER58_STRATEGY;
  if (parserName === "boss" && aiMode) return BOSS_AI_STRATEGY;
  if (aiMode) return DEFAULT_STRATEGY;
  return FREE_STRATEGY;
}
