/**
 * Bridge — 扩展侧与注入侧的统一桥梁
 *
 * 职责：
 * 1. 检测当前页面属于哪个平台
 * 2. 根据平台配置提供语义化方法（查找候选卡、打招呼、标记等）
 * 3. 内部将语义化方法翻译为 common.js 的原子操作（find / click / scroll / mark）
 *
 * 调用链路：
 *   orchestrator → bridge.语义方法() → 平台 TS 拿 selector → sendMessageToActiveTab({ action: "find" }) → common.js
 *
 * common.js 只做执行，bridge 只做翻译，orchestrator 只做编排
 */

import {
  sendMessageToActiveTab,
  queryActiveTab,
} from "../services/extension.js";
import {
  detectPlatform,
  isOnValidPage,
  getFirstPage,
} from "./platforms/index.js";
import type {
  PlatformConfig,
  PlatformPage,
  CandidateCard,
  SerializedElement,
} from "./platforms/types.js";

/** 查找候选卡的结果 */
export interface FindCandidatesResult {
  found: boolean;
  candidates: CandidateCard[];
}

/** 打招呼结果 */
export interface GreetResult {
  clicked: boolean;
  name: string;
}

/** 当前已确定的平台配置（运行时缓存） */
let currentPlatform: PlatformConfig | null = null;

/** 候选卡索引（内部维护） */
let candidateIndex = 0;

/** 候选卡列存（内部缓存） */
let candidateElements: SerializedElement[] = [];

/**
 * 检测当前活跃标签页的平台
 * @returns 平台配置，未识别返回 null
 */
export async function detectCurrentPlatform(): Promise<PlatformConfig | null> {
  const tab = await queryActiveTab();

  if (!tab?.url) return null;
  currentPlatform = detectPlatform(tab.url);
  candidateIndex = 0;
  candidateElements = [];
  return currentPlatform;
}

/**
 * 获取当前平台配置
 * @returns 平台配置，未检测返回 null
 */
export function getCurrentPlatform(): PlatformConfig | null {
  return currentPlatform;
}

/**
 * 校验当前页面是否在平台的有效页面列表中
 * 需先调用 detectCurrentPlatform() 检测平台
 * @returns { valid: boolean, page: PlatformPage | null } valid 表示是否有效，page 为推荐跳转页面
 */
export async function checkPageValidity(): Promise<{
  valid: boolean;
  page: PlatformPage | null;
}> {
  if (!currentPlatform) {
    return { valid: false, page: null };
  }
  const tab = await queryActiveTab();
  const valid = isOnValidPage(tab?.url || "", currentPlatform);
  const page = valid ? null : getFirstPage(currentPlatform);
  return { valid, page };
}

/**
 * 向注入侧发送原子指令
 * @param command - 指令对象
 * @returns 注入侧响应
 */
async function sendCommand(command: any): Promise<any> {
  try {
    return await sendMessageToActiveTab(command);
  } catch (error: any) {
    return null;
  }
}

/**
 * 尝试多个备选选择器，返回第一个成功的结果
 * @param selectors - 选择器数组，按优先级排列
 * @param action - 原子操作类型 "find" | "click" | "findById"
 * @param extra - 额外参数
 * @returns 第一个成功的结果
 */
async function trySelectors(
  selectors: string[],
  action: "find" | "click",
  extra: Record<string, any> = {},
): Promise<any> {
  for (const selector of selectors) {
    if (!selector) continue;
    const result = await sendCommand({
      action,
      selector,
      ...extra,
    });
    if (result?.found || result?.clicked) {
      return result;
    }
  }
  return null;
}

// ════════════════════════════════════════════════
// 语义化方法 — 供 orchestrator 调用
// ════════════════════════════════════════════════

/**
 * 检测注入侧是否存活
 * @returns 是否连通
 */
export async function ping(): Promise<boolean> {
  const result = await sendCommand({ action: "ping" });
  return result?.status === "ok";
}

/**
 * 滚动页面加载更多候选卡
 */
export async function scrollForMore(): Promise<void> {
  await sendCommand({ action: "scroll" });
  await new Promise((r) => setTimeout(r, 2000));
}

/**
 * 查找候选卡列表
 * @returns 候选卡信息数组
 */
export async function findCandidates(): Promise<FindCandidatesResult> {
  if (!currentPlatform) {
    return { found: false, candidates: [] };
  }

  const cardSelectors = currentPlatform.card.card;
  let elements: SerializedElement[] = [];

  for (const selector of cardSelectors) {
    if (!selector) continue;
    const result = await sendCommand({
      action: "find",
      selector,
      all: true,
      retries: 3,
      interval: 500,
    });
    if (result?.found && result.elements?.length > 0) {
      elements = result.elements;
      break;
    }
  }

  candidateElements = elements;

  const candidates: CandidateCard[] = elements.map((el, idx) => ({
    elementId: el.__id,
    index: idx,
    name: "",
    info: el.text,
  }));

  return { found: candidates.length > 0, candidates };
}

/**
 * 获取下一个未处理的候选卡
 * @returns 候选卡信息，没有更多返回 null
 */
export async function findNextCandidate(): Promise<CandidateCard | null> {
  if (!currentPlatform) return null;

  if (candidateIndex >= candidateElements.length) {
    await scrollForMore();
    const result = await findCandidates();
    if (!result.found) return null;
  }

  while (candidateIndex < candidateElements.length) {
    const el = candidateElements[candidateIndex];
    candidateIndex++;
    if (el) {
      return {
        elementId: el.__id,
        index: el.index,
        name: "",
        info: el.text,
      };
    }
  }

  return null;
}

/**
 * 获取候选人的详细信息（在候选卡内查找子元素）
 * @param elementId - 候选卡元素 ID
 * @returns 候选人姓名和详细信息
 */
export async function extractCandidateInfo(
  elementId: string,
): Promise<{ name: string; info: string }> {
  if (!currentPlatform) return { name: "", info: "" };

  const card = currentPlatform.card;
  let name = "";
  let infoParts: string[] = [];

  const nameResult = await sendCommand({
    action: "findById",
    id: elementId,
    childSelector: card.name,
  });
  if (nameResult?.found && nameResult.element) {
    name = nameResult.element.text?.split(/\s+/)[0] || "";
    infoParts.push(nameResult.element.text);
  }

  for (const sel of card.basicInfo) {
    if (!sel) continue;
    const r = await sendCommand({
      action: "findById",
      id: elementId,
      childSelector: sel,
    });
    if (r?.found && r.element?.text) infoParts.push(r.element.text);
  }

  for (const sel of card.education) {
    if (!sel) continue;
    const r = await sendCommand({
      action: "findById",
      id: elementId,
      childSelector: sel,
    });
    if (r?.found && r.element?.text) infoParts.push(r.element.text);
  }

  if (card.university) {
    const r = await sendCommand({
      action: "findById",
      id: elementId,
      childSelector: card.university,
    });
    if (r?.found && r.element?.text) infoParts.push(r.element.text);
  }

  for (const extra of currentPlatform.extras) {
    if (!extra.selector) continue;
    const r = await sendCommand({
      action: "findById",
      id: elementId,
      childSelector: extra.selector,
    });
    if (r?.found && r.element?.text)
      infoParts.push(`[${extra.label}]${r.element.text}`);
  }

  return { name, info: infoParts.join(" | ") };
}

/**
 * 打开候选人详情页
 * @param elementId - 候选卡元素 ID
 * @returns 是否打开成功 + 详细信息
 */
export async function openCandidateDetail(
  elementId: string,
): Promise<{ opened: boolean; detailedInfo: string }> {
  if (!currentPlatform) return { opened: false, detailedInfo: "" };

  const clicked = await sendCommand({ action: "click", id: elementId });
  if (!clicked?.clicked) {
    return { opened: false, detailedInfo: "" };
  }

  await new Promise((r) => setTimeout(r, 1500));

  const infoResult = await sendCommand({
    action: "findById",
    id: elementId,
    childSelector: currentPlatform.card.description,
  });

  return {
    opened: true,
    detailedInfo: infoResult?.element?.text || "",
  };
}

/**
 * 关闭候选人详情页
 * @returns 是否关闭成功
 */
export async function closeCandidateDetail(): Promise<boolean> {
  if (!currentPlatform) return false;

  const closeSelectors = currentPlatform.detail.closeBtn;
  for (const selector of closeSelectors) {
    if (!selector) continue;
    const result = await sendCommand({
      action: "click",
      selector,
      retries: 2,
      interval: 300,
    });
    if (result?.clicked) {
      await new Promise((r) => setTimeout(r, 500));
      return true;
    }
  }
  return false;
}

/**
 * 点击打招呼按钮
 * @param elementId - 候选卡元素 ID
 * @returns 是否点击成功
 */
export async function clickGreet(elementId: string): Promise<boolean> {
  if (!currentPlatform) return false;

  const greetSelectors = currentPlatform.actions.greetBtn;

  for (const selector of greetSelectors) {
    if (!selector) continue;
    const result = await sendCommand({
      action: "findById",
      id: elementId,
      childSelector: selector,
    });
    if (result?.found) {
      const clickResult = await sendCommand({
        action: "click",
        selector,
        retries: 2,
        interval: 300,
      });
      if (clickResult?.clicked) return true;
    }
  }

  const fallbackResult = await sendCommand({ action: "click", id: elementId });
  return fallbackResult?.clicked || false;
}

/**
 * 索要联系方式
 * @param elementId - 候选卡元素 ID
 * @returns 是否点击成功
 */
export async function collectContact(elementId: string): Promise<boolean> {
  if (!currentPlatform) return false;

  const phoneSelectors = currentPlatform.actions.phoneBtn;
  for (const selector of phoneSelectors) {
    if (!selector) continue;
    const result = await sendCommand({
      action: "click",
      selector,
      retries: 2,
      interval: 300,
    });
    if (result?.clicked) return true;
  }

  return false;
}

/**
 * 标记候选卡状态
 * @param elementId - 候选卡元素 ID
 * @param reason - 标记原因
 * @param type - 标记类型 matched/rejected/error
 */
export async function markElement(
  elementId: string,
  reason: string,
  type: "matched" | "rejected" | "error",
): Promise<void> {
  await sendCommand({ action: "mark", id: elementId, reason, type });
}

/**
 * 检查是否有新消息
 * @returns 是否有新消息
 */
export async function checkNewMessage(): Promise<boolean> {
  if (!currentPlatform) return false;

  const messageTip = currentPlatform.detail.messageTip;
  if (!messageTip) return false;

  const result = await sendCommand({
    action: "find",
    selector: messageTip,
    retries: 1,
    interval: 200,
  });

  return result?.found || false;
}

/**
 * 重置候选卡索引（开始新一轮扫描时调用）
 */
export function resetCandidateIndex(): void {
  candidateIndex = 0;
  candidateElements = [];
}

/**
 * 翻到下一页（仅支持翻页的平台）
 * @returns 是否翻页成功
 */
export async function navigateNextPage(): Promise<boolean> {
  if (!currentPlatform || !currentPlatform.behavior.supportsPaging)
    return false;

  const result = await sendCommand({
    action: "click",
    selector: currentPlatform.behavior.nextPageBtn,
    retries: 3,
    interval: 500,
  });

  if (result?.clicked) {
    await new Promise((r) => setTimeout(r, 2000));
    resetCandidateIndex();
    return true;
  }

  return false;
}
