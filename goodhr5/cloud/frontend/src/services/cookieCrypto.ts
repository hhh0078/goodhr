// 本文件负责集中管理前端侧 cookie 加解密与编解码辅助逻辑。
import { agentURL } from './localAgentApi'

type WrappedCookiePayload = {
  encrypted_data: string
  encrypted_keys: Record<string, string>
}

type AgentDecryptPayload = {
  encrypted_sk: string
  encrypted_data: string
}

/**
 * 将 Cookie JSON 对象编码为 Base64 文本，便于存储或传输。
 * @param {unknown} value - 任意可 JSON 序列化对象。
 * @returns {string} Base64 编码文本。
 */
export function encodeCookieJSON(value: unknown): string {
  const json = JSON.stringify(value ?? null)
  const bytes = new TextEncoder().encode(json)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

/**
 * 将 Base64 文本解码回 Cookie JSON 对象。
 * @param {string} encoded - Base64 编码文本。
 * @returns {T} 解码后的对象。
 */
export function decodeCookieJSON<T = any>(encoded: string): T {
  const binary = atob(encoded)
  const bytes = Uint8Array.from(binary, ch => ch.charCodeAt(0))
  const json = new TextDecoder().decode(bytes)
  return JSON.parse(json) as T
}

/**
 * 从云端 claim 响应中提取当前机器可用的密钥密文。
 * @param {WrappedCookiePayload} payload - 云端返回的 cookie 加密数据。
 * @param {string} machineID - 本地机器码。
 * @returns {AgentDecryptPayload} 返回传给本地解密接口的数据。
 */
export function pickDecryptPayload(payload: WrappedCookiePayload, machineID: string): AgentDecryptPayload {
  const encryptedSK = payload.encrypted_keys?.[machineID]
  if (!encryptedSK) throw new Error('当前机器无可用 cookie 密钥')
  if (!payload.encrypted_data) throw new Error('缺少 encrypted_data')
  return {
    encrypted_sk: encryptedSK,
    encrypted_data: payload.encrypted_data,
  }
}

/**
 * 调用本地 Agent 解密云端 cookie 数据。
 * @param {string} agentBaseURL - Local Agent HTTP 基础地址。
 * @param {AgentDecryptPayload} payload - 包含 encrypted_sk 和 encrypted_data。
 * @returns {Promise<any>} 返回解密后的 JSON 数据。
 */
export async function decryptCookieByAgent(agentBaseURL: string, payload: AgentDecryptPayload): Promise<any> {
  const res = await fetch(agentURL(agentBaseURL, '/api/v1/crypto/decrypt'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  const data = await res.json()
  if (!res.ok || !data.ok) throw new Error(data.error || data.detail || 'cookie 解密失败')
  const encoded = String(data.data || '')
  return decodeCookieJSON(encoded)
}
