// 本文件负责帮助中心指南和 AI 助手接口。
import { api, cloudApiBase, getAccessToken } from "../apiClient";

/**
 * 读取帮助中心系统指南。
 * @returns {Promise<any>} 返回系统指南 JSON。
 */
export async function getSystemGuide() {
  const data = await api("/api/help/guide", { auth: false });
  return data.guide || {};
}

/**
 * 流式调用帮助中心 AI 助手。
 * @param {any[]} messages - 当前聊天上下文。
 * @param {(chunk: string) => void} onChunk - 每段文本回调。
 * @returns {Promise<string>} 返回完整回答文本。
 */
export async function streamHelpChat(messages: any[], onChunk: (chunk: string) => void) {
  const res = await fetch(`${cloudApiBase()}/api/help/chat`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${getAccessToken()}`,
    },
    body: JSON.stringify({ messages }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(parseStreamError(text) || "帮助助手请求失败");
  }
  if (!res.body) return "";
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let result = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    const chunk = decoder.decode(value, { stream: true });
    if (!chunk) continue;
    result += chunk;
    onChunk(chunk);
  }
  const tail = decoder.decode();
  if (tail) {
    result += tail;
    onChunk(tail);
  }
  return result;
}

/**
 * 从流式错误响应中提取错误文案。
 * @param {string} text - 原始响应文本。
 * @returns {string} 错误文案。
 */
function parseStreamError(text: string) {
  try {
    const data = JSON.parse(text);
    return data.error || data.detail || "";
  } catch {
    return text;
  }
}
