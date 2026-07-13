/**
 * AI 决策模块 — 扩展侧
 *
 * 复用 src/services/ai.js 的 chatWithAI 接口，
 * 封装候选人筛选专用的 AI 决策逻辑：
 * - 构造筛选 prompt
 * - 调用 AI 接口
 * - 解析返回结果（是否通过 + 原因 + 费用）
 */

import { chatWithAI } from "../services/ai.js";

/** AI 决策结果 */
export interface AIDecisionResult {
  isok: boolean;
  msg: string;
  cost: string;
}

/** 流程上下文（Orchestrator 实例的子集） */
export interface OrchestratorContext {
  aiConfig: {
    clickPrompt: string;
    [key: string]: any;
  };
  jobDescription: string;
  [key: string]: any;
}

/**
 * 构建 AI 筛选决策的消息列表
 * @param clickPrompt - 粗筛提示词模板
 * @param jobDescription - 岗位说明
 * @param candidateInfo - 候选人信息文本
 * @returns 消息列表
 */
function buildFilterMessages(
  clickPrompt: string,
  jobDescription: string,
  candidateInfo: string,
): Array<{ role: string; content: string }> {
  const systemPrompt = clickPrompt
    ? clickPrompt.replace(/\$\{岗位要求\}/g, jobDescription)
    : `你是一个专业的HR助手。请根据以下岗位要求判断候选人是否匹配。

岗位要求：
${jobDescription}

请回复JSON格式：
- 匹配：{"isok": true, "msg": "匹配原因"}
- 不匹配：{"isok": false, "msg": "不匹配原因"}`;

  return [
    { role: "system", content: systemPrompt },
    { role: "user", content: `候选人信息：\n${candidateInfo}` },
  ];
}

/**
 * 解析 AI 返回的决策结果
 * @param responseText - AI 原始返回文本
 * @returns 解析后的决策结果
 */
function parseDecisionResponse(responseText: string): AIDecisionResult {
  try {
    const jsonMatch = responseText.match(/\{[\s\S]*\}/);
    if (!jsonMatch) {
      return { isok: false, msg: "AI返回格式异常", cost: "0" };
    }

    const parsed = JSON.parse(jsonMatch[0]);
    return {
      isok: !!parsed.isok,
      msg: parsed.msg || (parsed.isok ? "匹配" : "不匹配"),
      cost: parsed.cost || "0",
    };
  } catch {
    return { isok: false, msg: "AI返回解析失败", cost: "0" };
  }
}

/**
 * 请求 AI 做出筛选决策
 * @param ctx - 流程管理器实例（读取 aiConfig、jobDescription 等）
 * @param candidateInfo - 候选人信息文本
 * @returns AI 决策结果
 */
export async function requestAIDecision(ctx: OrchestratorContext, candidateInfo: string): Promise<AIDecisionResult> {
  const aiConfig = ctx.aiConfig || {};
  const jobDescription = ctx.jobDescription || "";

  try {
    const messages = buildFilterMessages(
      aiConfig.clickPrompt,
      jobDescription,
      candidateInfo,
    );

    const responseText = await chatWithAI({
      messages,
      temperature: 0.3,
    });

    return parseDecisionResponse(responseText);
  } catch (error: any) {
    console.error("[ai] AI决策异常:", error);
    return { isok: false, msg: `AI决策异常: ${error.message}`, cost: "0" };
  }
}
