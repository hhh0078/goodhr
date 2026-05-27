/**
 * 公共 AI 调用库
 * 封装与 ai.58it.cn 的对话接口，apiKey 和 model 由 configureAI() 统一设置
 */

const AI_CHAT_URL = "https://ai.58it.cn/v1/chat/completions";

let _apiKey = "";
let _model = "";

/** AI 配置参数 */
export interface AIConfigureOptions {
  apiKey?: string;
  model?: string;
}

/** AI 对话参数 */
export interface AIChatOptions {
  messages: Array<{ role: string; content: string }>;
  temperature?: number;
}

/**
 * 配置 AI 调用的密钥和模型
 * @param options - 配置参数，apiKey 和 model 均可选
 */
export function configureAI({ apiKey, model }: AIConfigureOptions): void {
  if (apiKey !== undefined) _apiKey = apiKey;
  if (model !== undefined) _model = model;
}

/**
 * 发送消息给 AI 并返回回复文本
 * @param options - 对话参数，包含消息列表和可选温度参数
 * @returns AI 回复文本
 */
export async function chatWithAI({ messages, temperature = 0.7 }: AIChatOptions): Promise<string> {
  if (!_apiKey) {
    throw new Error("缺少 API 密钥，请先绑定账号");
  }
  if (!_model) {
    throw new Error("缺少AI模型，请在AI配置中选择");
  }

  const response = await fetch(AI_CHAT_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${_apiKey}`,
    },
    body: JSON.stringify({
      model: _model,
      messages,
      temperature,
    }),
  });

  const data = await response.json().catch(() => ({}));

  if (!response.ok) {
    throw new Error(
      data?.error?.message || data?.message || `AI请求失败: ${response.status}`,
    );
  }

  const content = data?.choices?.[0]?.message?.content;
  if (!content) {
    throw new Error("AI返回内容为空");
  }

  return content as string;
}
